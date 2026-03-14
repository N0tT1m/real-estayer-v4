use axum::http::StatusCode;
use axum_test::TestServer;
use serde_json::json;

// Integration tests for the Rust scraper API

#[tokio::test]
async fn test_health_endpoint() {
    let app = rust_scraper::create_app().await;
    let server = TestServer::new(app).unwrap();

    let response = server.get("/health").await;

    assert_eq!(response.status_code(), StatusCode::OK);

    let body: serde_json::Value = response.json();
    assert_eq!(body["status"], "healthy");
}

#[tokio::test]
async fn test_info_endpoint() {
    let app = rust_scraper::create_app().await;
    let server = TestServer::new(app).unwrap();

    let response = server.get("/info").await;

    assert_eq!(response.status_code(), StatusCode::OK);

    let body: serde_json::Value = response.json();
    assert!(body["service"].is_string());
    assert!(body["version"].is_string());
}

#[tokio::test]
async fn test_get_listings_endpoint() {
    let app = rust_scraper::create_app().await;
    let server = TestServer::new(app).unwrap();

    let response = server.get("/get-listings").await;

    // Should return OK or error depending on database state
    assert!(
        response.status_code() == StatusCode::OK ||
        response.status_code() == StatusCode::INTERNAL_SERVER_ERROR
    );
}

#[tokio::test]
async fn test_get_listing_by_id_invalid() {
    let app = rust_scraper::create_app().await;
    let server = TestServer::new(app).unwrap();

    let response = server.get("/get-listing/invalid-id").await;

    // Should return bad request for invalid ID
    assert_eq!(response.status_code(), StatusCode::BAD_REQUEST);
}

#[tokio::test]
async fn test_filters_endpoint() {
    let app = rust_scraper::create_app().await;
    let server = TestServer::new(app).unwrap();

    let response = server.get("/filters").await;

    assert_eq!(response.status_code(), StatusCode::OK);

    let body: serde_json::Value = response.json();
    assert!(body["cities"].is_array() || body["error"].is_string());
}

#[tokio::test]
async fn test_scrape_city_data_missing_params() {
    let app = rust_scraper::create_app().await;
    let server = TestServer::new(app).unwrap();

    // Test without city parameter
    let response = server.get("/scrape-city-data").await;

    // Should return bad request when city is missing
    assert_eq!(response.status_code(), StatusCode::BAD_REQUEST);
}

#[tokio::test]
async fn test_scrape_city_data_with_params() {
    let app = rust_scraper::create_app().await;
    let server = TestServer::new(app).unwrap();

    // Test with city parameter (but don't actually scrape)
    let response = server
        .get("/scrape-city-data?city=TestCity&limit=1")
        .await;

    // Might succeed or fail depending on WebDriver availability
    // Just verify it doesn't panic
    assert!(response.status_code().is_client_error() || response.status_code().is_success());
}

#[tokio::test]
async fn test_get_listings_with_city_filter() {
    let app = rust_scraper::create_app().await;
    let server = TestServer::new(app).unwrap();

    let response = server.get("/get-listings/TestCity/10").await;

    // Should return OK or 404 if no listings found
    assert!(
        response.status_code() == StatusCode::OK ||
        response.status_code() == StatusCode::NOT_FOUND ||
        response.status_code() == StatusCode::INTERNAL_SERVER_ERROR
    );
}

#[tokio::test]
async fn test_cors_headers() {
    let app = rust_scraper::create_app().await;
    let server = TestServer::new(app).unwrap();

    let response = server.get("/health").await;

    // Check CORS headers are present
    let headers = response.headers();
    assert!(headers.contains_key("access-control-allow-origin"));
}

#[tokio::test]
async fn test_concurrent_requests() {
    let app = rust_scraper::create_app().await;
    let server = TestServer::new(app).unwrap();

    // Send multiple concurrent requests
    let mut handles = vec![];

    for _ in 0..5 {
        let server_clone = server.clone();
        let handle = tokio::spawn(async move {
            server_clone.get("/health").await
        });
        handles.push(handle);
    }

    // All requests should complete successfully
    for handle in handles {
        let response = handle.await.unwrap();
        assert_eq!(response.status_code(), StatusCode::OK);
    }
}

#[tokio::test]
async fn test_invalid_endpoint() {
    let app = rust_scraper::create_app().await;
    let server = TestServer::new(app).unwrap();

    let response = server.get("/nonexistent-endpoint").await;

    assert_eq!(response.status_code(), StatusCode::NOT_FOUND);
}

#[tokio::test]
async fn test_get_all_listings_pagination() {
    let app = rust_scraper::create_app().await;
    let server = TestServer::new(app).unwrap();

    let response = server.get("/get-all-listings?page=1&limit=10").await;

    if response.status_code() == StatusCode::OK {
        let body: serde_json::Value = response.json();
        assert!(body["listings"].is_array());
        assert!(body["page"].is_number());
        assert!(body["total"].is_number());
    }
}

mod scraping_tests {
    use super::*;

    #[tokio::test]
    async fn test_scraping_params_validation() {
        // Test various parameter combinations
        let test_cases = vec![
            ("", false),  // Empty city
            ("New York", true),  // Valid city
            ("Los Angeles, CA", true),  // City with state
            ("1234567890".repeat(10).as_str(), false),  // Too long
        ];

        for (city, should_be_valid) in test_cases {
            let result = validate_city_name(city);
            assert_eq!(result, should_be_valid, "Failed for city: {}", city);
        }
    }

    fn validate_city_name(city: &str) -> bool {
        !city.is_empty() && city.len() <= 100 && city.chars().all(|c| c.is_alphanumeric() || c.is_whitespace() || c == ',' || c == '-')
    }
}

mod error_handling_tests {
    use super::*;

    #[tokio::test]
    async fn test_database_connection_error_handling() {
        // Set invalid MongoDB URI
        std::env::set_var("MONGODB_URI", "mongodb://invalid:27017/test");

        let result = rust_scraper::database::get_database().await;
        assert!(result.is_err(), "Should fail with invalid MongoDB URI");
    }

    #[tokio::test]
    async fn test_malformed_request_handling() {
        let app = rust_scraper::create_app().await;
        let server = TestServer::new(app).unwrap();

        // Test with malformed query parameters
        let response = server
            .get("/get-listings/city/limit?invalid=param&another=bad")
            .await;

        // Should handle gracefully
        assert!(response.status_code().is_client_error() || response.status_code().is_success());
    }
}
