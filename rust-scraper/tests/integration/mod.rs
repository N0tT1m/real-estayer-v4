use axum_test::TestServer;
use rust_scraper::{routes, database};
use serde_json::json;

#[tokio::test]
async fn test_health_endpoint() {
    let app = routes::create_routes().await;
    let server = TestServer::new(app).unwrap();
    
    let response = server.get("/health").await;
    
    assert_eq!(response.status_code(), 200);
}

#[tokio::test]
async fn test_scraping_status_endpoint() {
    let app = routes::create_routes().await;
    let server = TestServer::new(app).unwrap();
    
    let response = server.get("/scraping/status").await;
    
    assert_eq!(response.status_code(), 200);
    let body = response.json::<serde_json::Value>();
    assert!(body.is_ok());
}

#[tokio::test]
async fn test_database_connection() {
    std::env::set_var("MONGODB_URI", "mongodb://localhost:27017");
    std::env::set_var("DATABASE_NAME", "real_estayer_test");
    
    let collection = database::get_collection().await;
    
    // Test basic collection operation
    let count = collection.count_documents(None).await.unwrap();
    assert!(count >= 0);
}