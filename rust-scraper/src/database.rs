// database.rs
use crate::models::*;
use anyhow::Result;
use futures::TryStreamExt;
use mongodb::{
    bson::{doc, Document},
    options::{ClientOptions, FindOptions},
    Client, Collection, Database
};
use mongodb::bson::oid::ObjectId;
use tokio::sync::OnceCell;


static DB: OnceCell<Database> = OnceCell::const_new();

async fn init_db() -> Database {
    let mongodb_uri = std::env::var("MONGODB_URI").unwrap_or_else(|_| {
        tracing::warn!("MONGODB_URI not set; defaulting to mongodb://localhost:27017/real_estayer");
        "mongodb://localhost:27017/real_estayer".to_string()
    });

    let db_name = mongodb_uri
        .rsplit('/')
        .next()
        .and_then(|tail| tail.split('?').next())
        .filter(|s| !s.is_empty())
        .unwrap_or("real_estayer")
        .to_string();

    let client_options = match ClientOptions::parse(&mongodb_uri).await {
        Ok(o) => o,
        Err(e) => {
            tracing::error!("Failed to parse MONGODB_URI: {}", e);
            panic!("invalid MONGODB_URI");
        }
    };
    let client = match Client::with_options(client_options) {
        Ok(c) => c,
        Err(e) => {
            tracing::error!("Failed to create Mongo client: {}", e);
            panic!("Mongo client init failed");
        }
    };
    client.database(&db_name)
}

pub async fn get_collection() -> Collection<Listing> {
    let db = DB.get_or_init(init_db).await;
    db.collection("listings")
}

// Validate a listing before insertion
fn validate_listing(listing: &Listing) -> bool {
    use tracing::{warn, debug};
    
    // Check required fields
    if listing.title.trim().is_empty() {
        warn!("Listing rejected: empty title");
        return false;
    }
    
    if listing.url.trim().is_empty() || !listing.url.starts_with("http") {
        warn!("Listing rejected: invalid URL: '{}'", listing.url);
        return false;
    }
    
    // At least one of description or price should be present
    if listing.description.trim().is_empty() && listing.price.trim().is_empty() {
        warn!("Listing rejected: both description and price are empty");
        return false;
    }
    
    // Validate price format if present
    if !listing.price.trim().is_empty() {
        if !listing.price.starts_with('$') {
            warn!("Listing rejected: invalid price format: '{}'", listing.price);
            return false;
        }
        
        // Extract numeric part and validate
        let price_num = listing.price[1..].replace(',', "");
        if let Ok(price_val) = price_num.parse::<f64>() {
            if price_val <= 0.0 || price_val > 50000.0 {
                warn!("Listing rejected: price out of reasonable range: ${}", price_val);
                return false;
            }
        } else {
            warn!("Listing rejected: non-numeric price: '{}'", listing.price);
            return false;
        }
    }
    
    // Validate description length if present
    if !listing.description.trim().is_empty() && listing.description.trim().len() < 10 {
        warn!("Listing rejected: description too short: '{}'", listing.description);
        return false;
    }
    
    debug!("Listing validation passed: title='{}', price='{}'", listing.title, listing.price);
    true
}

pub async fn insert_many(listings: Vec<Listing>) -> Result<Vec<String>> {
    use tracing::{info, warn, debug, error, span, Level};

    let span = span!(Level::INFO, "insert_many", count = listings.len());
    let _enter = span.enter();

    info!("Attempting to insert {} listings into database", listings.len());

    // Check for empty vector to prevent MongoDB error
    if listings.is_empty() {
        warn!("No listings provided to insert_many - returning empty result");
        return Ok(Vec::new());
    }

    // Validate and filter listings
    let original_count = listings.len();
    let valid_listings: Vec<Listing> = listings.into_iter()
        .filter(validate_listing)
        .collect();

    let rejected_count = original_count - valid_listings.len();
    if rejected_count > 0 {
        warn!("Rejected {} invalid listings out of {} total", rejected_count, original_count);
    }

    if valid_listings.is_empty() {
        error!("No valid listings remain after validation");
        return Ok(Vec::new());
    }

    info!("Proceeding with {} valid listings", valid_listings.len());

    let collection = get_collection().await;

    // Filter out duplicates by checking existing URLs in database
    let urls: Vec<String> = valid_listings.iter().map(|l| l.url.clone()).collect();
    let url_refs: Vec<&str> = urls.iter().map(|s| s.as_str()).collect();
    let existing_urls = get_existing_urls(&collection, &url_refs).await?;

    let valid_count = valid_listings.len();
    let unique_listings: Vec<Listing> = valid_listings
        .into_iter()
        .filter(|listing| !existing_urls.contains(&listing.url))
        .collect();

    let duplicate_count = valid_count - unique_listings.len();
    if duplicate_count > 0 {
        info!("Filtered out {} duplicate listings (already exist in database)", duplicate_count);
    }

    if unique_listings.is_empty() {
        info!("No new unique listings to insert - all {} listings already exist", duplicate_count);
        return Ok(Vec::new());
    }

    info!("Inserting {} new unique listings (filtered {} duplicates, rejected {} invalid)",
          unique_listings.len(), duplicate_count, rejected_count);

    // Log some sample data for verification
    for (i, listing) in unique_listings.iter().take(3).enumerate() {
        debug!("Unique listing {}: title='{}', location='{}', price='{}', region='{:?}', country='{:?}'",
               i + 1, listing.title, listing.location, listing.price, listing.region, listing.country);
        info!("Listing {} features ({} total): {:?}", i + 1, listing.features.len(), listing.features);
    }
    if unique_listings.len() > 3 {
        debug!("... and {} more unique listings", unique_listings.len() - 3);
    }

    let unique_count = unique_listings.len();

    match collection.insert_many(unique_listings, None).await {
        Ok(result) => {
            let inserted_ids: Vec<String> = result
                .inserted_ids
                .values()
                .map(|id| id.to_string())
                .collect();
            info!("Successfully inserted {} new listings into database (skipped {} duplicates, rejected {} invalid)",
                  inserted_ids.len(), duplicate_count, rejected_count);
            debug!("Inserted IDs: {:?}", inserted_ids.iter().take(3).collect::<Vec<_>>());
            Ok(inserted_ids)
        },
        Err(e) => {
            error!("Failed to insert {} unique listings into database: {}", unique_count, e);
            Err(e.into())
        }
    }
}

/// Get URLs that already exist in the database
async fn get_existing_urls(collection: &Collection<Listing>, urls: &[&str]) -> Result<std::collections::HashSet<String>> {
    use tracing::debug;

    let filter = doc! {
        "url": { "$in": urls }
    };

    let mut cursor = collection.find(filter, None).await?;
    let mut existing_urls = std::collections::HashSet::new();

    while let Some(listing) = cursor.try_next().await? {
        existing_urls.insert(listing.url);
    }

    debug!("Found {} existing URLs out of {} to check", existing_urls.len(), urls.len());
    Ok(existing_urls)
}

pub async fn get_listings_by_query(query: Document, limit: i64) -> Result<Vec<Listing>> {
    let collection = get_collection().await;
    
    let mut cursor = if limit > 0 {
        let options = FindOptions::builder().limit(limit).build();
        collection.find(query, options).await?
    } else {
        // No limit if limit <= 0
        collection.find(query, None).await?
    };

    let mut listings = Vec::new();
    while let Some(listing) = cursor.try_next().await? {
        listings.push(listing);
    }

    Ok(listings)
}

pub async fn get_listing_by_id(id: ObjectId) -> Result<Option<Listing>> {
    let collection = get_collection().await;
    Ok(collection.find_one(doc! { "_id": id }, None).await?)
}

pub async fn get_filters(query: Document, limit: i64) -> Result<FiltersResponse> {
    let collection = get_collection().await;

    let pipeline = vec![
        doc! { "$unwind": "$features" },
        doc! { "$group": { "_id": "$features" } },
        doc! { "$sort": { "_id": 1 } },
    ];

    let mut cursor = collection.aggregate(pipeline, None).await?;
    let mut features = Vec::new();

    while let Some(doc) = cursor.try_next().await? {
        if let Ok(feature) = doc.get_str("_id") {
            features.push(feature.to_string());
        }
    }

    let listings = get_listings_by_query(query, limit).await?;

    Ok(FiltersResponse {
        features,
        listings,
    })
}
