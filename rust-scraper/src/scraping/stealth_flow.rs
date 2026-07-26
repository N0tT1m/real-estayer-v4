// Split out of the former monolithic scraping.rs. Code is unchanged;
// only visibility was widened so cross-module calls resolve.

use super::*;
use crate::models::Listing;
use crate::stealth_browser::StealthDriver;
use anyhow::Result;
use std::collections::HashSet;
use tokio::time::{sleep, timeout, Duration};

/// Get listing URLs using the stealth browser (CDP-based, harder to detect)
/// Build the search URL for the stealth WebDriver path.
///
/// There are three search-URL builders and they are deliberately NOT the same.
/// This one single-encodes the query and sends the guest breakdown, amenity
/// filters, and the `monthly_*` window. `build_legacy_search_url` in
/// browser_flow double-encodes the query and sends only `adults=`.
/// `build_search_url` in fast_path matches this one except it omits the
/// `monthly_*` window entirely.
///
/// Merging them would change which listings Airbnb returns on each path, so
/// they stay separate until someone decides that deliberately.
pub(crate) fn build_stealth_search_url(
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
    let amenity_params_str = amenity_filter
        .map(|f| f.to_url_params())
        .unwrap_or_default();
    let encoded = urlencoding::encode(location);

    let base_url = match (check_in, check_out) {
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
                 {}",
                AIRBNB_BASE_URL, encoded, encoded,
                monthly_start, monthly_length, monthly_end,
                checkin, checkout, guest_params_str
            )
        }
        _ => format!(
            "{}s/{}/homes?refinement_paths%5B%5D=%2Fhomes&\
             query={}&\
             flexible_trip_lengths%5B%5D=one_week&monthly_start_date=2025-01-01&monthly_length=12&\
             monthly_end_date=2026-01-01&search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=flexible_dates&source=structured_search_input_header&\
             search_type=unknown&{}",
            AIRBNB_BASE_URL, encoded, encoded, guest_params_str
        ),
    };

    if amenity_params_str.is_empty() {
        base_url
    } else {
        format!("{}&{}", base_url, amenity_params_str)
    }
}

pub async fn get_place_urls_stealth(
    driver: &StealthDriver,
    location: &str,
    check_in_date: Option<&str>,
    check_out_date: Option<&str>,
    guest_params: Option<GuestParams>,
    amenity_filter: Option<AmenityFilter>,
) -> Result<Vec<String>> {
    use tracing::{debug, error, info, span, warn, Level};

    let span = span!(Level::INFO, "get_place_urls_stealth", location = %location);
    let _enter = span.enter();

    info!(
        "[STEALTH] Starting URL collection for location: {}",
        location
    );

    let guests = guest_params.unwrap_or_else(|| GuestParams::new(2, 0, 0, 0));

    // Log active filters
    if let Some(ref filter) = amenity_filter {
        if filter.has_filters() {
            info!(
                "[STEALTH] Active amenity filters - Hot tub: {}, Pool: {}, Waterfront: {}",
                filter.hot_tub, filter.pool, filter.waterfront
            );
        }
    }

    let search_url = build_stealth_search_url(
        location,
        check_in_date,
        check_out_date,
        &guests,
        amenity_filter.as_ref(),
    );

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
                info!(
                    "[STEALTH] Regex extraction added {} more URLs (total: {})",
                    urls.len() - before,
                    urls.len()
                );
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
                    info!(
                        "[STEALTH] JS extraction added {} more URLs (total: {})",
                        urls.len() - before,
                        urls.len()
                    );
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

    info!(
        "[STEALTH] Starting pagination check. Current URLs: {}",
        urls.len()
    );

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
                        info!(
                            "[STEALTH] No pagination element found on page {}",
                            page_number
                        );
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

            info!(
                "[STEALTH] Page {} added {} new URLs (total: {})",
                page_number,
                urls.len() - before,
                urls.len()
            );

            if urls.len() == before {
                info!("[STEALTH] No new URLs on page {}, stopping", page_number);
                break;
            }
        }
    }

    let url_list: Vec<String> = urls.into_iter().collect();
    info!(
        "[STEALTH] URL collection complete. Total unique URLs: {}",
        url_list.len()
    );

    // Log first few URLs
    for (i, url) in url_list.iter().take(5).enumerate() {
        debug!("[STEALTH] URL {}: {}", i + 1, url);
    }

    Ok(url_list)
}

/// Scrape individual listing details using stealth browser
pub async fn scrape_place_details_stealth(driver: &StealthDriver, url: &str) -> Result<Listing> {
    use tracing::{debug, error, info, span, warn, Level};

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
        match timeout(
            Duration::from_secs(30),
            driver.execute_script("window.scrollBy(0, 500);"),
        )
        .await
        {
            Ok(_) => {}
            Err(_) => {
                warn!(
                    "[STEALTH] Scroll {} timed out after 30s - skipping remaining scrolls",
                    i + 1
                );
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
        }
        Ok(Err(e)) => {
            error!("[STEALTH] Failed to get page source: {}", e);
            return Err(e);
        }
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

    let description = extract_description_from_source(&page_source).unwrap_or_default();

    let picture_url = extract_picture_url_from_source(&page_source).unwrap_or_default();

    let price = extract_price_from_source(&page_source).unwrap_or_default();

    let location = extract_location_from_source(&page_source).unwrap_or_default();

    let rating = extract_rating_from_source(&page_source).unwrap_or_default();

    let features = extract_amenities_from_source(&page_source);

    let house_details = extract_house_details_from_source(&page_source);

    // Extract region and country from page source
    let (region, country) = extract_region_country_from_source(&page_source);

    // Parse numeric price
    let price_numeric = price
        .trim_start_matches('$')
        .replace(',', "")
        .parse::<f64>()
        .ok();

    // Parse numeric rating
    let rating_numeric = rating.parse::<f64>().ok();

    info!(
        "[STEALTH] Extracted: title='{}', price='{}', rating='{}'",
        title, price, rating
    );

    // Validate we got meaningful data
    if title.is_empty() {
        warn!(
            "[STEALTH] Failed to extract title for listing: {}",
            full_url
        );
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

#[cfg(test)]
mod stealth_url_tests {
    use super::*;

    fn guests() -> GuestParams {
        GuestParams::new(2, 1, 0, 1)
    }

    // Lifted verbatim out of get_place_urls_stealth; these pin the exact
    // output so the extraction is demonstrably behaviour preserving.
    #[test]
    fn dated_stealth_url_is_exact() {
        let got = build_stealth_search_url(
            "Paris",
            Some("2026-09-10"),
            Some("2026-09-17"),
            &guests(),
            None,
        );
        let want = format!(
            "{}s/Paris/homes?refinement_paths%5B%5D=%2Fhomes&\
             query=Paris&\
             flexible_trip_lengths%5B%5D=one_week&\
             monthly_start_date=2026-09-01&monthly_length=3&monthly_end_date=2026-12-01&\
             search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=calendar&checkin=2026-09-10&checkout=2026-09-17&\
             source=structured_search_input_header&search_type=unknown&\
             adults=2&children=1&infants=0&pets=1",
            AIRBNB_BASE_URL
        );
        assert_eq!(got, want);
    }

    #[test]
    fn undated_stealth_url_is_exact() {
        let got = build_stealth_search_url("Paris", None, None, &guests(), None);
        let want = format!(
            "{}s/Paris/homes?refinement_paths%5B%5D=%2Fhomes&\
             query=Paris&\
             flexible_trip_lengths%5B%5D=one_week&monthly_start_date=2025-01-01&monthly_length=12&\
             monthly_end_date=2026-01-01&search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=flexible_dates&source=structured_search_input_header&\
             search_type=unknown&adults=2&children=1&infants=0&pets=1",
            AIRBNB_BASE_URL
        );
        assert_eq!(got, want);
    }

    #[test]
    fn amenity_filters_are_appended_only_when_set() {
        let none = build_stealth_search_url("Paris", None, None, &guests(), None);
        assert!(
            !none.ends_with('&'),
            "no trailing separator when unfiltered"
        );

        let empty = AmenityFilter::new(false, false, false);
        assert_eq!(
            build_stealth_search_url("Paris", None, None, &guests(), Some(&empty)),
            none,
            "an all-false filter must not alter the URL"
        );

        let filtered = AmenityFilter::new(true, false, false);
        let with = build_stealth_search_url("Paris", None, None, &guests(), Some(&filtered));
        assert!(with.starts_with(&none), "filters append to the base URL");
        assert!(with.len() > none.len(), "filter params were not appended");
    }

    // Unlike the legacy path, stealth encodes the query parameter ONCE.
    #[test]
    fn stealth_encodes_query_once_unlike_legacy() {
        let url = build_stealth_search_url("New York", None, None, &guests(), None);
        assert!(url.contains("s/New%20York/homes"), "{url}");
        assert!(
            url.contains("query=New%20York"),
            "stealth single-encodes: {url}"
        );
        assert!(
            !url.contains("query=New%2520York"),
            "must not double-encode: {url}"
        );
    }

    #[test]
    fn guest_breakdown_is_carried_through() {
        let g = GuestParams::new(4, 2, 1, 3);
        let url = build_stealth_search_url("Paris", None, None, &g, None);
        assert!(
            url.contains("adults=4&children=2&infants=1&pets=3"),
            "{url}"
        );
    }
}
