#[cfg(test)]
mod database_tests {
    use super::super::database::*;
    use super::super::models::*;
    use mongodb::bson::doc;

    // Note: These tests require a test MongoDB instance
    // Set TEST_MONGODB_URI environment variable for testing

    #[tokio::test]
    async fn test_get_database_connection() {
        // Test database connection establishment
        std::env::set_var("MONGODB_URI", "mongodb://localhost:27017/test_real_estayer");

        let result = get_database().await;
        assert!(result.is_ok(), "Should connect to test database");
    }

    #[tokio::test]
    async fn test_insert_listing() {
        let db = match get_database().await {
            Ok(db) => db,
            Err(_) => {
                println!("Skipping test - no MongoDB connection");
                return;
            }
        };

        let test_listing = Listing {
            id: None,
            url: "https://airbnb.com/test/123".to_string(),
            title: "Test Listing".to_string(),
            picture_url: Some("https://example.com/image.jpg".to_string()),
            description: Some("Test description".to_string()),
            price: Some("$100".to_string()),
            rating: Some("4.5".to_string()),
            location: "Test City".to_string(),
            features: vec!["WiFi".to_string(), "Pool".to_string()],
            house_details: vec!["2 guests".to_string(), "1 bedroom".to_string()],
            region: Some("Test Region".to_string()),
            country: Some("Test Country".to_string()),
            created_at: None,
        };

        let result = insert_listing(&db, test_listing).await;
        assert!(result.is_ok(), "Should insert listing successfully");

        // Cleanup
        if let Ok(id) = result {
            let _ = delete_listing(&db, &id.to_hex()).await;
        }
    }

    #[tokio::test]
    async fn test_get_listings() {
        let db = match get_database().await {
            Ok(db) => db,
            Err(_) => {
                println!("Skipping test - no MongoDB connection");
                return;
            }
        };

        // Insert test listing
        let test_listing = Listing {
            id: None,
            url: "https://airbnb.com/test/456".to_string(),
            title: "Test Listing for Get".to_string(),
            picture_url: None,
            description: None,
            price: Some("$200".to_string()),
            rating: Some("4.8".to_string()),
            location: "Test Location".to_string(),
            features: vec![],
            house_details: vec![],
            region: None,
            country: None,
            created_at: None,
        };

        let insert_result = insert_listing(&db, test_listing).await;
        assert!(insert_result.is_ok());

        // Test getting listings
        let listings = get_listings(&db, None, None).await;
        assert!(listings.is_ok(), "Should fetch listings successfully");

        let listings_vec = listings.unwrap();
        assert!(!listings_vec.is_empty(), "Should have at least one listing");

        // Cleanup
        if let Ok(id) = insert_result {
            let _ = delete_listing(&db, &id.to_hex()).await;
        }
    }

    #[tokio::test]
    async fn test_get_listing_by_id() {
        let db = match get_database().await {
            Ok(db) => db,
            Err(_) => {
                println!("Skipping test - no MongoDB connection");
                return;
            }
        };

        // Insert test listing
        let test_listing = Listing {
            id: None,
            url: "https://airbnb.com/test/789".to_string(),
            title: "Test Listing for Get by ID".to_string(),
            picture_url: None,
            description: None,
            price: Some("$150".to_string()),
            rating: Some("4.7".to_string()),
            location: "Test City".to_string(),
            features: vec![],
            house_details: vec![],
            region: None,
            country: None,
            created_at: None,
        };

        let id = insert_listing(&db, test_listing).await.unwrap();

        // Test getting listing by ID
        let listing = get_listing_by_id(&db, &id.to_hex()).await;
        assert!(listing.is_ok(), "Should fetch listing by ID");

        let fetched_listing = listing.unwrap();
        assert_eq!(fetched_listing.title, "Test Listing for Get by ID");

        // Cleanup
        let _ = delete_listing(&db, &id.to_hex()).await;
    }

    #[tokio::test]
    async fn test_update_listing() {
        let db = match get_database().await {
            Ok(db) => db,
            Err(_) => {
                println!("Skipping test - no MongoDB connection");
                return;
            }
        };

        // Insert test listing
        let mut test_listing = Listing {
            id: None,
            url: "https://airbnb.com/test/update".to_string(),
            title: "Original Title".to_string(),
            picture_url: None,
            description: None,
            price: Some("$100".to_string()),
            rating: Some("4.5".to_string()),
            location: "Original Location".to_string(),
            features: vec![],
            house_details: vec![],
            region: None,
            country: None,
            created_at: None,
        };

        let id = insert_listing(&db, test_listing.clone()).await.unwrap();

        // Update the listing
        test_listing.id = Some(id.clone());
        test_listing.title = "Updated Title".to_string();
        test_listing.price = Some("$200".to_string());

        let update_result = update_listing(&db, &id.to_hex(), test_listing).await;
        assert!(update_result.is_ok(), "Should update listing successfully");

        // Verify update
        let updated = get_listing_by_id(&db, &id.to_hex()).await.unwrap();
        assert_eq!(updated.title, "Updated Title");
        assert_eq!(updated.price, Some("$200".to_string()));

        // Cleanup
        let _ = delete_listing(&db, &id.to_hex()).await;
    }

    #[tokio::test]
    async fn test_delete_listing() {
        let db = match get_database().await {
            Ok(db) => db,
            Err(_) => {
                println!("Skipping test - no MongoDB connection");
                return;
            }
        };

        // Insert test listing
        let test_listing = Listing {
            id: None,
            url: "https://airbnb.com/test/delete".to_string(),
            title: "Listing to Delete".to_string(),
            picture_url: None,
            description: None,
            price: Some("$100".to_string()),
            rating: Some("4.5".to_string()),
            location: "Test Location".to_string(),
            features: vec![],
            house_details: vec![],
            region: None,
            country: None,
            created_at: None,
        };

        let id = insert_listing(&db, test_listing).await.unwrap();

        // Delete the listing
        let delete_result = delete_listing(&db, &id.to_hex()).await;
        assert!(delete_result.is_ok(), "Should delete listing successfully");

        // Verify deletion
        let get_result = get_listing_by_id(&db, &id.to_hex()).await;
        assert!(get_result.is_err(), "Listing should not exist after deletion");
    }

    #[tokio::test]
    async fn test_filter_listings_by_city() {
        let db = match get_database().await {
            Ok(db) => db,
            Err(_) => {
                println!("Skipping test - no MongoDB connection");
                return;
            }
        };

        // Insert test listings with different locations
        let listing1 = Listing {
            id: None,
            url: "https://airbnb.com/test/city1".to_string(),
            title: "Listing in New York".to_string(),
            picture_url: None,
            description: None,
            price: Some("$100".to_string()),
            rating: Some("4.5".to_string()),
            location: "New York".to_string(),
            features: vec![],
            house_details: vec![],
            region: None,
            country: Some("USA".to_string()),
            created_at: None,
        };

        let listing2 = Listing {
            id: None,
            url: "https://airbnb.com/test/city2".to_string(),
            title: "Listing in Los Angeles".to_string(),
            picture_url: None,
            description: None,
            price: Some("$150".to_string()),
            rating: Some("4.7".to_string()),
            location: "Los Angeles".to_string(),
            features: vec![],
            house_details: vec![],
            region: None,
            country: Some("USA".to_string()),
            created_at: None,
        };

        let id1 = insert_listing(&db, listing1).await.unwrap();
        let id2 = insert_listing(&db, listing2).await.unwrap();

        // Test filtering by city
        let ny_listings = get_listings(&db, Some("New York"), None).await;
        assert!(ny_listings.is_ok());
        let ny_results = ny_listings.unwrap();
        assert!(ny_results.iter().any(|l| l.location == "New York"));

        // Cleanup
        let _ = delete_listing(&db, &id1.to_hex()).await;
        let _ = delete_listing(&db, &id2.to_hex()).await;
    }
}
