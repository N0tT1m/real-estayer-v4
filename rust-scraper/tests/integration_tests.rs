//! Integration tests for the scraper's HTTP surface.
//!
//! These exercise the middleware stack — API-key enforcement, the /health
//! carve-out, and CORS headers — by calling the router directly via
//! `tower::ServiceExt::oneshot`. We deliberately avoid `axum-test` because it
//! targets axum 0.7 while this crate is on 0.8.
//!
//! Endpoints that talk to MongoDB or Chrome aren't exercised here; those are
//! environment-dependent and belong in a separate e2e suite.

use axum::{
    body::Body,
    http::{HeaderValue, Request, StatusCode},
};
use http_body_util::BodyExt;
use rust_scraper::{build_app, constant_time_eq, ApiKey};
use tower::ServiceExt;

const TEST_KEY: &str = "test-scraper-key-0123456789abcdef";

fn app() -> axum::Router {
    build_app(ApiKey(TEST_KEY.to_string()), vec![])
}

async fn send(req: Request<Body>) -> (StatusCode, String) {
    let resp = app().oneshot(req).await.expect("router handled request");
    let status = resp.status();
    let body = resp
        .into_body()
        .collect()
        .await
        .expect("collect body")
        .to_bytes();
    (status, String::from_utf8_lossy(&body).to_string())
}

#[tokio::test]
async fn health_is_public() {
    let req = Request::builder().uri("/health").body(Body::empty()).unwrap();
    let (status, _) = send(req).await;
    assert_eq!(status, StatusCode::OK, "health must not require the API key");
}

#[tokio::test]
async fn info_requires_api_key() {
    let req = Request::builder().uri("/info").body(Body::empty()).unwrap();
    let (status, _) = send(req).await;
    assert_eq!(status, StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn info_accepts_correct_api_key() {
    let req = Request::builder()
        .uri("/info")
        .header("x-api-key", TEST_KEY)
        .body(Body::empty())
        .unwrap();
    let (status, _) = send(req).await;
    // /info is a cheap handler that doesn't hit the DB, so OK is expected.
    assert_eq!(status, StatusCode::OK);
}

#[tokio::test]
async fn info_rejects_wrong_api_key() {
    let req = Request::builder()
        .uri("/info")
        .header("x-api-key", "wrong-key")
        .body(Body::empty())
        .unwrap();
    let (status, _) = send(req).await;
    assert_eq!(status, StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn info_rejects_empty_api_key() {
    let req = Request::builder()
        .uri("/info")
        .header("x-api-key", "")
        .body(Body::empty())
        .unwrap();
    let (status, _) = send(req).await;
    assert_eq!(status, StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn scrape_city_data_requires_api_key() {
    // /scrape-city-data is a write/trigger endpoint; verify it sits behind
    // the same gate even with query params present.
    let req = Request::builder()
        .uri("/scrape-city-data?city=Austin")
        .body(Body::empty())
        .unwrap();
    let (status, _) = send(req).await;
    assert_eq!(status, StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn unknown_route_with_valid_key_is_404_not_401() {
    // Confirms the API-key middleware runs before route matching short-circuits
    // on missing paths — a 404 here means auth passed.
    let req = Request::builder()
        .uri("/nonexistent-endpoint")
        .header("x-api-key", TEST_KEY)
        .body(Body::empty())
        .unwrap();
    let (status, _) = send(req).await;
    assert_eq!(status, StatusCode::NOT_FOUND);
}

#[tokio::test]
async fn cors_allowed_origin_echoed() {
    // When ALLOWED_ORIGINS includes the request's Origin, the CORS layer
    // should echo it back on a preflight.
    let app = build_app(
        ApiKey(TEST_KEY.to_string()),
        vec![HeaderValue::from_static("https://app.example.com")],
    );
    let req = Request::builder()
        .method("OPTIONS")
        .uri("/info")
        .header("origin", "https://app.example.com")
        .header("access-control-request-method", "GET")
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    let allow = resp
        .headers()
        .get("access-control-allow-origin")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    assert_eq!(allow, "https://app.example.com");
}

#[tokio::test]
async fn cors_disallowed_origin_not_echoed() {
    let app = build_app(
        ApiKey(TEST_KEY.to_string()),
        vec![HeaderValue::from_static("https://app.example.com")],
    );
    let req = Request::builder()
        .method("OPTIONS")
        .uri("/info")
        .header("origin", "https://evil.example.com")
        .header("access-control-request-method", "GET")
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    let allow = resp.headers().get("access-control-allow-origin");
    assert!(allow.is_none(), "evil origin must not be echoed");
}

#[test]
fn constant_time_eq_matches_equal() {
    assert!(constant_time_eq(b"same", b"same"));
}

#[test]
fn constant_time_eq_rejects_different() {
    assert!(!constant_time_eq(b"same", b"diff"));
    assert!(!constant_time_eq(b"short", b"shorter"));
    assert!(!constant_time_eq(b"", b"x"));
}
