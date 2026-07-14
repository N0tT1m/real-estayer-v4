use crate::{database, models::*, scraping::{scrape_region, send_email, AmenityFilter}, watchlist::*};
use crate::stealth_browser::StealthDriver;
use axum::{
    extract::{Path, Query},
    http::StatusCode,
    Json,
    response::IntoResponse,
};
use mongodb::bson::{doc, oid::ObjectId};
use serde_json::json;
use std::collections::HashMap;
use std::process::Command;
use tokio::time::sleep;
use std::time::Duration;
use std::sync::atomic::{AtomicBool, Ordering};
use thirtyfour::{ChromiumLikeCapabilities, DesiredCapabilities, WebDriver};
use anyhow::Result;
#[allow(unused_imports)]
use log::info;
use once_cell::sync::Lazy;
use std::sync::Mutex as StdMutex;

// Global lock to prevent concurrent scrape requests from killing each other's Chrome
static SCRAPE_IN_PROGRESS: AtomicBool = AtomicBool::new(false);

// Global scrape status for tracking progress
#[derive(Debug, Clone, serde::Serialize)]
pub struct ScrapeStatus {
    pub status: String,           // "idle", "scraping", "completed", "failed"
    pub city: Option<String>,
    pub current_listing: usize,
    pub total_listings: usize,
    pub successful: usize,
    pub failed: usize,
    pub message: String,
    pub started_at: Option<String>,
    pub completed_at: Option<String>,
    pub last_scraped_title: Option<String>,
}

impl Default for ScrapeStatus {
    fn default() -> Self {
        Self {
            status: "idle".to_string(),
            city: None,
            current_listing: 0,
            total_listings: 0,
            successful: 0,
            failed: 0,
            message: "No scrape in progress".to_string(),
            started_at: None,
            completed_at: None,
            last_scraped_title: None,
        }
    }
}

static SCRAPE_STATUS: Lazy<StdMutex<ScrapeStatus>> = Lazy::new(|| StdMutex::new(ScrapeStatus::default()));

// List of Canadian provinces and territories
const CANADIAN_PROVINCES: [&str; 13] = [
    "Alberta", "British Columbia", "Manitoba", "New Brunswick", "Newfoundland and Labrador",
    "Northwest Territories", "Nova Scotia", "Nunavut", "Ontario", "Prince Edward Island",
    "Quebec", "Saskatchewan", "Yukon"
];

// List of US states
const US_STATES: [&str; 50] = [
    "Alabama", "Alaska", "Arizona", "Arkansas", "California", "Colorado", "Connecticut", "Delaware", "Florida",
    "Georgia", "Hawaii", "Idaho", "Illinois", "Indiana", "Iowa", "Kansas", "Kentucky", "Louisiana", "Maine",
    "Maryland", "Massachusetts", "Michigan", "Minnesota", "Mississippi", "Missouri", "Montana", "Nebraska",
    "Nevada", "New Hampshire", "New Jersey", "New Mexico", "New York", "North Carolina", "North Dakota", "Ohio",
    "Oklahoma", "Oregon", "Pennsylvania", "Rhode Island", "South Carolina", "South Dakota", "Tennessee", "Texas",
    "Utah", "Vermont", "Virginia", "Washington", "West Virginia", "Wisconsin", "Wyoming"
];

/// Helper function to detect Chrome version
fn get_chrome_version() -> Result<String> {
    let output = if cfg!(target_os = "windows") {
        Command::new("reg")
            .args(&["query", "HKEY_CURRENT_USER\\Software\\Google\\Chrome\\BLBeacon", "/v", "version"])
            .output()
            .ok()
    } else if cfg!(target_os = "macos") {
        Command::new("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
            .arg("--version")
            .output()
            .ok()
    } else {
        Command::new("google-chrome")
            .arg("--version")
            .output()
            .ok()
    };

    if let Some(out) = output {
        let version_str = String::from_utf8_lossy(&out.stdout);
        // Extract version number (e.g., "141.0.7390.55")
        if let Some(version) = version_str.split_whitespace()
            .find(|s| s.chars().next().map_or(false, |c| c.is_numeric())) {
            return Ok(version.to_string());
        }
    }

    Err(anyhow::anyhow!("Could not detect Chrome version"))
}

/// Helper function to start ChromeDriver
async fn ensure_chromedriver_running() -> Result<()> {
    // Check if chromedriver is already running
    if let Ok(_) = reqwest::get("http://localhost:9515/status").await {
        log::info!("ChromeDriver is already running");
        return Ok(());
    }

    // Try to detect Chrome version and warn if there might be a mismatch
    if let Ok(chrome_version) = get_chrome_version() {
        log::info!("Detected Chrome version: {}", chrome_version);
    } else {
        log::warn!("Could not detect Chrome version - ChromeDriver might not match");
    }

    // Start chromedriver using local binary
    let chromedriver_path = if cfg!(target_os = "windows") {
        "./chromedriver.exe".to_string()
    } else {
        "./chromedriver".to_string()
    };

    log::info!("Starting ChromeDriver from: {}", chromedriver_path);
    let mut cmd = Command::new(&chromedriver_path);
    cmd.arg("--port=9515")
       .arg("--whitelisted-ips=")
       .arg("--disable-dev-shm-usage");

    cmd.spawn()
        .map_err(|e| anyhow::anyhow!("Failed to start ChromeDriver: {}. Please ensure ChromeDriver version matches your Chrome browser version. Download the correct version from https://googlechromelabs.github.io/chrome-for-testing/", e))?;

    // Wait for chromedriver to be ready
    for _ in 0..30 {
        if let Ok(_) = reqwest::get("http://localhost:9515/status").await {
            log::info!("ChromeDriver is ready");
            return Ok(());
        }
        sleep(Duration::from_millis(500)).await;
    }

    Err(anyhow::anyhow!("ChromeDriver failed to start within 15 seconds. This may be due to a version mismatch between ChromeDriver and Chrome browser."))
}

/// Helper function to create WebDriver with stealth/anti-detection configuration
pub async fn create_webdriver() -> Result<WebDriver> {
    log::info!("[WEBDRIVER] ========== CREATING NEW WEBDRIVER ==========");

    ensure_chromedriver_running().await?;

    let mut caps = DesiredCapabilities::chrome();

    // CRITICAL: Anti-detection flags - these are essential to avoid bot detection
    // This prevents Chrome from exposing navigator.webdriver = true
    caps.add_arg("--disable-blink-features=AutomationControlled")?;

    // Essential args for navigation
    caps.add_arg("--no-sandbox")?;
    caps.add_arg("--disable-dev-shm-usage")?;
    caps.add_arg("--window-size=1920,1080")?;
    caps.add_arg("--start-maximized")?;

    // Disable infobars (the "Chrome is being controlled" message)
    caps.add_arg("--disable-infobars")?;

    // Additional stealth flags
    caps.add_arg("--disable-extensions")?;
    caps.add_arg("--disable-plugins-discovery")?;
    caps.add_arg("--disable-default-apps")?;

    // GPU fixes for Windows Chrome 143
    caps.add_arg("--disable-gpu")?;
    caps.add_arg("--disable-gpu-compositing")?;
    caps.add_arg("--disable-gpu-sandbox")?;
    caps.add_arg("--enable-unsafe-swiftshader")?;
    caps.add_arg("--disable-webgl")?;
    caps.add_arg("--disable-webgl2")?;

    log::info!("Creating WebDriver with stealth/anti-detection configuration");

    match WebDriver::new("http://localhost:9515", caps).await {
        Ok(driver) => {
            log::info!("WebDriver created successfully with anti-detection settings");
            return Ok(driver);
        }
        Err(e) => return Err(anyhow::anyhow!("Failed to create WebDriver: {}", e)),
    }
}

/// Create a stealth driver using chromiumoxide (CDP-based, harder to detect)
pub async fn create_stealth_driver() -> Result<StealthDriver> {
    log::info!("[STEALTH] ========== CREATING NEW STEALTH DRIVER ==========");
    StealthDriver::new().await
}

pub async fn scrape_north_america() -> impl IntoResponse {
    let result = async {
        let mut total_listings = 0;
        let mut canada_listings = 0;
        let mut us_listings = 0;

        let driver = create_webdriver().await?;

        // Scrape Canadian provinces with Airbnb
        for province in CANADIAN_PROVINCES {
            match scrape_region(&driver, province, "Canada").await {
                Ok(count) => {
                    canada_listings += count;
                    total_listings += count;
                }
                Err(e) => log::error!("Error scraping {}, Canada: {}", province, e),
            }
        }

        // Scrape US states with Airbnb
        for state in US_STATES {
            match scrape_region(&driver, state, "USA").await {
                Ok(count) => {
                    us_listings += count;
                    total_listings += count;
                }
                Err(e) => log::error!("Error scraping {}, USA: {}", state, e),
            }
        }

        // Quit the WebDriver session
        if let Err(e) = driver.quit().await {
            log::error!("Error closing WebDriver session: {}", e);
        }

        Ok::<_, anyhow::Error>(Json(ScrapingResponse {
            message: "Airbnb scraping completed".to_string(),
            total_listings,
            canada_listings,
            us_listings,
        }))
    }
        .await;

    match result {
        Ok(response) => response.into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("Scraping failed: {}", e),
        )
            .into_response(),
    }
}

#[axum::debug_handler]
pub async fn scrape_city_data(
    Query(params): Query<HashMap<String, String>>,
) -> impl IntoResponse {
    log::info!("[SCRAPE_CITY] ========== NEW SCRAPE REQUEST ==========");
    log::info!("[SCRAPE_CITY] Received scrape request with params: {:?}", params);

    // Check if a scrape is already in progress - reject new requests to avoid killing Chrome
    if SCRAPE_IN_PROGRESS.swap(true, Ordering::SeqCst) {
        log::warn!("[SCRAPE_CITY] Scrape already in progress, rejecting new request");
        return (
            StatusCode::CONFLICT,
            Json(json!({
                "error": "A scrape is already in progress. Please wait for it to complete.",
                "status": "busy"
            }))
        ).into_response();
    }

    // Guard struct definition - will be created inside spawn to release lock when done
    struct ScrapeGuard;
    impl Drop for ScrapeGuard {
        fn drop(&mut self) {
            SCRAPE_IN_PROGRESS.store(false, Ordering::SeqCst);
            log::info!("[SCRAPE_CITY] Scrape lock released");
        }
    }
    // NOTE: Don't create guard here - let the spawn manage it so it stays alive

    let city = match params.get("city") {
        Some(c) => c.clone(),  // Clone to own the value for the spawned task
        None => {
            log::error!("Missing city parameter");
            return (StatusCode::BAD_REQUEST, "City parameter is required").into_response()
        }
    };

    // Extract optional parameters
    let state = params.get("state").cloned().unwrap_or_default();
    let country = params.get("country").cloned().unwrap_or_default();
    // Accept both check_in and check_in_date for backwards compatibility
    let check_in_date = params.get("check_in")
        .or_else(|| params.get("check_in_date"))
        .cloned()
        .unwrap_or_default();
    let check_out_date = params.get("check_out")
        .or_else(|| params.get("check_out_date"))
        .cloned()
        .unwrap_or_default();
    // Parse guest counts (adults, children, infants, pets)
    let adults = params.get("adults")
        .and_then(|a| a.parse::<i32>().ok())
        .unwrap_or(2);
    let children = params.get("children")
        .and_then(|c| c.parse::<i32>().ok())
        .unwrap_or(0);
    let infants = params.get("infants")
        .and_then(|i| i.parse::<i32>().ok())
        .unwrap_or(0);
    let pets = params.get("pets")
        .and_then(|p| p.parse::<i32>().ok())
        .unwrap_or(0);
    let _trip_duration = params.get("trip_duration")
        .and_then(|d| d.parse::<i32>().ok());
    let _date_mode = params.get("date_mode").cloned().unwrap_or_default();
    let limit = params.get("limit")
        .and_then(|l| l.parse::<usize>().ok()); // None means no limit (scrape all)

    // Parse badges_only filter (only save listings with badges like Superhost, Guest favorite, etc.)
    let badges_only = params.get("badges_only")
        .map(|v| v == "true")
        .unwrap_or(false);

    // Parse amenity filters (hot_tub, pool, waterfront)
    let hot_tub = params.get("hot_tub")
        .map(|v| v == "true")
        .unwrap_or(false);
    let pool = params.get("pool")
        .map(|v| v == "true")
        .unwrap_or(false);
    let waterfront = params.get("waterfront")
        .map(|v| v == "true")
        .unwrap_or(false);

    log::info!("Starting Airbnb scraping for city: {}, state: {}, country: {}, check-in: {}, check-out: {}, adults: {}, children: {}, infants: {}, pets: {}, limit: {}, badges_only: {}, hot_tub: {}, pool: {}, waterfront: {}",
               city, state, country, check_in_date, check_out_date, adults, children, infants, pets,
               limit.map(|l| l.to_string()).unwrap_or_else(|| "unlimited".to_string()), badges_only,
               hot_tub, pool, waterfront);

    // Clone city for response (before moving into spawn)
    let city_for_response = city.clone();

    // Spawn the scraping as a background task that won't be cancelled if client disconnects
    tokio::spawn(async move {
        // Move the guard into the spawned task so it stays alive until scraping completes
        let _guard = ScrapeGuard;

        // Initialize scrape status
        {
            let mut status = SCRAPE_STATUS.lock().unwrap();
            *status = ScrapeStatus {
                status: "scraping".to_string(),
                city: Some(city.clone()),
                current_listing: 0,
                total_listings: 0,
                successful: 0,
                failed: 0,
                message: "Initializing scraper...".to_string(),
                started_at: Some(chrono::Utc::now().to_rfc3339()),
                completed_at: None,
                last_scraped_title: None,
            };
        }

        let result: Result<Vec<Listing>, anyhow::Error> = async {
            use tracing::{info, error};
            use crate::scraping::{GuestParams, scrape_city_fast};

            info!("[FAST] Creating StealthDriver...");
            let driver = create_stealth_driver().await?;

            // Build a properly formatted search location.
            let search_location = if !state.is_empty() && !country.is_empty() {
                format!("{}, {}, {}", city, state, country)
            } else if !country.is_empty() {
                format!("{}, {}", city, country)
            } else {
                city.to_string()
            };

            let check_in_opt = if !check_in_date.is_empty() { Some(check_in_date.as_str()) } else { None };
            let check_out_opt = if !check_out_date.is_empty() { Some(check_out_date.as_str()) } else { None };
            let guest_params = GuestParams::new(adults, children, infants, pets);
            let amenity_filter = if hot_tub || pool || waterfront {
                Some(AmenityFilter::new(hot_tub, pool, waterfront))
            } else {
                None
            };

            // Tile for full coverage only on unlimited scrapes; always enrich so
            // amenity/badge filters see the full feature list.
            let limit_opt = match limit { Some(l) if l > 0 => Some(l), _ => None };
            let tiled = limit_opt.is_none();
            let enrich = true;

            info!("[FAST] Scraping {} (tiled={}, enrich={})", search_location, tiled, enrich);
            let scraped = scrape_city_fast(
                &driver, &search_location, guest_params, amenity_filter,
                check_in_opt, check_out_opt, limit_opt, enrich, tiled,
            ).await;

            if let Err(e) = driver.quit().await {
                error!("[FAST] Error closing driver: {}", e);
            }

            let scraped = scraped?;
            let found = scraped.len();
            info!("[FAST] Harvested {} listings for {}", found, search_location);
            {
                let mut status = SCRAPE_STATUS.lock().unwrap();
                status.total_listings = found;
                status.message = format!("Harvested {} listings, filtering...", found);
            }

            // Apply badge / amenity filters and stamp caller-declared region/country.
            let mut kept: Vec<Listing> = Vec::new();
            for mut details in scraped.into_iter() {
                let has_badge = details.features.iter().any(|f| {
                    let f = f.to_lowercase();
                    f.contains("superhost") || f.contains("guest fav") || f.contains("guest-fav")
                        || f.contains("rare find") || f.contains("highly rated") || f.contains("top rated")
                });
                let has_hot_tub = details.features.iter().any(|f| f == "Hot Tub");
                let has_pool = details.features.iter().any(|f| f == "Pool");
                let has_waterfront = details.features.iter().any(|f| f == "Waterfront");
                let passes_amenity =
                    (!hot_tub || has_hot_tub) && (!pool || has_pool) && (!waterfront || has_waterfront);

                if badges_only && !has_badge {
                    continue;
                }
                if !passes_amenity {
                    continue;
                }
                if !state.is_empty() {
                    details.region = Some(state.clone());
                }
                if !country.is_empty() {
                    details.country = Some(country.clone());
                }
                kept.push(details);
            }

            info!("[FAST] {} listings after filtering (from {})", kept.len(), found);
            {
                let mut status = SCRAPE_STATUS.lock().unwrap();
                status.successful = kept.len();
                status.message = format!("Inserting {} listings...", kept.len());
            }

            info!("[FAST] Inserting {} listings into database", kept.len());
            let inserted_ids = database::insert_many(kept.clone()).await?;
            info!("[FAST] Successfully inserted {} listings", inserted_ids.len());

            Ok::<Vec<Listing>, anyhow::Error>(kept)
        }
        .await;

        // Update final status and log the result
        match result {
            Ok(listings) => {
                log::info!("[SCRAPE_CITY] Background scrape completed successfully: {} listings saved to database", listings.len());
                // Update status to completed
                {
                    let mut status = SCRAPE_STATUS.lock().unwrap();
                    status.status = "completed".to_string();
                    status.message = format!("Completed! {} listings saved to database", listings.len());
                    status.completed_at = Some(chrono::Utc::now().to_rfc3339());
                }
            },
            Err(e) => {
                log::error!("[SCRAPE_CITY] Background scrape failed: {}", e);
                // Update status to failed
                {
                    let mut status = SCRAPE_STATUS.lock().unwrap();
                    status.status = "failed".to_string();
                    status.message = format!("Failed: {}", e);
                    status.completed_at = Some(chrono::Utc::now().to_rfc3339());
                }
            },
        }
        // Guard drops here, releasing the lock
    });

    // Return immediately to client - scrape continues in background
    log::info!("[SCRAPE_CITY] Scrape task spawned for city: {}, returning immediately", city_for_response);
    Json(json!({
        "status": "started",
        "message": format!("Scraping started for city: {}. Results will be saved to database.", city_for_response),
        "note": "Scrape is running in background. Check logs for progress or query database for results."
    })).into_response()
}

// You might also want a version without limit
pub async fn get_listings_without_limit(
    Path(city): Path<String>,
) -> impl IntoResponse {
    let result = async {
        let query = if !city.is_empty() {
            doc! { "location": { "$regex": city, "$options": "i" } }
        } else {
            doc! {}
        };

        let listings = database::get_listings_by_query(query, 0).await?; // 0 means no limit
        Ok::<_, anyhow::Error>(Json(listings))
    }
        .await;

    match result {
        Ok(response) => response.into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("Failed to get listings: {}", e),
        )
            .into_response(),
    }
}

pub async fn get_all_listings() -> impl IntoResponse {
    let result = async {
        let query = doc! {}; // Empty query to get all documents
        let listings = database::get_listings_by_query(query, 0).await?; // 0 limit means no limit
        Ok::<_, anyhow::Error>(Json(listings))
    }
        .await;

    match result {
        Ok(response) => response.into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("Failed to get listings: {}", e)
        )
            .into_response(),
    }
}

pub async fn get_listings(
    Path((city, limit)): Path<(String, Option<i64>)>, // Extract path parameters
) -> impl IntoResponse {
    let result = async {
        let limit = limit.unwrap_or(-1); // -1 means no limit

        let query = if !city.is_empty() {
            doc! { "location": { "$regex": city, "$options": "i" } }
        } else {
            doc! {}
        };

        let listings = database::get_listings_by_query(query, limit).await?;
        Ok::<_, anyhow::Error>(Json(listings))
    }
        .await;

    match result {
        Ok(response) => response.into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("Failed to get listings: {}", e),
        )
            .into_response(),
    }
}

pub async fn filters(Query(params): Query<HashMap<String, String>>) -> impl IntoResponse {
    let result = async {
        let search = params.get("search");
        let features: Vec<String> = params
            .get("features")
            .map(|f| f.split(',').map(String::from).collect())
            .unwrap_or_default();
        let limit = params
            .get("limit")
            .and_then(|l| l.parse::<i64>().ok())
            .unwrap_or(-1); // -1 means no limit in MongoDB

        let mut query = doc! {};
        if let Some(s) = search {
            query.insert("location", doc! { "$regex": s, "$options": "i" });
        }
        if !features.is_empty() {
            query.insert("features", doc! { "$all": &features });
        }

        let filters_result = database::get_filters(query, limit).await?;
        Ok::<_, anyhow::Error>(Json(filters_result))
    }
        .await;

    match result {
        Ok(response) => response.into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("Failed to get filters: {}", e),
        )
            .into_response(),
    }
}

pub async fn get_listing(Path(listing_id): Path<String>) -> impl IntoResponse {
    let result = async {
        let object_id = ObjectId::parse_str(&listing_id)
            .map_err(|_| anyhow::anyhow!("Invalid listing ID"))?;

        match database::get_listing_by_id(object_id).await? {
            Some(listing) => Ok::<_, anyhow::Error>(Json(listing)),
            None => Err(anyhow::anyhow!("Listing not found")),
        }
    }
        .await;

    match result {
        Ok(response) => response.into_response(),
        Err(e) => {
            let (status, message) = match e.to_string().as_str() {
                "Invalid listing ID" => (StatusCode::BAD_REQUEST, "Invalid listing ID"),
                "Listing not found" => (StatusCode::NOT_FOUND, "Listing not found"),
                _ => (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "Failed to get listing",
                ),
            };
            (status, message).into_response()
        }
    }
}

pub async fn info() -> impl IntoResponse {
    Json(json!({
        "environment": std::env::var("ENVIRONMENT").unwrap_or_else(|_| "production".to_string()),
        "status": "running"
    }))
}

pub async fn health() -> impl IntoResponse {
    Json(json!({
        "status": "healthy",
        "environment": std::env::var("ENVIRONMENT").unwrap_or_else(|_| "production".to_string())
    }))
}

/// Get current scrape status for polling
pub async fn scrape_status() -> impl IntoResponse {
    let status = SCRAPE_STATUS.lock().unwrap().clone();
    Json(status)
}

/// Test endpoint to verify stealth browser works with Airbnb
pub async fn test_stealth_browser() -> impl IntoResponse {
    async fn run_test() -> Result<serde_json::Value> {
        log::info!("[TEST-STEALTH] Starting stealth browser test...");

        // Create stealth driver
        let driver = create_stealth_driver().await?;
        log::info!("[TEST-STEALTH] Stealth driver created successfully");

        // Navigate to Airbnb
        let test_url = "https://www.airbnb.com/s/Montreal--Quebec--Canada/homes";
        log::info!("[TEST-STEALTH] Navigating to: {}", test_url);
        driver.goto(test_url).await?;

        // Wait and check if session is still valid
        for i in 1..=10 {
            tokio::time::sleep(Duration::from_secs(1)).await;
            let is_valid = driver.is_valid().await;
            let current_url = driver.current_url().await.unwrap_or_else(|_| "unknown".to_string());
            log::info!("[TEST-STEALTH] Check {}/10: valid={}, url={}", i, is_valid, current_url);

            if !is_valid {
                log::error!("[TEST-STEALTH] Session died after {} seconds!", i);
                return Ok(json!({
                    "success": false,
                    "message": format!("Session died after {} seconds", i),
                    "lasted_seconds": i
                }));
            }
        }

        // Get page source to verify content
        let source = driver.page_source().await.unwrap_or_default();
        let has_listings = source.contains("StaySearchResult") || source.contains("listing") || source.contains("homes");

        log::info!("[TEST-STEALTH] Test complete! Session lasted 10+ seconds, has_listings={}", has_listings);

        let _ = driver.quit().await;

        Ok(json!({
            "success": true,
            "message": "Stealth browser test passed - session lasted 10+ seconds",
            "lasted_seconds": 10,
            "has_listings": has_listings
        }))
    }

    match run_test().await {
        Ok(response) => Json(response).into_response(),
        Err(e) => {
            log::error!("[TEST-STEALTH] Test failed: {}", e);
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(json!({
                    "success": false,
                    "error": e.to_string()
                })),
            )
                .into_response()
        }
    }
}

// Global watchlist service instance (in production, this would be behind a proper state manager)
use std::sync::Arc;

static WATCHLIST_SERVICE: Lazy<Arc<StdMutex<AirbnbWatchlistService>>> =
    Lazy::new(|| Arc::new(StdMutex::new(AirbnbWatchlistService::new())));

#[derive(serde::Deserialize, serde::Serialize)]
pub struct AddWatchlistRequest {
    pub user_id: String,
    pub listing_url: String,
    pub listing_id: String,
    pub title: String,
    pub current_price: f64,
    pub target_price: f64,
    pub location: String,
    pub check_in_date: String,
    pub check_out_date: String,
    pub guests: i32,
    pub email: String,
}

pub async fn add_to_watchlist(Json(req): Json<AddWatchlistRequest>) -> impl IntoResponse {
    let item = WatchlistItem::new(
        req.user_id,
        req.listing_url,
        req.listing_id,
        req.title,
        req.current_price,
        req.target_price,
        req.location,
        req.check_in_date,
        req.check_out_date,
        req.guests,
        req.email,
    );

    match WATCHLIST_SERVICE.lock() {
        Ok(mut service) => {
            match service.add_to_watchlist(item).await {
                Ok(_) => Json(json!({
                    "status": "success",
                    "message": "Added to watchlist"
                })).into_response(),
                Err(_) => StatusCode::INTERNAL_SERVER_ERROR.into_response()
            }
        }
        Err(_) => StatusCode::SERVICE_UNAVAILABLE.into_response()
    }
}

pub async fn get_watchlist(Path(user_id): Path<String>) -> impl IntoResponse {
    match WATCHLIST_SERVICE.lock() {
        Ok(service) => {
            let items = service.get_user_watchlist(&user_id).await;
            Json(json!({
                "status": "success",
                "watchlist": items
            })).into_response()
        }
        Err(_) => StatusCode::SERVICE_UNAVAILABLE.into_response()
    }
}

pub async fn remove_from_watchlist(Path((user_id, listing_id)): Path<(String, String)>) -> impl IntoResponse {
    match WATCHLIST_SERVICE.lock() {
        Ok(mut service) => {
            match service.remove_from_watchlist(&user_id, &listing_id).await {
                Ok(_) => Json(json!({
                    "status": "success",
                    "message": "Removed from watchlist"
                })).into_response(),
                Err(e) => Json(json!({
                    "status": "error",
                    "message": e.to_string()
                })).into_response()
            }
        }
        Err(_) => Json(json!({
            "status": "error",
            "message": "Service unavailable"
        })).into_response()
    }
}

pub async fn check_price_updates() -> impl IntoResponse {
    match WATCHLIST_SERVICE.lock() {
        Ok(mut service) => {
            match service.check_price_updates().await {
                Ok(price_drops) => {
                    // Send alerts for price drops
                    for (item, drop_amount) in &price_drops {
                        if let Err(e) = service.send_price_alert(item, *drop_amount).await {
                            log::error!("Failed to send price alert: {}", e);
                        }
                    }

                    Json(json!({
                        "status": "success",
                        "message": format!("Checked prices, found {} price drops", price_drops.len()),
                        "price_drops": price_drops.len()
                    })).into_response()
                }
                Err(_) => StatusCode::INTERNAL_SERVER_ERROR.into_response()
            }
        }
        Err(_) => StatusCode::SERVICE_UNAVAILABLE.into_response()
    }
}

pub async fn get_price_history(Path(listing_id): Path<String>) -> impl IntoResponse {
    match WATCHLIST_SERVICE.lock() {
        Ok(service) => {
            let history = service.get_price_history(&listing_id).await;
            Json(json!({
                "status": "success",
                "price_history": history
            })).into_response()
        }
        Err(_) => StatusCode::SERVICE_UNAVAILABLE.into_response()
    }
}

pub async fn test_email() -> impl IntoResponse {
    tracing::info!("Test email endpoint called");
    
    match send_email().await {
        Ok(_) => {
            tracing::info!("Test email sent successfully");
            Json(json!({
                "status": "success",
                "message": "Test email sent successfully"
            })).into_response()
        }
        Err(e) => {
            tracing::error!("Failed to send test email: {}", e);
            Json(json!({
                "status": "error",
                "message": format!("Failed to send email: {}", e)
            })).into_response()
        }
    }
}