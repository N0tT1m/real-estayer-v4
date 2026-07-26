// Split out of the former monolithic scraping.rs. Code is unchanged;
// only visibility was widened so cross-module calls resolve.

use super::*;
use anyhow::Result;
use futures::stream::{FuturesUnordered, StreamExt};
use std::sync::Arc;
use thirtyfour::{By, WebDriver, WebElement};
use tokio::sync::Semaphore;
use tokio::time::{sleep, timeout, Duration};

// Helper function to construct and sanitize full URLs for Airbnb
pub(crate) fn construct_airbnb_url(path: &str) -> String {
    // First sanitize the path - fix HTML entities and encoding issues
    let sanitized = path
        .replace("&amp;", "&") // Fix HTML-encoded ampersands
        .replace("&lt;", "<") // Fix HTML-encoded less than
        .replace("&gt;", ">") // Fix HTML-encoded greater than
        .replace("&quot;", "\"") // Fix HTML-encoded quotes
        .replace("//rooms/", "/rooms/") // Fix double slashes
        .replace("//homes/", "/homes/"); // Fix double slashes

    if sanitized.starts_with("http") {
        sanitized
    } else {
        format!("{}{}", AIRBNB_BASE_URL, sanitized.trim_start_matches('/'))
    }
}

// Check if the WebDriver session is still valid
pub(crate) async fn check_session_valid(driver: &WebDriver) -> bool {
    use tracing::{info, warn};

    match driver.current_url().await {
        Ok(url) => {
            info!("[SESSION CHECK] Session is VALID - current URL: {}", url);
            true
        }
        Err(e) => {
            warn!("[SESSION CHECK] Session is INVALID - error: {}", e);
            false
        }
    }
}

// Dismiss any popups on the page (cookie consent, translation modal, etc.)
pub(crate) async fn dismiss_popups(driver: &WebDriver) {
    use tracing::{debug, error, info, warn};

    info!("[POPUP] === STARTING POPUP DISMISSAL ===");

    // First check if session is valid
    if !check_session_valid(driver).await {
        error!("[POPUP] Session invalid before popup dismissal - aborting");
        return;
    }

    // Try clicking close buttons that are ONLY within dialogs/modals (safe selectors)
    // These are specific to Airbnb's modal structure
    let safe_modal_selectors = [
        // Close button specifically within a dialog
        (
            "div[role='dialog'] button[aria-label='Close']",
            "dialog close button",
        ),
        // Airbnb's translation/currency modal close buttons
        (
            "button[data-testid='modal-close-button']",
            "modal-close-button",
        ),
        ("button[data-testid='close-button']", "close-button"),
        // Cookie consent accept buttons
        ("button[data-testid='accept-btn']", "accept-btn"),
        (
            "button[data-testid='accept-cookies-button']",
            "accept-cookies",
        ),
        // Translation modal specific
        (
            "[data-testid='translation-announce-modal'] button[aria-label='Close']",
            "translation modal close",
        ),
    ];

    info!(
        "[POPUP] Checking {} modal selectors...",
        safe_modal_selectors.len()
    );

    for (selector, name) in safe_modal_selectors {
        info!("[POPUP] Trying selector: {} ({})", selector, name);

        match driver.find(By::Css(selector)).await {
            Ok(element) => {
                info!("[POPUP] FOUND element with selector: {}", selector);

                // Check if displayed
                match element.is_displayed().await {
                    Ok(true) => {
                        info!("[POPUP] Element IS displayed, attempting click...");
                        match element.click().await {
                            Ok(_) => {
                                info!("[POPUP] SUCCESS! Clicked: {}", name);
                                sleep(Duration::from_millis(500)).await;

                                // Verify session still valid after click
                                if !check_session_valid(driver).await {
                                    error!("[POPUP] SESSION DIED AFTER CLICKING: {}", name);
                                    return;
                                }
                                info!("[POPUP] Session still valid after click");
                                break;
                            }
                            Err(e) => {
                                warn!("[POPUP] Failed to click {}: {}", name, e);
                            }
                        }
                    }
                    Ok(false) => {
                        debug!("[POPUP] Element found but NOT displayed: {}", selector);
                    }
                    Err(e) => {
                        warn!("[POPUP] Error checking if displayed: {}", e);
                    }
                }
            }
            Err(_) => {
                debug!("[POPUP] Selector not found: {}", selector);
            }
        }
    }

    info!("[POPUP] Trying Escape key...");
    match driver.execute("document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', keyCode: 27, bubbles: true}));", vec![]).await {
        Ok(_) => info!("[POPUP] Escape key sent successfully"),
        Err(e) => warn!("[POPUP] Failed to send Escape key: {}", e),
    }

    sleep(Duration::from_millis(500)).await;

    // Final session check
    if !check_session_valid(driver).await {
        error!("[POPUP] SESSION DIED AFTER POPUP DISMISSAL!");
    } else {
        info!("[POPUP] === POPUP DISMISSAL COMPLETED - Session still valid ===");
    }
}

// Configure WebDriver with realistic browser settings
pub async fn configure_realistic_browser(driver: &WebDriver) -> Result<()> {
    use tracing::{debug, info};

    info!("Configuring browser with enhanced anti-detection settings...");

    // Set a realistic viewport size
    let viewport = VIEWPORT_SIZES[fastrand::usize(0..VIEWPORT_SIZES.len())];
    debug!("Setting viewport to {}x{}", viewport.0, viewport.1);
    let script = format!("window.resizeTo({}, {});", viewport.0, viewport.1);
    let _ = driver.execute(&script, vec![]).await;

    // Add realistic browser properties and behaviors
    let enhancement_scripts = [
        // Make webdriver property undefined
        "Object.defineProperty(navigator, 'webdriver', {get: () => undefined});",
        // Add realistic screen properties
        &format!(
            "Object.defineProperty(screen, 'width', {{get: () => {}}});",
            viewport.0
        ),
        &format!(
            "Object.defineProperty(screen, 'height', {{get: () => {}}});",
            viewport.1
        ),
        // Add realistic navigator properties
        "Object.defineProperty(navigator, 'language', {get: () => 'en-US'});",
        "Object.defineProperty(navigator, 'languages', {get: () => ['en-US', 'en']});",
        "Object.defineProperty(navigator, 'platform', {get: () => 'MacIntel'});",
        // Add realistic timing behavior
        "window.chrome = {runtime: {}};",
        // Override automation detection
        "Object.defineProperty(navigator, 'plugins', {get: () => [{name: 'Chrome PDF Plugin'}]});",
        "Object.defineProperty(navigator, 'mimeTypes', {get: () => [{type: 'application/pdf'}]});",
    ];

    for script in enhancement_scripts {
        let _ = driver.execute(script, vec![]).await;
        human_delay(50, 150).await;
    }

    // Add random mouse movements and scrolling to simulate human behavior
    let _ = driver
        .execute(
            "document.addEventListener('DOMContentLoaded', function() {
        // Simulate mouse movement
        let moveEvent = new MouseEvent('mousemove', {
            clientX: Math.random() * window.innerWidth,
            clientY: Math.random() * window.innerHeight
        });
        document.dispatchEvent(moveEvent);
    });",
            vec![],
        )
        .await;

    let user_agent = USER_AGENTS[fastrand::usize(0..USER_AGENTS.len())];
    debug!("Using user agent: {}", user_agent);

    info!("Browser configured with enhanced anti-detection settings");
    Ok(())
}

// Add realistic headers to requests (for future HTTP client implementation)
pub fn get_realistic_headers() -> std::collections::HashMap<String, String> {
    let mut headers = std::collections::HashMap::new();

    let user_agent = USER_AGENTS[fastrand::usize(0..USER_AGENTS.len())];
    let accept = ACCEPT_HEADERS[fastrand::usize(0..ACCEPT_HEADERS.len())];
    let sec_ch_ua = SEC_CH_UA_VALUES[fastrand::usize(0..SEC_CH_UA_VALUES.len())];

    headers.insert("User-Agent".to_string(), user_agent.to_string());
    headers.insert("Accept".to_string(), accept.to_string());
    headers.insert("Accept-Language".to_string(), "en-US,en;q=0.9".to_string());
    headers.insert(
        "Accept-Encoding".to_string(),
        "gzip, deflate, br, zstd".to_string(),
    );
    headers.insert("Cache-Control".to_string(), "no-cache".to_string());
    headers.insert("Pragma".to_string(), "no-cache".to_string());
    headers.insert("Sec-Ch-Ua".to_string(), sec_ch_ua.to_string());
    headers.insert("Sec-Ch-Ua-Mobile".to_string(), "?0".to_string());
    headers.insert("Sec-Ch-Ua-Platform".to_string(), "\"Windows\"".to_string());
    headers.insert("Sec-Fetch-Dest".to_string(), "document".to_string());
    headers.insert("Sec-Fetch-Mode".to_string(), "navigate".to_string());
    headers.insert("Sec-Fetch-Site".to_string(), "none".to_string());
    headers.insert("Sec-Fetch-User".to_string(), "?1".to_string());
    headers.insert("Upgrade-Insecure-Requests".to_string(), "1".to_string());

    headers
}

// Human-like delay function
pub(crate) async fn human_delay(min_millis: u64, max_millis: u64) {
    let delay = fastrand::u64(min_millis..=max_millis);
    sleep(Duration::from_millis(delay)).await;
}

// Wait for elements to appear, similar to Python's wait_for_elements
pub(crate) async fn wait_for_elements(
    driver: &WebDriver,
    by: By,
    timeout_secs: u64,
) -> Result<Vec<WebElement>> {
    let timeout_duration = Duration::from_secs(timeout_secs);

    timeout(timeout_duration, async {
        loop {
            match driver.find_all(by.clone()).await {
                Ok(elements) if !elements.is_empty() => return Ok(elements),
                _ => sleep(Duration::from_millis(500)).await,
            }
        }
    })
    .await
    .map_err(|_| anyhow::anyhow!("Timeout waiting for elements"))?
}

// Check if room links are present and populated with href attributes
pub(crate) async fn check_for_room_links(driver: &WebDriver) -> bool {
    use tracing::debug;

    let selectors_to_check = [
        "l1ovpqvx", // Primary selector from your example
        "c1w4n3ae", // Alternative selector
        "bewl01v",  // Another alternative
    ];

    for selector in selectors_to_check {
        if let Ok(elements) = driver.find_all(By::ClassName(selector)).await {
            for (i, element) in elements.iter().take(5).enumerate() {
                if let Ok(Some(href)) = element.attr("href").await {
                    if href.contains("/rooms/") && !href.trim().is_empty() {
                        debug!(
                            "Found room link with selector '{}' element {}: {}",
                            selector, i, href
                        );
                        return true;
                    }
                }
            }
        }
    }

    // Also check for any links with room patterns using XPath
    if let Ok(elements) = driver
        .find_all(By::XPath("//a[contains(@href, '/rooms/')]"))
        .await
    {
        if !elements.is_empty() {
            debug!("Found {} room links via XPath", elements.len());
            return true;
        }
    }

    false
}

// Simulate human scrolling
pub(crate) async fn human_scroll(driver: &WebDriver) -> Result<()> {
    let scroll_steps = fastrand::usize(3..=8);

    for _ in 0..scroll_steps {
        let scroll_amount = fastrand::u32(200..=800);
        driver
            .execute(&format!("window.scrollBy(0, {});", scroll_amount), vec![])
            .await?;
        human_delay(500, 2000).await;

        // Sometimes scroll back up
        if fastrand::f32() < 0.3 {
            let scroll_back = fastrand::u32(100..=scroll_amount / 2);
            driver
                .execute(&format!("window.scrollBy(0, -{});", scroll_back), vec![])
                .await?;
            human_delay(500, 1500).await;
        }
    }
    Ok(())
}

// Rest of the helper functions remain unchanged
pub(crate) async fn close_modal(driver: &WebDriver) -> Result<()> {
    if let Ok(close_button) = driver
        .find(By::XPath("//button[@aria-label='Close']"))
        .await
    {
        if close_button.is_clickable().await? {
            close_button.click().await?;
            sleep(Duration::from_secs(1)).await;
            log::info!("Successfully closed modal");
        }
    }
    Ok(())
}

pub(crate) async fn click_show_all_amenities(driver: &WebDriver) -> Result<bool> {
    close_modal(driver).await?;

    // Try multiple strategies to find the "Show all amenities" button
    let button_selectors = [
        // Primary: XPath for button containing the text
        By::XPath("//button[contains(., 'Show all') and contains(., 'amenities')]"),
        // Fallback: span inside button with the text
        By::XPath(
            "//button//span[contains(text(), 'Show all') and contains(text(), 'amenities')]/..",
        ),
        // Fallback: button with specific class (from current Airbnb HTML)
        By::XPath("//button[contains(@class, 'l1ovpqvx')]//span[contains(text(), 'amenities')]/.."),
    ];

    let mut button_found = None;
    for selector in &button_selectors {
        if let Ok(btn) = driver.find(selector.clone()).await {
            log::info!("Found amenities button with selector: {:?}", selector);
            button_found = Some(btn);
            break;
        }
    }

    let button = match button_found {
        Some(btn) => btn,
        None => {
            log::warn!("Failed to find 'Show all amenities' button with any selector");
            return Ok(false);
        }
    };

    // Scroll to and click the button
    driver
        .execute(
            "arguments[0].scrollIntoView({block: 'center'});",
            vec![button.to_json()?],
        )
        .await?;

    sleep(Duration::from_millis(500)).await;

    driver
        .execute("arguments[0].click();", vec![button.to_json()?])
        .await?;

    // Wait for the amenities modal to open - try multiple indicators
    sleep(Duration::from_millis(800)).await;

    // Check if modal opened by looking for dialog or amenity content
    let modal_opened = driver.find(By::XPath("//div[@role='dialog']")).await.is_ok()
        || driver.find(By::ClassName("twad414")).await.is_ok()
        || driver.find(By::XPath("//section[contains(@aria-label, 'amenities') or contains(@aria-label, 'Amenities')]")).await.is_ok();

    if modal_opened {
        log::info!("Successfully clicked 'Show all amenities' button and modal opened");
        Ok(true)
    } else {
        log::warn!("Clicked amenities button but modal may not have opened");
        Ok(true) // Still return true since we clicked, let scrape_features try to get what's available
    }
}

pub(crate) async fn scrape_features(driver: &WebDriver) -> Result<Vec<String>> {
    let modal_opened = click_show_all_amenities(driver).await?;

    let mut raw_features: Vec<String> = Vec::new();

    if modal_opened {
        // Try multiple selectors for amenity items in the modal
        let amenity_selectors = [
            // Legacy class (may still work on some pages)
            By::ClassName("twad414"),
            // Dialog amenity items - look for text within list items in the dialog
            By::XPath("//div[@role='dialog']//div[contains(@class, 'twad414')]"),
            // Section-based amenities
            By::XPath("//div[@role='dialog']//section//div[contains(@class, '_19xnuo97')]"),
            // Generic: any div with data-section-id containing "AMENITIES"
            By::XPath("//div[@role='dialog']//*[contains(@data-section-id, 'AMENITIES')]//div[contains(@class, 't1')]"),
            // Amenity rows - look for structured amenity items
            By::XPath("//div[@role='dialog']//div[contains(@class, '_1byskwn')]"),
            // Fallback: look for spans/divs in dialog that might be amenity titles
            By::XPath("//div[@role='dialog']//div[contains(@class, 'lgx66tx')]"),
        ];

        for selector in &amenity_selectors {
            if let Ok(elements) = driver.find_all(selector.clone()).await {
                if !elements.is_empty() {
                    log::info!(
                        "Found {} amenity elements with selector: {:?}",
                        elements.len(),
                        selector
                    );
                    raw_features = scrape_elements_parallel(elements).await?;
                    if !raw_features.is_empty() {
                        break;
                    }
                }
            }
        }

        // If still empty, try a JavaScript-based extraction as last resort
        if raw_features.is_empty() {
            log::info!("Trying JavaScript extraction for amenities");
            if let Ok(result) = driver
                .execute(
                    r#"
                const dialog = document.querySelector('div[role="dialog"]');
                if (!dialog) return [];

                // Try to find amenity text nodes
                const amenities = [];
                const walker = document.createTreeWalker(dialog, NodeFilter.SHOW_TEXT, null, false);
                while (walker.nextNode()) {
                    const text = walker.currentNode.textContent.trim();
                    // Filter for likely amenity names (skip buttons, numbers, etc.)
                    if (text.length > 2 && text.length < 80 &&
                        !text.match(/^[\d,.$]+$/) &&
                        !text.match(/^(Show|Close|Back|Next|See|View)/i)) {
                        amenities.push(text);
                    }
                }
                return [...new Set(amenities)].slice(0, 50);
                "#,
                    vec![],
                )
                .await
            {
                if let Some(arr) = result.json().as_array() {
                    for item in arr {
                        if let Some(s) = item.as_str() {
                            raw_features.push(s.to_string());
                        }
                    }
                    log::info!(
                        "JavaScript extraction found {} potential amenities",
                        raw_features.len()
                    );
                }
            }
        }
    }

    // If modal didn't open or no features found, try page-level amenities
    if raw_features.is_empty() {
        log::info!("Trying page-level amenity extraction");
        let page_selectors = [
            By::XPath("//div[contains(@class, 'amenities')]//div[contains(@class, 'title')]"),
            By::XPath("//div[contains(@data-section-id, 'AMENITIES')]//div"),
            By::XPath("//*[contains(@aria-label, 'amenities')]//div"),
        ];

        for selector in &page_selectors {
            if let Ok(elements) = driver.find_all(selector.clone()).await {
                if !elements.is_empty() {
                    log::info!("Found {} page-level amenity elements", elements.len());
                    raw_features = scrape_elements_parallel(elements).await?;
                    if !raw_features.is_empty() {
                        break;
                    }
                }
            }
        }
    }

    log::info!("=== RAW AMENITIES FOUND ({}) ===", raw_features.len());
    for (i, feature) in raw_features.iter().enumerate() {
        log::info!("  [{}] {:?}", i + 1, feature);
    }

    // Normalize all features to canonical form
    let normalized: Vec<String> = raw_features
        .into_iter()
        .filter(|f| !f.trim().is_empty() && f.len() > 1)
        .map(|f| normalize_amenity(&f))
        .filter(|f| !f.is_empty())
        .collect::<std::collections::HashSet<_>>()
        .into_iter()
        .collect();

    log::info!("=== NORMALIZED AMENITIES ({}) ===", normalized.len());
    for (i, feature) in normalized.iter().enumerate() {
        log::info!("  [{}] {}", i + 1, feature);
    }
    Ok(normalized)
}

pub(crate) async fn scrape_elements_parallel(elements: Vec<WebElement>) -> Result<Vec<String>> {
    let semaphore = Arc::new(Semaphore::new(MAX_CONCURRENT_SCRAPES));
    let mut tasks: FuturesUnordered<_> = elements
        .into_iter()
        .map(|element| {
            let permit = semaphore.clone().acquire_owned();
            async move {
                let _permit = permit.await?;
                let text = element.text().await?;
                Ok::<String, anyhow::Error>(text)
            }
        })
        .collect();

    let mut results = Vec::new();
    while let Some(result) = tasks.next().await {
        if let Ok(text) = result {
            results.push(text);
        }
    }

    Ok(results)
}

pub(crate) async fn scrape_house_details(driver: &WebDriver) -> Result<Vec<String>> {
    let elements = driver.find_all(By::ClassName("l7n4lsf")).await?;
    let details_futures = elements
        .into_iter()
        .map(|element| async move { element.text().await });

    let details = futures::future::join_all(details_futures)
        .await
        .into_iter()
        .filter_map(|r| r.ok())
        .collect();

    log::info!("Scraped the details about the AirBnB");
    Ok(details)
}

pub(crate) async fn get_text_or_empty(driver: &WebDriver, by: By) -> Result<String> {
    match driver.find(by).await {
        Ok(element) => Ok(element.text().await.unwrap_or_else(|_| String::new())),
        Err(_) => Ok(String::new()),
    }
}

pub(crate) async fn extract_description_from_dom(driver: &WebDriver) -> Result<String> {
    use tracing::debug;

    // Minimal DOM selectors - only for when page source extraction fails
    let description_selectors = [
        "[data-testid*='description']",
        "h1", // Property title as last resort
    ];

    for (i, selector) in description_selectors.iter().enumerate() {
        debug!("Trying description selector {}: {}", i + 1, selector);

        // Add timeout to prevent hanging on missing elements
        let find_result = timeout(Duration::from_secs(10), driver.find(By::Css(*selector))).await;

        if let Ok(Ok(element)) = find_result {
            if let Ok(text) = element.text().await {
                let trimmed = text.trim();
                if !trimmed.is_empty() && trimmed.len() > 10 {
                    let preview: String = trimmed.chars().take(50).collect();
                    debug!(
                        "Found description with selector '{}': '{}'",
                        selector, preview
                    );
                    return Ok(trimmed.to_string());
                }
            }
        }
    }

    debug!("No description found with DOM selectors");
    Err(anyhow::anyhow!("No description found"))
}

pub(crate) async fn extract_price_from_dom(driver: &WebDriver) -> Result<String> {
    use tracing::debug;

    // Try multiple selectors for price
    let price_selectors = [
        "span.umg93v9", // Current Airbnb price span - PRIORITY
        "span[data-testid='price-availability-row-label-price']",
        "[data-testid='price-availability-row'] span",
        "div._tyxjp1",   // Common Airbnb price class
        "span._1p7iugi", // Another price class
        ".price-range",
        "[data-testid*='price']",
        "span[class*='price']",
        "div[class*='price']",
    ];

    for (i, selector) in price_selectors.iter().enumerate() {
        debug!("Trying price selector {}: {}", i + 1, selector);

        // Add timeout to prevent hanging on missing elements
        let find_result = timeout(Duration::from_secs(10), driver.find(By::Css(*selector))).await;

        if let Ok(Ok(element)) = find_result {
            if let Ok(text) = element.text().await {
                let trimmed = text.trim();
                if trimmed.contains('$') {
                    debug!("Found price with selector '{}': '{}'", selector, trimmed);
                    return Ok(trimmed.to_string());
                }
            }
        }
    }

    debug!("No price found with DOM selectors");
    Err(anyhow::anyhow!("No price found"))
}

// Fallback data extraction using the old method
pub(crate) async fn extract_data_fallback(
    page_source: &str,
    driver: &WebDriver,
) -> Result<(String, String, String, String, String, String, Vec<String>)> {
    use tracing::debug;

    // Extract title from meta tag or JSON-LD
    debug!("Extracting title...");
    let title = extract_title_from_source(page_source).unwrap_or_else(|| {
        // Fallback to h1 tag
        futures::executor::block_on(get_text_or_empty(driver, By::Tag("h1"))).unwrap_or_default()
    });

    // Extract picture URL from meta tag or JSON-LD
    debug!("Extracting picture URL...");
    let picture_url = extract_picture_url_from_source(page_source).unwrap_or_default();

    // Extract description with multiple fallbacks
    debug!("Extracting description...");
    let description = extract_description_from_source(page_source)
        .or_else(|| {
            // Fallback: try to get description from DOM elements
            debug!("Meta description failed, trying DOM extraction");
            futures::executor::block_on(extract_description_from_dom(driver)).ok()
        })
        .unwrap_or_else(|| {
            // Last resort: generate description from title
            debug!("All description extraction failed, generating from title");
            if !title.is_empty() {
                format!("Property listing: {}", title)
            } else {
                String::new()
            }
        });

    // Extract price with multiple fallbacks
    debug!("Extracting price...");
    let price = extract_price_from_source(page_source)
        .or_else(|| {
            // Fallback: try to get price from DOM elements
            debug!("Meta price failed, trying DOM extraction");
            futures::executor::block_on(extract_price_from_dom(driver)).ok()
        })
        .or_else(|| {
            // Extract from title if it contains price info
            debug!("DOM price failed, trying title extraction");
            extract_price_from_title(&title)
        })
        .unwrap_or_default();

    // Extract rating from JSON-LD
    debug!("Extracting rating...");
    let rating = extract_rating_from_source(page_source).unwrap_or_default();

    // Extract location from JSON-LD or meta tags
    debug!("Extracting location...");
    let location = extract_location_from_source(page_source).unwrap_or_default();

    let features = Vec::new(); // Will be populated later

    Ok((
        title,
        description,
        price,
        rating,
        location,
        picture_url,
        features,
    ))
}
