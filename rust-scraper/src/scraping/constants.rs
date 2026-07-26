// Split out of the former monolithic scraping.rs.

pub(crate) const MAX_CONCURRENT_SCRAPES: usize = 1; // Reduced to 1 for stability - multiple browsers cause session conflicts
pub(crate) const AIRBNB_BASE_URL: &str = "https://www.airbnb.com/";

// Browser viewport sizes for realistic sessions. Unlike the UA/platform
// claims, viewport is not cross-referenced against anything, so it can still be
// drawn independently.
pub const VIEWPORT_SIZES: &[(u32, u32)] = &[
    (1920, 1080),
    (1366, 768),
    (1536, 864),
    (1440, 900),
    (1280, 720),
    (1600, 900),
    (2560, 1440),
];

// USER_AGENTS / ACCEPT_HEADERS / SEC_CH_UA_VALUES used to live here as three
// separate lists sampled independently, which let a Firefox UA ship with Chrome
// client hints. They are now fields on a single `BrowserProfile`; see
// `scraping::profile`.

/// Airbnb never returns more than ~270-300 results for one search query. When a
/// tile returns this many listings we assume results are truncated and split it.
///
/// This is a BACKSTOP, not the primary signal — see AIRBNB_SEARCH_PAGE_CAP.
/// A saturated tile yields 15 pages x 18 = 270 raw results, but the harvest
/// de-duplicates across pages and drops entries that fail to parse, so a real
/// saturated tile lands around 230. Keying subdivision on this number alone
/// meant a full tile measured 230, compared 230 >= 270, and declined to split.
pub(crate) const TILE_SPLIT_THRESHOLD: usize = 270;
/// Pages Airbnb offers for a maxed-out search. `paginationInfo.pageCursors`
/// carries exactly this many entries when results are capped, which is the
/// authoritative truncation signal: it counts what Airbnb withheld rather than
/// what we managed to keep.
pub(crate) const AIRBNB_SEARCH_PAGE_CAP: usize = 15;
/// Attempts for a page that came back blocked (503 / challenge) before the
/// scrape gives up. Paginating multiplied requests per tile by ~15x, which is
/// what started drawing 503s on a direct connection.
pub(crate) const BLOCKED_PAGE_RETRIES: usize = 3;
/// First backoff after a block; doubles per retry (30s, 60s, 120s). Airbnb's
/// throttling is measured in minutes, so retrying in milliseconds just spends
/// the remaining attempts without letting the limit decay.
pub(crate) const BLOCKED_BACKOFF_SECS: u64 = 30;
/// Max recursion depth for bounding-box subdivision (4^6 = 4096 tiles worst case).
pub(crate) const MAX_TILE_DEPTH: usize = 6;
/// Pages to follow per search before giving up. A page holds ~18 results and
/// Airbnb truncates a single query around 270-300, so ~15 pages reaches the
/// cap; the limit is a runaway guard, not the normal stopping condition
/// (an absent next-page cursor is).
pub(crate) const MAX_SEARCH_PAGES: usize = 16;
/// Concurrent HTTP enrichment fetches. Unlike browsers, these don't conflict.
pub(crate) const ENRICH_CONCURRENCY: usize = 10;
