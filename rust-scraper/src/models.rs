// models.rs
use serde::{Deserialize, Serialize};
use mongodb::bson::oid::ObjectId;

#[derive(Debug, Serialize, Deserialize)]
pub struct HealthResponse {
    pub status: String,
    pub environment: String,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct InfoResponse {
    pub environment: String,
    pub status: String,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct Host {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(default)]
    pub is_superhost: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub image_url: Option<String>,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct Listing {
    #[serde(rename = "_id", skip_serializing_if = "Option::is_none")]
    pub id: Option<ObjectId>,
    pub url: String,
    pub title: String,
    pub picture_url: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub pictures: Vec<String>,
    pub description: String,
    pub price: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub price_numeric: Option<f64>,
    pub rating: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub rating_numeric: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reviews_count: Option<i32>,
    pub location: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub coordinates: Option<Coordinates>,
    pub features: Vec<String>,
    pub house_details: Vec<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub host: Option<Host>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub region: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub country: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub property_type: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub created_at: Option<chrono::DateTime<chrono::Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub scraped_at: Option<chrono::DateTime<chrono::Utc>>,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct Coordinates {
    pub lat: f64,
    pub lng: f64,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct FiltersResponse {
    pub features: Vec<String>,
    pub listings: Vec<Listing>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct ScrapingResponse {
    pub message: String,
    pub total_listings: i32,
    pub canada_listings: i32,
    pub us_listings: i32,
}