// watchlist.rs - Airbnb Price Monitoring and Watchlist Service
use anyhow::{Result, anyhow};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use tokio::time::{sleep, Duration};
use thirtyfour::{By, WebDriver};
use crate::routes::create_webdriver;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WatchlistItem {
    pub user_id: String,
    pub listing_url: String,
    pub listing_id: String,
    pub title: String,
    pub current_price: f64,
    pub target_price: f64,  // Alert when price drops to or below this
    pub original_price: f64,
    pub location: String,
    pub check_in_date: String,
    pub check_out_date: String,
    pub guests: i32,
    pub email: String,
    pub created_at: DateTime<Utc>,
    pub last_checked: Option<DateTime<Utc>>,
    pub last_price_update: Option<DateTime<Utc>>,
    pub is_active: bool,
    pub alert_count: i32,
    pub max_alerts: i32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PriceHistory {
    pub listing_id: String,
    pub price: f64,
    pub timestamp: DateTime<Utc>,
    pub currency: String,
    pub source: String,
}

impl WatchlistItem {
    pub fn new(
        user_id: String,
        listing_url: String,
        listing_id: String,
        title: String,
        current_price: f64,
        target_price: f64,
        location: String,
        check_in_date: String,
        check_out_date: String,
        guests: i32,
        email: String,
    ) -> Self {
        Self {
            user_id,
            listing_url,
            listing_id,
            title,
            current_price,
            target_price,
            original_price: current_price,
            location,
            check_in_date,
            check_out_date,
            guests,
            email,
            created_at: Utc::now(),
            last_checked: None,
            last_price_update: None,
            is_active: true,
            alert_count: 0,
            max_alerts: 10,
        }
    }
}

pub struct AirbnbWatchlistService {
    watchlist_items: Vec<WatchlistItem>,
    price_history: Vec<PriceHistory>,
}

impl AirbnbWatchlistService {
    pub fn new() -> Self {
        Self {
            watchlist_items: Vec::new(),
            price_history: Vec::new(),
        }
    }

    pub async fn add_to_watchlist(&mut self, item: WatchlistItem) -> Result<()> {
        // Check if item already exists
        if self.watchlist_items.iter().any(|w| w.listing_id == item.listing_id && w.user_id == item.user_id) {
            return Err(anyhow!("Item already in watchlist"));
        }

        self.watchlist_items.push(item);
        log::info!("Added item to watchlist: {}", self.watchlist_items.last().unwrap().title);
        Ok(())
    }

    pub async fn remove_from_watchlist(&mut self, user_id: &str, listing_id: &str) -> Result<()> {
        let initial_len = self.watchlist_items.len();
        self.watchlist_items.retain(|item| !(item.user_id == user_id && item.listing_id == listing_id));
        
        if self.watchlist_items.len() < initial_len {
            log::info!("Removed item from watchlist: {}", listing_id);
            Ok(())
        } else {
            Err(anyhow!("Item not found in watchlist"))
        }
    }

    pub async fn get_user_watchlist(&self, user_id: &str) -> Vec<&WatchlistItem> {
        self.watchlist_items.iter()
            .filter(|item| item.user_id == user_id && item.is_active)
            .collect()
    }

    pub async fn check_price_updates(&mut self) -> Result<Vec<(WatchlistItem, f64)>> {
        let mut price_drops = Vec::new();
        let driver = create_webdriver().await?;

        for i in 0..self.watchlist_items.len() {
            let item = &mut self.watchlist_items[i];
            if !item.is_active || item.alert_count >= item.max_alerts {
                continue;
            }

            let listing_url = item.listing_url.clone();
            match Self::scrape_current_price(&driver, &listing_url).await {
                Ok(current_price) => {
                    let price_changed = (current_price - item.current_price).abs() > 0.01;
                    
                    if price_changed {
                        // Record price history
                        self.price_history.push(PriceHistory {
                            listing_id: item.listing_id.clone(),
                            price: current_price,
                            timestamp: Utc::now(),
                            currency: "USD".to_string(),
                            source: "airbnb".to_string(),
                        });

                        let old_price = item.current_price;
                        item.current_price = current_price;
                        item.last_price_update = Some(Utc::now());

                        // Check if price dropped below target
                        if current_price <= item.target_price && current_price < old_price {
                            let price_drop = old_price - current_price;
                            price_drops.push((item.clone(), price_drop));
                            item.alert_count += 1;
                            log::info!("Price drop alert for {}: ${:.2} -> ${:.2} (dropped ${:.2})", 
                                     item.title, old_price, current_price, price_drop);
                        }
                    }

                    item.last_checked = Some(Utc::now());
                }
                Err(e) => {
                    log::error!("Failed to check price for {}: {}", item.title, e);
                }
            }

            // Rate limiting between requests
            sleep(Duration::from_secs(2)).await;
        }

        driver.quit().await.ok();
        Ok(price_drops)
    }

    async fn scrape_current_price(driver: &WebDriver, listing_url: &str) -> Result<f64> {
        driver.goto(listing_url).await?;
        sleep(Duration::from_secs(3)).await;

        // Try different price selectors that Airbnb uses
        let price_selectors = vec![
            "._1y74zjx",      // Main price selector
            "._j1kt73",       // Alternative price selector
            "._tyxjp1",       // Another price format
            "[data-testid='price-display']", // Testid selector
        ];

        for selector in price_selectors {
            if let Ok(price_element) = driver.find(By::ClassName(selector)).await {
                if let Ok(price_text) = price_element.text().await {
                    if let Some(price) = Self::extract_price_from_text(&price_text) {
                        return Ok(price);
                    }
                }
            }
        }

        // Try CSS selector approach
        if let Ok(price_element) = driver.find(By::Css("span[aria-hidden='true']")).await {
            if let Ok(price_text) = price_element.text().await {
                if let Some(price) = Self::extract_price_from_text(&price_text) {
                    return Ok(price);
                }
            }
        }

        Err(anyhow!("Could not find price on page"))
    }

    fn extract_price_from_text(text: &str) -> Option<f64> {
        // Extract price from text like "$123", "$1,234", "$123 total", etc.
        let cleaned = text
            .replace("$", "")
            .replace(",", "")
            .replace(" total", "")
            .replace(" night", "")
            .replace("per night", "")
            .trim()
            .split_whitespace()
            .next()?
            .to_string();

        cleaned.parse::<f64>().ok()
    }

    pub async fn get_price_history(&self, listing_id: &str) -> Vec<&PriceHistory> {
        self.price_history.iter()
            .filter(|h| h.listing_id == listing_id)
            .collect()
    }

    pub async fn send_price_alert(&self, item: &WatchlistItem, price_drop: f64) -> Result<()> {
        // Email sending would be implemented here
        // For now, just log the alert
        log::info!(
            "PRICE ALERT: {} dropped by ${:.2} to ${:.2}. Target was ${:.2}. Notify: {}",
            item.title,
            price_drop,
            item.current_price,
            item.target_price,
            item.email
        );

        // TODO: Implement actual email sending using lettre or similar crate
        // Placeholder for email implementation
        self.send_email_notification(
            &item.email,
            "Price Drop Alert",
            &format!(
                "Good news! The price for '{}' has dropped by ${:.2} to ${:.2}. Your target price was ${:.2}.",
                item.title, price_drop, item.current_price, item.target_price
            )
        ).await?;
        
        Ok(())
    }

    // Email notification placeholder method
    async fn send_email_notification(&self, email: &str, subject: &str, body: &str) -> Result<(), anyhow::Error> {
        tracing::info!("Email notification placeholder - To: {}, Subject: {}, Body: {}", email, subject, body);
        
        // TODO: Implement actual email sending using lettre crate
        // Example implementation would be:
        // let email = Message::builder()
        //     .from("noreply@realestayer.com".parse()?)
        //     .to(email.parse()?)
        //     .subject(subject)
        //     .body(String::from(body))?;
        // 
        // let creds = Credentials::new("smtp_username".to_owned(), "smtp_password".to_owned());
        // let mailer = SmtpTransport::relay("smtp.gmail.com")?
        //     .credentials(creds)
        //     .build();
        // 
        // mailer.send(&email)?;
        
        Ok(())
    }

    pub async fn cleanup_old_alerts(&mut self, days_old: i64) {
        let cutoff_date = Utc::now() - chrono::Duration::days(days_old);
        
        self.watchlist_items.retain(|item| {
            if let Some(last_checked) = item.last_checked {
                last_checked > cutoff_date
            } else {
                item.created_at > cutoff_date
            }
        });

        self.price_history.retain(|history| history.timestamp > cutoff_date);
        
        log::info!("Cleaned up old watchlist items and price history");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_extract_price_from_text() {
        assert_eq!(AirbnbWatchlistService::extract_price_from_text("$123"), Some(123.0));
        assert_eq!(AirbnbWatchlistService::extract_price_from_text("$1,234"), Some(1234.0));
        assert_eq!(AirbnbWatchlistService::extract_price_from_text("$123 total"), Some(123.0));
        assert_eq!(AirbnbWatchlistService::extract_price_from_text("$456 per night"), Some(456.0));
        assert_eq!(AirbnbWatchlistService::extract_price_from_text("invalid"), None);
    }

    #[tokio::test]
    async fn test_watchlist_operations() {
        let mut service = AirbnbWatchlistService::new();
        
        let item = WatchlistItem::new(
            "user123".to_string(),
            "https://airbnb.com/rooms/123".to_string(),
            "123".to_string(),
            "Test Listing".to_string(),
            100.0,
            80.0,
            "New York".to_string(),
            "2024-01-01".to_string(),
            "2024-01-05".to_string(),
            2,
            "test@example.com".to_string(),
        );

        // Test adding to watchlist
        assert!(service.add_to_watchlist(item.clone()).await.is_ok());
        assert_eq!(service.watchlist_items.len(), 1);

        // Test duplicate addition
        assert!(service.add_to_watchlist(item.clone()).await.is_err());

        // Test getting user watchlist
        let user_items = service.get_user_watchlist("user123").await;
        assert_eq!(user_items.len(), 1);

        // Test removal
        assert!(service.remove_from_watchlist("user123", "123").await.is_ok());
        assert_eq!(service.watchlist_items.len(), 0);
    }
}
