// scraping.rs
use anyhow::Result;
use futures::stream::{FuturesUnordered, StreamExt};
use std::collections::HashSet;
use thirtyfour::{By, WebDriver, WebElement};
use tokio::time::{sleep, Duration, timeout};
use tokio::sync::Semaphore;
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering as AtomicOrdering};
use crate::models::Listing;
use crate::stealth_browser::StealthDriver;
use base64::{Engine as _};
use chrono::Datelike;
use mail_send::mail_builder::MessageBuilder;
use mail_send::SmtpClientBuilder;

const MAX_CONCURRENT_SCRAPES: usize = 1; // Reduced to 1 for stability - multiple browsers cause session conflicts
const AIRBNB_BASE_URL: &str = "https://www.airbnb.com/";

// Updated user agents for 2025 - realistic PC browsers
pub const USER_AGENTS: &[&str] = &[
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.2 Safari/605.1.15",
    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 11.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
];

// Browser viewport sizes for realistic sessions
pub const VIEWPORT_SIZES: &[(u32, u32)] = &[
    (1920, 1080), (1366, 768), (1536, 864), (1440, 900), (1280, 720), (1600, 900), (2560, 1440)
];

// Accept headers that real browsers send
pub const ACCEPT_HEADERS: &[&str] = &[
    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
    "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
];

// Chrome Sec-Ch-Ua values for authenticity
pub const SEC_CH_UA_VALUES: &[&str] = &[
    "\"Google Chrome\";v=\"131\", \"Chromium\";v=\"131\", \"Not_A Brand\";v=\"24\"",
    "\"Google Chrome\";v=\"130\", \"Chromium\";v=\"130\", \"Not_A Brand\";v=\"99\"",
    "\"Firefox\";v=\"133\", \"Not A(Brand\";v=\"24\", \"Chromium\";v=\"133\""
];

// Helper function to construct and sanitize full URLs for Airbnb
fn construct_airbnb_url(path: &str) -> String {
    // First sanitize the path - fix HTML entities and encoding issues
    let sanitized = path
        .replace("&amp;", "&")      // Fix HTML-encoded ampersands
        .replace("&lt;", "<")       // Fix HTML-encoded less than
        .replace("&gt;", ">")       // Fix HTML-encoded greater than
        .replace("&quot;", "\"")    // Fix HTML-encoded quotes
        .replace("//rooms/", "/rooms/")  // Fix double slashes
        .replace("//homes/", "/homes/"); // Fix double slashes

    if sanitized.starts_with("http") {
        sanitized
    } else {
        format!("{}{}", AIRBNB_BASE_URL, sanitized.trim_start_matches('/'))
    }
}

// Check if the WebDriver session is still valid
async fn check_session_valid(driver: &WebDriver) -> bool {
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
async fn dismiss_popups(driver: &WebDriver) {
    use tracing::{info, debug, warn, error};

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
        ("div[role='dialog'] button[aria-label='Close']", "dialog close button"),
        // Airbnb's translation/currency modal close buttons
        ("button[data-testid='modal-close-button']", "modal-close-button"),
        ("button[data-testid='close-button']", "close-button"),
        // Cookie consent accept buttons
        ("button[data-testid='accept-btn']", "accept-btn"),
        ("button[data-testid='accept-cookies-button']", "accept-cookies"),
        // Translation modal specific
        ("[data-testid='translation-announce-modal'] button[aria-label='Close']", "translation modal close"),
    ];

    info!("[POPUP] Checking {} modal selectors...", safe_modal_selectors.len());

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
    use tracing::{info, debug};
    
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
        &format!("Object.defineProperty(screen, 'width', {{get: () => {}}});", viewport.0),
        &format!("Object.defineProperty(screen, 'height', {{get: () => {}}});", viewport.1),
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
    let _ = driver.execute("document.addEventListener('DOMContentLoaded', function() {
        // Simulate mouse movement
        let moveEvent = new MouseEvent('mousemove', {
            clientX: Math.random() * window.innerWidth,
            clientY: Math.random() * window.innerHeight
        });
        document.dispatchEvent(moveEvent);
    });", vec![]).await;
    
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
    headers.insert("Accept-Encoding".to_string(), "gzip, deflate, br, zstd".to_string());
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
async fn human_delay(min_millis: u64, max_millis: u64) {
    let delay = fastrand::u64(min_millis..=max_millis);
    sleep(Duration::from_millis(delay)).await;
}

// Wait for elements to appear, similar to Python's wait_for_elements
async fn wait_for_elements(driver: &WebDriver, by: By, timeout_secs: u64) -> Result<Vec<WebElement>> {
    let timeout_duration = Duration::from_secs(timeout_secs);
    
    timeout(timeout_duration, async {
        loop {
            match driver.find_all(by.clone()).await {
                Ok(elements) if !elements.is_empty() => return Ok(elements),
                _ => sleep(Duration::from_millis(500)).await,
            }
        }
    }).await.map_err(|_| anyhow::anyhow!("Timeout waiting for elements"))?
}

// Check if room links are present and populated with href attributes
async fn check_for_room_links(driver: &WebDriver) -> bool {
    use tracing::debug;
    
    let selectors_to_check = [
        "l1ovpqvx",  // Primary selector from your example
        "c1w4n3ae",  // Alternative selector
        "bewl01v",   // Another alternative
    ];
    
    for selector in selectors_to_check {
        if let Ok(elements) = driver.find_all(By::ClassName(selector)).await {
            for (i, element) in elements.iter().take(5).enumerate() {
                if let Ok(Some(href)) = element.attr("href").await {
                    if href.contains("/rooms/") && !href.trim().is_empty() {
                        debug!("Found room link with selector '{}' element {}: {}", selector, i, href);
                        return true;
                    }
                }
            }
        }
    }
    
    // Also check for any links with room patterns using XPath
    if let Ok(elements) = driver.find_all(By::XPath("//a[contains(@href, '/rooms/')]")).await {
        if !elements.is_empty() {
            debug!("Found {} room links via XPath", elements.len());
            return true;
        }
    }
    
    false
}

// Simulate human scrolling
async fn human_scroll(driver: &WebDriver) -> Result<()> {
    let scroll_steps = fastrand::usize(3..=8);
    
    for _ in 0..scroll_steps {
        let scroll_amount = fastrand::u32(200..=800);
        driver.execute(&format!("window.scrollBy(0, {});", scroll_amount), vec![]).await?;
        human_delay(500, 2000).await;
        
        // Sometimes scroll back up
        if fastrand::f32() < 0.3 {
            let scroll_back = fastrand::u32(100..=scroll_amount/2);
            driver.execute(&format!("window.scrollBy(0, -{});", scroll_back), vec![]).await?;
            human_delay(500, 1500).await;
        }
    }
    Ok(())
}

pub async fn get_place_urls(driver: &WebDriver, location: &str, check_in_date: Option<&str>, check_out_date: Option<&str>, guests: Option<i32>) -> Result<Vec<String>> {
    use tracing::{info, debug, warn, error, span, Level};
    
    let span = span!(Level::INFO, "get_place_urls", location = %location);
    let _enter = span.enter();
    
    info!("Starting URL collection for location: {}", location);

    // Configure browser with realistic settings first
    configure_realistic_browser(driver).await?;

    let encoded_location = urlencoding::encode(location);
    let adults = guests.unwrap_or(2);
    
    let search_url = if let (Some(checkin), Some(checkout)) = (check_in_date, check_out_date) {
        // Calculate trip duration for flexible search optimization
        let duration_days = if let (Ok(start), Ok(end)) = (
            chrono::NaiveDate::parse_from_str(checkin, "%Y-%m-%d"),
            chrono::NaiveDate::parse_from_str(checkout, "%Y-%m-%d")
        ) {
            (end - start).num_days()
        } else {
            7 // Default to 1 week if parsing fails
        };
        
        // Use appropriate flexible_trip_length based on duration
        let _flexible_length = if duration_days <= 3 {
            "weekend"
        } else if duration_days <= 7 {
            "one_week"
        } else if duration_days <= 30 {
            "one_month"
        } else {
            "three_months"
        };

        // Extract month info for monthly parameters
        let checkin_date = chrono::NaiveDate::parse_from_str(checkin, "%Y-%m-%d").ok();
        let (monthly_start, monthly_length, monthly_end) = if let Some(date) = checkin_date {
            let start_of_month = format!("{}-{:02}-01", date.year(), date.month());
            let next_quarter = date + chrono::Duration::days(90);
            let end_date = format!("{}-{:02}-01", next_quarter.year(), next_quarter.month());
            (start_of_month, "3", end_date)
        } else {
            ("2025-01-01".to_string(), "3", "2025-04-01".to_string())
        };
        
        format!(
            "{}s/{}/homes?refinement_paths%5B%5D=%2Fhomes&\
             query={}&\
             flexible_trip_lengths%5B%5D=one_week&\
             monthly_start_date={}&monthly_length={}&monthly_end_date={}&\
             search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=calendar&checkin={}&checkout={}&\
             source=structured_search_input_header&search_type=unknown&\
             adults={}",
            AIRBNB_BASE_URL, 
            urlencoding::encode(location),
            urlencoding::encode(&urlencoding::encode(location)),
            monthly_start, monthly_length, monthly_end,
            checkin, checkout, adults
        )
    } else {
        format!(
            "{}s/{}/homes?refinement_paths%5B%5D=%2Fhomes&\
             query={}&\
             flexible_trip_lengths%5B%5D=one_week&monthly_start_date=2025-01-01&monthly_length=12&\
             monthly_end_date=2026-01-01&search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=flexible_dates&source=structured_search_input_header&\
             search_type=unknown&adults={}",
            AIRBNB_BASE_URL, 
            urlencoding::encode(location),
            urlencoding::encode(&urlencoding::encode(location)),
            adults
        )
    };

    info!("Constructed search URL: {}", search_url);
    debug!("Encoded location: {}", encoded_location);

    // First, go to a blank page to inject anti-detection JS before Airbnb can detect us
    info!("[ANTI-BOT] Step 1: Navigating to blank page to inject anti-detection JS...");
    match driver.goto("about:blank").await {
        Ok(_) => {
            info!("[ANTI-BOT] Successfully navigated to blank page");
            // Brief wait
            sleep(Duration::from_millis(500)).await;

            // Inject comprehensive anti-detection JavaScript (puppeteer-stealth style)
            info!("[ANTI-BOT] Step 2: Injecting comprehensive anti-detection JavaScript...");
            let anti_detect_js = r#"
                // Remove webdriver property completely
                Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
                delete navigator.__proto__.webdriver;

                // Mock realistic plugins
                Object.defineProperty(navigator, 'plugins', {
                    get: () => {
                        const plugins = {
                            0: { name: 'Chrome PDF Plugin', filename: 'internal-pdf-viewer', description: 'Portable Document Format' },
                            1: { name: 'Chrome PDF Viewer', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai', description: '' },
                            2: { name: 'Native Client', filename: 'internal-nacl-plugin', description: '' },
                            length: 3
                        };
                        plugins.item = (index) => plugins[index] || null;
                        plugins.namedItem = (name) => Object.values(plugins).find(p => p.name === name) || null;
                        plugins.refresh = () => {};
                        return plugins;
                    }
                });

                // Mock realistic mimeTypes
                Object.defineProperty(navigator, 'mimeTypes', {
                    get: () => {
                        const mimes = {
                            0: { type: 'application/pdf', suffixes: 'pdf', description: 'Portable Document Format' },
                            length: 1
                        };
                        mimes.item = (index) => mimes[index] || null;
                        mimes.namedItem = (name) => Object.values(mimes).find(m => m.type === name) || null;
                        return mimes;
                    }
                });

                // Languages
                Object.defineProperty(navigator, 'languages', { get: () => ['en-US', 'en'] });
                Object.defineProperty(navigator, 'language', { get: () => 'en-US' });

                // Hardware concurrency (realistic CPU cores)
                Object.defineProperty(navigator, 'hardwareConcurrency', { get: () => 8 });

                // Device memory
                Object.defineProperty(navigator, 'deviceMemory', { get: () => 8 });

                // Platform
                Object.defineProperty(navigator, 'platform', { get: () => 'Win32' });

                // Override permissions query
                const originalQuery = window.navigator.permissions.query;
                window.navigator.permissions.query = (parameters) => (
                    parameters.name === 'notifications' ?
                        Promise.resolve({ state: Notification.permission }) :
                        originalQuery(parameters)
                );

                // Chrome object (many sites check for this)
                window.chrome = {
                    runtime: {},
                    loadTimes: function() {},
                    csi: function() {},
                    app: {}
                };

                // Override toString to hide modifications
                const originalToString = Function.prototype.toString;
                Function.prototype.toString = function() {
                    if (this === navigator.permissions.query) {
                        return 'function query() { [native code] }';
                    }
                    return originalToString.call(this);
                };

                // Fake battery API
                navigator.getBattery = () => Promise.resolve({
                    charging: true,
                    chargingTime: 0,
                    dischargingTime: Infinity,
                    level: 1
                });
            "#;
            if let Err(e) = driver.execute(anti_detect_js, vec![]).await {
                warn!("[ANTI-BOT] Failed to inject anti-detection JS: {}", e);
            } else {
                info!("[ANTI-BOT] Anti-detection JS injected successfully");
            }

            // Check if session still valid after blank page
            if !check_session_valid(driver).await {
                error!("[ANTI-BOT] Session died on blank page - GPU issue!");
                return Err(anyhow::anyhow!("Session died on blank page - likely GPU/Chrome issue"));
            }

            info!("[ANTI-BOT] Anti-detection JS ready, proceeding to Airbnb...");
        }
        Err(e) => {
            error!("[ANTI-BOT] Failed to navigate to blank page: {}", e);
            return Err(e.into());
        }
    }

    // Now navigate to the search URL
    info!("[ANTI-BOT] Step 3: Now navigating to search URL...");
    match driver.goto(&search_url).await {
        Ok(_) => {
            info!("Successfully navigated to search URL");

            // Wait for navigation to complete
            sleep(Duration::from_secs(3)).await;
            
            let current_url = driver.current_url().await.map(|u| u.to_string()).unwrap_or_else(|_| "unknown".to_string());
            let current_url_str = current_url.as_str();
            info!("Current URL after navigation: {}", current_url_str);
            
            // Check if navigation actually worked
            if current_url_str == "data:," || current_url_str.starts_with("data:") {
                error!("Navigation failed - browser shows data: URL instead of Airbnb");
                let page_title = driver.title().await.unwrap_or_else(|_| "unknown".to_string());
                error!("Page title: '{}'", page_title);
                
                // Try to get page source for debugging
                if let Ok(page_source) = driver.source().await {
                    // Use chars().take() for safe UTF-8 truncation
                    let source_preview: String = page_source.chars().take(200).collect();
                    error!("Page source preview: {}", source_preview);
                }
                
                return Err(anyhow::anyhow!("Browser navigation failed - showing data: URL instead of loading Airbnb"));
            }
            
            // Check if we got redirected or blocked
            if current_url_str.contains("captcha") || current_url_str.contains("blocked") {
                error!("Detected CAPTCHA or block page. Current URL: {}", current_url_str);
                return Err(anyhow::anyhow!("Got blocked by Airbnb"));
            }

            // Dismiss any popups (cookie consent, translation, etc.)
            dismiss_popups(driver).await;
        },
        Err(e) => {
            error!("Failed to navigate to search URL: {}", e);
            return Err(e.into());
        }
    }

    let mut urls = HashSet::new();
    let semaphore = Arc::new(Semaphore::new(MAX_CONCURRENT_SCRAPES));

    // Safety limits: maximum pages and URLs to prevent infinite loops and resource exhaustion
    const MAX_PAGES: usize = 15; // Airbnb typically shows max 300 listings (15 pages of ~20 listings each)
    const MAX_URLS: usize = 500; // Maximum total URLs to collect for safety

    let mut page_number = 1;
    loop {
        info!("[LOOP] ========== STARTING PAGE {} LOOP ==========", page_number);

        // Check session validity at start of each loop iteration
        if !check_session_valid(driver).await {
            error!("[LOOP] SESSION INVALID at start of page {} loop!", page_number);
            break;
        }

        // Safety checks: don't scrape more than MAX_PAGES or MAX_URLS
        if page_number > MAX_PAGES {
            warn!("[LOOP] Reached maximum page limit ({}) for safety, stopping pagination", MAX_PAGES);
            break;
        }
        if urls.len() >= MAX_URLS {
            warn!("[LOOP] Reached maximum URL limit ({}) for safety, stopping pagination", MAX_URLS);
            break;
        }

        info!("[LOOP] Processing page {} - starting 20 second wait for JS...", page_number);
        sleep(Duration::from_secs(20)).await;

        // Check session after wait
        if !check_session_valid(driver).await {
            error!("[LOOP] SESSION DIED during 20 second wait on page {}!", page_number);
            break;
        }

        let additional_delay = fastrand::u64(5000..10000);
        info!("[LOOP] Additional random delay: {}ms", additional_delay);
        sleep(Duration::from_millis(additional_delay)).await;

        // Check session after additional delay
        if !check_session_valid(driver).await {
            error!("[LOOP] SESSION DIED during additional delay on page {}!", page_number);
            break;
        }

        info!("[LOOP] Starting to check for room links on page {}...", page_number);

        // Wait for dynamic content to load by checking for actual room links
        let mut room_links_loaded = false;
        for attempt in 1..=15 {
            info!("[LOOP] Attempt {}/15 to find room links on page {}", attempt, page_number);

            // Check if we have room links with href attributes
            let has_room_links = check_for_room_links(driver).await;
            if has_room_links {
                room_links_loaded = true;
                info!("[LOOP] Room links detected after {} attempts on page {}", attempt, page_number);
                break;
            }

            // Also check body content as fallback
            match driver.find(By::Tag("body")).await {
                Ok(body) => {
                    match body.text().await {
                        Ok(text) => {
                            if text.len() > 2000 && (text.contains("Room in") || text.contains("Apartment in") || text.contains("Entire")) {
                                info!("[LOOP] Body content suggests listings are present (length: {})", text.len());
                                // Wait a bit more for links to be populated
                                sleep(Duration::from_secs(5)).await;
                                break;
                            }
                        }
                        Err(e) => {
                            warn!("[LOOP] Error getting body text: {}", e);
                        }
                    }
                }
                Err(e) => {
                    warn!("[LOOP] Error finding body element: {}", e);
                }
            }

            info!("[LOOP] Waiting for room links to load, attempt {}/15 on page {}", attempt, page_number);
            sleep(Duration::from_secs(4)).await;
        }

        if !room_links_loaded {
            warn!("[LOOP] Room links may not be fully loaded after 60 seconds on page {}", page_number);
        }

        // Log page title and URL for debugging
        let page_title = driver.title().await.unwrap_or_else(|_| "unknown".to_string());
        let current_url = driver.current_url().await.map(|u| u.to_string()).unwrap_or_else(|_| "unknown".to_string());
        let current_url_str = current_url.as_str();
        info!("Page {} - Title: '{}', URL: {}", page_number, page_title, current_url_str);

        let mut places_found = false;
        let listings_before = urls.len();

        // Try to extract listing URLs from Airbnb's JSON data first
        info!("Attempting to extract listing URLs from Airbnb JSON data on page {}", page_number);
        if let Ok(page_source) = driver.source().await {
            if let Some(json_data) = extract_airbnb_json_data(&page_source) {
                let airbnb_urls = extract_all_listing_urls_from_json(&json_data);
                let mut json_extracted = 0;
                for url in airbnb_urls {
                    if urls.insert(url.clone()) {
                        json_extracted += 1;
                        debug!("Airbnb JSON added URL: {}", url);
                    }
                }
                if json_extracted > 0 {
                    info!("Airbnb JSON extracted {} URLs on page {}", json_extracted, page_number);
                    places_found = true;
                }
            } else {
                // Fallback to JSON-LD if Airbnb JSON not found
                debug!("Airbnb JSON not found, trying JSON-LD");
                if let Some(json_ld_urls) = extract_listing_urls_from_json_ld(&page_source) {
                    let mut json_ld_extracted = 0;
                    for url in json_ld_urls {
                        if urls.insert(url.clone()) {
                            json_ld_extracted += 1;
                            debug!("JSON-LD added URL: {}", url);
                        }
                    }
                    if json_ld_extracted > 0 {
                        info!("JSON-LD extracted {} URLs on page {}", json_ld_extracted, page_number);
                        places_found = true;
                    }
                }
            }
        }

        // Take a screenshot for debugging and ensure logs directory exists
        if let Err(_) = std::fs::create_dir_all("logs") {
            debug!("Could not create logs directory, skipping screenshots");
        } else if let Ok(screenshot) = driver.screenshot_as_png().await {
            let screenshot_path = format!("logs/page_{}_screenshot_{}.png", page_number, chrono::Utc::now().timestamp());
            if let Err(e) = std::fs::write(&screenshot_path, screenshot) {
                warn!("Failed to save screenshot: {}", e);
            } else {
                debug!("Saved screenshot to: {}", screenshot_path);
            }
        }

        // Try multiple selectors as fallbacks, prioritizing most reliable ones
        let selectors_to_try = [
            "l1ovpqvx",        // Primary selector from user example - highest priority
            "c1w4n3ae",        // Secondary selector
            "bewl01v",         // Third option
            "b1kg238b",        // From user's example
            "g1hysso5",        // Additional alternative
            "c965t3n",         // Additional selector
            "a3g92ry",         // Additional selector
            "atm_7l_1j28jx2",  // Legacy fallback
            "lr88w8j",         // Legacy fallback
        ];

        if !places_found {
            info!("Trying {} different selectors to find listings on page {}", selectors_to_try.len(), page_number);
            
            // Try each selector until we find listings
            for (i, selector) in selectors_to_try.iter().enumerate() {
                debug!("Attempt {}/{} - Trying selector: '{}'", i + 1, selectors_to_try.len(), selector);
                match wait_for_elements(driver, By::ClassName(*selector), 5).await {
                    Ok(places) => {
                        info!("SUCCESS: Found {} listing elements with selector '{}' on page {}", places.len(), selector, page_number);
                        places_found = true;

                        let mut tasks: FuturesUnordered<_> = places.into_iter().enumerate().map(|(place_idx, place)| {
                        let permit = semaphore.clone().acquire_owned();
                        async move {
                            let _permit = permit.await?;
                            debug!("Processing place element {}", place_idx);
                            
                            // Wait for element to be fully loaded with href
                            for retry in 0..3 {
                                match place.attr("href").await {
                                    Ok(Some(href)) if !href.is_empty() => {
                                        debug!("Place {} href: {}", place_idx, href);
                                        if href.contains("/rooms/") {
                                            let full_url = construct_airbnb_url(&href);
                                            info!("Found valid room URL {}: {}", place_idx, full_url);
                                            return Ok::<Option<String>, anyhow::Error>(Some(full_url));
                                        } else if href.contains("/homes/") {
                                            let full_url = construct_airbnb_url(&href);
                                            info!("Found valid home URL {}: {}", place_idx, full_url);
                                            return Ok::<Option<String>, anyhow::Error>(Some(full_url));
                                        } else {
                                            debug!("Place {} href doesn't contain '/rooms/' or '/homes/': {}", place_idx, href);
                                            return Ok::<Option<String>, anyhow::Error>(None);
                                        }
                                    },
                                    Ok(Some(_)) => {
                                        debug!("Place {} has empty href, retrying {} more times", place_idx, 2 - retry);
                                    },
                                    Ok(None) => {
                                        debug!("Place {} has no href attribute, retrying {} more times", place_idx, 2 - retry);
                                    },
                                    Err(e) => {
                                        debug!("Failed to get href for place {} (attempt {}): {}", place_idx, retry + 1, e);
                                    }
                                }
                                
                                if retry < 2 {
                                    sleep(Duration::from_millis(500)).await;
                                }
                            }
                            
                            debug!("Could not get valid href for place {} after 3 attempts", place_idx);
                            Ok::<Option<String>, anyhow::Error>(None)
                        }
                    }).collect();

                    let mut extracted_urls = 0;
                    while let Some(result) = tasks.next().await {
                        match result {
                            Ok(Some(url)) => {
                                if urls.insert(url.clone()) {
                                    extracted_urls += 1;
                                    debug!("Added new URL (total: {}): {}", urls.len(), url);
                                } else {
                                    debug!("Duplicate URL ignored: {}", url);
                                }
                            },
                            Ok(None) => {
                                debug!("No URL extracted from this element");
                            },
                            Err(e) => {
                                warn!("Error processing place element: {}", e);
                            }
                        }
                    }
                    info!("Extracted {} new URLs with selector '{}' on page {}", extracted_urls, selector, page_number);
                    break; // Found listings with this selector, no need to try others
                }
                Err(e) => {
                    debug!("Selector '{}' failed: {}", selector, e);
                }
            }
        }
        }
        
        // If class selectors failed, try multiple XPath strategies as fallback
        if !places_found {
            warn!("All class selectors failed on page {}, trying XPath fallbacks", page_number);
            
            // Try multiple XPath strategies with better patterns
            let xpath_selectors = [
                "//a[contains(@href, '/rooms/') and contains(@class, 'l1ovpqvx')]", // Primary pattern from user example
                "//a[contains(@href, '/rooms/')]",                           // Direct room links
                "//a[contains(@href, '/homes/')]",                           // Direct home links  
                "//a[contains(@href, '/rooms/') or contains(@href, '/homes/')]", // Both patterns
                "//a[contains(@class, 'l1ovpqvx') and @href]",               // Links with primary class
                "//a[contains(@class, 'c1w4n3ae') and @href]",               // Links with secondary class
                "//a[contains(@class, 'bewl01v') and @href]",                // Links with tertiary class
                "//*[@data-testid and contains(@data-testid, 'listing')]//a", // Any listing testid
            ];
            
            for (i, xpath) in xpath_selectors.iter().enumerate() {
                debug!("Trying XPath {}/{}: {}", i + 1, xpath_selectors.len(), xpath);
                match driver.find_all(By::XPath(*xpath)).await {
                    Ok(places) if !places.is_empty() => {
                        info!("XPath '{}' found {} elements on page {}", xpath, places.len(), page_number);
                        
                        let mut tasks: FuturesUnordered<_> = places.into_iter().enumerate().map(|(place_idx, place)| {
                            let permit = semaphore.clone().acquire_owned();
                            async move {
                                let _permit = permit.await?;
                                debug!("XPath place {} processing", place_idx);
                                // Wait for href to be populated
                                for retry in 0..3 {
                                    if let Ok(Some(href)) = place.attr("href").await {
                                        if !href.trim().is_empty() {
                                            let full_url = construct_airbnb_url(&href);
                                            debug!("XPath found URL {}: {}", place_idx, full_url);
                                            // Filter to ensure we only get actual listing URLs
                                            if href.contains("/rooms/") || href.contains("/homes/") {
                                                return Ok::<Option<String>, anyhow::Error>(Some(full_url));
                                            } else {
                                                debug!("XPath place {} href not a listing: {}", place_idx, href);
                                                return Ok::<Option<String>, anyhow::Error>(None);
                                            }
                                        }
                                    }
                                    if retry < 2 {
                                        sleep(Duration::from_millis(300)).await;
                                    }
                                }
                                debug!("XPath place {} has no valid href after retries", place_idx);
                                Ok::<Option<String>, anyhow::Error>(None)
                            }
                        }).collect();

                        let mut xpath_extracted = 0;
                        while let Some(result) = tasks.next().await {
                            if let Ok(Some(url)) = result {
                                if urls.insert(url.clone()) {
                                    xpath_extracted += 1;
                                    debug!("XPath added URL: {}", url);
                                }
                            }
                        }
                        info!("XPath '{}' extracted {} new URLs on page {}", xpath, xpath_extracted, page_number);
                        if xpath_extracted > 0 {
                            places_found = true;
                            break; // Found listings, no need to try more XPath selectors
                        }
                    },
                    Ok(_) => {
                        debug!("XPath '{}' found 0 matching elements on page {}", xpath, page_number);
                    },
                    Err(e) => {
                        debug!("XPath '{}' failed on page {}: {}", xpath, page_number, e);
                    }
                }
            }
        }

        let new_listings = urls.len() - listings_before;
        info!("Page {} summary: found {} new listings (total: {})", page_number, new_listings, urls.len());

        if !places_found {
            warn!("No listings found with DOM selectors on page {}, trying page source regex extraction", page_number);
            // Enhanced page source analysis and debugging
            if let Ok(page_source) = driver.source().await {
                // Check if page contains expected Airbnb content
                let has_airbnb_content = page_source.contains("data-deferred-state-0") || 
                                        page_source.contains("searchResults") ||
                                        page_source.contains("Room in") ||
                                        page_source.contains("Apartment in");
                
                if !has_airbnb_content {
                    error!("Page {} doesn't appear to contain Airbnb listing content", page_number);
                    // Save page source for debugging bot detection issues
                    if let Err(_) = std::fs::create_dir_all("logs") {
                        debug!("Could not create logs directory");
                    } else {
                        let source_path = format!("logs/page_{}_bot_detected_{}.html", page_number, chrono::Utc::now().timestamp());
                        if let Err(e) = std::fs::write(&source_path, &page_source) {
                            warn!("Failed to save page source: {}", e);
                        } else {
                            info!("Saved page source for bot detection analysis: {}", source_path);
                        }
                    }
                }
                
                // Try regex extraction as last resort
                let regex_urls = extract_listing_urls_from_source(&page_source);
                let mut regex_extracted = 0;
                for url in regex_urls {
                    if urls.insert(url.clone()) {
                        regex_extracted += 1;
                        debug!("Regex extracted URL: {}", url);
                    }
                }
                if regex_extracted > 0 {
                    info!("Source regex extracted {} URLs on page {}", regex_extracted, page_number);
                    places_found = true;
                }
                
                if !places_found {
                    error!("No listings found on page {} with any method", page_number);
                    warn!("Page source length: {} chars, contains room links: {}, contains homes links: {}", 
                          page_source.len(), 
                          page_source.contains("/rooms/"),
                          page_source.contains("/homes/"));
                    
                    // Save page source for debugging
                    if let Err(_) = std::fs::create_dir_all("logs") {
                        debug!("Could not create logs directory");
                    } else {
                        let source_path = format!("logs/page_{}_no_listings_{}.html", page_number, chrono::Utc::now().timestamp());
                        if let Err(e) = std::fs::write(&source_path, page_source) {
                            warn!("Failed to save page source: {}", e);
                        } else {
                            info!("Saved page source for debugging: {}", source_path);
                        }
                    }
                }
            }
        }

        // Try to navigate to next page
        info!("Looking for next page button on page {}", page_number);
        
        // Multiple strategies to find next button
        let next_button_selectors = [
            "//a[@aria-label='Next']",
            "//a[@aria-label='Next page']", 
            "//button[@aria-label='Next']",
            "//a[contains(@class, 'next')]",
            "//button[contains(@class, 'next')]",
            "//a[contains(text(), 'Next')]",
        ];
        
        let mut found_next_button = false;
        for selector in next_button_selectors {
            match driver.find(By::XPath(selector)).await {
                Ok(next_button) => {
                    info!("Found next button on page {} with selector: {}", page_number, selector);
                    match next_button.is_clickable().await {
                        Ok(true) => {
                            info!("Next button is clickable, navigating to page {}", page_number + 1);
                            
                            // Scroll to button first
                            let _ = driver.execute(
                                "arguments[0].scrollIntoView({behavior: 'smooth', block: 'center'});",
                                vec![next_button.to_json()?]
                            ).await;
                            
                            sleep(Duration::from_millis(fastrand::u64(1000..2000))).await;
                            
                            if let Err(e) = next_button.click().await {
                                warn!("Failed to click next button: {}", e);
                                continue; // Try next selector
                            }
                            
                            page_number += 1;
                            found_next_button = true;
                            
                            // Enhanced wait time after pagination with human behavior
                            info!("Waiting for page {} to load...", page_number);
                            sleep(Duration::from_secs(8)).await;
                            
                            // Simulate user looking at new page
                            human_scroll(driver).await?;
                            sleep(Duration::from_secs(2)).await;
                            
                            break;
                        },
                        Ok(false) => {
                            debug!("Next button with selector '{}' not clickable on page {}", selector, page_number);
                            continue; // Try next selector
                        },
                        Err(e) => {
                            debug!("Failed to check if next button is clickable with selector '{}': {}", selector, e);
                            continue; // Try next selector
                        }
                    }
                }
                Err(_) => {
                    debug!("Next button not found with selector '{}' on page {}", selector, page_number);
                    continue; // Try next selector
                }
            }
        }
        
        if !found_next_button {
            info!("No clickable next button found on page {} with any selector, ending pagination", page_number);
            break;
        }
    }

    info!("URL collection completed. Total URLs collected: {}", urls.len());
    let url_list: Vec<String> = urls.into_iter().collect();
    info!("Returning {} unique URLs", url_list.len());
    
    // Log first few URLs for verification
    for (i, url) in url_list.iter().take(5).enumerate() {
        debug!("URL {}: {}", i + 1, url);
    }
    if url_list.len() > 5 {
        info!("... and {} more URLs", url_list.len() - 5);
    }

    if let Err(e) = send_email().await {
        error!("Failed to send completion email: {}", e);
    }
    
    Ok(url_list)
}

// Send email once scraper completes. All configuration comes from environment
// variables so the binary ships no credentials. If SMTP_HOST or SMTP_USER is
// missing the function no-ops so development can run without an SMTP setup.
pub async fn send_email() -> Result<()> {
    let host = match std::env::var("SMTP_HOST") {
        Ok(v) if !v.is_empty() => v,
        _ => {
            tracing::info!("SMTP_HOST not set; skipping completion email");
            return Ok(());
        }
    };
    let port: u16 = std::env::var("SMTP_PORT")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(587);
    let implicit_tls = std::env::var("SMTP_IMPLICIT_TLS")
        .ok()
        .map(|v| v == "1" || v.eq_ignore_ascii_case("true"))
        .unwrap_or(port == 465);

    let user = std::env::var("SMTP_USER").unwrap_or_default();
    let password = std::env::var("SMTP_PASSWORD").unwrap_or_default();
    if user.is_empty() || password.is_empty() {
        tracing::info!("SMTP credentials not set; skipping completion email");
        return Ok(());
    }

    let from_address = std::env::var("SMTP_FROM").unwrap_or_else(|_| user.clone());
    let to_addresses: Vec<(String, String)> = std::env::var("SMTP_TO")
        .unwrap_or_default()
        .split(',')
        .filter_map(|raw| {
            let trimmed = raw.trim();
            if trimmed.is_empty() {
                None
            } else {
                Some(("Recipient".to_string(), trimmed.to_string()))
            }
        })
        .collect();
    if to_addresses.is_empty() {
        tracing::info!("SMTP_TO not set; skipping completion email");
        return Ok(());
    }

    let to_refs: Vec<(&str, &str)> = to_addresses
        .iter()
        .map(|(n, a)| (n.as_str(), a.as_str()))
        .collect();

    let message = MessageBuilder::new()
        .from(("Automation", from_address.as_str()))
        .to(to_refs)
        .subject("Scraping Complete")
        .html_body("<h1>Scraping has completed</h1>")
        .text_body("Scraping has completed, you should now see new listing available.");

    tracing::info!("Connecting to SMTP {}:{} (implicit_tls: {})", host, port, implicit_tls);

    let mut client = SmtpClientBuilder::new(host.as_str(), port)
        .implicit_tls(implicit_tls)
        .credentials((user.as_str(), password.as_str()))
        .connect()
        .await?;

    client.send(message).await.map_err(|e| e.into())
}

// Rest of the helper functions remain unchanged
async fn close_modal(driver: &WebDriver) -> Result<()> {
    if let Ok(close_button) = driver.find(By::XPath("//button[@aria-label='Close']")).await {
        if close_button.is_clickable().await? {
            close_button.click().await?;
            sleep(Duration::from_secs(1)).await;
            log::info!("Successfully closed modal");
        }
    }
    Ok(())
}

async fn click_show_all_amenities(driver: &WebDriver) -> Result<bool> {
    close_modal(driver).await?;

    // Try multiple strategies to find the "Show all amenities" button
    let button_selectors = [
        // Primary: XPath for button containing the text
        By::XPath("//button[contains(., 'Show all') and contains(., 'amenities')]"),
        // Fallback: span inside button with the text
        By::XPath("//button//span[contains(text(), 'Show all') and contains(text(), 'amenities')]/.."),
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
    driver.execute(
        "arguments[0].scrollIntoView({block: 'center'});",
        vec![button.to_json()?],
    ).await?;

    sleep(Duration::from_millis(500)).await;

    driver.execute(
        "arguments[0].click();",
        vec![button.to_json()?],
    ).await?;

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

async fn scrape_features(driver: &WebDriver) -> Result<Vec<String>> {
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
                    log::info!("Found {} amenity elements with selector: {:?}", elements.len(), selector);
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
            if let Ok(result) = driver.execute(
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
            ).await {
                if let Some(arr) = result.json().as_array() {
                    for item in arr {
                        if let Some(s) = item.as_str() {
                            raw_features.push(s.to_string());
                        }
                    }
                    log::info!("JavaScript extraction found {} potential amenities", raw_features.len());
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

async fn scrape_elements_parallel(elements: Vec<WebElement>) -> Result<Vec<String>> {
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

async fn scrape_house_details(driver: &WebDriver) -> Result<Vec<String>> {
    let elements = driver.find_all(By::ClassName("l7n4lsf")).await?;
    let details_futures = elements.into_iter().map(|element| async move {
        element.text().await
    });

    let details = futures::future::join_all(details_futures)
        .await
        .into_iter()
        .filter_map(|r| r.ok())
        .collect();

    log::info!("Scraped the details about the AirBnB");
    Ok(details)
}

async fn get_text_or_empty(driver: &WebDriver, by: By) -> Result<String> {
    match driver.find(by).await {
        Ok(element) => {
            Ok(element.text().await.unwrap_or_else(|_| String::new()))
        }
        Err(_) => Ok(String::new()),
    }
}


// Extract data from page source using regex patterns
fn extract_title_from_source(html: &str) -> Option<String> {
    // Try JSON-LD first
    if let Some(json_ld) = extract_json_ld(html) {
        if let Some(name) = json_ld.get("name").and_then(|v| v.as_str()) {
            if !name.is_empty() && name != "\"" {
                return Some(name.to_string());
            }
        }
    }
    
    // Try meta og:title
    if let Some(title) = extract_meta_content(html, "og:title") {
        return Some(title);
    }
    
    // Try page title
    if let Some(captures) = regex::Regex::new(r"<title>([^<]+)</title>").unwrap().captures(html) {
        if let Some(title) = captures.get(1) {
            let title_text = title.as_str().trim();
            // Extract just the property name part before " - Apartments"
            if let Some(pos) = title_text.find(" - Apartments") {
                return Some(title_text[..pos].trim_matches('"').to_string());
            }
            return Some(title_text.to_string());
        }
    }
    
    None
}

fn extract_picture_url_from_source(html: &str) -> Option<String> {
    // Try JSON-LD first
    if let Some(json_ld) = extract_json_ld(html) {
        if let Some(images) = json_ld.get("image").and_then(|v| v.as_array()) {
            if let Some(first_image) = images.first().and_then(|v| v.as_str()) {
                return Some(first_image.to_string());
            }
        }
    }
    
    // Try meta og:image
    if let Some(image_url) = extract_meta_content(html, "og:image") {
        return Some(image_url);
    }
    
    // Try meta twitter:image
    if let Some(image_url) = extract_meta_content(html, "twitter:image") {
        return Some(image_url);
    }
    
    None
}

/// Safely truncate a string to approximately `max_chars` characters.
/// This avoids panics from slicing in the middle of multi-byte UTF-8 characters.
fn safe_truncate(s: &str, max_chars: usize) -> &str {
    if s.len() <= max_chars {
        return s;
    }
    // Find the last valid char boundary at or before max_chars
    let mut end = max_chars;
    while end > 0 && !s.is_char_boundary(end) {
        end -= 1;
    }
    &s[..end]
}

fn extract_description_from_source(html: &str) -> Option<String> {
    use tracing::debug;

    // Try JSON-LD first
    if let Some(json_ld) = extract_json_ld(html) {
        if let Some(description) = json_ld.get("description").and_then(|v| v.as_str()) {
            let trimmed = description.trim();
            // Validate description is meaningful
            if !trimmed.is_empty() && trimmed != "null" && trimmed != "undefined" && trimmed.len() >= 10 {
                debug!("Found valid description in JSON-LD: '{}'", safe_truncate(trimmed, 100));
                return Some(trimmed.to_string());
            }
        }
    }
    
    // Try meta description
    if let Some(description) = extract_meta_content(html, "description") {
        let trimmed = description.trim();
        // Validate description is meaningful
        if !trimmed.is_empty() && trimmed != "null" && trimmed != "undefined" && trimmed.len() >= 10 {
            debug!("Found valid description in meta: '{}'", safe_truncate(trimmed, 100));
            
            // Extract just the description part, remove the date prefix
            if let Some(pos) = trimmed.find(" - ") {
                let desc_part = &trimmed[pos + 3..];
                if let Some(dot_pos) = desc_part.find(". ") {
                    let clean_desc = desc_part[dot_pos + 2..].trim().to_string();
                    if clean_desc.len() >= 10 {
                        debug!("Cleaned description: '{}'", safe_truncate(&clean_desc, 100));
                        return Some(clean_desc);
                    }
                }
            }
            return Some(trimmed.to_string());
        }
    }
    
    debug!("No valid description found (empty, null, or too short)");
    None
}

fn extract_price_from_source(html: &str) -> Option<String> {
    use tracing::{debug, info, warn};

    // Try Airbnb's data-deferred-state-0 JSON first (most reliable for individual listing pages)
    if let Some(price) = extract_price_from_airbnb_json(html) {
        info!("Found price from Airbnb JSON: {}", price);
        return Some(price);
    }

    // Try JSON-LD structured data
    if let Some(json_ld) = extract_json_ld(html) {
        // Look for offers or priceSpecification in JSON-LD
        if let Some(offers) = json_ld.get("offers") {
            if let Some(price) = offers.get("price").and_then(|p| p.as_str()) {
                if let Ok(price_val) = price.parse::<f64>() {
                    if price_val >= 10.0 && price_val <= 10000.0 {
                        debug!("Found price in JSON-LD offers: ${}", price_val);
                        return Some(format!("${}", price_val));
                    }
                }
            }
        }

        // Check for other price fields in JSON-LD
        if let Some(price_range) = json_ld.get("priceRange").and_then(|p| p.as_str()) {
            let price_patterns = [
                r"\$(\d+)",
                r"(\d+)",
            ];
            for pattern in price_patterns {
                if let Ok(regex) = regex::Regex::new(pattern) {
                    if let Some(captures) = regex.captures(price_range) {
                        if let Some(price_match) = captures.get(1) {
                            if let Ok(price_val) = price_match.as_str().parse::<u32>() {
                                if price_val >= 10 && price_val <= 10000 {
                                    debug!("Found price in JSON-LD priceRange: ${}", price_val);
                                    return Some(format!("${}", price_val));
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    // Try meta description - contains "for $199" pattern
    if let Some(description) = extract_meta_content(html, "description") {
        info!("Found meta description for price: '{}'", description);
        debug!("Meta description length: {} chars", description.len());

        // Try multiple price patterns with validation
        let price_patterns = [
            r"for \$(\d+)",              // "for $199"
            r"unit for \$(\d+)",         // "Entire rental unit for $115"
            r"\$(\d+)\b",                // "$199" (word boundary to avoid partial matches)
            r"(\d+) per night",          // "199 per night"
            r"(\d+)/night",              // "199/night"
            r"Starting at \$(\d+)",      // "Starting at $199"
            r"from \$(\d+)",             // "from $199"
        ];

        for (i, pattern) in price_patterns.iter().enumerate() {
            info!("Trying price pattern {}: '{}'", i + 1, pattern);
            if let Ok(regex) = regex::Regex::new(pattern) {
                if let Some(captures) = regex.captures(&description) {
                    info!("Pattern '{}' matched! Captures: {:?}", pattern, captures);
                    if let Some(price) = captures.get(1) {
                        let price_num = price.as_str();
                        info!("Extracted price number: '{}'", price_num);
                        // Validate price is reasonable (between $10 and $10000 per night)
                        if let Ok(price_val) = price_num.parse::<u32>() {
                            info!("Parsed price value: {}", price_val);
                            if price_val >= 10 && price_val <= 10000 {
                                let price_str = format!("${}", price_num);
                                info!("Found valid price with pattern '{}': '{}'", pattern, price_str);
                                return Some(price_str);
                            } else {
                                warn!("Price {} outside reasonable range (10-10000)", price_val);
                            }
                        } else {
                            warn!("Failed to parse price '{}' as number", price_num);
                        }
                    } else {
                        warn!("Pattern matched but no capture group 1 found");
                    }
                } else {
                    debug!("Pattern '{}' did not match", pattern);
                }
            } else {
                warn!("Failed to compile regex pattern: '{}'", pattern);
            }
        }

        warn!("No valid price pattern matched in meta description");
    } else {
        warn!("No meta description found in HTML for price extraction");
    }

    info!("No valid price found in any source");
    None
}

/// Extract price from Airbnb's data-deferred-state-0 JSON structure
/// This is the primary source for pricing on individual listing pages
fn extract_price_from_airbnb_json(html: &str) -> Option<String> {
    use tracing::{debug, info};

    let json_data = extract_airbnb_json_data(html)?;

    // Debug: Save JSON to file for analysis (first time only)
    static SAVED: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);
    if !SAVED.swap(true, std::sync::atomic::Ordering::SeqCst) {
        if let Ok(json_str) = serde_json::to_string_pretty(&json_data) {
            let _ = std::fs::create_dir_all("logs");
            let path = format!("logs/airbnb_json_debug_{}.json", chrono::Utc::now().timestamp());
            if std::fs::write(&path, &json_str).is_ok() {
                info!("Saved Airbnb JSON to {} for debugging ({} bytes)", path, json_str.len());
            }
        }
    }

    // Helper function to recursively search for price fields in JSON
    fn find_price_in_json(value: &serde_json::Value, depth: usize) -> Option<String> {
        if depth > 20 {
            return None; // Prevent infinite recursion
        }

        match value {
            serde_json::Value::Object(map) => {
                // Check for common price field names
                let price_fields = [
                    "price", "priceString", "discountedPrice", "originalPrice",
                    "displayPrice", "formattedPrice", "priceForDisplay", "total",
                    "nightlyPrice", "basePrice", "priceLabel", "amount",
                ];

                for field in price_fields {
                    if let Some(price_val) = map.get(field) {
                        if let Some(price_str) = price_val.as_str() {
                            // Check if it looks like a price (contains $ or a number)
                            if price_str.contains('$') || price_str.chars().any(|c| c.is_ascii_digit()) {
                                let cleaned = price_str.trim();
                                if !cleaned.is_empty() && cleaned != "$0" {
                                    return Some(cleaned.to_string());
                                }
                            }
                        } else if let Some(num) = price_val.as_f64() {
                            if num >= 10.0 && num <= 50000.0 {
                                return Some(format!("${:.0}", num));
                            }
                        } else if let Some(num) = price_val.as_i64() {
                            if num >= 10 && num <= 50000 {
                                return Some(format!("${}", num));
                            }
                        }
                    }
                }

                // Look for structuredDisplayPrice which Airbnb commonly uses
                if let Some(structured) = map.get("structuredDisplayPrice") {
                    if let Some(primary) = structured.get("primaryLine") {
                        if let Some(price) = primary.get("price").and_then(|p| p.as_str()) {
                            if !price.is_empty() {
                                return Some(price.to_string());
                            }
                        }
                        if let Some(price) = primary.get("discountedPrice").and_then(|p| p.as_str()) {
                            if !price.is_empty() {
                                return Some(price.to_string());
                            }
                        }
                        if let Some(access_label) = primary.get("accessibilityLabel").and_then(|p| p.as_str()) {
                            // Try to extract price from accessibility label like "$150 per night"
                            if let Some(price) = extract_price_from_text(access_label) {
                                return Some(price);
                            }
                        }
                    }
                }

                // Look for bookItPrice
                if let Some(book_it) = map.get("bookItPrice") {
                    if let Some(price) = book_it.get("price").and_then(|p| p.as_str()) {
                        if !price.is_empty() {
                            return Some(price.to_string());
                        }
                    }
                }

                // Look in sections array for pricing section
                if let Some(sections) = map.get("sections").and_then(|s| s.as_array()) {
                    for section in sections {
                        if let Some(section_data) = section.get("section") {
                            if let Some(price) = find_price_in_json(section_data, depth + 1) {
                                return Some(price);
                            }
                        }
                    }
                }

                // Look in sectionArray
                if let Some(section_array) = map.get("sectionArray").and_then(|s| s.as_array()) {
                    for section in section_array {
                        if let Some(section_data) = section.get("section") {
                            if let Some(price) = find_price_in_json(section_data, depth + 1) {
                                return Some(price);
                            }
                        }
                    }
                }

                // Recursively search in nested objects
                for (_key, val) in map {
                    if let Some(price) = find_price_in_json(val, depth + 1) {
                        return Some(price);
                    }
                }
            }
            serde_json::Value::Array(arr) => {
                for item in arr {
                    if let Some(price) = find_price_in_json(item, depth + 1) {
                        return Some(price);
                    }
                }
            }
            _ => {}
        }

        None
    }

    // Try to navigate to known Airbnb JSON paths for individual listing pages
    // Path 1: niobeClientData -> various indices for PDP (Product Detail Page) data
    if let Some(niobe_data) = json_data.get("niobeClientData").and_then(|d| d.as_array()) {
        for entry in niobe_data {
            // Check for PdpFramework data
            if let Some(data) = entry.get("data") {
                // Try stayProductDetailPage path
                if let Some(pdp) = data.get("presentation")
                    .and_then(|p| p.get("stayProductDetailPage")) {

                    debug!("Found stayProductDetailPage, searching for price...");

                    // Look in sections
                    if let Some(sections) = pdp.get("sections") {
                        if let Some(price) = find_price_in_json(sections, 0) {
                            info!("Found price in stayProductDetailPage sections: {}", price);
                            return Some(price);
                        }
                    }

                    // Search entire pdp structure
                    if let Some(price) = find_price_in_json(pdp, 0) {
                        info!("Found price in stayProductDetailPage: {}", price);
                        return Some(price);
                    }
                }

                // Try other potential paths
                if let Some(price) = find_price_in_json(data, 0) {
                    info!("Found price in niobeClientData entry: {}", price);
                    return Some(price);
                }
            }
        }
    }

    // Fallback: search entire JSON structure
    debug!("Searching entire JSON structure for price...");
    if let Some(price) = find_price_in_json(&json_data, 0) {
        info!("Found price via full JSON search: {}", price);
        return Some(price);
    }

    debug!("No price found in Airbnb JSON");
    None
}

/// Extract price from text using regex patterns
fn extract_price_from_text(text: &str) -> Option<String> {
    let price_patterns = [
        r"\$(\d+(?:,\d{3})*(?:\.\d{2})?)",  // $150, $1,500, $150.00
        r"(\d+(?:,\d{3})*(?:\.\d{2})?) per night",  // 150 per night
        r"(\d+(?:,\d{3})*(?:\.\d{2})?)/night",  // 150/night
    ];

    for pattern in price_patterns {
        if let Ok(regex) = regex::Regex::new(pattern) {
            if let Some(captures) = regex.captures(text) {
                if let Some(price_match) = captures.get(1) {
                    let price_str = price_match.as_str().replace(',', "");
                    if let Ok(price_val) = price_str.parse::<f64>() {
                        if price_val >= 10.0 && price_val <= 50000.0 {
                            return Some(format!("${}", price_match.as_str()));
                        }
                    }
                }
            }
        }
    }

    None
}

fn extract_rating_from_source(html: &str) -> Option<String> {
    // Try JSON-LD aggregateRating
    if let Some(json_ld) = extract_json_ld(html) {
        if let Some(rating_obj) = json_ld.get("aggregateRating") {
            if let Some(rating_value) = rating_obj.get("ratingValue").and_then(|v| v.as_f64()) {
                return Some(rating_value.to_string());
            }
        }
    }
    
    // Try meta og:title which contains "★4.89"
    if let Some(title) = extract_meta_content(html, "og:title") {
        if let Some(captures) = regex::Regex::new(r"★(\d+\.\d+)").unwrap().captures(&title) {
            if let Some(rating) = captures.get(1) {
                return Some(rating.as_str().to_string());
            }
        }
    }
    
    None
}

fn extract_location_from_source(html: &str) -> Option<String> {
    // Try JSON-LD address
    if let Some(json_ld) = extract_json_ld(html) {
        if let Some(address) = json_ld.get("address") {
            if let Some(locality) = address.get("addressLocality").and_then(|v| v.as_str()) {
                return Some(locality.to_string());
            }
        }
    }
    
    // Try meta og:title which contains location info
    if let Some(title) = extract_meta_content(html, "og:title") {
        if let Some(pos) = title.find(" in ") {
            let location_part = &title[pos + 4..];
            if let Some(end_pos) = location_part.find(" ·") {
                return Some(location_part[..end_pos].to_string());
            }
        }
    }
    
    None
}

async fn extract_description_from_dom(driver: &WebDriver) -> Result<String> {
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
                    debug!("Found description with selector '{}': '{}'", selector, preview);
                    return Ok(trimmed.to_string());
                }
            }
        }
    }

    debug!("No description found with DOM selectors");
    Err(anyhow::anyhow!("No description found"))
}

async fn extract_price_from_dom(driver: &WebDriver) -> Result<String> {
    use tracing::debug;

    // Try multiple selectors for price
    let price_selectors = [
        "span.umg93v9",  // Current Airbnb price span - PRIORITY
        "span[data-testid='price-availability-row-label-price']",
        "[data-testid='price-availability-row'] span",
        "div._tyxjp1",  // Common Airbnb price class
        "span._1p7iugi",  // Another price class
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

fn extract_price_from_title(title: &str) -> Option<String> {
    use tracing::debug;
    
    // Try to extract price from title if it contains price information
    let price_patterns = [
        r"\$(\d+)",           // $199
        r"(\d+) per night",   // 199 per night
        r"(\d+)/night",       // 199/night
        r"from \$(\d+)",      // from $199
    ];
    
    for pattern in price_patterns {
        if let Ok(regex) = regex::Regex::new(pattern) {
            if let Some(captures) = regex.captures(title) {
                if let Some(price) = captures.get(1) {
                    let price_str = format!("${}", price.as_str());
                    debug!("Extracted price from title: '{}'", price_str);
                    return Some(price_str);
                }
            }
        }
    }
    
    debug!("No price found in title: '{}'", title);
    None
}

fn extract_listing_urls_from_source(html: &str) -> Vec<String> {
    use tracing::{debug, info};
    
    let mut urls = Vec::new();
    
    // Try multiple regex patterns to find listing URLs in the page source
    let patterns = [
        r#"href="(/rooms/[^"]+)""#,                    // Direct room links
        r#"href="(/homes/[^"]+)""#,                    // Home links
        r#""url":"([^"]*(?:/rooms/|/homes/)[^"]*)"#,   // JSON url field
        r#""@id":"([^"]*(?:/rooms/|/homes/)[^"]*)"#,   // JSON-LD @id field
        r#"https://www\.airbnb\.com/rooms/[0-9]+"#,    // Full room URLs
        r#"https://www\.airbnb\.com/homes/[0-9]+"#,    // Full home URLs
    ];
    
    for (i, pattern) in patterns.iter().enumerate() {
        if let Ok(regex) = regex::Regex::new(pattern) {
            let matches: Vec<_> = regex.captures_iter(html).collect();
            debug!("Pattern {} found {} matches", i + 1, matches.len());
            
            for captures in matches {
                let url_str = if captures.len() > 1 {
                    captures.get(1).map(|m| m.as_str()).unwrap_or("")
                } else {
                    captures.get(0).map(|m| m.as_str()).unwrap_or("")
                };
                
                if !url_str.is_empty() {
                    let full_url = construct_airbnb_url(url_str);
                    if !urls.contains(&full_url) {
                        urls.push(full_url);
                    }
                }
            }
        }
    }
    
    if !urls.is_empty() {
        info!("Regex extracted {} unique listing URLs from page source", urls.len());
    } else {
        debug!("No listing URLs found in page source using regex");
    }
    
    urls
}

fn extract_listing_urls_from_json_ld(html: &str) -> Option<Vec<String>> {
    use tracing::{debug, info};
    
    // Extract all JSON-LD blocks and look for listing URLs
    let patterns = [
        r#"<script type="application/ld\+json">([^<]+)</script>"#,
        r#"<script type="application/ld\+json">\s*([^<]+)\s*</script>"#,
        r#"<script[^>]*type="application/ld\+json"[^>]*>([^<]+)</script>"#,
    ];
    
    let mut listing_urls = Vec::new();
    
    for pattern in patterns {
        if let Ok(regex) = regex::Regex::new(pattern) {
            for captures in regex.captures_iter(html) {
                if let Some(json_str) = captures.get(1) {
                    let json_text = json_str.as_str().trim();
                    let preview: String = json_text.chars().take(200).collect();
                    debug!("Processing JSON-LD content: {}", preview);

                    match serde_json::from_str::<serde_json::Value>(json_text) {
                        Ok(json_value) => {
                            // Look for listing URLs in various JSON-LD structures
                            extract_urls_from_json_value(&json_value, &mut listing_urls);
                        },
                        Err(e) => {
                            debug!("Failed to parse JSON-LD: {}", e);
                        }
                    }
                }
            }
        }
    }
    
    if !listing_urls.is_empty() {
        info!("Extracted {} listing URLs from JSON-LD", listing_urls.len());
        Some(listing_urls)
    } else {
        debug!("No listing URLs found in JSON-LD");
        None
    }
}

fn extract_urls_from_json_value(value: &serde_json::Value, urls: &mut Vec<String>) {
    use tracing::debug;
    
    match value {
        serde_json::Value::Object(obj) => {
            // Look for URL fields
            if let Some(url_val) = obj.get("url") {
                if let Some(url_str) = url_val.as_str() {
                    if url_str.contains("/rooms/") || url_str.contains("/homes/") {
                        let full_url = construct_airbnb_url(url_str);
                        urls.push(full_url);
                        debug!("Found listing URL in JSON-LD: {}", url_str);
                    }
                }
            }
            
            // Look for @id fields
            if let Some(id_val) = obj.get("@id") {
                if let Some(id_str) = id_val.as_str() {
                    if id_str.contains("/rooms/") || id_str.contains("/homes/") {
                        let full_url = construct_airbnb_url(id_str);
                        urls.push(full_url);
                        debug!("Found listing URL in JSON-LD @id: {}", id_str);
                    }
                }
            }
            
            // Recursively search in all object values
            for (_, v) in obj {
                extract_urls_from_json_value(v, urls);
            }
        },
        serde_json::Value::Array(arr) => {
            // Recursively search in all array elements
            for item in arr {
                extract_urls_from_json_value(item, urls);
            }
        },
        serde_json::Value::String(s) => {
            // Check if string itself is a listing URL
            if s.contains("/rooms/") || s.contains("/homes/") {
                if s.starts_with("http") || s.starts_with("/") {
                    let full_url = construct_airbnb_url(s);
                    urls.push(full_url);
                    debug!("Found listing URL in JSON-LD string: {}", s);
                }
            }
        },
        _ => {} // Ignore other types
    }
}

fn extract_json_ld(html: &str) -> Option<serde_json::Value> {
    use tracing::debug;
    
    // Find JSON-LD script tag - try multiple patterns
    let patterns = [
        r#"<script type="application/ld\+json">([^<]+)</script>"#,
        r#"<script type="application/ld\+json">\s*([^<]+)\s*</script>"#,
        r#"<script[^>]*type="application/ld\+json"[^>]*>([^<]+)</script>"#,
    ];
    
    for pattern in patterns {
        if let Ok(regex) = regex::Regex::new(pattern) {
            if let Some(captures) = regex.captures(html) {
                if let Some(json_str) = captures.get(1) {
                    let json_text = json_str.as_str().trim();
                    // Safely truncate for logging (handle multi-byte UTF-8 chars like emojis)
                    let preview: String = json_text.chars().take(200).collect();
                    debug!("Found JSON-LD content: {}", preview);
                    
                    match serde_json::from_str::<serde_json::Value>(json_text) {
                        Ok(json_value) => {
                            debug!("Successfully parsed JSON-LD");
                            return Some(json_value);
                        },
                        Err(e) => {
                            debug!("Failed to parse JSON-LD: {}", e);
                        }
                    }
                }
            }
        }
    }
    
    debug!("No JSON-LD found in HTML");
    None
}

fn extract_region_from_url(url: &str) -> Option<String> {
    use tracing::debug;
    
    // Try to extract region from URL patterns or referrer
    // Common patterns in Airbnb search URLs or listing URLs
    if let Ok(parsed_url) = url::Url::parse(url) {
        // Check query parameters for location info
        for (key, value) in parsed_url.query_pairs() {
            if key == "location" || key == "place_id" || key == "region" {
                let location = value.to_string();
                debug!("Found region in URL parameter '{}': '{}'", key, location);
                
                // Extract city/region name from location string
                let parts: Vec<&str> = location.split(',').collect();
                if !parts.is_empty() {
                    let region = parts[0].trim().to_string();
                    if !region.is_empty() {
                        debug!("Extracted region from URL: '{}'", region);
                        return Some(region);
                    }
                }
            }
        }
        
        // Check if path contains location info
        let path = parsed_url.path();
        if path.contains("/s/") {
            // Pattern like /s/Toronto--ON--Canada/homes
            if let Some(start) = path.find("/s/") {
                let location_part = &path[start + 3..];
                if let Some(end) = location_part.find('/') {
                    let location = &location_part[..end];
                    let parts: Vec<&str> = location.split("--").collect();
                    if !parts.is_empty() {
                        let region = parts[0].replace('-', " ");
                        debug!("Extracted region from URL path: '{}'", region);
                        return Some(region);
                    }
                }
            }
        }
    }
    
    debug!("No region found in URL: '{}'", url);
    None
}

fn extract_region_country_from_source(html: &str) -> (Option<String>, Option<String>) {
    use tracing::debug;
    
    // Try to extract from JSON-LD address
    if let Some(json_ld) = extract_json_ld(html) {
        if let Some(address) = json_ld.get("address") {
            let region = address.get("addressRegion")
                .and_then(|v| v.as_str())
                .filter(|s| !s.trim().is_empty() && *s != "null" && *s != "undefined")
                .map(|s| s.trim().to_string());
            let country = address.get("addressCountry")
                .and_then(|v| v.as_str())
                .filter(|s| !s.trim().is_empty() && *s != "null" && *s != "undefined")
                .map(|s| s.trim().to_string());
            
            if region.is_some() || country.is_some() {
                debug!("Found region/country in JSON-LD: {:?}/{:?}", region, country);
                return (region, country);
            }
        }
    }
    
    // Try to extract from meta og:title which might contain state/country info
    if let Some(title) = extract_meta_content(html, "og:title") {
        // Pattern: "Rental unit in Traverse City · ★4.89 · 1 bedroom · 3 beds · 1 bath"
        // Look for patterns like "City, State" or "City, Country"
        if let Some(pos) = title.find(" in ") {
            let location_part = &title[pos + 4..];
            if let Some(end_pos) = location_part.find(" ·") {
                let full_location = &location_part[..end_pos];
                
                // Split by comma to get city, state/region, country
                let parts: Vec<&str> = full_location.split(',').map(|s| s.trim()).filter(|s| !s.is_empty()).collect();
                
                match parts.len() {
                    2 => {
                        // "City, State" or "City, Country"
                        let region_or_country = parts[1].to_string();
                        // Comprehensive US states and territories list
                        let us_states = [
                            "Alabama", "Alaska", "Arizona", "Arkansas", "California", "Colorado", "Connecticut", "Delaware",
                            "Florida", "Georgia", "Hawaii", "Idaho", "Illinois", "Indiana", "Iowa", "Kansas", "Kentucky",
                            "Louisiana", "Maine", "Maryland", "Massachusetts", "Michigan", "Minnesota", "Mississippi",
                            "Missouri", "Montana", "Nebraska", "Nevada", "New Hampshire", "New Jersey", "New Mexico",
                            "New York", "North Carolina", "North Dakota", "Ohio", "Oklahoma", "Oregon", "Pennsylvania",
                            "Rhode Island", "South Carolina", "South Dakota", "Tennessee", "Texas", "Utah", "Vermont",
                            "Virginia", "Washington", "West Virginia", "Wisconsin", "Wyoming", "DC", "District of Columbia",
                            "AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "ID", "IL", "IN", "IA", "KS",
                            "KY", "LA", "ME", "MD", "MA", "MI", "MN", "MS", "MO", "MT", "NE", "NV", "NH", "NJ", "NM",
                            "NY", "NC", "ND", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VT", "VA", "WA", "WV", "WI", "WY"
                        ];
                        if us_states.contains(&region_or_country.as_str()) {
                            return (Some(region_or_country), Some("United States".to_string()));
                        } else {
                            // Check for common countries
                            let countries = ["Canada", "Mexico", "United Kingdom", "France", "Germany", "Spain", "Italy", "Australia", "Japan"];
                            if countries.contains(&region_or_country.as_str()) {
                                return (None, Some(region_or_country));
                            } else {
                                // Assume it's a region/state
                                return (Some(region_or_country), None);
                            }
                        }
                    },
                    3 => {
                        // "City, State, Country"
                        let region = parts[1].trim();
                        let country = parts[2].trim();
                        if !region.is_empty() && !country.is_empty() {
                            return (Some(region.to_string()), Some(country.to_string()));
                        }
                    },
                    _ => {}
                }
            }
        }
    }
    
    // Try to extract from page title
    if let Some(captures) = regex::Regex::new(r"<title>([^<]+)</title>").unwrap().captures(html) {
        if let Some(title) = captures.get(1) {
            let title_text = title.as_str();
            // Look for "in City, State, Country" pattern
            if title_text.contains("United States") {
                return (None, Some("United States".to_string()));
            }
            // Check for other countries
            let countries = ["Canada", "Mexico", "United Kingdom", "France", "Germany", "Spain", "Italy", "Australia", "Japan"];
            for country in countries {
                if title_text.contains(country) {
                    return (None, Some(country.to_string()));
                }
            }
        }
    }
    
    debug!("No valid region/country found");
    (None, None)
}

fn extract_meta_content(html: &str, property: &str) -> Option<String> {
    // Try property="og:title" format
    let property_pattern = format!(r#"<meta property="{}" content="([^"]+)""#, regex::escape(property));
    if let Some(captures) = regex::Regex::new(&property_pattern).unwrap().captures(html) {
        if let Some(content) = captures.get(1) {
            return Some(content.as_str().replace("&quot;", "\"").replace("&amp;", "&"));
        }
    }
    
    // Try name="description" format
    let name_pattern = format!(r#"<meta name="{}" content="([^"]+)""#, regex::escape(property));
    if let Some(captures) = regex::Regex::new(&name_pattern).unwrap().captures(html) {
        if let Some(content) = captures.get(1) {
            return Some(content.as_str().replace("&quot;", "\"").replace("&amp;", "&"));
        }
    }
    
    None
}

// Extract data from script tag with id="data-deferred-state-0"
fn extract_airbnb_json_data(html: &str) -> Option<serde_json::Value> {
    use tracing::{debug, info};
    
    // Look for the script tag containing JSON data
    let pattern = r#"<script id="data-deferred-state-0"[^>]*type="application/json">([^<]+)</script>"#;
    if let Ok(regex) = regex::Regex::new(pattern) {
        if let Some(captures) = regex.captures(html) {
            if let Some(json_str) = captures.get(1) {
                let json_text = json_str.as_str().trim();
                debug!("Found Airbnb JSON data, length: {} chars", json_text.len());
                
                match serde_json::from_str::<serde_json::Value>(json_text) {
                    Ok(json_value) => {
                        info!("Successfully parsed Airbnb JSON data");
                        return Some(json_value);
                    },
                    Err(e) => {
                        debug!("Failed to parse Airbnb JSON data: {}", e);
                    }
                }
            }
        }
    }
    
    debug!("No Airbnb JSON data found in HTML");
    None
}

// Extract listing data from the Airbnb JSON structure
fn extract_listing_from_json(json_data: &serde_json::Value) -> Option<(String, String, String, String, String, String, Vec<String>)> {
    use tracing::{debug, info};
    
    // Navigate to the search results in the JSON structure
    let search_results = json_data
        .get("niobeClientData")?
        .get(1)?
        .get("data")?
        .get("presentation")?
        .get("staysSearch")?
        .get("results")?
        .get("searchResults")?
        .as_array()?;
    
    if search_results.is_empty() {
        debug!("No search results found in JSON data");
        return None;
    }
    
    // Get the first listing for now (in a full scraper, you'd iterate through all)
    let first_listing = &search_results[0];
    
    // Extract title
    let title = first_listing
        .get("title")?
        .as_str()
        .unwrap_or("").to_string();
    
    // Extract price - try multiple possible price fields
    let price = first_listing
        .get("structuredDisplayPrice")?
        .get("primaryLine")?
        .get("price")
        .and_then(|p| p.as_str())
        .or_else(|| {
            // Try discounted price field
            first_listing
                .get("structuredDisplayPrice")?
                .get("primaryLine")?
                .get("discountedPrice")
                .and_then(|p| p.as_str())
        })
        .filter(|p| !p.trim().is_empty()) // Filter out empty strings
        .map(|p| p.to_string())
        .unwrap_or_else(|| {
            // If no price found in JSON structure, return empty to trigger fallback
            debug!("No price found in JSON structure, will use fallback extraction");
            String::new()
        });
    
    // Extract rating
    let rating = first_listing
        .get("avgRatingLocalized")?
        .as_str()
        .unwrap_or("").to_string();
    
    // Extract location from title (e.g., "Condo in Traverse City")
    let location = if title.contains(" in ") {
        title.split(" in ").nth(1).unwrap_or("").to_string()
    } else {
        "".to_string()
    };
    
    // Extract property name/description
    let description = first_listing
        .get("demandStayListing")?
        .get("description")?
        .get("name")?
        .get("localizedStringWithTranslationPreference")?
        .as_str()
        .unwrap_or("").to_string();
    
    // Extract picture URL
    let picture_url = first_listing
        .get("contextualPictures")?
        .get(0)?
        .get("picture")?
        .as_str()
        .unwrap_or("").to_string();
    
    // Extract features (beds, baths, etc.)
    let mut features = Vec::new();
    
    // Add bed info
    if let Some(bed_info) = first_listing
        .get("structuredContent")?
        .get("primaryLine")?
        .get(0)?
        .get("body")?
        .as_str() {
        features.push(bed_info.to_string());
    }
    
    // Add badges (Superhost, Guest favorite, etc.)
    if let Some(badges) = first_listing.get("badges").and_then(|b| b.as_array()) {
        for badge in badges {
            if let Some(badge_text) = badge.get("text").and_then(|t| t.as_str()) {
                features.push(badge_text.to_string());
            }
        }
    }
    
    // Add payment messages (Free cancellation, etc.)
    if let Some(payment_msgs) = first_listing.get("paymentMessages").and_then(|p| p.as_array()) {
        for msg in payment_msgs {
            if let Some(msg_text) = msg.get("text").and_then(|t| t.as_str()) {
                features.push(msg_text.to_string());
            }
        }
    }
    
    info!("Extracted from JSON: title='{}', price='{}', rating='{}', features={}", 
          title, price, rating, features.len());
    
    Some((title, description, price, rating, location, picture_url, features))
}

// Extract all listing URLs from the Airbnb JSON structure
fn extract_all_listing_urls_from_json(json_data: &serde_json::Value) -> Vec<String> {
    use tracing::{debug, info};
    
    let mut urls = Vec::new();
    
    // Navigate to the search results in the JSON structure
    if let Some(search_results) = json_data
        .get("niobeClientData")
        .and_then(|d| d.get(1))
        .and_then(|d| d.get("data"))
        .and_then(|d| d.get("presentation"))
        .and_then(|d| d.get("staysSearch"))
        .and_then(|d| d.get("results"))
        .and_then(|d| d.get("searchResults"))
        .and_then(|d| d.as_array()) {
        
        info!("Found {} listings in JSON data", search_results.len());
        
        for (i, listing) in search_results.iter().enumerate() {
            // Try to extract the listing ID from various possible fields
            if let Some(listing_id) = listing
                .get("demandStayListing")
                .and_then(|l| l.get("id"))
                .and_then(|id| id.as_str()) {
                
                // The ID is base64 encoded, we need to decode it to get the numeric ID
                if let Ok(decoded_bytes) = base64::engine::general_purpose::STANDARD.decode(listing_id) {
                    if let Ok(decoded_str) = std::str::from_utf8(&decoded_bytes) {
                        // Extract numeric ID from decoded string like "DemandStayListing:633486763306530765"
                        if let Some(numeric_id) = decoded_str.split(':').nth(1) {
                            let url = format!("https://www.airbnb.com/rooms/{}", numeric_id);
                            debug!("Extracted URL {}: {} from listing {}", urls.len() + 1, url, i + 1);
                            urls.push(url);
                        } else {
                            debug!("Failed to split decoded string for listing {}: '{}'", i + 1, decoded_str);
                        }
                    } else {
                        debug!("Failed to decode base64 to UTF-8 for listing {}", i + 1);
                    }
                } else {
                    debug!("Failed to decode base64 for listing {}: '{}'", i + 1, listing_id);
                }
            } else {
                debug!("No demandStayListing.id found for listing {}", i + 1);
            }
        }
        
        if !urls.is_empty() {
            info!("Successfully extracted {} listing URLs from JSON", urls.len());
        } else {
            debug!("No valid listing URLs found in JSON structure");
        }
        
    } else {
        debug!("Could not navigate to search results in JSON structure");
    }
    
    urls
}

// Fallback data extraction using the old method
async fn extract_data_fallback(page_source: &str, driver: &WebDriver) -> Result<(String, String, String, String, String, String, Vec<String>)> {
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
    
    Ok((title, description, price, rating, location, picture_url, features))
}


// Keep the original WebDriver-based function as fallback
pub async fn scrape_place_details(driver: &WebDriver, url: &str) -> Result<Listing> {
    use tracing::{info, debug, warn, error, span, Level};
    
    let span = span!(Level::INFO, "scrape_place_details", url = %url);
    let _enter = span.enter();
    
    info!("Starting to scrape place details");
    
    // Configure browser with realistic settings first
    configure_realistic_browser(driver).await?;
    
    // Ensure we have a full URL before navigating
    let full_url = construct_airbnb_url(url);
    debug!("Full URL constructed: {}", full_url);
    
    info!("Navigating to listing page...");
    match driver.goto(&full_url).await {
        Ok(_) => {
            let current_url = driver.current_url().await.map(|u| u.to_string()).unwrap_or_else(|_| "unknown".to_string());
            let current_url_str = current_url.as_str();
            info!("Successfully navigated to listing. Current URL: {}", current_url_str);
            
            // Check if we got redirected to an error page
            if current_url_str.contains("error") || current_url_str.contains("not-found") {
                error!("Redirected to error page: {}", current_url_str);
                return Err(anyhow::anyhow!("Listing not found or error page"));
            }
        },
        Err(e) => {
            error!("Failed to navigate to listing: {}", e);
            return Err(e.into());
        }
    }
    
    // Increase page load wait time
    info!("Waiting 8 seconds for page to load...");
    sleep(Duration::from_secs(8)).await;

    // Add random delay variation to avoid detection
    let random_delay = fastrand::u64(1000..3000);
    debug!("Additional random delay: {}ms", random_delay);
    human_delay(1000, 3000).await;
    
    // Take screenshot for debugging
    if let Ok(screenshot) = driver.screenshot_as_png().await {
        let screenshot_path = format!("logs/listing_screenshot_{}.png", chrono::Utc::now().timestamp());
        if let Err(e) = std::fs::write(&screenshot_path, screenshot) {
            warn!("Failed to save listing screenshot: {}", e);
        } else {
            debug!("Saved listing screenshot: {}", screenshot_path);
        }
    }
    
    // Simulate human behavior
    info!("Simulating human scrolling...");
    human_scroll(driver).await?;

    info!("Extracting listing data...");

    // Wait for JSON-LD to be loaded before extracting page source
    debug!("Waiting for JSON-LD structured data to load...");
    
    // Wait a bit longer for all scripts to load
    human_delay(2000, 4000).await;
    
    // Try to wait for JSON-LD script specifically
    let _ = timeout(Duration::from_secs(10), async {
        loop {
            if let Ok(scripts) = driver.find_all(By::Css("script[type='application/ld+json']")).await {
                if !scripts.is_empty() {
                    debug!("Found {} JSON-LD script(s)", scripts.len());
                    break;
                }
            }
            sleep(Duration::from_millis(500)).await;
        }
    }).await;
    
    // Extract data from page source
    debug!("Getting page source...");
    let page_source = driver.source().await?;
    
    // Try extracting data from Airbnb's JSON first (most reliable)
    debug!("Attempting JSON extraction...");
    let (mut title, mut description, mut price, mut rating, mut location, picture_url, mut features) = 
        if let Some(json_data) = extract_airbnb_json_data(&page_source) {
            if let Some(data) = extract_listing_from_json(&json_data) {
                info!("Successfully extracted data from JSON");
                data
            } else {
                info!("JSON found but data extraction failed, falling back to old method");
                // Fallback to old extraction methods
                extract_data_fallback(&page_source, driver).await?
            }
        } else {
            info!("No JSON data found, using fallback extraction");
            // Fallback to old extraction methods
            extract_data_fallback(&page_source, driver).await?
        };
    
    // Supplement any missing fields with fallback extraction
    debug!("Checking for missing fields and supplementing with fallback extraction");
    
    if price.trim().is_empty() {
        info!("Price is empty, using fallback price extraction");
        
        // Try multiple price extraction methods in order
        if let Some(meta_price) = extract_price_from_source(&page_source) {
            price = meta_price;
            info!("Meta/JSON-LD price extraction succeeded: '{}'", price);
        } else {
            info!("Meta/JSON-LD price extraction failed, trying DOM extraction");
            if let Ok(dom_price) = extract_price_from_dom(driver).await {
                price = dom_price;
                info!("DOM price extraction succeeded: '{}'", price);
            } else {
                info!("DOM price extraction failed, trying title extraction");
                if let Some(title_price) = extract_price_from_title(&title) {
                    price = title_price;
                    info!("Title price extraction succeeded: '{}'", price);
                } else {
                    price = String::new();
                    warn!("All price extraction methods failed!");
                }
            }
        }
    }
    
    if title.trim().is_empty() {
        debug!("Title is empty, using fallback title extraction");
        title = extract_title_from_source(&page_source).unwrap_or_default();
    }
    
    if description.trim().is_empty() {
        debug!("Description is empty, using fallback description extraction");
        description = extract_description_from_source(&page_source).unwrap_or_default();
    }
    
    if rating.trim().is_empty() {
        debug!("Rating is empty, using fallback rating extraction");
        rating = extract_rating_from_source(&page_source).unwrap_or_default();
    }
    
    if location.trim().is_empty() {
        debug!("Location is empty, using fallback location extraction");
        location = extract_location_from_source(&page_source).unwrap_or_default();
    }
    
    info!("Extracted title: '{}'", title);
    info!("Extracted price: '{}'", price);
    debug!("Extracted picture URL: '{}'", picture_url);
    debug!("Extracted rating: '{}'", rating);
    info!("Extracted location: '{}'", location);
    debug!("Extracted description length: {} chars", description.len());

    // Extract region and country
    debug!("Extracting region and country...");
    let (mut region, country) = extract_region_country_from_source(&page_source);
    
    // If region extraction from HTML failed, try to extract from URL
    if region.is_none() || region.as_ref().unwrap().trim().is_empty() {
        if let Some(url_region) = extract_region_from_url(url) {
            info!("Extracted region from URL: '{}'", url_region);
            region = Some(url_region);
        }
    }

    // Extract additional features if not already in JSON data
    info!("Extracting additional features...");
    if features.is_empty() {
        features = scrape_features(driver).await.unwrap_or_default();
    } else {
        // Add more features from DOM if we found some in JSON
        let additional_features = scrape_features(driver).await.unwrap_or_default();
        for feature in additional_features {
            if !features.contains(&feature) {
                features.push(feature);
            }
        }
    }
    info!("Extracted {} total features", features.len());
    for (i, feature) in features.iter().take(5).enumerate() {
        debug!("Feature {}: {}", i + 1, feature);
    }
    if features.len() > 5 {
        debug!("... and {} more features", features.len() - 5);
    }

    // Extract house details with logging
    info!("Extracting house details...");
    let house_details = scrape_house_details(driver).await?;
    info!("Extracted {} house details", house_details.len());
    for (i, detail) in house_details.iter().take(3).enumerate() {
        debug!("House detail {}: {}", i + 1, detail);
    }

    // Ensure we have region/country or default to reasonable values
    let (final_region, final_country) = match (region, country) {
        (Some(r), Some(c)) => (Some(r), Some(c)),
        (Some(r), None) => {
            // If we have region but no country, try to infer country
            if ["AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "ID", "IL", "IN", "IA", "KS",
                "KY", "LA", "ME", "MD", "MA", "MI", "MN", "MS", "MO", "MT", "NE", "NV", "NH", "NJ", "NM",
                "NY", "NC", "ND", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VT", "VA", "WA", "WV", "WI", "WY"].contains(&r.as_str()) {
                (Some(r), Some("United States".to_string()))
            } else {
                (Some(r), None)
            }
        },
        (None, Some(c)) => (None, Some(c)),
        (None, None) => {
            // Try to infer from location
            if !location.is_empty() {
                if location.contains(", CA") || location.contains(", NY") || location.contains(", FL") {
                    (None, Some("United States".to_string()))
                } else {
                    warn!("No region/country found for location: '{}'", location);
                    (None, None)
                }
            } else {
                (None, None)
            }
        }
    };

    // More lenient validation - we have a title which means we found a listing
    if title.is_empty() {
        error!("Listing missing title - title: '{}', description: '{}', price: '{}'", title, description, price);
        
        // Save page source for debugging
        if let Ok(page_source) = driver.source().await {
            let source_path = format!("logs/invalid_listing_source_{}.html", chrono::Utc::now().timestamp());
            if let Err(e) = std::fs::write(&source_path, page_source) {
                warn!("Failed to save page source: {}", e);
            } else {
                info!("Saved page source for debugging: {}", source_path);
            }
        }
        
        return Err(anyhow::anyhow!("Listing missing title: title='{}', description='{}', price='{}'', location='{}''", title, description, price, location));
    }

    // Log warning if description or price is missing but don't fail
    if description.is_empty() {
        warn!("Listing missing description for title: '{}'", title);
    }
    if price.is_empty() {
        warn!("Listing missing price for title: '{}'", title);
    }

    // Parse numeric price
    let price_numeric = price.trim_start_matches('$')
        .replace(',', "")
        .parse::<f64>()
        .ok();

    // Parse numeric rating
    let rating_numeric = rating.parse::<f64>().ok();

    let listing = Listing {
        id: None,
        url: full_url.clone(),
        title: title.clone(),
        picture_url,
        pictures: Vec::new(), // Could be populated from gallery scraping
        description,
        price: price.clone(),
        price_numeric,
        rating,
        rating_numeric,
        reviews_count: None,
        location: location.clone(),
        coordinates: None, // Could be extracted from map data
        features,
        house_details,
        host: None,
        region: final_region,
        country: final_country,
        property_type: None, // Could be extracted from listing details
        created_at: None,
        scraped_at: Some(chrono::Utc::now()),
    };

    info!("Successfully scraped listing: title='{}', location='{}', price='{}'", title, location, price);
    
    Ok(listing)
}

pub async fn scrape_region(driver: &WebDriver, region: &str, country: &str) -> Result<i32> {
    use tracing::{info, warn, error};

    info!("[SCRAPE_REGION] Scraping Airbnb listings for {}, {}", region, country);

    let place_urls = get_place_urls(driver, &format!("{}, {}", region, country), None, None, None).await?;
    if place_urls.is_empty() {
        warn!("No place URLs found for {}, {}", region, country);
        return Ok(0);
    }
    
    info!("[SCRAPE_REGION] Found {} URLs to scrape for {}, {}", place_urls.len(), region, country);
    info!("[SCRAPE_REGION] Using SINGLE WebDriver instance for all URLs");

    let mut region_listings = Vec::new();
    let mut failed_count = 0;

    // Use the SAME driver for all URLs - no need to create new ones!
    for (i, url) in place_urls.iter().enumerate() {
        info!("[SCRAPE_REGION] Scraping URL {}/{}: {}", i + 1, place_urls.len(), url);

        // Check session is still valid before each scrape
        if !check_session_valid(driver).await {
            error!("[SCRAPE_REGION] WebDriver session died before scraping URL {}", i + 1);
            break;
        }

        match scrape_place_details(driver, url).await {
            Ok(mut details) => {
                // Ensure region and country are set, with fallbacks
                if details.region.is_none() || details.region.as_ref().unwrap().trim().is_empty() {
                    details.region = Some(region.to_string());
                }
                if details.country.is_none() || details.country.as_ref().unwrap().trim().is_empty() {
                    details.country = Some(country.to_string());
                }
                info!("[SCRAPE_REGION] Successfully scraped: {}", details.title);
                region_listings.push(details);
            },
            Err(e) => {
                warn!("[SCRAPE_REGION] Failed to scrape {}: {}", url, e);
                failed_count += 1;
            }
        }

        // Small delay between scrapes to be polite
        sleep(Duration::from_millis(1000)).await;
    }
    
    info!("Successfully scraped {} listings, {} failed for {}, {}", 
          region_listings.len(), failed_count, region, country);

    if region_listings.is_empty() {
        warn!("No valid listings scraped for {}, {}", region, country);
        return Ok(0);
    }

    let inserted_ids = crate::database::insert_many(region_listings).await?;
    let count = inserted_ids.len() as i32;
    info!("Successfully inserted {} Airbnb listings for {}, {} (failed: {})",
          count, region, country, failed_count);

    Ok(count)
}

// ============================================================================
// STEALTH BROWSER VERSIONS - Using chromiumoxide CDP instead of WebDriver
// ============================================================================

/// Guest parameters for Airbnb search
#[derive(Debug, Clone, Default)]
pub struct GuestParams {
    pub adults: i32,
    pub children: i32,
    pub infants: i32,
    pub pets: i32,
}

impl GuestParams {
    pub fn new(adults: i32, children: i32, infants: i32, pets: i32) -> Self {
        Self { adults, children, infants, pets }
    }

    pub fn from_total(guests: i32) -> Self {
        Self { adults: guests, children: 0, infants: 0, pets: 0 }
    }
}

/// Amenity filters for Airbnb search
/// Airbnb IDs: Hot tub=25, Pool=7, Waterfront=Tag:686
#[derive(Debug, Clone, Default)]
pub struct AmenityFilter {
    pub hot_tub: bool,
    pub pool: bool,
    pub waterfront: bool,
}

impl AmenityFilter {
    pub fn new(hot_tub: bool, pool: bool, waterfront: bool) -> Self {
        Self { hot_tub, pool, waterfront }
    }

    /// Build the URL query string for amenity filters
    pub fn to_url_params(&self) -> String {
        let mut params = Vec::new();

        // Airbnb amenity IDs
        if self.hot_tub {
            params.push("amenities%5B%5D=25"); // Hot tub
        }
        if self.pool {
            params.push("amenities%5B%5D=7"); // Pool
        }
        if self.waterfront {
            params.push("kg_and_tags%5B%5D=Tag%3A686"); // Waterfront (uses tag, not amenity)
        }

        params.join("&")
    }

    /// Check if any filter is active
    pub fn has_filters(&self) -> bool {
        self.hot_tub || self.pool || self.waterfront
    }
}

/// Get listing URLs using the stealth browser (CDP-based, harder to detect)
pub async fn get_place_urls_stealth(
    driver: &StealthDriver,
    location: &str,
    check_in_date: Option<&str>,
    check_out_date: Option<&str>,
    guest_params: Option<GuestParams>,
    amenity_filter: Option<AmenityFilter>,
) -> Result<Vec<String>> {
    use tracing::{info, debug, warn, error, span, Level};

    let span = span!(Level::INFO, "get_place_urls_stealth", location = %location);
    let _enter = span.enter();

    info!("[STEALTH] Starting URL collection for location: {}", location);

    let guests = guest_params.unwrap_or_else(|| GuestParams::new(2, 0, 0, 0));

    // Build guest parameters string
    let guest_params_str = format!(
        "adults={}&children={}&infants={}&pets={}",
        guests.adults, guests.children, guests.infants, guests.pets
    );

    // Build amenity filter string
    let amenity_params_str = amenity_filter
        .as_ref()
        .map(|f| f.to_url_params())
        .unwrap_or_default();

    // Log active filters
    if let Some(ref filter) = amenity_filter {
        if filter.has_filters() {
            info!("[STEALTH] Active amenity filters - Hot tub: {}, Pool: {}, Waterfront: {}",
                filter.hot_tub, filter.pool, filter.waterfront);
        }
    }

    // Build search URL
    let search_url = if let (Some(checkin), Some(checkout)) = (check_in_date, check_out_date) {
        let checkin_date = chrono::NaiveDate::parse_from_str(checkin, "%Y-%m-%d").ok();
        let (monthly_start, monthly_length, monthly_end) = if let Some(date) = checkin_date {
            let start_of_month = format!("{}-{:02}-01", date.year(), date.month());
            let next_quarter = date + chrono::Duration::days(90);
            let end_date = format!("{}-{:02}-01", next_quarter.year(), next_quarter.month());
            (start_of_month, "3", end_date)
        } else {
            ("2025-01-01".to_string(), "3", "2025-04-01".to_string())
        };

        let base_url = format!(
            "{}s/{}/homes?refinement_paths%5B%5D=%2Fhomes&\
             query={}&\
             flexible_trip_lengths%5B%5D=one_week&\
             monthly_start_date={}&monthly_length={}&monthly_end_date={}&\
             search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=calendar&checkin={}&checkout={}&\
             source=structured_search_input_header&search_type=unknown&\
             {}",
            AIRBNB_BASE_URL,
            urlencoding::encode(location),
            urlencoding::encode(location),
            monthly_start, monthly_length, monthly_end,
            checkin, checkout, guest_params_str
        );
        // Append amenity filters if present
        if amenity_params_str.is_empty() {
            base_url
        } else {
            format!("{}&{}", base_url, amenity_params_str)
        }
    } else {
        let base_url = format!(
            "{}s/{}/homes?refinement_paths%5B%5D=%2Fhomes&\
             query={}&\
             flexible_trip_lengths%5B%5D=one_week&monthly_start_date=2025-01-01&monthly_length=12&\
             monthly_end_date=2026-01-01&search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=flexible_dates&source=structured_search_input_header&\
             search_type=unknown&{}",
            AIRBNB_BASE_URL,
            urlencoding::encode(location),
            urlencoding::encode(location),
            guest_params_str
        );
        // Append amenity filters if present
        if amenity_params_str.is_empty() {
            base_url
        } else {
            format!("{}&{}", base_url, amenity_params_str)
        }
    };

    info!("[STEALTH] Navigating to: {}", search_url);

    // Navigate to search page
    driver.goto(&search_url).await?;

    // Wait for page to load
    info!("[STEALTH] Waiting for page to load...");
    sleep(Duration::from_secs(5)).await;

    // Scroll to load lazy content using JavaScript
    info!("[STEALTH] Scrolling page to load all listings...");
    for i in 1..=5 {
        let scroll_amount = 800 * i;
        let scroll_js = format!("window.scrollBy(0, {});", scroll_amount);
        let _ = driver.execute_script(&scroll_js).await;
        sleep(Duration::from_millis(1500)).await;
        debug!("[STEALTH] Scroll {} complete", i);
    }

    // Additional wait for dynamic content
    sleep(Duration::from_secs(3)).await;

    let mut urls = HashSet::new();

    // Get page source and extract URLs
    info!("[STEALTH] Extracting URLs from page source...");
    match driver.page_source().await {
        Ok(page_source) => {
            // Try Airbnb JSON data first (most reliable)
            if let Some(json_data) = extract_airbnb_json_data(&page_source) {
                let airbnb_urls = extract_all_listing_urls_from_json(&json_data);
                for url in airbnb_urls {
                    if urls.insert(url.clone()) {
                        debug!("[STEALTH] JSON extracted URL: {}", url);
                    }
                }
                info!("[STEALTH] JSON extraction found {} URLs", urls.len());
            }

            // Try JSON-LD as fallback
            if urls.is_empty() {
                if let Some(json_ld_urls) = extract_listing_urls_from_json_ld(&page_source) {
                    for url in json_ld_urls {
                        if urls.insert(url.clone()) {
                            debug!("[STEALTH] JSON-LD extracted URL: {}", url);
                        }
                    }
                    info!("[STEALTH] JSON-LD extraction found {} URLs", urls.len());
                }
            }

            // Try regex extraction as last resort
            if urls.is_empty() || urls.len() < 5 {
                let regex_urls = extract_listing_urls_from_source(&page_source);
                let before = urls.len();
                for url in regex_urls {
                    urls.insert(url);
                }
                info!("[STEALTH] Regex extraction added {} more URLs (total: {})",
                      urls.len() - before, urls.len());
            }

            // Also try extracting URLs via JavaScript
            let js_extract = r#"
                (function() {
                    const urls = [];
                    document.querySelectorAll('a[href*="/rooms/"]').forEach(a => {
                        const href = a.getAttribute('href');
                        if (href && href.includes('/rooms/')) {
                            const fullUrl = href.startsWith('http') ? href : 'https://www.airbnb.com' + href;
                            urls.push(fullUrl.split('?')[0]); // Remove query params for dedup
                        }
                    });
                    return [...new Set(urls)];
                })()
            "#;

            if let Ok(js_result) = driver.execute_script(js_extract).await {
                if let Some(arr) = js_result.as_array() {
                    let before = urls.len();
                    for v in arr {
                        if let Some(url) = v.as_str() {
                            urls.insert(url.to_string());
                        }
                    }
                    info!("[STEALTH] JS extraction added {} more URLs (total: {})",
                          urls.len() - before, urls.len());
                }
            }
        }
        Err(e) => {
            error!("[STEALTH] Failed to get page source: {}", e);
        }
    }

    // Check for pagination and get more pages
    const MAX_PAGES: usize = 15;
    let mut page_number = 1;

    info!("[STEALTH] Starting pagination check. Current URLs: {}", urls.len());

    while page_number < MAX_PAGES && urls.len() < 500 {
        // Check if there's a next page using multiple selectors
        // Airbnb uses various pagination patterns
        let pagination_check = driver.execute_script(r#"
            (function() {
                // Try multiple selectors for the "Next" button
                const selectors = [
                    'a[aria-label="Next"]',
                    'a[aria-label="Next page"]',
                    'nav[aria-label="Search results pagination"] a:last-child',
                    'button[aria-label="Next"]',
                    '[data-testid="pagination-next"]',
                    'a[href*="items_offset"]',  // Airbnb uses offset-based pagination
                    'nav a[href*="cursor="]'     // Or cursor-based
                ];

                for (const selector of selectors) {
                    const btn = document.querySelector(selector);
                    if (btn) {
                        // Check if it's actually a "next" link (not "previous")
                        const href = btn.getAttribute('href') || '';
                        const text = btn.textContent || '';
                        const ariaLabel = btn.getAttribute('aria-label') || '';

                        // Return info about what we found
                        return {
                            found: true,
                            selector: selector,
                            href: href.substring(0, 200),
                            text: text.substring(0, 50),
                            ariaLabel: ariaLabel
                        };
                    }
                }

                // Also check for pagination nav element
                const paginationNav = document.querySelector('nav[aria-label*="pagination"], nav[aria-label*="Pagination"]');
                if (paginationNav) {
                    const links = paginationNav.querySelectorAll('a');
                    const lastLink = links[links.length - 1];
                    if (lastLink) {
                        return {
                            found: true,
                            selector: 'pagination nav last link',
                            href: (lastLink.getAttribute('href') || '').substring(0, 200),
                            text: (lastLink.textContent || '').substring(0, 50)
                        };
                    }
                }

                return { found: false };
            })()
        "#).await;

        let has_next = match &pagination_check {
            Ok(value) => {
                if let Some(found) = value.get("found").and_then(|v| v.as_bool()) {
                    if found {
                        info!("[STEALTH] Pagination found: {:?}", value);
                    } else {
                        info!("[STEALTH] No pagination element found on page {}", page_number);
                    }
                    found
                } else {
                    warn!("[STEALTH] Unexpected pagination check result: {:?}", value);
                    false
                }
            }
            Err(e) => {
                warn!("[STEALTH] Pagination check failed: {}", e);
                false
            }
        };

        if !has_next {
            info!("[STEALTH] No more pages found after page {}", page_number);
            break;
        }

        // Click next page using the found selector
        info!("[STEALTH] Navigating to page {}", page_number + 1);
        let click_result = driver.execute_script(r#"
            (function() {
                const selectors = [
                    'a[aria-label="Next"]',
                    'a[aria-label="Next page"]',
                    'nav[aria-label="Search results pagination"] a:last-child',
                    'button[aria-label="Next"]',
                    '[data-testid="pagination-next"]'
                ];

                for (const selector of selectors) {
                    const btn = document.querySelector(selector);
                    if (btn) {
                        btn.click();
                        return { clicked: true, selector: selector };
                    }
                }

                // Try clicking by finding pagination nav
                const paginationNav = document.querySelector('nav[aria-label*="pagination"], nav[aria-label*="Pagination"]');
                if (paginationNav) {
                    const links = paginationNav.querySelectorAll('a');
                    const lastLink = links[links.length - 1];
                    if (lastLink) {
                        lastLink.click();
                        return { clicked: true, selector: 'pagination nav last link' };
                    }
                }

                return { clicked: false };
            })()
        "#).await;

        if let Ok(result) = &click_result {
            info!("[STEALTH] Click result: {:?}", result);
        }

        page_number += 1;
        sleep(Duration::from_secs(5)).await;

        // Scroll new page
        for i in 1..=3 {
            let scroll_js = format!("window.scrollBy(0, {});", 600 * i);
            let _ = driver.execute_script(&scroll_js).await;
            sleep(Duration::from_millis(1000)).await;
        }

        // Extract from new page
        if let Ok(page_source) = driver.page_source().await {
            let before = urls.len();

            if let Some(json_data) = extract_airbnb_json_data(&page_source) {
                for url in extract_all_listing_urls_from_json(&json_data) {
                    urls.insert(url);
                }
            }

            let regex_urls = extract_listing_urls_from_source(&page_source);
            for url in regex_urls {
                urls.insert(url);
            }

            info!("[STEALTH] Page {} added {} new URLs (total: {})",
                  page_number, urls.len() - before, urls.len());

            if urls.len() == before {
                info!("[STEALTH] No new URLs on page {}, stopping", page_number);
                break;
            }
        }
    }

    let url_list: Vec<String> = urls.into_iter().collect();
    info!("[STEALTH] URL collection complete. Total unique URLs: {}", url_list.len());

    // Log first few URLs
    for (i, url) in url_list.iter().take(5).enumerate() {
        debug!("[STEALTH] URL {}: {}", i + 1, url);
    }

    Ok(url_list)
}

/// Scrape individual listing details using stealth browser
pub async fn scrape_place_details_stealth(driver: &StealthDriver, url: &str) -> Result<Listing> {
    use tracing::{info, debug, warn, error, span, Level};

    let span = span!(Level::INFO, "scrape_place_details_stealth", url = %url);
    let _enter = span.enter();

    info!("[STEALTH] Scraping listing: {}", url);

    // Ensure we have a full URL
    let full_url = if url.starts_with("http") {
        url.to_string()
    } else {
        format!("https://www.airbnb.com{}", url)
    };

    // Navigate to the listing page (with timeout)
    info!("[STEALTH] Step 1: Navigating...");
    match timeout(Duration::from_secs(30), driver.goto(&full_url)).await {
        Ok(result) => result?,
        Err(_) => {
            error!("[STEALTH] Navigation timed out after 30s");
            return Err(anyhow::anyhow!("Navigation timed out"));
        }
    }
    info!("[STEALTH] Navigation complete");

    // Wait for page to load
    info!("[STEALTH] Step 2: Waiting 4s for page load...");
    sleep(Duration::from_secs(4)).await;

    // Scroll down to load lazy content (with timeouts to detect frozen browser)
    info!("[STEALTH] Step 3: Scrolling...");
    let mut scroll_failed = false;
    for i in 0..3 {
        match timeout(Duration::from_secs(30), driver.execute_script("window.scrollBy(0, 500);")).await {
            Ok(_) => {},
            Err(_) => {
                warn!("[STEALTH] Scroll {} timed out after 30s - skipping remaining scrolls", i + 1);
                scroll_failed = true;
                break;
            }
        }
        sleep(Duration::from_millis(500)).await;
    }
    if scroll_failed {
        // Give browser time to recover before continuing
        sleep(Duration::from_secs(3)).await;
    }

    // Get page source for extraction (with timeout)
    info!("[STEALTH] Step 4: Getting page source...");
    let page_source = match timeout(Duration::from_secs(30), driver.page_source()).await {
        Ok(Ok(source)) => {
            info!("[STEALTH] Got page source: {} bytes", source.len());
            source
        },
        Ok(Err(e)) => {
            error!("[STEALTH] Failed to get page source: {}", e);
            return Err(e);
        },
        Err(_) => {
            error!("[STEALTH] Get page source timed out after 30s");
            return Err(anyhow::anyhow!("Get page source timed out"));
        }
    };

    // Extract listing ID from URL
    let listing_id = extract_listing_id_from_url(&full_url);
    debug!("[STEALTH] Listing ID: {}", listing_id);

    // Extract data from page source using existing helper functions
    let title = extract_title_from_source(&page_source)
        .unwrap_or_else(|| format!("Airbnb Listing {}", listing_id));

    let description = extract_description_from_source(&page_source)
        .unwrap_or_default();

    let picture_url = extract_picture_url_from_source(&page_source)
        .unwrap_or_default();

    let price = extract_price_from_source(&page_source)
        .unwrap_or_default();

    let location = extract_location_from_source(&page_source)
        .unwrap_or_default();

    let rating = extract_rating_from_source(&page_source)
        .unwrap_or_default();

    let features = extract_amenities_from_source(&page_source);

    let house_details = extract_house_details_from_source(&page_source);

    // Extract region and country from page source
    let (region, country) = extract_region_country_from_source(&page_source);

    // Parse numeric price
    let price_numeric = price.trim_start_matches('$')
        .replace(',', "")
        .parse::<f64>()
        .ok();

    // Parse numeric rating
    let rating_numeric = rating.parse::<f64>().ok();

    info!("[STEALTH] Extracted: title='{}', price='{}', rating='{}'",
          title, price, rating);

    // Validate we got meaningful data
    if title.is_empty() {
        warn!("[STEALTH] Failed to extract title for listing: {}", full_url);
        return Err(anyhow::anyhow!("Failed to extract listing title"));
    }

    Ok(Listing {
        id: None,
        url: full_url,
        title,
        picture_url,
        pictures: Vec::new(),
        description,
        price,
        price_numeric,
        rating,
        rating_numeric,
        reviews_count: None,
        location,
        coordinates: None,
        features,
        house_details,
        host: None,
        region,
        country,
        property_type: None,
        created_at: None,
        scraped_at: Some(chrono::Utc::now()),
    })
}

/// Helper function to extract listing ID from URL
fn extract_listing_id_from_url(url: &str) -> String {
    // Extract from /rooms/12345 pattern
    if let Some(start) = url.find("/rooms/") {
        let rest = &url[start + 7..];
        let end = rest.find(|c: char| !c.is_ascii_digit()).unwrap_or(rest.len());
        return rest[..end].to_string();
    }
    // Fallback
    "unknown".to_string()
}

/// Normalize an amenity name to a canonical form
/// Uses contains-based matching to handle Airbnb's verbose amenity names
/// e.g., "Shared hot tub - available all year, open specific hours" -> "Hot Tub"
fn normalize_amenity(amenity: &str) -> String {
    let lower = amenity.to_lowercase();
    let lower = lower.trim();

    // Check contains for each amenity type (order matters - more specific first)

    // Hot tub variations - all the slang and brand names
    if lower.contains("hot tub") || lower.contains("hottub") || lower.contains("hot-tub")
        || lower.contains("jacuzzi") || lower.contains("jaccuzi") || lower.contains("jacuzi")
        || lower.contains("whirlpool") || lower.contains("jetted tub") || lower.contains("jet tub")
        || lower.contains("soaking tub") || lower.contains("spa tub") || lower.contains("bubble tub")
        || lower.contains("hydrotherapy") || lower.contains("plunge pool")
        || (lower.contains("spa") && !lower.contains("space")) // "spa" but not "workspace"
    {
        return "Hot Tub".to_string();
    }

    // Pool variations (after hot tub to avoid "plunge pool" matching pool)
    if lower.contains("pool") || lower.contains("swimming") {
        return "Pool".to_string();
    }

    // Waterfront/views - check before beach access
    if lower.contains("waterfront") || lower.contains("lakefront") || lower.contains("beachfront")
        || lower.contains("oceanfront") || lower.contains("riverfront") || lower.contains("seafront")
        || lower.contains("lake view") || lower.contains("ocean view") || lower.contains("sea view")
        || lower.contains("water view") || lower.contains("beach view") || lower.contains("bay view")
        || lower.contains("harbor view") || lower.contains("marina view")
    {
        return "Waterfront".to_string();
    }

    // Beach/water access
    if lower.contains("beach access") || lower.contains("lake access") || lower.contains("private beach")
        || lower.contains("dock") || lower.contains("pier") || lower.contains("boat slip")
        || lower.contains("kayak") || lower.contains("canoe")
    {
        return "Beach Access".to_string();
    }

    // Air conditioning
    if lower.contains("air conditioning") || lower.contains("air-conditioning") || lower.contains("aircon")
        || lower.contains("central air") || lower.contains("mini split") || lower.contains("climate control")
        || lower == "a/c" || lower == "ac"
    {
        return "Air Conditioning".to_string();
    }

    // WiFi/Internet
    if lower.contains("wifi") || lower.contains("wi-fi") || lower.contains("wi fi")
        || lower.contains("wireless") || lower.contains("internet") || lower.contains("broadband")
    {
        return "WiFi".to_string();
    }

    // Kitchen (check before workspace to avoid "kitchenette" issues)
    if lower.contains("kitchen") || lower.contains("kitchenette") || lower.contains("cooking")
        || lower.contains("stove") || lower.contains("oven") || lower.contains("microwave")
        || lower.contains("refrigerator") || lower.contains("fridge")
    {
        return "Kitchen".to_string();
    }

    // Washer
    if lower.contains("washer") || lower.contains("washing machine") || lower.contains("laundry")
        || lower.contains("clothes washer")
    {
        return "Washer".to_string();
    }

    // Dryer
    if lower.contains("dryer") || lower.contains("tumble dry") || lower.contains("clothes dry") {
        return "Dryer".to_string();
    }

    // Workspace/office
    if lower.contains("workspace") || lower.contains("work space") || lower.contains("desk")
        || lower.contains("office") || lower.contains("work from home")
    {
        return "Workspace".to_string();
    }

    // Parking
    if lower.contains("parking") || lower.contains("garage") || lower.contains("carport")
        || lower.contains("driveway") || lower.contains("car park")
    {
        return "Parking".to_string();
    }

    // Gym/Fitness
    if lower.contains("gym") || lower.contains("fitness") || lower.contains("exercise")
        || lower.contains("workout") || lower.contains("weights") || lower.contains("treadmill")
    {
        return "Gym".to_string();
    }

    // Fireplace
    if lower.contains("fireplace") || lower.contains("fire place") || lower.contains("wood burning")
        || lower.contains("gas fire") || lower.contains("fire pit")
    {
        return "Fireplace".to_string();
    }

    // BBQ/Grill
    if lower.contains("bbq") || lower.contains("grill") || lower.contains("barbecue")
        || lower.contains("outdoor kitchen") || lower.contains("smoker")
    {
        return "BBQ Grill".to_string();
    }

    // EV Charger
    if lower.contains("ev charger") || lower.contains("ev charging") || lower.contains("electric vehicle")
        || lower.contains("tesla charger") || lower.contains("charging station")
    {
        return "EV Charger".to_string();
    }

    // Pets
    if lower.contains("pets allowed") || lower.contains("pet friendly") || lower.contains("pet-friendly")
        || lower.contains("pets ok") || lower.contains("dog friendly") || lower.contains("cat friendly")
        || lower.contains("dogs allowed") || lower.contains("cats allowed")
    {
        return "Pets Allowed".to_string();
    }

    // Heating
    if lower.contains("heating") || lower.contains("heater") || lower.contains("furnace")
        || lower.contains("radiant heat") || lower.contains("central heat")
        || (lower.contains("heated") && !lower.contains("heated pool"))
        || lower == "heat"
    {
        return "Heating".to_string();
    }

    // TV/Entertainment
    if lower.contains("tv") || lower.contains("television") || lower.contains("smart tv")
        || lower.contains("cable") || lower.contains("netflix") || lower.contains("streaming")
        || lower.contains("home theater") || lower.contains("projector") || lower.contains("roku")
        || lower.contains("apple tv") || lower.contains("chromecast")
    {
        return "TV".to_string();
    }

    // Sauna/Steam
    if lower.contains("sauna") || lower.contains("steam room") || lower.contains("steam shower") {
        return "Sauna".to_string();
    }

    // Elevator/Accessibility
    if lower.contains("elevator") || lower.contains("lift") || lower.contains("wheelchair")
        || lower.contains("accessible") || lower.contains("step-free")
    {
        return "Elevator".to_string();
    }

    // Security
    if lower.contains("security") || lower.contains("alarm") || lower.contains("safe")
        || lower.contains("lockbox") || lower.contains("doorman") || lower.contains("concierge")
        || lower.contains("gated") || lower.contains("security camera")
    {
        return "Security".to_string();
    }

    // Outdoor space
    if lower.contains("balcony") || lower.contains("patio") || lower.contains("deck")
        || lower.contains("terrace") || lower.contains("garden") || lower.contains("yard")
        || lower.contains("porch") || lower.contains("veranda") || lower.contains("rooftop")
    {
        return "Outdoor Space".to_string();
    }

    // Game room
    if lower.contains("game room") || lower.contains("pool table") || lower.contains("billiards")
        || lower.contains("ping pong") || lower.contains("foosball") || lower.contains("arcade")
    {
        return "Game Room".to_string();
    }

    // Crib/baby bed
    if lower.contains("crib") || lower.contains("baby bed") || lower.contains("pack 'n play")
        || lower.contains("pack n play") || lower.contains("travel crib") || lower.contains("bassinet")
    {
        return "Crib".to_string();
    }

    // High chair
    if lower.contains("high chair") || lower.contains("highchair") || lower.contains("booster seat") {
        return "High Chair".to_string();
    }

    // Bathtub (regular, not hot tub - soaking tub is handled in Hot Tub above)
    if lower.contains("bathtub") || lower.contains("bath tub")
        || lower.contains("clawfoot tub") || lower.contains("garden tub")
    {
        return "Bathtub".to_string();
    }

    // Coffee maker
    if lower.contains("coffee maker") || lower.contains("coffee machine") || lower.contains("espresso")
        || lower.contains("keurig") || lower.contains("nespresso") || lower.contains("french press")
    {
        return "Coffee Maker".to_string();
    }

    // Dishwasher
    if lower.contains("dishwasher") {
        return "Dishwasher".to_string();
    }

    // Self check-in
    if lower.contains("self check-in") || lower.contains("self checkin") || lower.contains("keyless")
        || lower.contains("smart lock") || lower.contains("keypad") || lower.contains("lockbox")
    {
        return "Self Check-in".to_string();
    }

    // Hair dryer
    if lower.contains("hair dryer") || lower.contains("hairdryer") || lower.contains("blow dryer") {
        return "Hair Dryer".to_string();
    }

    // Iron
    if lower.contains("iron") && !lower.contains("ironing board") {
        return "Iron".to_string();
    }

    // Long term stays
    if lower.contains("long term") || lower.contains("monthly") {
        return "Long Term Stays".to_string();
    }

    // Default: Capitalize first letter of each word
    amenity.split_whitespace()
        .map(|word| {
            let mut chars = word.chars();
            match chars.next() {
                Some(first) => first.to_uppercase().chain(chars).collect(),
                None => String::new(),
            }
        })
        .collect::<Vec<_>>()
        .join(" ")
}

/// Extract amenities from page source
fn extract_amenities_from_source(html: &str) -> Vec<String> {
    use tracing::info;
    let mut amenities = Vec::new();

    info!("[AMENITIES] Starting amenity extraction from page source ({} bytes)", html.len());

    // Try to find amenities in the Airbnb JSON data
    if let Some(json_data) = extract_airbnb_json_data(html) {
        info!("[AMENITIES] Found embedded JSON data, searching for amenities...");
        // Helper to extract amenity title from an amenity object
        fn extract_amenity_title(amenity: &serde_json::Value, amenities: &mut Vec<String>) {
            // Only add if available (or if available field doesn't exist)
            let is_available = amenity.get("available").and_then(|v| v.as_bool()).unwrap_or(true);
            if !is_available {
                return;
            }

            if let Some(title) = amenity.get("title").and_then(|t| t.as_str()) {
                if !title.is_empty() && !amenities.contains(&title.to_string()) {
                    amenities.push(title.to_string());
                }
            }
        }

        // Helper to process amenity groups (works for both old and new Airbnb structures)
        fn process_amenity_groups(groups: &serde_json::Value, amenities: &mut Vec<String>) {
            if let Some(arr) = groups.as_array() {
                for group in arr {
                    // Try "amenities" array (old structure)
                    if let Some(amenity_list) = group.get("amenities").and_then(|a| a.as_array()) {
                        for amenity in amenity_list {
                            extract_amenity_title(amenity, amenities);
                        }
                    }
                    // Try "amenities" as direct items
                    if let Some(items) = group.get("items").and_then(|i| i.as_array()) {
                        for item in items {
                            extract_amenity_title(item, amenities);
                        }
                    }
                    // The group itself might have title (category name - skip these)
                }
            }
        }

        // Helper to recursively find amenity-related data
        fn find_amenities_in_json(value: &serde_json::Value, amenities: &mut Vec<String>, depth: usize) {
            // Increased limits to capture all amenities
            if depth > 25 || amenities.len() > 200 {
                return;
            }

            match value {
                serde_json::Value::Object(map) => {
                    // NEW: Look for seeAllAmenitiesGroups (Airbnb's current structure)
                    if let Some(groups) = map.get("seeAllAmenitiesGroups") {
                        process_amenity_groups(groups, amenities);
                    }

                    // NEW: Look for previewAmenitiesGroups
                    if let Some(groups) = map.get("previewAmenitiesGroups") {
                        process_amenity_groups(groups, amenities);
                    }

                    // Look for amenityGroups (older structure)
                    if let Some(groups) = map.get("amenityGroups") {
                        process_amenity_groups(groups, amenities);
                    }

                    // Look for previewAmenities (array of amenity objects)
                    if let Some(preview) = map.get("previewAmenities").and_then(|p| p.as_array()) {
                        for amenity in preview {
                            extract_amenity_title(amenity, amenities);
                        }
                    }

                    // Look for highlightedAmenities
                    if let Some(highlighted) = map.get("highlightedAmenities").and_then(|h| h.as_array()) {
                        for amenity in highlighted {
                            extract_amenity_title(amenity, amenities);
                        }
                    }

                    // Look for amenity sections by title
                    if let Some(title) = map.get("title").and_then(|t| t.as_str()) {
                        if title.to_lowercase().contains("amenities") ||
                           title.to_lowercase().contains("offers") ||
                           title.to_lowercase().contains("what this place") {
                            if let Some(items) = map.get("items").and_then(|i| i.as_array()) {
                                for item in items {
                                    extract_amenity_title(item, amenities);
                                }
                            }
                        }
                    }

                    // Look for badges (Superhost, Guest favorite, etc.)
                    if let Some(badges) = map.get("badges").and_then(|b| b.as_array()) {
                        for badge in badges {
                            if let Some(text) = badge.get("text").and_then(|t| t.as_str()) {
                                if !amenities.contains(&text.to_string()) {
                                    amenities.push(text.to_string());
                                }
                            }
                            if let Some(title) = badge.get("title").and_then(|t| t.as_str()) {
                                if !amenities.contains(&title.to_string()) {
                                    amenities.push(title.to_string());
                                }
                            }
                        }
                    }

                    // Check for Superhost flag
                    if map.get("isSuperhost").and_then(|v| v.as_bool()).unwrap_or(false) {
                        if !amenities.contains(&"Superhost".to_string()) {
                            amenities.push("Superhost".to_string());
                        }
                    }

                    // Look for host badge/tier
                    if let Some(host) = map.get("host") {
                        if host.get("isSuperhost").and_then(|v| v.as_bool()).unwrap_or(false) {
                            if !amenities.contains(&"Superhost".to_string()) {
                                amenities.push("Superhost".to_string());
                            }
                        }
                    }

                    // Look for Guest favorite / highly rated indicators
                    if let Some(badge_type) = map.get("badgeType").and_then(|b| b.as_str()) {
                        let badge_str = badge_type.to_string();
                        if !amenities.contains(&badge_str) {
                            amenities.push(badge_str);
                        }
                    }

                    // Recurse into nested objects
                    for (_key, val) in map {
                        find_amenities_in_json(val, amenities, depth + 1);
                    }
                }
                serde_json::Value::Array(arr) => {
                    for item in arr {
                        find_amenities_in_json(item, amenities, depth + 1);
                    }
                }
                _ => {}
            }
        }

        find_amenities_in_json(&json_data, &mut amenities, 0);

        if !amenities.is_empty() {
            info!("[AMENITIES] === RAW AMENITIES FROM JSON ({}) ===", amenities.len());
            for (i, amenity) in amenities.iter().enumerate() {
                info!("[AMENITIES]   [{}] {:?}", i + 1, amenity);
            }
            // Normalize and deduplicate
            let normalized: Vec<String> = amenities.iter()
                .map(|a| normalize_amenity(a))
                .collect::<std::collections::HashSet<_>>()
                .into_iter()
                .collect();
            info!("[AMENITIES] === NORMALIZED AMENITIES ({}) ===", normalized.len());
            for (i, amenity) in normalized.iter().enumerate() {
                info!("[AMENITIES]   [{}] {}", i + 1, amenity);
            }
            return normalized;
        } else {
            info!("[AMENITIES] No amenities found in JSON data");
        }
    } else {
        info!("[AMENITIES] No embedded JSON data found in page source");
    }

    // Fallback: Try regex patterns for common amenities (comprehensive list)
    info!("[AMENITIES] Falling back to regex-based amenity detection");
    let common_amenities = [
        // WiFi variations
        "wifi", "Wi-Fi", "wi fi", "wireless", "internet", "broadband",
        // Kitchen variations
        "kitchen", "kitchenette", "full kitchen", "cooking", "stove", "oven", "microwave",
        // Parking variations
        "parking", "Free parking", "garage", "carport", "driveway", "car park",
        // Laundry variations
        "washer", "washing machine", "laundry", "clothes washer",
        "dryer", "tumble dryer", "clothes dryer",
        // Climate control
        "air conditioning", "AC", "A/C", "aircon", "climate control", "central air", "mini split",
        "heating", "heater", "furnace", "radiant heat", "central heating", "heated",
        // Pool variations
        "pool", "swimming", "indoor pool", "outdoor pool", "private pool", "shared pool", "infinity pool",
        // Hot tub/spa variations
        "hot tub", "hottub", "jacuzzi", "jaccuzi", "whirlpool", "jetted tub", "jet tub",
        "soaking tub", "spa tub", "hydrotherapy", "plunge pool", "spa",
        // Gym/fitness variations
        "gym", "fitness", "exercise", "workout", "weights", "treadmill", "home gym",
        // TV/entertainment
        "TV", "television", "smart tv", "cable", "netflix", "streaming", "home theater", "projector",
        // Workspace variations
        "workspace", "dedicated workspace", "work space", "desk", "office", "work from home", "home office",
        // Waterfront variations
        "waterfront", "lakefront", "beachfront", "oceanfront", "riverfront", "seafront",
        "lake view", "ocean view", "sea view", "water view", "beach view", "bay view",
        // Beach/water access
        "beach access", "lake access", "private beach", "beach", "dock", "boat dock", "pier",
        // Fireplace variations
        "fireplace", "fire place", "indoor fireplace", "wood burning", "gas fireplace", "fire pit",
        // BBQ/outdoor cooking
        "bbq", "grill", "barbecue", "outdoor kitchen", "smoker",
        // EV charging
        "ev charger", "ev charging", "electric vehicle", "tesla charger", "charging station",
        // Pets
        "pet friendly", "pets allowed", "pets ok", "dog friendly", "cat friendly",
        // Sauna/steam
        "sauna", "steam room", "steam shower",
        // Elevator/accessibility
        "elevator", "lift", "wheelchair", "accessible",
        // Security
        "security", "alarm", "safe", "lockbox", "doorman", "concierge", "gated",
        // Outdoor space
        "balcony", "patio", "deck", "terrace", "garden", "yard", "outdoor space", "porch",
    ];

    let html_lower = html.to_lowercase();
    for amenity in common_amenities {
        if html_lower.contains(&amenity.to_lowercase()) {
            let normalized = normalize_amenity(amenity);
            if !amenities.contains(&normalized) {
                amenities.push(normalized);
            }
        }
    }

    // Extract badges from HTML using the specific badge CSS class
    // Airbnb uses class="t1qa5xaj" for badge text elements
    use regex::Regex;
    if let Ok(badge_regex) = Regex::new(r#"class="[^"]*t1qa5xaj[^"]*"[^>]*>([^<]+)<"#) {
        for cap in badge_regex.captures_iter(html) {
            if let Some(badge_text) = cap.get(1) {
                let badge = badge_text.as_str().trim().to_string();
                if !badge.is_empty() && !amenities.contains(&badge) {
                    tracing::info!("[BADGE] Found badge from CSS class: '{}'", badge);
                    amenities.push(badge);
                }
            }
        }
    }

    // Also try a simpler pattern - look for the class followed by text
    if let Ok(badge_regex2) = Regex::new(r#"t1qa5xaj[^>]*>([^<]{2,50})<"#) {
        for cap in badge_regex2.captures_iter(html) {
            if let Some(badge_text) = cap.get(1) {
                let badge = badge_text.as_str().trim().to_string();
                // Only add if it looks like a badge (contains known badge keywords)
                let badge_lower = badge.to_lowercase();
                if (badge_lower.contains("superhost") ||
                    badge_lower.contains("guest fav") ||
                    badge_lower.contains("rare find") ||
                    badge_lower.contains("highly rated")) &&
                   !amenities.contains(&badge) {
                    tracing::info!("[BADGE] Found badge: '{}'", badge);
                    amenities.push(badge);
                }
            }
        }
    }

    info!("[AMENITIES] === REGEX FALLBACK AMENITIES ({}) ===", amenities.len());
    for (i, amenity) in amenities.iter().enumerate() {
        info!("[AMENITIES]   [{}] {}", i + 1, amenity);
    }

    amenities
}

/// Extract house details from page source
fn extract_house_details_from_source(html: &str) -> Vec<String> {
    use tracing::debug;
    let mut details = Vec::new();

    // Try Airbnb JSON data first
    if let Some(json_data) = extract_airbnb_json_data(html) {
        // Helper to find house details in JSON
        fn find_house_details_in_json(value: &serde_json::Value, details: &mut Vec<String>, depth: usize) {
            if depth > 15 || details.len() > 20 {
                return;
            }

            match value {
                serde_json::Value::Object(map) => {
                    // Look for sharingConfig which contains room type info
                    if let Some(sharing) = map.get("sharingConfig") {
                        if let Some(property_type) = sharing.get("propertyType").and_then(|t| t.as_str()) {
                            if !details.contains(&property_type.to_string()) {
                                details.push(property_type.to_string());
                            }
                        }
                    }

                    // Look for overview section with room counts
                    if let Some(overview_items) = map.get("overviewItems").and_then(|o| o.as_array()) {
                        for item in overview_items {
                            if let Some(title) = item.get("title").and_then(|t| t.as_str()) {
                                if !details.contains(&title.to_string()) {
                                    details.push(title.to_string());
                                }
                            }
                        }
                    }

                    // Look for listing details section
                    if let Some(section_type) = map.get("sectionComponentType").and_then(|t| t.as_str()) {
                        if section_type.contains("OVERVIEW") || section_type.contains("ROOM") {
                            if let Some(items) = map.get("items").and_then(|i| i.as_array()) {
                                for item in items {
                                    if let Some(title) = item.get("title").and_then(|t| t.as_str()) {
                                        if !details.contains(&title.to_string()) {
                                            details.push(title.to_string());
                                        }
                                    }
                                }
                            }
                        }
                    }

                    // Look for structuredContent with property info
                    if let Some(content) = map.get("structuredContent") {
                        if let Some(primary) = content.get("primaryLine").and_then(|p| p.as_array()) {
                            for line in primary {
                                if let Some(body) = line.get("body").and_then(|b| b.as_str()) {
                                    if !details.contains(&body.to_string()) {
                                        details.push(body.to_string());
                                    }
                                }
                            }
                        }
                        if let Some(secondary) = content.get("secondaryLine").and_then(|s| s.as_array()) {
                            for line in secondary {
                                if let Some(body) = line.get("body").and_then(|b| b.as_str()) {
                                    if !details.contains(&body.to_string()) {
                                        details.push(body.to_string());
                                    }
                                }
                            }
                        }
                    }

                    // Look for previewAmenityGroups which sometimes contains room info
                    if let Some(preview_groups) = map.get("previewAmenityGroups").and_then(|g| g.as_array()) {
                        for group in preview_groups {
                            if let Some(group_title) = group.get("title").and_then(|t| t.as_str()) {
                                if group_title.to_lowercase().contains("bedroom") ||
                                   group_title.to_lowercase().contains("bathroom") ||
                                   group_title.to_lowercase().contains("space") {
                                    if let Some(amenities) = group.get("amenities").and_then(|a| a.as_array()) {
                                        for amenity in amenities {
                                            if let Some(title) = amenity.get("title").and_then(|t| t.as_str()) {
                                                if !details.contains(&title.to_string()) {
                                                    details.push(title.to_string());
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }

                    // Recurse into nested objects
                    for (_key, val) in map {
                        find_house_details_in_json(val, details, depth + 1);
                    }
                }
                serde_json::Value::Array(arr) => {
                    for item in arr {
                        find_house_details_in_json(item, details, depth + 1);
                    }
                }
                _ => {}
            }
        }

        find_house_details_in_json(&json_data, &mut details, 0);

        if !details.is_empty() {
            debug!("Found {} house details from JSON", details.len());
            return details;
        }
    }

    // Try JSON-LD fallback
    if let Some(json_ld) = extract_json_ld(html) {
        // Try to get number of guests
        if let Some(occupancy) = json_ld.get("occupancy").and_then(|o| o.as_u64()) {
            details.push(format!("{} guests", occupancy));
        }
        // Try to get number of beds
        if let Some(beds) = json_ld.get("numberOfBeds").and_then(|b| b.as_u64()) {
            details.push(format!("{} beds", beds));
        }
    }

    // Try regex for common patterns
    let patterns = [
        (r"(\d+)\s*guests?", "guests"),
        (r"(\d+)\s*bedrooms?", "bedrooms"),
        (r"(\d+)\s*beds?", "beds"),
        (r"(\d+)\s*baths?", "baths"),
        (r"(\d+)\s*bathrooms?", "bathrooms"),
    ];

    for (pattern, suffix) in patterns {
        if let Ok(re) = regex::Regex::new(pattern) {
            if let Some(caps) = re.captures(html) {
                if let Some(num) = caps.get(1) {
                    let detail = format!("{} {}", num.as_str(), suffix);
                    if !details.contains(&detail) {
                        details.push(detail);
                    }
                }
            }
        }
    }

    details
}

// ============================================================================
// FAST PATH — search-JSON harvest, parallel HTTP enrichment, map-tile coverage
// ============================================================================
//
// The legacy stealth path navigates a browser to every /rooms/<id> page and
// re-scrapes fields the search-results JSON already contains. These functions
// instead:
//   Phase 1  build listings directly from the search JSON already downloaded,
//   Phase 2  enrich the few missing fields (amenities, house details, region)
//            over bounded parallel HTTP instead of one browser at a time,
//   Phase 3  subdivide the search area by map bounding-box so we break past
//            Airbnb's ~300-results-per-query ceiling.

use crate::models::Coordinates;

/// Airbnb never returns more than ~270-300 results for one search query. When a
/// tile returns this many listings we assume results are truncated and split it.
const TILE_SPLIT_THRESHOLD: usize = 270;
/// Max recursion depth for bounding-box subdivision (4^6 = 4096 tiles worst case).
const MAX_TILE_DEPTH: usize = 6;
/// Concurrent HTTP enrichment fetches. Unlike browsers, these don't conflict.
const ENRICH_CONCURRENCY: usize = 10;

/// Decode Airbnb's base64 "DemandStayListing:12345" id into the numeric id.
fn decode_listing_id(encoded: &str) -> Option<String> {
    let decoded = base64::engine::general_purpose::STANDARD.decode(encoded).ok()?;
    let text = std::str::from_utf8(&decoded).ok()?;
    text.split(':').nth(1).map(|s| s.to_string())
}

/// Pull the first number (with optional decimal) out of a string like
/// "$1,234 night" -> 1234.0 or "4.95 (312)" -> 4.95. Commas are treated as
/// thousands separators.
fn first_number(text: &str) -> Option<f64> {
    let mut buf = String::new();
    let mut seen_dot = false;
    for c in text.chars() {
        if c.is_ascii_digit() {
            buf.push(c);
        } else if c == '.' && !seen_dot && !buf.is_empty() {
            seen_dot = true;
            buf.push(c);
        } else if c == ',' && !buf.is_empty() {
            continue; // thousands separator inside a number
        } else if !buf.is_empty() {
            break; // number ended
        }
    }
    buf.parse::<f64>().ok()
}

/// Build a full Listing from one entry of the search-results JSON array. Returns
/// None if the entry has no decodable id or title.
fn listing_from_search_result(entry: &serde_json::Value) -> Option<Listing> {
    let encoded_id = entry
        .get("demandStayListing")
        .and_then(|l| l.get("id"))
        .and_then(|id| id.as_str())?;
    let numeric_id = decode_listing_id(encoded_id)?;
    let url = format!("https://www.airbnb.com/rooms/{}", numeric_id);

    let title = entry.get("title").and_then(|v| v.as_str()).unwrap_or_default().to_string();
    if title.is_empty() {
        return None;
    }

    // Price: primaryLine.price, falling back to discountedPrice.
    let price = entry
        .get("structuredDisplayPrice")
        .and_then(|p| p.get("primaryLine"))
        .and_then(|p| {
            p.get("price").and_then(|v| v.as_str())
                .or_else(|| p.get("discountedPrice").and_then(|v| v.as_str()))
        })
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .unwrap_or_default();
    let price_numeric = first_number(&price);

    // Rating: avgRatingLocalized looks like "4.95 (312)".
    let rating_localized = entry.get("avgRatingLocalized").and_then(|v| v.as_str()).unwrap_or_default();
    let rating = rating_localized.split_whitespace().next().unwrap_or_default().to_string();
    let rating_numeric = first_number(rating_localized);
    let reviews_count = rating_localized
        .split_once('(')
        .and_then(|(_, rest)| first_number(rest))
        .map(|n| n as i32);

    // Description / property name.
    let description = entry
        .get("demandStayListing")
        .and_then(|l| l.get("description"))
        .and_then(|d| d.get("name"))
        .and_then(|n| n.get("localizedStringWithTranslationPreference"))
        .and_then(|v| v.as_str())
        .unwrap_or_default()
        .to_string();

    // Location parsed from a "<Type> in <Place>" title.
    let location = title.split_once(" in ").map(|(_, place)| place.to_string()).unwrap_or_default();

    // Pictures.
    let mut pictures = Vec::new();
    if let Some(pics) = entry.get("contextualPictures").and_then(|p| p.as_array()) {
        for pic in pics {
            if let Some(u) = pic.get("picture").and_then(|v| v.as_str()) {
                pictures.push(u.to_string());
            }
        }
    }
    let picture_url = pictures.first().cloned().unwrap_or_default();

    // Coordinates (used by the map-tiling pass). Airbnb has used both
    // "coordinate"/"coordinates" and latitude/lat naming over time.
    let coordinates = entry
        .get("coordinate")
        .or_else(|| entry.get("coordinates"))
        .and_then(|c| {
            let lat = c.get("latitude").or_else(|| c.get("lat")).and_then(|v| v.as_f64())?;
            let lng = c.get("longitude").or_else(|| c.get("lng")).and_then(|v| v.as_f64())?;
            Some(Coordinates { lat, lng })
        });

    // Features: bed/room summary + badges + payment messages.
    let mut features = Vec::new();
    if let Some(body) = entry
        .get("structuredContent")
        .and_then(|s| s.get("primaryLine"))
        .and_then(|p| p.get(0))
        .and_then(|p| p.get("body"))
        .and_then(|v| v.as_str())
    {
        if !body.is_empty() {
            features.push(body.to_string());
        }
    }
    if let Some(badges) = entry.get("badges").and_then(|b| b.as_array()) {
        for badge in badges {
            if let Some(t) = badge.get("text").and_then(|v| v.as_str()) {
                if !t.is_empty() {
                    features.push(t.to_string());
                }
            }
        }
    }
    if let Some(msgs) = entry.get("paymentMessages").and_then(|p| p.as_array()) {
        for msg in msgs {
            if let Some(t) = msg.get("text").and_then(|v| v.as_str()) {
                if !t.is_empty() {
                    features.push(t.to_string());
                }
            }
        }
    }

    Some(Listing {
        id: None,
        url,
        title,
        picture_url,
        pictures,
        description,
        price,
        price_numeric,
        rating,
        rating_numeric,
        reviews_count,
        location,
        coordinates,
        features,
        house_details: Vec::new(),
        host: None,
        region: None,
        country: None,
        property_type: None,
        created_at: None,
        scraped_at: Some(chrono::Utc::now()),
    })
}

/// Phase 1: build listings for every result in the search-page JSON.
pub fn extract_all_listings_from_json(json_data: &serde_json::Value) -> Vec<Listing> {
    use tracing::{debug, info};
    let mut listings = Vec::new();
    let search_results = json_data
        .get("niobeClientData")
        .and_then(|d| d.get(1))
        .and_then(|d| d.get("data"))
        .and_then(|d| d.get("presentation"))
        .and_then(|d| d.get("staysSearch"))
        .and_then(|d| d.get("results"))
        .and_then(|d| d.get("searchResults"))
        .and_then(|d| d.as_array());
    match search_results {
        Some(arr) => {
            for entry in arr {
                if let Some(listing) = listing_from_search_result(entry) {
                    listings.push(listing);
                }
            }
            info!("[FAST] Built {} listings from search JSON", listings.len());
        }
        None => debug!("[FAST] searchResults array not found in JSON"),
    }
    listings
}

/// A geographic bounding box for map-based search subdivision.
#[derive(Clone, Copy, Debug)]
pub struct BoundingBox {
    pub sw_lat: f64,
    pub sw_lng: f64,
    pub ne_lat: f64,
    pub ne_lng: f64,
}

impl BoundingBox {
    /// Split into four equal quadrants for recursive subdivision.
    fn quarters(&self) -> [BoundingBox; 4] {
        let mid_lat = (self.sw_lat + self.ne_lat) / 2.0;
        let mid_lng = (self.sw_lng + self.ne_lng) / 2.0;
        [
            BoundingBox { sw_lat: self.sw_lat, sw_lng: self.sw_lng, ne_lat: mid_lat, ne_lng: mid_lng }, // SW
            BoundingBox { sw_lat: self.sw_lat, sw_lng: mid_lng, ne_lat: mid_lat, ne_lng: self.ne_lng }, // SE
            BoundingBox { sw_lat: mid_lat, sw_lng: self.sw_lng, ne_lat: self.ne_lat, ne_lng: mid_lng }, // NW
            BoundingBox { sw_lat: mid_lat, sw_lng: mid_lng, ne_lat: self.ne_lat, ne_lng: self.ne_lng }, // NE
        ]
    }

    /// Derive a padded bounding box from the coordinates of harvested listings.
    /// Returns None if too few listings carry coordinates to bound an area.
    fn from_listings(listings: &[Listing]) -> Option<BoundingBox> {
        let coords: Vec<&Coordinates> = listings.iter().filter_map(|l| l.coordinates.as_ref()).collect();
        if coords.len() < 2 {
            return None;
        }
        let mut min_lat = f64::MAX;
        let mut max_lat = f64::MIN;
        let mut min_lng = f64::MAX;
        let mut max_lng = f64::MIN;
        for c in &coords {
            min_lat = min_lat.min(c.lat);
            max_lat = max_lat.max(c.lat);
            min_lng = min_lng.min(c.lng);
            max_lng = max_lng.max(c.lng);
        }
        // Pad by 15% (min 0.01 deg) so edge listings aren't clipped.
        let lat_pad = ((max_lat - min_lat) * 0.15).max(0.01);
        let lng_pad = ((max_lng - min_lng) * 0.15).max(0.01);
        Some(BoundingBox {
            sw_lat: min_lat - lat_pad,
            sw_lng: min_lng - lng_pad,
            ne_lat: max_lat + lat_pad,
            ne_lng: max_lng + lng_pad,
        })
    }
}

/// Build the name-based Airbnb search URL (mirrors get_place_urls_stealth).
fn build_search_url(
    location: &str,
    check_in: Option<&str>,
    check_out: Option<&str>,
    guests: &GuestParams,
    amenity_filter: Option<&AmenityFilter>,
) -> String {
    let guest_params_str = format!(
        "adults={}&children={}&infants={}&pets={}",
        guests.adults, guests.children, guests.infants, guests.pets
    );
    let amenity_params_str = amenity_filter.map(|f| f.to_url_params()).unwrap_or_default();
    let mut url = if let (Some(checkin), Some(checkout)) = (check_in, check_out) {
        format!(
            "{}s/{}/homes?refinement_paths%5B%5D=%2Fhomes&query={}&\
             search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=calendar&checkin={}&checkout={}&\
             source=structured_search_input_header&search_type=unknown&{}",
            AIRBNB_BASE_URL, urlencoding::encode(location), urlencoding::encode(location),
            checkin, checkout, guest_params_str
        )
    } else {
        format!(
            "{}s/{}/homes?refinement_paths%5B%5D=%2Fhomes&query={}&\
             search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=flexible_dates&source=structured_search_input_header&\
             search_type=unknown&{}",
            AIRBNB_BASE_URL, urlencoding::encode(location), urlencoding::encode(location),
            guest_params_str
        )
    };
    if !amenity_params_str.is_empty() {
        url.push('&');
        url.push_str(&amenity_params_str);
    }
    url
}

/// Build a map-bounded search URL for a bounding box (Phase 3 tiling).
fn build_map_search_url(
    location: &str,
    bbox: &BoundingBox,
    guests: &GuestParams,
    amenity_filter: Option<&AmenityFilter>,
) -> String {
    let guest_params_str = format!(
        "adults={}&children={}&infants={}&pets={}",
        guests.adults, guests.children, guests.infants, guests.pets
    );
    let amenity_params_str = amenity_filter.map(|f| f.to_url_params()).unwrap_or_default();
    let mut url = format!(
        "{}s/{}/homes?refinement_paths%5B%5D=%2Fhomes&query={}&search_by_map=true&\
         search_mode=regular_search&channel=EXPLORE&source=structured_search_input_header&\
         search_type=user_map_move&ne_lat={}&ne_lng={}&sw_lat={}&sw_lng={}&zoom=12&{}",
        AIRBNB_BASE_URL, urlencoding::encode(location), urlencoding::encode(location),
        bbox.ne_lat, bbox.ne_lng, bbox.sw_lat, bbox.sw_lng, guest_params_str
    );
    if !amenity_params_str.is_empty() {
        url.push('&');
        url.push_str(&amenity_params_str);
    }
    url
}

/// Navigate to a search URL, scroll to load lazy content, and harvest every
/// listing present in the embedded JSON.
async fn harvest_search_page(driver: &StealthDriver, url: &str) -> Vec<Listing> {
    use tracing::{info, warn};
    info!("[FAST] Harvesting search page: {}", url);
    if let Err(e) = driver.goto(url).await {
        warn!("[FAST] Failed to navigate to {}: {}", url, e);
        return Vec::new();
    }
    sleep(Duration::from_secs(5)).await;
    for i in 1..=5 {
        let _ = driver.execute_script(&format!("window.scrollBy(0, {});", 800 * i)).await;
        sleep(Duration::from_millis(1200)).await;
    }
    sleep(Duration::from_secs(2)).await;
    match driver.page_source().await {
        Ok(html) => match extract_airbnb_json_data(&html) {
            Some(json) => extract_all_listings_from_json(&json),
            None => {
                warn!("[FAST] No embedded JSON on page {}", url);
                Vec::new()
            }
        },
        Err(e) => {
            warn!("[FAST] Failed to read page source for {}: {}", url, e);
            Vec::new()
        }
    }
}

/// Phase 3: recursively harvest a bounding box, splitting into quadrants whenever
/// a tile returns enough results to look truncated. Deduplicates by URL via `seen`.
async fn collect_listings_tiled(
    driver: &StealthDriver,
    location: &str,
    bbox: BoundingBox,
    guests: &GuestParams,
    amenity_filter: Option<&AmenityFilter>,
    depth: usize,
    seen: &mut HashSet<String>,
    out: &mut Vec<Listing>,
) {
    use tracing::info;
    let url = build_map_search_url(location, &bbox, guests, amenity_filter);
    let tile_listings = harvest_search_page(driver, &url).await;
    let returned = tile_listings.len();
    let mut added = 0;
    for listing in tile_listings {
        if seen.insert(listing.url.clone()) {
            out.push(listing);
            added += 1;
        }
    }
    info!("[TILE] depth={} returned={} new={} total={}", depth, returned, added, out.len());

    if returned >= TILE_SPLIT_THRESHOLD && depth < MAX_TILE_DEPTH {
        info!("[TILE] tile at depth {} looks truncated ({} results) - subdividing", depth, returned);
        for sub in bbox.quarters() {
            Box::pin(collect_listings_tiled(
                driver, location, sub, guests, amenity_filter, depth + 1, seen, out,
            ))
            .await;
        }
    }
}

/// Build a reqwest client with browser-like headers. Forces identity encoding
/// because this reqwest build has no decompression features enabled, so a
/// gzip/br/zstd response would otherwise arrive as undecodable bytes.
fn build_http_client() -> Result<reqwest::Client> {
    use reqwest::header::{HeaderMap, HeaderName, HeaderValue};
    let mut headers = HeaderMap::new();
    for (key, value) in get_realistic_headers() {
        if key.eq_ignore_ascii_case("accept-encoding") {
            continue; // overridden below
        }
        if let (Ok(name), Ok(val)) = (
            HeaderName::from_bytes(key.as_bytes()),
            HeaderValue::from_str(&value),
        ) {
            headers.insert(name, val);
        }
    }
    headers.insert(reqwest::header::ACCEPT_ENCODING, HeaderValue::from_static("identity"));
    let client = reqwest::Client::builder()
        .default_headers(headers)
        .timeout(Duration::from_secs(30))
        .build()?;
    Ok(client)
}

/// Fetch a listing's /rooms page over HTTP and fill the fields the search JSON
/// lacks (full amenities, house details, region/country). No browser involved.
async fn enrich_listing_http(client: &reqwest::Client, mut listing: Listing) -> Listing {
    use tracing::{debug, warn};
    match client.get(&listing.url).send().await {
        Ok(resp) => match resp.text().await {
            Ok(html) => {
                for a in extract_amenities_from_source(&html) {
                    if !listing.features.contains(&a) {
                        listing.features.push(a);
                    }
                }
                let house = extract_house_details_from_source(&html);
                if !house.is_empty() {
                    listing.house_details = house;
                }
                if listing.region.is_none() || listing.country.is_none() {
                    let (region, country) = extract_region_country_from_source(&html);
                    if listing.region.is_none() {
                        listing.region = region;
                    }
                    if listing.country.is_none() {
                        listing.country = country;
                    }
                }
                if listing.description.is_empty() {
                    if let Some(d) = extract_description_from_source(&html) {
                        listing.description = d;
                    }
                }
                debug!("[ENRICH] {} -> {} features", listing.url, listing.features.len());
            }
            Err(e) => warn!("[ENRICH] Failed to read body for {}: {}", listing.url, e),
        },
        Err(e) => warn!("[ENRICH] Failed to fetch {}: {}", listing.url, e),
    }
    listing
}

/// Phase 2: enrich listings over bounded, parallel HTTP fetches.
pub async fn enrich_listings_parallel(listings: Vec<Listing>, concurrency: usize) -> Vec<Listing> {
    use tracing::{info, warn};
    if listings.is_empty() {
        return listings;
    }
    let client = match build_http_client() {
        Ok(c) => c,
        Err(e) => {
            warn!("[ENRICH] Could not build HTTP client, skipping enrichment: {}", e);
            return listings;
        }
    };
    info!("[ENRICH] Enriching {} listings with concurrency {}", listings.len(), concurrency);
    let enriched = futures::stream::iter(listings.into_iter().map(|listing| {
        let client = client.clone();
        async move {
            let result = enrich_listing_http(&client, listing).await;
            // Polite jitter between requests.
            sleep(Duration::from_millis(150 + fastrand::u64(0..350))).await;
            result
        }
    }))
    .buffer_unordered(concurrency)
    .collect::<Vec<_>>()
    .await;
    info!("[ENRICH] Enrichment complete: {} listings", enriched.len());
    enriched
}

/// High-level fast scrape: Phase 1 harvest, optional Phase 3 tiling for full
/// coverage, then optional Phase 2 HTTP enrichment. Returns listings ready for
/// filtering and insertion by the caller.
pub async fn scrape_city_fast(
    driver: &StealthDriver,
    location: &str,
    guests: GuestParams,
    amenity_filter: Option<AmenityFilter>,
    check_in: Option<&str>,
    check_out: Option<&str>,
    limit: Option<usize>,
    enrich: bool,
    tiled: bool,
) -> Result<Vec<Listing>> {
    use tracing::{info, warn};

    // Phase 1: name-based search harvest.
    let base_url = build_search_url(location, check_in, check_out, &guests, amenity_filter.as_ref());
    let mut base = harvest_search_page(driver, &base_url).await;
    info!("[FAST] Phase 1 harvested {} listings for {}", base.len(), location);

    // Phase 3: subdivide by map bounding box for fuller coverage.
    let mut listings = if tiled {
        match BoundingBox::from_listings(&base) {
            Some(bbox) => {
                info!("[FAST] Phase 3 tiling bbox {:?}", bbox);
                let mut seen: HashSet<String> = base.iter().map(|l| l.url.clone()).collect();
                let mut out = std::mem::take(&mut base);
                collect_listings_tiled(
                    driver, location, bbox, &guests, amenity_filter.as_ref(), 0, &mut seen, &mut out,
                )
                .await;
                info!("[FAST] Phase 3 produced {} unique listings", out.len());
                out
            }
            None => {
                warn!("[FAST] No coordinates available for tiling; using Phase 1 results only");
                base
            }
        }
    } else {
        base
    };

    // Apply the caller's limit before the (potentially expensive) enrichment.
    if let Some(max) = limit {
        if max > 0 && listings.len() > max {
            listings.truncate(max);
        }
    }

    // Phase 2: enrich missing fields over parallel HTTP.
    if enrich {
        listings = enrich_listings_parallel(listings, ENRICH_CONCURRENCY).await;
    }

    Ok(listings)
}

// ============================================================================
// "SCRAPE EVERYTHING" — top-down bounding-box tiling with streaming inserts
// ============================================================================
//
// scrape_city_fast tiles *within* a city (bbox derived from a name search).
// This path instead seeds the recursion with a large preset box (a whole
// country/continent) and inserts each tile's listings as it goes, so memory
// stays bounded no matter how many listings a run turns up. Dense areas (metros)
// recurse deep; sparse rural areas stay shallow — that's how rural listings the
// name-search path would miss get covered.

/// Preset bounding boxes for large-area "scrape everything" runs.
pub fn preset_bbox(name: &str) -> Option<BoundingBox> {
    match name.to_lowercase().as_str() {
        // Small, dense validation box over central Austin, TX. Finishes in a
        // minute or two and exercises harvest + a split or two — used by the
        // admin "Test" button to confirm the pipeline before a big run.
        "test" => Some(BoundingBox {
            sw_lat: 30.20, sw_lng: -97.85, ne_lat: 30.35, ne_lng: -97.68,
        }),
        // Continental United States (excludes Alaska/Hawaii).
        "usa" | "us" | "continental-us" => Some(BoundingBox {
            sw_lat: 24.5, sw_lng: -125.0, ne_lat: 49.5, ne_lng: -66.9,
        }),
        "north-america" | "na" => Some(BoundingBox {
            sw_lat: 14.0, sw_lng: -168.0, ne_lat: 72.0, ne_lng: -52.0,
        }),
        "world" => Some(BoundingBox {
            sw_lat: -56.0, sw_lng: -180.0, ne_lat: 72.0, ne_lng: 180.0,
        }),
        _ => None,
    }
}

/// Recursively harvest a bounding box, inserting each tile's fresh listings to
/// the database as it goes (bounded memory), and subdividing tiles that hit the
/// result cap. `seen` deduplicates URLs across the whole run; the atomics report
/// running progress to the caller.
async fn tile_and_store(
    driver: &StealthDriver,
    location: &str,
    bbox: BoundingBox,
    guests: &GuestParams,
    amenity_filter: Option<&AmenityFilter>,
    depth: usize,
    max_depth: usize,
    enrich: bool,
    seen: &mut HashSet<String>,
    inserted_total: &AtomicUsize,
    tiles_processed: &AtomicUsize,
) {
    use tracing::{info, warn};

    let url = build_map_search_url(location, &bbox, guests, amenity_filter);
    let tile_listings = harvest_search_page(driver, &url).await;
    let returned = tile_listings.len();
    tiles_processed.fetch_add(1, AtomicOrdering::SeqCst);

    // Keep only URLs not already seen earlier in this run.
    let mut fresh: Vec<Listing> = Vec::new();
    for listing in tile_listings {
        if seen.insert(listing.url.clone()) {
            fresh.push(listing);
        }
    }
    let fresh_count = fresh.len();

    if !fresh.is_empty() {
        if enrich {
            fresh = enrich_listings_parallel(fresh, ENRICH_CONCURRENCY).await;
        }
        match crate::database::insert_many(fresh).await {
            Ok(ids) => {
                inserted_total.fetch_add(ids.len(), AtomicOrdering::SeqCst);
            }
            Err(e) => warn!("[TILE] insert failed at depth {}: {}", depth, e),
        }
    }

    info!(
        "[TILE] depth={} returned={} fresh={} | tiles={} inserted={}",
        depth, returned, fresh_count,
        tiles_processed.load(AtomicOrdering::SeqCst),
        inserted_total.load(AtomicOrdering::SeqCst)
    );

    // If the tile looks truncated, subdivide to reach the hidden listings.
    if returned >= TILE_SPLIT_THRESHOLD && depth < max_depth {
        for sub in bbox.quarters() {
            Box::pin(tile_and_store(
                driver, location, sub, guests, amenity_filter,
                depth + 1, max_depth, enrich, seen, inserted_total, tiles_processed,
            ))
            .await;
        }
    }
}

/// Top-level "scrape everything in this box" driver. Seeds recursive map-tiling
/// with an arbitrary bounding box and streams results to the database. Returns
/// (listings_inserted, tiles_processed).
pub async fn scrape_region_tiled(
    driver: &StealthDriver,
    location: &str,
    bbox: BoundingBox,
    guests: GuestParams,
    amenity_filter: Option<AmenityFilter>,
    enrich: bool,
    max_depth: usize,
    inserted_total: &AtomicUsize,
    tiles_processed: &AtomicUsize,
) -> Result<(usize, usize)> {
    use tracing::info;
    info!(
        "[REGION] Starting tiled scrape of bbox {:?} (max_depth={}, enrich={})",
        bbox, max_depth, enrich
    );
    let mut seen: HashSet<String> = HashSet::new();
    tile_and_store(
        driver, location, bbox, &guests, amenity_filter.as_ref(),
        0, max_depth, enrich, &mut seen, inserted_total, tiles_processed,
    )
    .await;
    let inserted = inserted_total.load(AtomicOrdering::SeqCst);
    let tiles = tiles_processed.load(AtomicOrdering::SeqCst);
    info!("[REGION] Complete: {} listings inserted across {} tiles", inserted, tiles);
    Ok((inserted, tiles))
}

#[cfg(test)]
mod fast_path_tests {
    use super::*;

    #[test]
    fn first_number_parses_prices_and_ratings() {
        assert_eq!(first_number("$1,234 night"), Some(1234.0));
        assert_eq!(first_number("$1,234.56"), Some(1234.56));
        assert_eq!(first_number("4.95 (312)"), Some(4.95));
        assert_eq!(first_number("no digits"), None);
        assert_eq!(first_number("$99"), Some(99.0));
    }

    #[test]
    fn decode_listing_id_extracts_numeric_id() {
        let encoded = base64::engine::general_purpose::STANDARD
            .encode("DemandStayListing:633486763306530765");
        assert_eq!(decode_listing_id(&encoded), Some("633486763306530765".to_string()));
        assert_eq!(decode_listing_id("not base64!!!"), None);
    }

    #[test]
    fn bounding_box_quarters_partition_without_gaps() {
        let bbox = BoundingBox { sw_lat: 0.0, sw_lng: 0.0, ne_lat: 4.0, ne_lng: 8.0 };
        let quarters = bbox.quarters();
        // Every quarter is half the size in each dimension.
        for q in &quarters {
            assert!((q.ne_lat - q.sw_lat - 2.0).abs() < 1e-9);
            assert!((q.ne_lng - q.sw_lng - 4.0).abs() < 1e-9);
        }
        // The four quarters together cover the original extent.
        let min_lat = quarters.iter().map(|q| q.sw_lat).fold(f64::MAX, f64::min);
        let max_lat = quarters.iter().map(|q| q.ne_lat).fold(f64::MIN, f64::max);
        assert_eq!(min_lat, 0.0);
        assert_eq!(max_lat, 4.0);
    }

    #[test]
    fn bounding_box_from_listings_needs_two_coords() {
        let mut a = Listing_stub();
        a.coordinates = Some(Coordinates { lat: 10.0, lng: 20.0 });
        let mut b = Listing_stub();
        b.coordinates = Some(Coordinates { lat: 12.0, lng: 24.0 });
        let bbox = BoundingBox::from_listings(&[a.clone(), b]).expect("two coords -> bbox");
        assert!(bbox.sw_lat < 10.0 && bbox.ne_lat > 12.0); // padded outward
        assert!(bbox.sw_lng < 20.0 && bbox.ne_lng > 24.0);
        // A single coordinate cannot define an area.
        assert!(BoundingBox::from_listings(&[a]).is_none());
    }

    #[test]
    fn extract_all_listings_from_json_builds_full_listings() {
        let encoded_id = base64::engine::general_purpose::STANDARD
            .encode("DemandStayListing:12345");
        let json = serde_json::json!({
            "niobeClientData": [
                "ignored",
                { "data": { "presentation": { "staysSearch": { "results": { "searchResults": [
                    {
                        "demandStayListing": {
                            "id": encoded_id,
                            "description": { "name": {
                                "localizedStringWithTranslationPreference": "Cozy Cabin"
                            }}
                        },
                        "title": "Cabin in Traverse City",
                        "structuredDisplayPrice": { "primaryLine": { "price": "$150 night" } },
                        "avgRatingLocalized": "4.90 (128)",
                        "contextualPictures": [ { "picture": "https://img/1.jpg" } ],
                        "coordinate": { "latitude": 44.76, "longitude": -85.62 },
                        "structuredContent": { "primaryLine": [ { "body": "2 beds" } ] },
                        "badges": [ { "text": "Guest favorite" } ]
                    }
                ]}}}}}
            ]
        });

        let listings = extract_all_listings_from_json(&json);
        assert_eq!(listings.len(), 1);
        let l = &listings[0];
        assert_eq!(l.url, "https://www.airbnb.com/rooms/12345");
        assert_eq!(l.title, "Cabin in Traverse City");
        assert_eq!(l.location, "Traverse City");
        assert_eq!(l.price, "$150 night");
        assert_eq!(l.price_numeric, Some(150.0));
        assert_eq!(l.rating, "4.90");
        assert_eq!(l.rating_numeric, Some(4.90));
        assert_eq!(l.reviews_count, Some(128));
        assert_eq!(l.description, "Cozy Cabin");
        assert_eq!(l.picture_url, "https://img/1.jpg");
        let coord = l.coordinates.as_ref().expect("coordinates parsed");
        assert!((coord.lat - 44.76).abs() < 1e-9);
        assert!((coord.lng - -85.62).abs() < 1e-9);
        assert!(l.features.contains(&"2 beds".to_string()));
        assert!(l.features.contains(&"Guest favorite".to_string()));
    }

    #[test]
    fn extract_all_listings_from_json_skips_entries_without_id() {
        let json = serde_json::json!({
            "niobeClientData": [ "x", { "data": { "presentation": { "staysSearch": {
                "results": { "searchResults": [ { "title": "No id here" } ] }
            }}}}]
        });
        assert!(extract_all_listings_from_json(&json).is_empty());
    }

    // Minimal Listing with required string fields empty; helpers under test only
    // touch coordinates.
    #[allow(non_snake_case)]
    fn Listing_stub() -> Listing {
        Listing {
            id: None,
            url: String::new(),
            title: String::new(),
            picture_url: String::new(),
            pictures: Vec::new(),
            description: String::new(),
            price: String::new(),
            price_numeric: None,
            rating: String::new(),
            rating_numeric: None,
            reviews_count: None,
            location: String::new(),
            coordinates: None,
            features: Vec::new(),
            house_details: Vec::new(),
            host: None,
            region: None,
            country: None,
            property_type: None,
            created_at: None,
            scraped_at: None,
        }
    }
}