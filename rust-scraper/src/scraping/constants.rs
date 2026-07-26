// Split out of the former monolithic scraping.rs. Code is unchanged;
// only visibility was widened so cross-module calls resolve.

pub(crate) const MAX_CONCURRENT_SCRAPES: usize = 1; // Reduced to 1 for stability - multiple browsers cause session conflicts
pub(crate) const AIRBNB_BASE_URL: &str = "https://www.airbnb.com/";

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
    (1920, 1080),
    (1366, 768),
    (1536, 864),
    (1440, 900),
    (1280, 720),
    (1600, 900),
    (2560, 1440),
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
    "\"Firefox\";v=\"133\", \"Not A(Brand\";v=\"24\", \"Chromium\";v=\"133\"",
];

/// Airbnb never returns more than ~270-300 results for one search query. When a
/// tile returns this many listings we assume results are truncated and split it.
pub(crate) const TILE_SPLIT_THRESHOLD: usize = 270;
/// Max recursion depth for bounding-box subdivision (4^6 = 4096 tiles worst case).
pub(crate) const MAX_TILE_DEPTH: usize = 6;
/// Pages to follow per search before giving up. A page holds ~18 results and
/// Airbnb truncates a single query around 270-300, so ~15 pages reaches the
/// cap; the limit is a runaway guard, not the normal stopping condition
/// (an absent next-page cursor is).
pub(crate) const MAX_SEARCH_PAGES: usize = 16;
/// Concurrent HTTP enrichment fetches. Unlike browsers, these don't conflict.
pub(crate) const ENRICH_CONCURRENCY: usize = 10;
