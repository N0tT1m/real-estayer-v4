// Split out of the former monolithic scraping.rs. Code is unchanged;
// only visibility was widened so cross-module calls resolve.

use super::*;
use crate::models::Listing;
use anyhow::Result;
use chrono::Datelike;
use futures::stream::{FuturesUnordered, StreamExt};
use std::collections::HashSet;
use std::sync::Arc;
use thirtyfour::{By, WebDriver};
use tokio::sync::Semaphore;
use tokio::time::{sleep, timeout, Duration};

/// The `monthly_*` query window Airbnb wants alongside a dated search: the
/// first of the check-in month, a fixed 3-month length, and the first of the
/// month 90 days later. Unparseable input falls back to a fixed 2025 window,
/// preserving the original behaviour.
pub(crate) fn monthly_window(check_in: &str) -> (String, &'static str, String) {
    match chrono::NaiveDate::parse_from_str(check_in, "%Y-%m-%d") {
        Ok(date) => {
            let start_of_month = format!("{}-{:02}-01", date.year(), date.month());
            let next_quarter = date + chrono::Duration::days(90);
            let end_date = format!("{}-{:02}-01", next_quarter.year(), next_quarter.month());
            (start_of_month, "3", end_date)
        }
        Err(_) => ("2025-01-01".to_string(), "3", "2025-04-01".to_string()),
    }
}

/// Build the search URL for the legacy WebDriver scrape path.
///
/// NOTE: `flexible_trip_lengths` is hardcoded to `one_week` here. The original
/// inline version computed a duration-based bucket (weekend / one_week /
/// one_month / three_months) into a variable it then never used, so the emitted
/// URL has always said `one_week` regardless of trip length. That behaviour is
/// preserved deliberately — changing it would change what Airbnb returns, which
/// is a scraping decision rather than a refactor.
pub(crate) fn build_legacy_search_url(
    location: &str,
    check_in: Option<&str>,
    check_out: Option<&str>,
    guests: Option<i32>,
) -> String {
    let adults = guests.unwrap_or(2);
    // The query parameter is double-encoded; the path segment is encoded once.
    let path_location = urlencoding::encode(location);
    let once_encoded = urlencoding::encode(location);
    let query_location = urlencoding::encode(&once_encoded);

    match (check_in, check_out) {
        (Some(checkin), Some(checkout)) => {
            let (monthly_start, monthly_length, monthly_end) = monthly_window(checkin);
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
                path_location,
                query_location,
                monthly_start, monthly_length, monthly_end,
                checkin, checkout, adults
            )
        }
        _ => format!(
            "{}s/{}/homes?refinement_paths%5B%5D=%2Fhomes&\
             query={}&\
             flexible_trip_lengths%5B%5D=one_week&monthly_start_date=2025-01-01&monthly_length=12&\
             monthly_end_date=2026-01-01&search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=flexible_dates&source=structured_search_input_header&\
             search_type=unknown&adults={}",
            AIRBNB_BASE_URL, path_location, query_location, adults
        ),
    }
}

pub async fn get_place_urls(
    driver: &WebDriver,
    location: &str,
    check_in_date: Option<&str>,
    check_out_date: Option<&str>,
    guests: Option<i32>,
) -> Result<Vec<String>> {
    use tracing::{debug, error, info, span, warn, Level};

    let span = span!(Level::INFO, "get_place_urls", location = %location);
    let _enter = span.enter();

    info!("Starting URL collection for location: {}", location);

    // Configure browser with realistic settings first
    configure_realistic_browser(driver).await?;

    let search_url = build_legacy_search_url(location, check_in_date, check_out_date, guests);

    info!("Constructed search URL: {}", search_url);

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
                return Err(anyhow::anyhow!(
                    "Session died on blank page - likely GPU/Chrome issue"
                ));
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

            let current_url = driver
                .current_url()
                .await
                .map(|u| u.to_string())
                .unwrap_or_else(|_| "unknown".to_string());
            let current_url_str = current_url.as_str();
            info!("Current URL after navigation: {}", current_url_str);

            // Check if navigation actually worked
            if current_url_str == "data:," || current_url_str.starts_with("data:") {
                error!("Navigation failed - browser shows data: URL instead of Airbnb");
                let page_title = driver
                    .title()
                    .await
                    .unwrap_or_else(|_| "unknown".to_string());
                error!("Page title: '{}'", page_title);

                // Try to get page source for debugging
                if let Ok(page_source) = driver.source().await {
                    // Use chars().take() for safe UTF-8 truncation
                    let source_preview: String = page_source.chars().take(200).collect();
                    error!("Page source preview: {}", source_preview);
                }

                return Err(anyhow::anyhow!(
                    "Browser navigation failed - showing data: URL instead of loading Airbnb"
                ));
            }

            // Check if we got redirected or blocked
            if current_url_str.contains("captcha") || current_url_str.contains("blocked") {
                error!(
                    "Detected CAPTCHA or block page. Current URL: {}",
                    current_url_str
                );
                return Err(anyhow::anyhow!("Got blocked by Airbnb"));
            }

            // Dismiss any popups (cookie consent, translation, etc.)
            dismiss_popups(driver).await;
        }
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
        info!(
            "[LOOP] ========== STARTING PAGE {} LOOP ==========",
            page_number
        );

        // Check session validity at start of each loop iteration
        if !check_session_valid(driver).await {
            error!(
                "[LOOP] SESSION INVALID at start of page {} loop!",
                page_number
            );
            break;
        }

        // Safety checks: don't scrape more than MAX_PAGES or MAX_URLS
        if page_number > MAX_PAGES {
            warn!(
                "[LOOP] Reached maximum page limit ({}) for safety, stopping pagination",
                MAX_PAGES
            );
            break;
        }
        if urls.len() >= MAX_URLS {
            warn!(
                "[LOOP] Reached maximum URL limit ({}) for safety, stopping pagination",
                MAX_URLS
            );
            break;
        }

        info!(
            "[LOOP] Processing page {} - starting 20 second wait for JS...",
            page_number
        );
        sleep(Duration::from_secs(20)).await;

        // Check session after wait
        if !check_session_valid(driver).await {
            error!(
                "[LOOP] SESSION DIED during 20 second wait on page {}!",
                page_number
            );
            break;
        }

        let additional_delay = fastrand::u64(5000..10000);
        info!("[LOOP] Additional random delay: {}ms", additional_delay);
        sleep(Duration::from_millis(additional_delay)).await;

        // Check session after additional delay
        if !check_session_valid(driver).await {
            error!(
                "[LOOP] SESSION DIED during additional delay on page {}!",
                page_number
            );
            break;
        }

        info!(
            "[LOOP] Starting to check for room links on page {}...",
            page_number
        );

        // Wait for dynamic content to load by checking for actual room links
        let mut room_links_loaded = false;
        for attempt in 1..=15 {
            info!(
                "[LOOP] Attempt {}/15 to find room links on page {}",
                attempt, page_number
            );

            // Check if we have room links with href attributes
            let has_room_links = check_for_room_links(driver).await;
            if has_room_links {
                room_links_loaded = true;
                info!(
                    "[LOOP] Room links detected after {} attempts on page {}",
                    attempt, page_number
                );
                break;
            }

            // Also check body content as fallback
            match driver.find(By::Tag("body")).await {
                Ok(body) => {
                    match body.text().await {
                        Ok(text) => {
                            if text.len() > 2000
                                && (text.contains("Room in")
                                    || text.contains("Apartment in")
                                    || text.contains("Entire"))
                            {
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

            info!(
                "[LOOP] Waiting for room links to load, attempt {}/15 on page {}",
                attempt, page_number
            );
            sleep(Duration::from_secs(4)).await;
        }

        if !room_links_loaded {
            warn!(
                "[LOOP] Room links may not be fully loaded after 60 seconds on page {}",
                page_number
            );
        }

        // Log page title and URL for debugging
        let page_title = driver
            .title()
            .await
            .unwrap_or_else(|_| "unknown".to_string());
        let current_url = driver
            .current_url()
            .await
            .map(|u| u.to_string())
            .unwrap_or_else(|_| "unknown".to_string());
        let current_url_str = current_url.as_str();
        info!(
            "Page {} - Title: '{}', URL: {}",
            page_number, page_title, current_url_str
        );

        let mut places_found = false;
        let listings_before = urls.len();

        // Try to extract listing URLs from Airbnb's JSON data first
        info!(
            "Attempting to extract listing URLs from Airbnb JSON data on page {}",
            page_number
        );
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
                    info!(
                        "Airbnb JSON extracted {} URLs on page {}",
                        json_extracted, page_number
                    );
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
                        info!(
                            "JSON-LD extracted {} URLs on page {}",
                            json_ld_extracted, page_number
                        );
                        places_found = true;
                    }
                }
            }
        }

        // Take a screenshot for debugging and ensure logs directory exists
        if std::fs::create_dir_all("logs").is_err() {
            debug!("Could not create logs directory, skipping screenshots");
        } else if let Ok(screenshot) = driver.screenshot_as_png().await {
            let screenshot_path = format!(
                "logs/page_{}_screenshot_{}.png",
                page_number,
                chrono::Utc::now().timestamp()
            );
            if let Err(e) = std::fs::write(&screenshot_path, screenshot) {
                warn!("Failed to save screenshot: {}", e);
            } else {
                debug!("Saved screenshot to: {}", screenshot_path);
            }
        }

        // Try multiple selectors as fallbacks, prioritizing most reliable ones
        let selectors_to_try = [
            "l1ovpqvx",       // Primary selector from user example - highest priority
            "c1w4n3ae",       // Secondary selector
            "bewl01v",        // Third option
            "b1kg238b",       // From user's example
            "g1hysso5",       // Additional alternative
            "c965t3n",        // Additional selector
            "a3g92ry",        // Additional selector
            "atm_7l_1j28jx2", // Legacy fallback
            "lr88w8j",        // Legacy fallback
        ];

        if !places_found {
            info!(
                "Trying {} different selectors to find listings on page {}",
                selectors_to_try.len(),
                page_number
            );

            // Try each selector until we find listings
            for (i, selector) in selectors_to_try.iter().enumerate() {
                debug!(
                    "Attempt {}/{} - Trying selector: '{}'",
                    i + 1,
                    selectors_to_try.len(),
                    selector
                );
                match wait_for_elements(driver, By::ClassName(*selector), 5).await {
                    Ok(places) => {
                        info!(
                            "SUCCESS: Found {} listing elements with selector '{}' on page {}",
                            places.len(),
                            selector,
                            page_number
                        );
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
                                }
                                Ok(None) => {
                                    debug!("No URL extracted from this element");
                                }
                                Err(e) => {
                                    warn!("Error processing place element: {}", e);
                                }
                            }
                        }
                        info!(
                            "Extracted {} new URLs with selector '{}' on page {}",
                            extracted_urls, selector, page_number
                        );
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
            warn!(
                "All class selectors failed on page {}, trying XPath fallbacks",
                page_number
            );

            // Try multiple XPath strategies with better patterns
            let xpath_selectors = [
                "//a[contains(@href, '/rooms/') and contains(@class, 'l1ovpqvx')]", // Primary pattern from user example
                "//a[contains(@href, '/rooms/')]", // Direct room links
                "//a[contains(@href, '/homes/')]", // Direct home links
                "//a[contains(@href, '/rooms/') or contains(@href, '/homes/')]", // Both patterns
                "//a[contains(@class, 'l1ovpqvx') and @href]", // Links with primary class
                "//a[contains(@class, 'c1w4n3ae') and @href]", // Links with secondary class
                "//a[contains(@class, 'bewl01v') and @href]", // Links with tertiary class
                "//*[@data-testid and contains(@data-testid, 'listing')]//a", // Any listing testid
            ];

            for (i, xpath) in xpath_selectors.iter().enumerate() {
                debug!(
                    "Trying XPath {}/{}: {}",
                    i + 1,
                    xpath_selectors.len(),
                    xpath
                );
                match driver.find_all(By::XPath(*xpath)).await {
                    Ok(places) if !places.is_empty() => {
                        info!(
                            "XPath '{}' found {} elements on page {}",
                            xpath,
                            places.len(),
                            page_number
                        );

                        let mut tasks: FuturesUnordered<_> = places
                            .into_iter()
                            .enumerate()
                            .map(|(place_idx, place)| {
                                let permit = semaphore.clone().acquire_owned();
                                async move {
                                    let _permit = permit.await?;
                                    debug!("XPath place {} processing", place_idx);
                                    // Wait for href to be populated
                                    for retry in 0..3 {
                                        if let Ok(Some(href)) = place.attr("href").await {
                                            if !href.trim().is_empty() {
                                                let full_url = construct_airbnb_url(&href);
                                                debug!(
                                                    "XPath found URL {}: {}",
                                                    place_idx, full_url
                                                );
                                                // Filter to ensure we only get actual listing URLs
                                                if href.contains("/rooms/")
                                                    || href.contains("/homes/")
                                                {
                                                    return Ok::<Option<String>, anyhow::Error>(
                                                        Some(full_url),
                                                    );
                                                } else {
                                                    debug!(
                                                        "XPath place {} href not a listing: {}",
                                                        place_idx, href
                                                    );
                                                    return Ok::<Option<String>, anyhow::Error>(
                                                        None,
                                                    );
                                                }
                                            }
                                        }
                                        if retry < 2 {
                                            sleep(Duration::from_millis(300)).await;
                                        }
                                    }
                                    debug!(
                                        "XPath place {} has no valid href after retries",
                                        place_idx
                                    );
                                    Ok::<Option<String>, anyhow::Error>(None)
                                }
                            })
                            .collect();

                        let mut xpath_extracted = 0;
                        while let Some(result) = tasks.next().await {
                            if let Ok(Some(url)) = result {
                                if urls.insert(url.clone()) {
                                    xpath_extracted += 1;
                                    debug!("XPath added URL: {}", url);
                                }
                            }
                        }
                        info!(
                            "XPath '{}' extracted {} new URLs on page {}",
                            xpath, xpath_extracted, page_number
                        );
                        if xpath_extracted > 0 {
                            places_found = true;
                            break; // Found listings, no need to try more XPath selectors
                        }
                    }
                    Ok(_) => {
                        debug!(
                            "XPath '{}' found 0 matching elements on page {}",
                            xpath, page_number
                        );
                    }
                    Err(e) => {
                        debug!("XPath '{}' failed on page {}: {}", xpath, page_number, e);
                    }
                }
            }
        }

        let new_listings = urls.len() - listings_before;
        info!(
            "Page {} summary: found {} new listings (total: {})",
            page_number,
            new_listings,
            urls.len()
        );

        if !places_found {
            warn!("No listings found with DOM selectors on page {}, trying page source regex extraction", page_number);
            // Enhanced page source analysis and debugging
            if let Ok(page_source) = driver.source().await {
                // Check if page contains expected Airbnb content
                let has_airbnb_content = page_source.contains("data-deferred-state-0")
                    || page_source.contains("searchResults")
                    || page_source.contains("Room in")
                    || page_source.contains("Apartment in");

                if !has_airbnb_content {
                    error!(
                        "Page {} doesn't appear to contain Airbnb listing content",
                        page_number
                    );
                    // Save page source for debugging bot detection issues
                    if std::fs::create_dir_all("logs").is_err() {
                        debug!("Could not create logs directory");
                    } else {
                        let source_path = format!(
                            "logs/page_{}_bot_detected_{}.html",
                            page_number,
                            chrono::Utc::now().timestamp()
                        );
                        if let Err(e) = std::fs::write(&source_path, &page_source) {
                            warn!("Failed to save page source: {}", e);
                        } else {
                            info!(
                                "Saved page source for bot detection analysis: {}",
                                source_path
                            );
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
                    info!(
                        "Source regex extracted {} URLs on page {}",
                        regex_extracted, page_number
                    );
                    places_found = true;
                }

                if !places_found {
                    error!("No listings found on page {} with any method", page_number);
                    warn!("Page source length: {} chars, contains room links: {}, contains homes links: {}",
                          page_source.len(),
                          page_source.contains("/rooms/"),
                          page_source.contains("/homes/"));

                    // Save page source for debugging
                    if std::fs::create_dir_all("logs").is_err() {
                        debug!("Could not create logs directory");
                    } else {
                        let source_path = format!(
                            "logs/page_{}_no_listings_{}.html",
                            page_number,
                            chrono::Utc::now().timestamp()
                        );
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
                    info!(
                        "Found next button on page {} with selector: {}",
                        page_number, selector
                    );
                    match next_button.is_clickable().await {
                        Ok(true) => {
                            info!(
                                "Next button is clickable, navigating to page {}",
                                page_number + 1
                            );

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
                        }
                        Ok(false) => {
                            debug!(
                                "Next button with selector '{}' not clickable on page {}",
                                selector, page_number
                            );
                            continue; // Try next selector
                        }
                        Err(e) => {
                            debug!("Failed to check if next button is clickable with selector '{}': {}", selector, e);
                            continue; // Try next selector
                        }
                    }
                }
                Err(_) => {
                    debug!(
                        "Next button not found with selector '{}' on page {}",
                        selector, page_number
                    );
                    continue; // Try next selector
                }
            }
        }

        if !found_next_button {
            info!(
                "No clickable next button found on page {} with any selector, ending pagination",
                page_number
            );
            break;
        }
    }

    info!(
        "URL collection completed. Total URLs collected: {}",
        urls.len()
    );
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

// Keep the original WebDriver-based function as fallback
pub async fn scrape_place_details(driver: &WebDriver, url: &str) -> Result<Listing> {
    use tracing::{debug, error, info, span, warn, Level};

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
            let current_url = driver
                .current_url()
                .await
                .map(|u| u.to_string())
                .unwrap_or_else(|_| "unknown".to_string());
            let current_url_str = current_url.as_str();
            info!(
                "Successfully navigated to listing. Current URL: {}",
                current_url_str
            );

            // Check if we got redirected to an error page
            if current_url_str.contains("error") || current_url_str.contains("not-found") {
                error!("Redirected to error page: {}", current_url_str);
                return Err(anyhow::anyhow!("Listing not found or error page"));
            }
        }
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
        let screenshot_path = format!(
            "logs/listing_screenshot_{}.png",
            chrono::Utc::now().timestamp()
        );
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
            if let Ok(scripts) = driver
                .find_all(By::Css("script[type='application/ld+json']"))
                .await
            {
                if !scripts.is_empty() {
                    debug!("Found {} JSON-LD script(s)", scripts.len());
                    break;
                }
            }
            sleep(Duration::from_millis(500)).await;
        }
    })
    .await;

    // Extract data from page source
    debug!("Getting page source...");
    let page_source = driver.source().await?;

    // Try extracting data from Airbnb's JSON first (most reliable)
    debug!("Attempting JSON extraction...");
    let (
        mut title,
        mut description,
        mut price,
        mut rating,
        mut location,
        picture_url,
        mut features,
    ) = if let Some(json_data) = extract_airbnb_json_data(&page_source) {
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
            if [
                "AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "ID", "IL", "IN",
                "IA", "KS", "KY", "LA", "ME", "MD", "MA", "MI", "MN", "MS", "MO", "MT", "NE", "NV",
                "NH", "NJ", "NM", "NY", "NC", "ND", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN",
                "TX", "UT", "VT", "VA", "WA", "WV", "WI", "WY",
            ]
            .contains(&r.as_str())
            {
                (Some(r), Some("United States".to_string()))
            } else {
                (Some(r), None)
            }
        }
        (None, Some(c)) => (None, Some(c)),
        (None, None) => {
            // Try to infer from location
            if !location.is_empty() {
                if location.contains(", CA")
                    || location.contains(", NY")
                    || location.contains(", FL")
                {
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
        error!(
            "Listing missing title - title: '{}', description: '{}', price: '{}'",
            title, description, price
        );

        // Save page source for debugging
        if let Ok(page_source) = driver.source().await {
            let source_path = format!(
                "logs/invalid_listing_source_{}.html",
                chrono::Utc::now().timestamp()
            );
            if let Err(e) = std::fs::write(&source_path, page_source) {
                warn!("Failed to save page source: {}", e);
            } else {
                info!("Saved page source for debugging: {}", source_path);
            }
        }

        return Err(anyhow::anyhow!(
            "Listing missing title: title='{}', description='{}', price='{}'', location='{}''",
            title,
            description,
            price,
            location
        ));
    }

    // Log warning if description or price is missing but don't fail
    if description.is_empty() {
        warn!("Listing missing description for title: '{}'", title);
    }
    if price.is_empty() {
        warn!("Listing missing price for title: '{}'", title);
    }

    // Parse numeric price
    let price_numeric = price
        .trim_start_matches('$')
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

    info!(
        "Successfully scraped listing: title='{}', location='{}', price='{}'",
        title, location, price
    );

    Ok(listing)
}

pub async fn scrape_region(driver: &WebDriver, region: &str, country: &str) -> Result<i32> {
    use tracing::{error, info, warn};

    info!(
        "[SCRAPE_REGION] Scraping Airbnb listings for {}, {}",
        region, country
    );

    let place_urls = get_place_urls(
        driver,
        &format!("{}, {}", region, country),
        None,
        None,
        None,
    )
    .await?;
    if place_urls.is_empty() {
        warn!("No place URLs found for {}, {}", region, country);
        return Ok(0);
    }

    info!(
        "[SCRAPE_REGION] Found {} URLs to scrape for {}, {}",
        place_urls.len(),
        region,
        country
    );
    info!("[SCRAPE_REGION] Using SINGLE WebDriver instance for all URLs");

    let mut region_listings = Vec::new();
    let mut failed_count = 0;

    // Use the SAME driver for all URLs - no need to create new ones!
    for (i, url) in place_urls.iter().enumerate() {
        info!(
            "[SCRAPE_REGION] Scraping URL {}/{}: {}",
            i + 1,
            place_urls.len(),
            url
        );

        // Check session is still valid before each scrape
        if !check_session_valid(driver).await {
            error!(
                "[SCRAPE_REGION] WebDriver session died before scraping URL {}",
                i + 1
            );
            break;
        }

        match scrape_place_details(driver, url).await {
            Ok(mut details) => {
                // Ensure region and country are set, with fallbacks
                if details.region.is_none() || details.region.as_ref().unwrap().trim().is_empty() {
                    details.region = Some(region.to_string());
                }
                if details.country.is_none() || details.country.as_ref().unwrap().trim().is_empty()
                {
                    details.country = Some(country.to_string());
                }
                info!("[SCRAPE_REGION] Successfully scraped: {}", details.title);
                region_listings.push(details);
            }
            Err(e) => {
                warn!("[SCRAPE_REGION] Failed to scrape {}: {}", url, e);
                failed_count += 1;
            }
        }

        // Small delay between scrapes to be polite
        sleep(Duration::from_millis(1000)).await;
    }

    info!(
        "Successfully scraped {} listings, {} failed for {}, {}",
        region_listings.len(),
        failed_count,
        region,
        country
    );

    if region_listings.is_empty() {
        warn!("No valid listings scraped for {}, {}", region, country);
        return Ok(0);
    }

    let inserted_ids = crate::database::insert_many(region_listings).await?;
    let count = inserted_ids.len() as i32;
    info!(
        "Successfully inserted {} Airbnb listings for {}, {} (failed: {})",
        count, region, country, failed_count
    );

    Ok(count)
}

// ============================================================================
// STEALTH BROWSER VERSIONS - Using chromiumoxide CDP instead of WebDriver
// ============================================================================

#[cfg(test)]
mod url_tests {
    use super::*;

    // build_legacy_search_url was lifted verbatim out of get_place_urls. These
    // pin the exact emitted URL so the extraction is demonstrably behaviour
    // preserving, not just plausible.

    #[test]
    fn dated_search_url_is_exact() {
        let got = build_legacy_search_url("Paris", Some("2026-09-10"), Some("2026-09-17"), Some(2));
        let want = format!(
            "{}s/Paris/homes?refinement_paths%5B%5D=%2Fhomes&\
             query=Paris&\
             flexible_trip_lengths%5B%5D=one_week&\
             monthly_start_date=2026-09-01&monthly_length=3&monthly_end_date=2026-12-01&\
             search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=calendar&checkin=2026-09-10&checkout=2026-09-17&\
             source=structured_search_input_header&search_type=unknown&\
             adults=2",
            AIRBNB_BASE_URL
        );
        assert_eq!(got, want);
    }

    #[test]
    fn undated_search_url_is_exact() {
        let got = build_legacy_search_url("Paris", None, None, None);
        let want = format!(
            "{}s/Paris/homes?refinement_paths%5B%5D=%2Fhomes&\
             query=Paris&\
             flexible_trip_lengths%5B%5D=one_week&monthly_start_date=2025-01-01&monthly_length=12&\
             monthly_end_date=2026-01-01&search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=flexible_dates&source=structured_search_input_header&\
             search_type=unknown&adults=2",
            AIRBNB_BASE_URL
        );
        assert_eq!(got, want, "no guest count must default to 2 adults");
    }

    #[test]
    fn a_missing_half_of_the_date_range_falls_back_to_undated() {
        let only_in = build_legacy_search_url("Paris", Some("2026-09-10"), None, None);
        let neither = build_legacy_search_url("Paris", None, None, None);
        assert_eq!(only_in, neither, "one date alone is not a dated search");
    }

    // The path segment is encoded once, the query parameter twice. Collapsing
    // those to a single encoding silently changes what Airbnb is asked for.
    #[test]
    fn location_path_is_encoded_once_and_query_twice() {
        let url = build_legacy_search_url("New York", None, None, None);
        assert!(url.contains("s/New%20York/homes"), "path segment: {url}");
        assert!(url.contains("query=New%2520York"), "query parameter: {url}");
    }

    #[test]
    fn guest_count_is_carried_through() {
        let url = build_legacy_search_url("Paris", None, None, Some(6));
        assert!(url.ends_with("adults=6"), "{url}");
    }

    // ---- monthly_window ----

    #[test]
    fn monthly_window_starts_at_the_checkin_month() {
        let (start, len, end) = monthly_window("2026-09-10");
        assert_eq!(start, "2026-09-01");
        assert_eq!(len, "3");
        // +90 days from 2026-09-10 is 2026-12-09, so the first of that month.
        assert_eq!(end, "2026-12-01");
    }

    #[test]
    fn monthly_window_crosses_a_year_boundary() {
        let (start, _, end) = monthly_window("2026-11-15");
        assert_eq!(start, "2026-11-01");
        // +90 days lands in Feb 2027.
        assert_eq!(end, "2027-02-01");
    }

    #[test]
    fn monthly_window_falls_back_on_unparseable_input() {
        for bad in ["", "not-a-date", "10/09/2026", "2026-13-45"] {
            let (start, len, end) = monthly_window(bad);
            assert_eq!(
                (start.as_str(), len, end.as_str()),
                ("2025-01-01", "3", "2025-04-01"),
                "input {bad:?} should use the fixed fallback window"
            );
        }
    }
}
