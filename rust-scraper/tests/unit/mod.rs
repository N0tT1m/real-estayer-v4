use rust_scraper::models::*;
use serde_json;

#[test]
fn test_listing_serialization() {
    let listing = Listing {
        id: Some("test_id".to_string()),
        title: "Test Listing".to_string(),
        description: "Test Description".to_string(),
        price: 150.0,
        location: "Test Location".to_string(),
        bedrooms: 2,
        bathrooms: 1,
        amenities: vec!["WiFi".to_string(), "Kitchen".to_string()],
        images: vec!["https://example.com/image1.jpg".to_string()],
        url: "https://example.com/listing".to_string(),
        scraped_at: chrono::Utc::now(),
    };

    let serialized = serde_json::to_string(&listing).unwrap();
    let deserialized: Listing = serde_json::from_str(&serialized).unwrap();

    assert_eq!(listing.title, deserialized.title);
    assert_eq!(listing.price, deserialized.price);
    assert_eq!(listing.bedrooms, deserialized.bedrooms);
    assert_eq!(listing.amenities.len(), deserialized.amenities.len());
}

#[test]
fn test_scraping_job_serialization() {
    let job = ScrapingJob {
        id: Some("test_job_id".to_string()),
        url: "https://example.com".to_string(),
        status: "pending".to_string(),
        created_at: chrono::Utc::now(),
        completed_at: None,
        error_message: None,
        listings_found: 0,
    };

    let serialized = serde_json::to_string(&job).unwrap();
    let deserialized: ScrapingJob = serde_json::from_str(&serialized).unwrap();

    assert_eq!(job.url, deserialized.url);
    assert_eq!(job.status, deserialized.status);
    assert_eq!(job.listings_found, deserialized.listings_found);
}