//! Airbnb scraping. Formerly a single 4,945-line `scraping.rs`; split by
//! concern with the code itself unchanged.
//!
//! | module         | what lives here                                        |
//! |----------------|--------------------------------------------------------|
//! | `constants`    | user agents, viewports, tiling thresholds              |
//! | `browser`      | WebDriver session setup, popups, scrolling, DOM reads  |
//! | `extract`      | pure HTML/JSON parsers (no driver, easily testable)    |
//! | `amenities`    | amenity/house-detail normalisation lookup tables       |
//! | `params`       | `GuestParams`, `AmenityFilter`, `BoundingBox`          |
//! | `email`        | SMTP run-report                                        |
//! | `fast_path`    | search-JSON harvest + map tiling + parallel enrichment |
//! | `browser_flow` | legacy serial WebDriver scrape                         |
//! | `stealth_flow` | stealth-driver variants of the above                   |
//!
//! Everything is re-exported below, so `crate::scraping::X` resolves exactly
//! as it did when this was one file.

mod amenities;
mod browser;
mod browser_flow;
mod constants;
mod email;
mod extract;
mod fast_path;
mod params;
mod stealth_flow;

// Re-exported so `crate::scraping::X` keeps resolving exactly as it did
// when this module was a single file.
pub(crate) use amenities::*;
pub use browser::*;
pub use browser_flow::*;
pub use constants::*;
pub use email::*;
pub use extract::*;
pub use fast_path::*;
pub use params::*;
pub use stealth_flow::*;
