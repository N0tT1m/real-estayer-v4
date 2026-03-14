// Library file for rust-scraper to support testing
pub mod models;
pub mod database;
pub mod stealth_browser;  // Must be before routes which uses it
pub mod routes;
pub mod scraping;
pub mod watchlist;

use axum::{
    routing::get,
    Router,
};
use tower_http::cors::{Any, CorsLayer};

// Export functions needed for testing
pub async fn create_app() -> Router {
    let cors = CorsLayer::new()
        .allow_origin(Any)
        .allow_methods(Any)
        .allow_headers(Any);

    Router::new()
        .route("/scrape-north-america", get(routes::scrape_north_america))
        .route("/scrape-city-data", get(routes::scrape_city_data))
        .route("/get-listings", get(routes::get_listings_without_limit))
        .route("/get-all-listings", get(routes::get_all_listings))
        .route("/filters", get(routes::filters))
        .route("/get-listings/:city/:limit", get(routes::get_listings))
        .route("/get-listings/:city", get(routes::get_listings_without_limit))
        .route("/get-listing/:listing_id", get(routes::get_listing))
        .route("/info", get(routes::info))
        .route("/health", get(routes::health))
        .route("/test-email", get(routes::test_email))
        .route("/test-stealth", get(routes::test_stealth_browser))
        .layer(cors)
}
