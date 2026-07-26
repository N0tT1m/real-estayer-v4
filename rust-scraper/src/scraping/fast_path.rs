// Split out of the former monolithic scraping.rs. Code is unchanged;
// only visibility was widened so cross-module calls resolve.

use super::*;
use crate::models::Listing;
use crate::stealth_browser::StealthDriver;
use anyhow::Result;
use futures::stream::StreamExt;
use std::collections::HashSet;
use std::sync::atomic::{AtomicUsize, Ordering as AtomicOrdering};
use tokio::time::{sleep, Duration};

/// Build the name-based Airbnb search URL for the fast path.
///
/// NOT identical to `build_stealth_search_url`: this one omits the `monthly_*`
/// window that the stealth path sends. See that function's doc comment for the
/// full three-way comparison.
pub(crate) fn build_search_url(
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
    let mut url = if let (Some(checkin), Some(checkout)) = (check_in, check_out) {
        format!(
            "{}s/{}/homes?refinement_paths%5B%5D=%2Fhomes&query={}&\
             search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=calendar&checkin={}&checkout={}&\
             source=structured_search_input_header&search_type=unknown&{}",
            AIRBNB_BASE_URL,
            urlencoding::encode(location),
            urlencoding::encode(location),
            checkin,
            checkout,
            guest_params_str
        )
    } else {
        format!(
            "{}s/{}/homes?refinement_paths%5B%5D=%2Fhomes&query={}&\
             search_mode=regular_search&price_filter_input_type=2&channel=EXPLORE&\
             date_picker_type=flexible_dates&source=structured_search_input_header&\
             search_type=unknown&{}",
            AIRBNB_BASE_URL,
            urlencoding::encode(location),
            urlencoding::encode(location),
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
pub(crate) fn build_map_search_url(
    location: &str,
    bbox: &BoundingBox,
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
    let mut url = format!(
        "{}s/{}/homes?refinement_paths%5B%5D=%2Fhomes&query={}&search_by_map=true&\
         search_mode=regular_search&channel=EXPLORE&source=structured_search_input_header&\
         search_type=user_map_move&ne_lat={}&ne_lng={}&sw_lat={}&sw_lng={}&zoom=12&{}",
        AIRBNB_BASE_URL,
        urlencoding::encode(location),
        urlencoding::encode(location),
        bbox.ne_lat,
        bbox.ne_lng,
        bbox.sw_lat,
        bbox.sw_lng,
        guest_params_str
    );
    if !amenity_params_str.is_empty() {
        url.push('&');
        url.push_str(&amenity_params_str);
    }
    url
}

/// Navigate to a search URL, scroll to load lazy content, and harvest every
/// listing present in the embedded JSON, along with Airbnb's next-page cursor.
/// Callers want `harvest_search_all_pages`; this is one page of that walk.
async fn harvest_search_page_paged(
    driver: &StealthDriver,
    url: &str,
) -> (Vec<Listing>, Vec<String>) {
    use tracing::{info, warn};
    info!("[FAST] Harvesting search page: {}", url);
    if let Err(e) = driver.goto(url).await {
        warn!("[FAST] Failed to navigate to {}: {}", url, e);
        return (Vec::new(), Vec::new());
    }
    sleep(Duration::from_secs(5)).await;
    for i in 1..=5 {
        let _ = driver
            .execute_script(&format!("window.scrollBy(0, {});", 800 * i))
            .await;
        sleep(Duration::from_millis(1200)).await;
    }
    sleep(Duration::from_secs(2)).await;
    match driver.page_source().await {
        Ok(html) => match extract_airbnb_json_data(&html) {
            Some(json) => (
                extract_all_listings_from_json(&json),
                find_page_cursors(&json),
            ),
            None => {
                warn!("[FAST] No embedded JSON on page {}", url);
                (Vec::new(), Vec::new())
            }
        },
        Err(e) => {
            warn!("[FAST] Failed to read page source for {}: {}", url, e);
            (Vec::new(), Vec::new())
        }
    }
}

/// Walk every page of a search by following Airbnb's own pagination cursor.
///
/// One page carries roughly 18 results, while a single search query exposes
/// ~270-300 before Airbnb truncates it. Harvesting only the first page made
/// `TILE_SPLIT_THRESHOLD` (270) unreachable, so bounding-box subdivision never
/// fired and an entire continent came back as one tile of 18 listings.
///
/// The first response carries the cursors for every page, so page 1 tells us
/// how many pages exist; the rest are fetched by replaying those cursors.
///
/// Stops early on any page that contributes nothing new. That guard is what
/// makes an ignored or changed cursor param degrade to single-page behaviour
/// instead of re-harvesting page one `MAX_SEARCH_PAGES` times.
pub(crate) async fn harvest_search_all_pages(
    driver: &StealthDriver,
    base_url: &str,
) -> Vec<Listing> {
    use tracing::info;
    let mut out: Vec<Listing> = Vec::new();
    let mut seen: HashSet<String> = HashSet::new();

    let (first, cursors) = harvest_search_page_paged(driver, base_url).await;
    let first_returned = first.len();
    for listing in first {
        if seen.insert(listing.url.clone()) {
            out.push(listing);
        }
    }
    info!(
        "[FAST] page 1/{}: returned={} new={} (search reports {} pages)",
        MAX_SEARCH_PAGES,
        first_returned,
        out.len(),
        cursors.len().max(1),
    );

    // cursors[0] is the page just fetched.
    for (idx, cursor) in cursors.iter().enumerate().skip(1) {
        if idx >= MAX_SEARCH_PAGES {
            info!(
                "[FAST] stopping at the {}-page cap ({} cursors offered)",
                MAX_SEARCH_PAGES,
                cursors.len()
            );
            break;
        }
        let url = format!(
            "{}&cursor={}&pagination_search=true",
            base_url,
            urlencoding::encode(cursor)
        );
        let (listings, _) = harvest_search_page_paged(driver, &url).await;
        let returned = listings.len();
        let mut added = 0;
        for listing in listings {
            if seen.insert(listing.url.clone()) {
                out.push(listing);
                added += 1;
            }
        }
        info!(
            "[FAST] page {}/{}: returned={} new={} total={}",
            idx + 1,
            cursors.len(),
            returned,
            added,
            out.len()
        );
        if added == 0 {
            info!("[FAST] page {} added nothing new - stopping", idx + 1);
            break;
        }
    }
    out
}

/// Phase 3: recursively harvest a bounding box, splitting into quadrants whenever
/// a tile returns enough results to look truncated. Deduplicates by URL via `seen`.
// These take a search's full parameter set (location, dates, guest counts,
// filters, limits) which genuinely belongs together at the call site.
// Grouping them into a params struct would be an improvement, but it is a
// signature change across the scrape entry points rather than a lint fix.
#[allow(clippy::too_many_arguments)]
pub(crate) async fn collect_listings_tiled(
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
    let tile_listings = harvest_search_all_pages(driver, &url).await;
    let returned = tile_listings.len();
    let mut added = 0;
    for listing in tile_listings {
        if seen.insert(listing.url.clone()) {
            out.push(listing);
            added += 1;
        }
    }
    info!(
        "[TILE] depth={} returned={} new={} total={}",
        depth,
        returned,
        added,
        out.len()
    );

    if returned >= TILE_SPLIT_THRESHOLD && depth < MAX_TILE_DEPTH {
        info!(
            "[TILE] tile at depth {} looks truncated ({} results) - subdividing",
            depth, returned
        );
        for sub in bbox.quarters() {
            Box::pin(collect_listings_tiled(
                driver,
                location,
                sub,
                guests,
                amenity_filter,
                depth + 1,
                seen,
                out,
            ))
            .await;
        }
    }
}

/// Build a reqwest client with browser-like headers.
///
/// This used to force `Accept-Encoding: identity` because the reqwest build had
/// no decompression features, which made it the single most conspicuous header
/// we sent — no real browser ever asks for uncompressed HTML. The gzip/brotli/
/// deflate/zstd features are now enabled in Cargo.toml, so the profile's real
/// `Accept-Encoding` goes out and reqwest transparently decompresses the reply.
pub(crate) fn build_http_client() -> Result<reqwest::Client> {
    build_http_client_with_proxy(proxy::next())
}

/// [`build_http_client`] against a specific proxy (`None` for a direct
/// connection). Split out so the caller can spread a run across several egress
/// addresses; see [`proxy`].
pub(crate) fn build_http_client_with_proxy(proxy_url: Option<String>) -> Result<reqwest::Client> {
    use reqwest::header::{HeaderMap, HeaderName, HeaderValue};
    let mut headers = HeaderMap::new();
    for (key, value) in get_realistic_headers() {
        if let (Ok(name), Ok(val)) = (
            HeaderName::from_bytes(key.as_bytes()),
            HeaderValue::from_str(&value),
        ) {
            headers.insert(name, val);
        }
    }

    let mut builder = reqwest::Client::builder()
        .default_headers(headers)
        .timeout(Duration::from_secs(30))
        // Chrome always negotiates HTTP/2 with a site like Airbnb. This build had
        // reqwest's `http2` feature off (a consequence of default-features =
        // false), so it spoke HTTP/1.1 only while claiming to be Chrome — visible
        // both in the ALPN list we advertise during the handshake and to the
        // server directly. Enabled in Cargo.toml; kept off the 1.1-only path.
        //
        // Chrome also keeps a cookie jar. Without one we never echo back the
        // session cookies Airbnb sets, so every request looks like a first visit
        // from a browser that should have state.
        .cookie_store(true);

    if let Some(url) = proxy_url {
        // all() covers http, https, and CONNECT tunnelling, which is what the
        // rotating-residential-proxy services hand out.
        match reqwest::Proxy::all(&url) {
            Ok(p) => {
                tracing::info!("[PROXY] HTTP client routed via {}", proxy::redact(&url));
                builder = builder.proxy(p);
            }
            Err(e) => {
                // A malformed entry must not silently downgrade the whole run to
                // a direct connection the operator did not ask for.
                return Err(anyhow::anyhow!(
                    "invalid proxy {}: {}",
                    proxy::redact(&url),
                    e
                ));
            }
        }
    }

    Ok(builder.build()?)
}

/// Fetch a listing's /rooms page over HTTP and fill the fields the search JSON
/// lacks (full amenities, house details, region/country). No browser involved.
pub(crate) async fn enrich_listing_http(client: &reqwest::Client, mut listing: Listing) -> Listing {
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
                debug!(
                    "[ENRICH] {} -> {} features",
                    listing.url,
                    listing.features.len()
                );
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
            warn!(
                "[ENRICH] Could not build HTTP client, skipping enrichment: {}",
                e
            );
            return listings;
        }
    };
    info!(
        "[ENRICH] Enriching {} listings with concurrency {}",
        listings.len(),
        concurrency
    );
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
// These take a search's full parameter set (location, dates, guest counts,
// filters, limits) which genuinely belongs together at the call site.
// Grouping them into a params struct would be an improvement, but it is a
// signature change across the scrape entry points rather than a lint fix.
#[allow(clippy::too_many_arguments)]
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
    let base_url = build_search_url(
        location,
        check_in,
        check_out,
        &guests,
        amenity_filter.as_ref(),
    );
    let mut base = harvest_search_all_pages(driver, &base_url).await;
    info!(
        "[FAST] Phase 1 harvested {} listings for {}",
        base.len(),
        location
    );

    // Phase 3: subdivide by map bounding box for fuller coverage.
    let mut listings = if tiled {
        match BoundingBox::from_listings(&base) {
            Some(bbox) => {
                info!("[FAST] Phase 3 tiling bbox {:?}", bbox);
                let mut seen: HashSet<String> = base.iter().map(|l| l.url.clone()).collect();
                let mut out = std::mem::take(&mut base);
                collect_listings_tiled(
                    driver,
                    location,
                    bbox,
                    &guests,
                    amenity_filter.as_ref(),
                    0,
                    &mut seen,
                    &mut out,
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
            sw_lat: 30.20,
            sw_lng: -97.85,
            ne_lat: 30.35,
            ne_lng: -97.68,
        }),
        // Continental United States (excludes Alaska/Hawaii).
        "usa" | "us" | "continental-us" => Some(BoundingBox {
            sw_lat: 24.5,
            sw_lng: -125.0,
            ne_lat: 49.5,
            ne_lng: -66.9,
        }),
        "north-america" | "na" => Some(BoundingBox {
            sw_lat: 14.0,
            sw_lng: -168.0,
            ne_lat: 72.0,
            ne_lng: -52.0,
        }),
        "world" => Some(BoundingBox {
            sw_lat: -56.0,
            sw_lng: -180.0,
            ne_lat: 72.0,
            ne_lng: 180.0,
        }),
        _ => None,
    }
}

/// Recursively harvest a bounding box, inserting each tile's fresh listings to
/// the database as it goes (bounded memory), and subdividing tiles that hit the
/// result cap. `seen` deduplicates URLs across the whole run; the atomics report
/// running progress to the caller.
// These take a search's full parameter set (location, dates, guest counts,
// filters, limits) which genuinely belongs together at the call site.
// Grouping them into a params struct would be an improvement, but it is a
// signature change across the scrape entry points rather than a lint fix.
#[allow(clippy::too_many_arguments)]
pub(crate) async fn tile_and_store(
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
    let tile_listings = harvest_search_all_pages(driver, &url).await;
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
        depth,
        returned,
        fresh_count,
        tiles_processed.load(AtomicOrdering::SeqCst),
        inserted_total.load(AtomicOrdering::SeqCst)
    );

    // If the tile looks truncated, subdivide to reach the hidden listings.
    if returned >= TILE_SPLIT_THRESHOLD && depth < max_depth {
        for sub in bbox.quarters() {
            Box::pin(tile_and_store(
                driver,
                location,
                sub,
                guests,
                amenity_filter,
                depth + 1,
                max_depth,
                enrich,
                seen,
                inserted_total,
                tiles_processed,
            ))
            .await;
        }
    }
}

/// Top-level "scrape everything in this box" driver. Seeds recursive map-tiling
/// with an arbitrary bounding box and streams results to the database. Returns
/// (listings_inserted, tiles_processed).
// These take a search's full parameter set (location, dates, guest counts,
// filters, limits) which genuinely belongs together at the call site.
// Grouping them into a params struct would be an improvement, but it is a
// signature change across the scrape entry points rather than a lint fix.
#[allow(clippy::too_many_arguments)]
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
        driver,
        location,
        bbox,
        &guests,
        amenity_filter.as_ref(),
        0,
        max_depth,
        enrich,
        &mut seen,
        inserted_total,
        tiles_processed,
    )
    .await;
    let inserted = inserted_total.load(AtomicOrdering::SeqCst);
    let tiles = tiles_processed.load(AtomicOrdering::SeqCst);
    info!(
        "[REGION] Complete: {} listings inserted across {} tiles",
        inserted, tiles
    );
    Ok((inserted, tiles))
}

#[cfg(test)]
mod fast_path_tests {
    use super::*;
    // Test-only: these were covered by the old monolith's file-wide imports.
    use crate::models::Coordinates;
    use base64::Engine as _;

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
        assert_eq!(
            decode_listing_id(&encoded),
            Some("633486763306530765".to_string())
        );
        assert_eq!(decode_listing_id("not base64!!!"), None);
    }

    #[test]
    fn bounding_box_quarters_partition_without_gaps() {
        let bbox = BoundingBox {
            sw_lat: 0.0,
            sw_lng: 0.0,
            ne_lat: 4.0,
            ne_lng: 8.0,
        };
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
        a.coordinates = Some(Coordinates {
            lat: 10.0,
            lng: 20.0,
        });
        let mut b = Listing_stub();
        b.coordinates = Some(Coordinates {
            lat: 12.0,
            lng: 24.0,
        });
        let bbox = BoundingBox::from_listings(&[a.clone(), b]).expect("two coords -> bbox");
        assert!(bbox.sw_lat < 10.0 && bbox.ne_lat > 12.0); // padded outward
        assert!(bbox.sw_lng < 20.0 && bbox.ne_lng > 24.0);
        // A single coordinate cannot define an area.
        assert!(BoundingBox::from_listings(&[a]).is_none());
    }

    #[test]
    fn extract_all_listings_from_json_builds_full_listings() {
        let encoded_id =
            base64::engine::general_purpose::STANDARD.encode("DemandStayListing:12345");
        // Real current shape: niobeClientData is [[ _, {data} ]] and the
        // coordinate is nested under demandStayListing.location.coordinate.
        let json = serde_json::json!({
            "niobeClientData": [[
                "ignored",
                { "data": { "presentation": { "staysSearch": { "results": { "searchResults": [
                    {
                        "demandStayListing": {
                            "id": encoded_id,
                            "location": { "coordinate": { "latitude": 44.76, "longitude": -85.62 } },
                            "description": { "name": {
                                "localizedStringWithTranslationPreference": "Cozy Cabin"
                            }}
                        },
                        "title": "Cabin in Traverse City",
                        "structuredDisplayPrice": { "primaryLine": { "price": "$150 night" } },
                        "avgRatingLocalized": "4.90 (128)",
                        "contextualPictures": [ { "picture": "https://img/1.jpg" } ],
                        "structuredContent": { "primaryLine": [ { "body": "2 beds" } ] },
                        "badges": [ { "text": "Guest favorite" } ]
                    }
                ]}}}}}
            ]]
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
