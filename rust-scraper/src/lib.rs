// Library surface for rust-scraper. The binary is a thin wrapper over
// build_app: factoring the router out here lets integration tests exercise the
// middleware stack without spinning up a real listener or depending on
// MongoDB/Chrome.

pub mod models;
pub mod database;
pub mod stealth_browser;
pub mod routes;
pub mod scraping;
pub mod watchlist;

use axum::{
    extract::Request,
    http::{header, HeaderValue, Method, StatusCode},
    middleware::{self, Next},
    response::Response,
    routing::get,
    Router,
};
use tower_http::cors::{AllowOrigin, CorsLayer};

#[derive(Clone)]
pub struct ApiKey(pub String);

/// Require X-API-Key on every non-health request. The /health endpoint stays
/// open so Docker/load balancers can probe liveness without the secret.
pub async fn require_api_key(req: Request, next: Next) -> Result<Response, StatusCode> {
    if req.uri().path() == "/health" {
        return Ok(next.run(req).await);
    }
    let Some(expected) = req.extensions().get::<ApiKey>().cloned() else {
        return Err(StatusCode::INTERNAL_SERVER_ERROR);
    };
    let provided = req
        .headers()
        .get("x-api-key")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    if provided.is_empty() || !constant_time_eq(provided.as_bytes(), expected.0.as_bytes()) {
        return Err(StatusCode::UNAUTHORIZED);
    }
    Ok(next.run(req).await)
}

pub fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    let mut diff = 0u8;
    for (x, y) in a.iter().zip(b.iter()) {
        diff |= x ^ y;
    }
    diff == 0
}

async fn inject_api_key(
    api_key: ApiKey,
    mut req: Request,
    next: Next,
) -> Result<Response, StatusCode> {
    req.extensions_mut().insert(api_key);
    Ok(next.run(req).await)
}

/// Build the full axum Router with CORS + API-key enforcement applied.
///
/// `api_key` must be non-empty; callers (main.rs, tests) are responsible for
/// enforcing that invariant. `allowed_origins` of an empty slice means no
/// cross-origin browser access — equivalent to the default in prod.
pub fn build_app(api_key: ApiKey, allowed_origins: Vec<HeaderValue>) -> Router {
    let cors = if allowed_origins.is_empty() {
        CorsLayer::new()
            .allow_methods([Method::GET, Method::POST])
            .allow_headers([header::CONTENT_TYPE, header::HeaderName::from_static("x-api-key")])
    } else {
        CorsLayer::new()
            .allow_origin(AllowOrigin::list(allowed_origins))
            .allow_methods([Method::GET, Method::POST])
            .allow_headers([header::CONTENT_TYPE, header::HeaderName::from_static("x-api-key")])
    };

    Router::new()
        .route("/scrape-north-america", get(routes::scrape_north_america))
        .route("/scrape-city-data", get(routes::scrape_city_data))
        .route("/scrape/status", get(routes::scrape_status))
        .route("/get-listings", get(routes::get_listings_without_limit))
        .route("/get-all-listings", get(routes::get_all_listings))
        .route("/filters", get(routes::filters))
        .route("/get-listings/{city}/{limit}", get(routes::get_listings))
        .route("/get-listings/{city}", get(routes::get_listings_without_limit))
        .route("/get-listing/{listing_id}", get(routes::get_listing))
        .route("/info", get(routes::info))
        .route("/health", get(routes::health))
        .route("/test-email", get(routes::test_email))
        .route("/test-stealth", get(routes::test_stealth_browser))
        .layer(middleware::from_fn(require_api_key))
        .layer(middleware::from_fn({
            let key = api_key.clone();
            move |req, next| inject_api_key(key.clone(), req, next)
        }))
        .layer(cors)
}
