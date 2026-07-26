// Split out of the former monolithic scraping.rs. Code is unchanged;
// only visibility was widened so cross-module calls resolve.

use crate::models::Coordinates;
use crate::models::Listing;

/// Guest parameters for Airbnb search
#[derive(Debug, Clone, Default)]
pub struct GuestParams {
    pub adults: i32,
    pub children: i32,
    pub infants: i32,
    pub pets: i32,
}

impl GuestParams {
    pub fn new(adults: i32, children: i32, infants: i32, pets: i32) -> Self {
        Self { adults, children, infants, pets }
    }

    pub fn from_total(guests: i32) -> Self {
        Self { adults: guests, children: 0, infants: 0, pets: 0 }
    }
}

/// Amenity filters for Airbnb search
/// Airbnb IDs: Hot tub=25, Pool=7, Waterfront=Tag:686
#[derive(Debug, Clone, Default)]
pub struct AmenityFilter {
    pub hot_tub: bool,
    pub pool: bool,
    pub waterfront: bool,
}

impl AmenityFilter {
    pub fn new(hot_tub: bool, pool: bool, waterfront: bool) -> Self {
        Self { hot_tub, pool, waterfront }
    }

    /// Build the URL query string for amenity filters
    pub fn to_url_params(&self) -> String {
        let mut params = Vec::new();

        // Airbnb amenity IDs
        if self.hot_tub {
            params.push("amenities%5B%5D=25"); // Hot tub
        }
        if self.pool {
            params.push("amenities%5B%5D=7"); // Pool
        }
        if self.waterfront {
            params.push("kg_and_tags%5B%5D=Tag%3A686"); // Waterfront (uses tag, not amenity)
        }

        params.join("&")
    }

    /// Check if any filter is active
    pub fn has_filters(&self) -> bool {
        self.hot_tub || self.pool || self.waterfront
    }
}

/// A geographic bounding box for map-based search subdivision.
#[derive(Clone, Copy, Debug)]
pub struct BoundingBox {
    pub sw_lat: f64,
    pub sw_lng: f64,
    pub ne_lat: f64,
    pub ne_lng: f64,
}

impl BoundingBox {
    /// Split into four equal quadrants for recursive subdivision.
    pub(crate) fn quarters(&self) -> [BoundingBox; 4] {
        let mid_lat = (self.sw_lat + self.ne_lat) / 2.0;
        let mid_lng = (self.sw_lng + self.ne_lng) / 2.0;
        [
            BoundingBox { sw_lat: self.sw_lat, sw_lng: self.sw_lng, ne_lat: mid_lat, ne_lng: mid_lng }, // SW
            BoundingBox { sw_lat: self.sw_lat, sw_lng: mid_lng, ne_lat: mid_lat, ne_lng: self.ne_lng }, // SE
            BoundingBox { sw_lat: mid_lat, sw_lng: self.sw_lng, ne_lat: self.ne_lat, ne_lng: mid_lng }, // NW
            BoundingBox { sw_lat: mid_lat, sw_lng: mid_lng, ne_lat: self.ne_lat, ne_lng: self.ne_lng }, // NE
        ]
    }

    /// Derive a padded bounding box from the coordinates of harvested listings.
    /// Returns None if too few listings carry coordinates to bound an area.
    pub(crate) fn from_listings(listings: &[Listing]) -> Option<BoundingBox> {
        let coords: Vec<&Coordinates> = listings.iter().filter_map(|l| l.coordinates.as_ref()).collect();
        if coords.len() < 2 {
            return None;
        }
        let mut min_lat = f64::MAX;
        let mut max_lat = f64::MIN;
        let mut min_lng = f64::MAX;
        let mut max_lng = f64::MIN;
        for c in &coords {
            min_lat = min_lat.min(c.lat);
            max_lat = max_lat.max(c.lat);
            min_lng = min_lng.min(c.lng);
            max_lng = max_lng.max(c.lng);
        }
        // Pad by 15% (min 0.01 deg) so edge listings aren't clipped.
        let lat_pad = ((max_lat - min_lat) * 0.15).max(0.01);
        let lng_pad = ((max_lng - min_lng) * 0.15).max(0.01);
        Some(BoundingBox {
            sw_lat: min_lat - lat_pad,
            sw_lng: min_lng - lng_pad,
            ne_lat: max_lat + lat_pad,
            ne_lng: max_lng + lng_pad,
        })
    }
}
