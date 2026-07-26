// Split out of the former monolithic scraping.rs. Code is unchanged;
// only visibility was widened so cross-module calls resolve.

use super::*;
use crate::models::Coordinates;
use crate::models::Listing;
use base64::Engine as _;

// Extract data from page source using regex patterns
pub(crate) fn extract_title_from_source(html: &str) -> Option<String> {
    // Try JSON-LD first
    if let Some(json_ld) = extract_json_ld(html) {
        if let Some(name) = json_ld.get("name").and_then(|v| v.as_str()) {
            if !name.is_empty() && name != "\"" {
                return Some(name.to_string());
            }
        }
    }

    // Try meta og:title
    if let Some(title) = extract_meta_content(html, "og:title") {
        return Some(title);
    }

    // Try page title
    if let Some(captures) = regex::Regex::new(r"<title>([^<]+)</title>")
        .unwrap()
        .captures(html)
    {
        if let Some(title) = captures.get(1) {
            let title_text = title.as_str().trim();
            // Extract just the property name part before " - Apartments"
            if let Some(pos) = title_text.find(" - Apartments") {
                return Some(title_text[..pos].trim_matches('"').to_string());
            }
            return Some(title_text.to_string());
        }
    }

    None
}

pub(crate) fn extract_picture_url_from_source(html: &str) -> Option<String> {
    // Try JSON-LD first
    if let Some(json_ld) = extract_json_ld(html) {
        if let Some(images) = json_ld.get("image").and_then(|v| v.as_array()) {
            if let Some(first_image) = images.first().and_then(|v| v.as_str()) {
                return Some(first_image.to_string());
            }
        }
    }

    // Try meta og:image
    if let Some(image_url) = extract_meta_content(html, "og:image") {
        return Some(image_url);
    }

    // Try meta twitter:image
    if let Some(image_url) = extract_meta_content(html, "twitter:image") {
        return Some(image_url);
    }

    None
}

/// Safely truncate a string to approximately `max_chars` characters.
/// This avoids panics from slicing in the middle of multi-byte UTF-8 characters.
pub(crate) fn safe_truncate(s: &str, max_chars: usize) -> &str {
    if s.len() <= max_chars {
        return s;
    }
    // Find the last valid char boundary at or before max_chars
    let mut end = max_chars;
    while end > 0 && !s.is_char_boundary(end) {
        end -= 1;
    }
    &s[..end]
}

pub(crate) fn extract_description_from_source(html: &str) -> Option<String> {
    use tracing::debug;

    // Try JSON-LD first
    if let Some(json_ld) = extract_json_ld(html) {
        if let Some(description) = json_ld.get("description").and_then(|v| v.as_str()) {
            let trimmed = description.trim();
            // Validate description is meaningful
            if !trimmed.is_empty()
                && trimmed != "null"
                && trimmed != "undefined"
                && trimmed.len() >= 10
            {
                debug!(
                    "Found valid description in JSON-LD: '{}'",
                    safe_truncate(trimmed, 100)
                );
                return Some(trimmed.to_string());
            }
        }
    }

    // Try meta description
    if let Some(description) = extract_meta_content(html, "description") {
        let trimmed = description.trim();
        // Validate description is meaningful
        if !trimmed.is_empty() && trimmed != "null" && trimmed != "undefined" && trimmed.len() >= 10
        {
            debug!(
                "Found valid description in meta: '{}'",
                safe_truncate(trimmed, 100)
            );

            // Extract just the description part, remove the date prefix
            if let Some(pos) = trimmed.find(" - ") {
                let desc_part = &trimmed[pos + 3..];
                if let Some(dot_pos) = desc_part.find(". ") {
                    let clean_desc = desc_part[dot_pos + 2..].trim().to_string();
                    if clean_desc.len() >= 10 {
                        debug!("Cleaned description: '{}'", safe_truncate(&clean_desc, 100));
                        return Some(clean_desc);
                    }
                }
            }
            return Some(trimmed.to_string());
        }
    }

    debug!("No valid description found (empty, null, or too short)");
    None
}

pub(crate) fn extract_price_from_source(html: &str) -> Option<String> {
    use tracing::{debug, info, warn};

    // Try Airbnb's data-deferred-state-0 JSON first (most reliable for individual listing pages)
    if let Some(price) = extract_price_from_airbnb_json(html) {
        info!("Found price from Airbnb JSON: {}", price);
        return Some(price);
    }

    // Try JSON-LD structured data
    if let Some(json_ld) = extract_json_ld(html) {
        // Look for offers or priceSpecification in JSON-LD
        if let Some(offers) = json_ld.get("offers") {
            if let Some(price) = offers.get("price").and_then(|p| p.as_str()) {
                if let Ok(price_val) = price.parse::<f64>() {
                    if (10.0..=10000.0).contains(&price_val) {
                        debug!("Found price in JSON-LD offers: ${}", price_val);
                        return Some(format!("${}", price_val));
                    }
                }
            }
        }

        // Check for other price fields in JSON-LD
        if let Some(price_range) = json_ld.get("priceRange").and_then(|p| p.as_str()) {
            let price_patterns = [r"\$(\d+)", r"(\d+)"];
            for pattern in price_patterns {
                if let Ok(regex) = regex::Regex::new(pattern) {
                    if let Some(captures) = regex.captures(price_range) {
                        if let Some(price_match) = captures.get(1) {
                            if let Ok(price_val) = price_match.as_str().parse::<u32>() {
                                if (10..=10000).contains(&price_val) {
                                    debug!("Found price in JSON-LD priceRange: ${}", price_val);
                                    return Some(format!("${}", price_val));
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    // Try meta description - contains "for $199" pattern
    if let Some(description) = extract_meta_content(html, "description") {
        info!("Found meta description for price: '{}'", description);
        debug!("Meta description length: {} chars", description.len());

        // Try multiple price patterns with validation
        let price_patterns = [
            r"for \$(\d+)",         // "for $199"
            r"unit for \$(\d+)",    // "Entire rental unit for $115"
            r"\$(\d+)\b",           // "$199" (word boundary to avoid partial matches)
            r"(\d+) per night",     // "199 per night"
            r"(\d+)/night",         // "199/night"
            r"Starting at \$(\d+)", // "Starting at $199"
            r"from \$(\d+)",        // "from $199"
        ];

        for (i, pattern) in price_patterns.iter().enumerate() {
            info!("Trying price pattern {}: '{}'", i + 1, pattern);
            if let Ok(regex) = regex::Regex::new(pattern) {
                if let Some(captures) = regex.captures(&description) {
                    info!("Pattern '{}' matched! Captures: {:?}", pattern, captures);
                    if let Some(price) = captures.get(1) {
                        let price_num = price.as_str();
                        info!("Extracted price number: '{}'", price_num);
                        // Validate price is reasonable (between $10 and $10000 per night)
                        if let Ok(price_val) = price_num.parse::<u32>() {
                            info!("Parsed price value: {}", price_val);
                            if (10..=10000).contains(&price_val) {
                                let price_str = format!("${}", price_num);
                                info!(
                                    "Found valid price with pattern '{}': '{}'",
                                    pattern, price_str
                                );
                                return Some(price_str);
                            } else {
                                warn!("Price {} outside reasonable range (10-10000)", price_val);
                            }
                        } else {
                            warn!("Failed to parse price '{}' as number", price_num);
                        }
                    } else {
                        warn!("Pattern matched but no capture group 1 found");
                    }
                } else {
                    debug!("Pattern '{}' did not match", pattern);
                }
            } else {
                warn!("Failed to compile regex pattern: '{}'", pattern);
            }
        }

        warn!("No valid price pattern matched in meta description");
    } else {
        warn!("No meta description found in HTML for price extraction");
    }

    info!("No valid price found in any source");
    None
}

/// Extract price from Airbnb's data-deferred-state-0 JSON structure
/// This is the primary source for pricing on individual listing pages
pub(crate) fn extract_price_from_airbnb_json(html: &str) -> Option<String> {
    use tracing::{debug, info};

    let json_data = extract_airbnb_json_data(html)?;

    // Debug: Save JSON to file for analysis (first time only)
    static SAVED: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);
    if !SAVED.swap(true, std::sync::atomic::Ordering::SeqCst) {
        if let Ok(json_str) = serde_json::to_string_pretty(&json_data) {
            let _ = std::fs::create_dir_all("logs");
            let path = format!(
                "logs/airbnb_json_debug_{}.json",
                chrono::Utc::now().timestamp()
            );
            if std::fs::write(&path, &json_str).is_ok() {
                info!(
                    "Saved Airbnb JSON to {} for debugging ({} bytes)",
                    path,
                    json_str.len()
                );
            }
        }
    }

    // Helper function to recursively search for price fields in JSON
    fn find_price_in_json(value: &serde_json::Value, depth: usize) -> Option<String> {
        if depth > 20 {
            return None; // Prevent infinite recursion
        }

        match value {
            serde_json::Value::Object(map) => {
                // Check for common price field names
                let price_fields = [
                    "price",
                    "priceString",
                    "discountedPrice",
                    "originalPrice",
                    "displayPrice",
                    "formattedPrice",
                    "priceForDisplay",
                    "total",
                    "nightlyPrice",
                    "basePrice",
                    "priceLabel",
                    "amount",
                ];

                for field in price_fields {
                    if let Some(price_val) = map.get(field) {
                        if let Some(price_str) = price_val.as_str() {
                            // Check if it looks like a price (contains $ or a number)
                            if price_str.contains('$')
                                || price_str.chars().any(|c| c.is_ascii_digit())
                            {
                                let cleaned = price_str.trim();
                                if !cleaned.is_empty() && cleaned != "$0" {
                                    return Some(cleaned.to_string());
                                }
                            }
                        } else if let Some(num) = price_val.as_f64() {
                            if (10.0..=50000.0).contains(&num) {
                                return Some(format!("${:.0}", num));
                            }
                        } else if let Some(num) = price_val.as_i64() {
                            if (10..=50000).contains(&num) {
                                return Some(format!("${}", num));
                            }
                        }
                    }
                }

                // Look for structuredDisplayPrice which Airbnb commonly uses
                if let Some(structured) = map.get("structuredDisplayPrice") {
                    if let Some(primary) = structured.get("primaryLine") {
                        if let Some(price) = primary.get("price").and_then(|p| p.as_str()) {
                            if !price.is_empty() {
                                return Some(price.to_string());
                            }
                        }
                        if let Some(price) = primary.get("discountedPrice").and_then(|p| p.as_str())
                        {
                            if !price.is_empty() {
                                return Some(price.to_string());
                            }
                        }
                        if let Some(access_label) =
                            primary.get("accessibilityLabel").and_then(|p| p.as_str())
                        {
                            // Try to extract price from accessibility label like "$150 per night"
                            if let Some(price) = extract_price_from_text(access_label) {
                                return Some(price);
                            }
                        }
                    }
                }

                // Look for bookItPrice
                if let Some(book_it) = map.get("bookItPrice") {
                    if let Some(price) = book_it.get("price").and_then(|p| p.as_str()) {
                        if !price.is_empty() {
                            return Some(price.to_string());
                        }
                    }
                }

                // Look in sections array for pricing section
                if let Some(sections) = map.get("sections").and_then(|s| s.as_array()) {
                    for section in sections {
                        if let Some(section_data) = section.get("section") {
                            if let Some(price) = find_price_in_json(section_data, depth + 1) {
                                return Some(price);
                            }
                        }
                    }
                }

                // Look in sectionArray
                if let Some(section_array) = map.get("sectionArray").and_then(|s| s.as_array()) {
                    for section in section_array {
                        if let Some(section_data) = section.get("section") {
                            if let Some(price) = find_price_in_json(section_data, depth + 1) {
                                return Some(price);
                            }
                        }
                    }
                }

                // Recursively search in nested objects
                for (_key, val) in map {
                    if let Some(price) = find_price_in_json(val, depth + 1) {
                        return Some(price);
                    }
                }
            }
            serde_json::Value::Array(arr) => {
                for item in arr {
                    if let Some(price) = find_price_in_json(item, depth + 1) {
                        return Some(price);
                    }
                }
            }
            _ => {}
        }

        None
    }

    // Try to navigate to known Airbnb JSON paths for individual listing pages
    // Path 1: niobeClientData -> various indices for PDP (Product Detail Page) data
    if let Some(niobe_data) = json_data.get("niobeClientData").and_then(|d| d.as_array()) {
        for entry in niobe_data {
            // Check for PdpFramework data
            if let Some(data) = entry.get("data") {
                // Try stayProductDetailPage path
                if let Some(pdp) = data
                    .get("presentation")
                    .and_then(|p| p.get("stayProductDetailPage"))
                {
                    debug!("Found stayProductDetailPage, searching for price...");

                    // Look in sections
                    if let Some(sections) = pdp.get("sections") {
                        if let Some(price) = find_price_in_json(sections, 0) {
                            info!("Found price in stayProductDetailPage sections: {}", price);
                            return Some(price);
                        }
                    }

                    // Search entire pdp structure
                    if let Some(price) = find_price_in_json(pdp, 0) {
                        info!("Found price in stayProductDetailPage: {}", price);
                        return Some(price);
                    }
                }

                // Try other potential paths
                if let Some(price) = find_price_in_json(data, 0) {
                    info!("Found price in niobeClientData entry: {}", price);
                    return Some(price);
                }
            }
        }
    }

    // Fallback: search entire JSON structure
    debug!("Searching entire JSON structure for price...");
    if let Some(price) = find_price_in_json(&json_data, 0) {
        info!("Found price via full JSON search: {}", price);
        return Some(price);
    }

    debug!("No price found in Airbnb JSON");
    None
}

/// Extract price from text using regex patterns
pub(crate) fn extract_price_from_text(text: &str) -> Option<String> {
    let price_patterns = [
        r"\$(\d+(?:,\d{3})*(?:\.\d{2})?)",         // $150, $1,500, $150.00
        r"(\d+(?:,\d{3})*(?:\.\d{2})?) per night", // 150 per night
        r"(\d+(?:,\d{3})*(?:\.\d{2})?)/night",     // 150/night
    ];

    for pattern in price_patterns {
        if let Ok(regex) = regex::Regex::new(pattern) {
            if let Some(captures) = regex.captures(text) {
                if let Some(price_match) = captures.get(1) {
                    let price_str = price_match.as_str().replace(',', "");
                    if let Ok(price_val) = price_str.parse::<f64>() {
                        if (10.0..=50000.0).contains(&price_val) {
                            return Some(format!("${}", price_match.as_str()));
                        }
                    }
                }
            }
        }
    }

    None
}

pub(crate) fn extract_rating_from_source(html: &str) -> Option<String> {
    // Try JSON-LD aggregateRating
    if let Some(json_ld) = extract_json_ld(html) {
        if let Some(rating_obj) = json_ld.get("aggregateRating") {
            if let Some(rating_value) = rating_obj.get("ratingValue").and_then(|v| v.as_f64()) {
                return Some(rating_value.to_string());
            }
        }
    }

    // Try meta og:title which contains "★4.89"
    if let Some(title) = extract_meta_content(html, "og:title") {
        if let Some(captures) = regex::Regex::new(r"★(\d+\.\d+)").unwrap().captures(&title) {
            if let Some(rating) = captures.get(1) {
                return Some(rating.as_str().to_string());
            }
        }
    }

    None
}

pub(crate) fn extract_location_from_source(html: &str) -> Option<String> {
    // Try JSON-LD address
    if let Some(json_ld) = extract_json_ld(html) {
        if let Some(address) = json_ld.get("address") {
            if let Some(locality) = address.get("addressLocality").and_then(|v| v.as_str()) {
                return Some(locality.to_string());
            }
        }
    }

    // Try meta og:title which contains location info
    if let Some(title) = extract_meta_content(html, "og:title") {
        if let Some(pos) = title.find(" in ") {
            let location_part = &title[pos + 4..];
            if let Some(end_pos) = location_part.find(" ·") {
                return Some(location_part[..end_pos].to_string());
            }
        }
    }

    None
}

pub(crate) fn extract_price_from_title(title: &str) -> Option<String> {
    use tracing::debug;

    // Try to extract price from title if it contains price information
    let price_patterns = [
        r"\$(\d+)",         // $199
        r"(\d+) per night", // 199 per night
        r"(\d+)/night",     // 199/night
        r"from \$(\d+)",    // from $199
    ];

    for pattern in price_patterns {
        if let Ok(regex) = regex::Regex::new(pattern) {
            if let Some(captures) = regex.captures(title) {
                if let Some(price) = captures.get(1) {
                    let price_str = format!("${}", price.as_str());
                    debug!("Extracted price from title: '{}'", price_str);
                    return Some(price_str);
                }
            }
        }
    }

    debug!("No price found in title: '{}'", title);
    None
}

pub(crate) fn extract_listing_urls_from_source(html: &str) -> Vec<String> {
    use tracing::{debug, info};

    let mut urls = Vec::new();

    // Try multiple regex patterns to find listing URLs in the page source
    let patterns = [
        r#"href="(/rooms/[^"]+)""#,                  // Direct room links
        r#"href="(/homes/[^"]+)""#,                  // Home links
        r#""url":"([^"]*(?:/rooms/|/homes/)[^"]*)"#, // JSON url field
        r#""@id":"([^"]*(?:/rooms/|/homes/)[^"]*)"#, // JSON-LD @id field
        r#"https://www\.airbnb\.com/rooms/[0-9]+"#,  // Full room URLs
        r#"https://www\.airbnb\.com/homes/[0-9]+"#,  // Full home URLs
    ];

    for (i, pattern) in patterns.iter().enumerate() {
        if let Ok(regex) = regex::Regex::new(pattern) {
            let matches: Vec<_> = regex.captures_iter(html).collect();
            debug!("Pattern {} found {} matches", i + 1, matches.len());

            for captures in matches {
                let url_str = if captures.len() > 1 {
                    captures.get(1).map(|m| m.as_str()).unwrap_or("")
                } else {
                    captures.get(0).map(|m| m.as_str()).unwrap_or("")
                };

                if !url_str.is_empty() {
                    let full_url = construct_airbnb_url(url_str);
                    if !urls.contains(&full_url) {
                        urls.push(full_url);
                    }
                }
            }
        }
    }

    if !urls.is_empty() {
        info!(
            "Regex extracted {} unique listing URLs from page source",
            urls.len()
        );
    } else {
        debug!("No listing URLs found in page source using regex");
    }

    urls
}

pub(crate) fn extract_listing_urls_from_json_ld(html: &str) -> Option<Vec<String>> {
    use tracing::{debug, info};

    // Extract all JSON-LD blocks and look for listing URLs
    let patterns = [
        r#"<script type="application/ld\+json">([^<]+)</script>"#,
        r#"<script type="application/ld\+json">\s*([^<]+)\s*</script>"#,
        r#"<script[^>]*type="application/ld\+json"[^>]*>([^<]+)</script>"#,
    ];

    let mut listing_urls = Vec::new();

    for pattern in patterns {
        if let Ok(regex) = regex::Regex::new(pattern) {
            for captures in regex.captures_iter(html) {
                if let Some(json_str) = captures.get(1) {
                    let json_text = json_str.as_str().trim();
                    let preview: String = json_text.chars().take(200).collect();
                    debug!("Processing JSON-LD content: {}", preview);

                    match serde_json::from_str::<serde_json::Value>(json_text) {
                        Ok(json_value) => {
                            // Look for listing URLs in various JSON-LD structures
                            extract_urls_from_json_value(&json_value, &mut listing_urls);
                        }
                        Err(e) => {
                            debug!("Failed to parse JSON-LD: {}", e);
                        }
                    }
                }
            }
        }
    }

    if !listing_urls.is_empty() {
        info!("Extracted {} listing URLs from JSON-LD", listing_urls.len());
        Some(listing_urls)
    } else {
        debug!("No listing URLs found in JSON-LD");
        None
    }
}

pub(crate) fn extract_urls_from_json_value(value: &serde_json::Value, urls: &mut Vec<String>) {
    use tracing::debug;

    match value {
        serde_json::Value::Object(obj) => {
            // Look for URL fields
            if let Some(url_val) = obj.get("url") {
                if let Some(url_str) = url_val.as_str() {
                    if url_str.contains("/rooms/") || url_str.contains("/homes/") {
                        let full_url = construct_airbnb_url(url_str);
                        urls.push(full_url);
                        debug!("Found listing URL in JSON-LD: {}", url_str);
                    }
                }
            }

            // Look for @id fields
            if let Some(id_val) = obj.get("@id") {
                if let Some(id_str) = id_val.as_str() {
                    if id_str.contains("/rooms/") || id_str.contains("/homes/") {
                        let full_url = construct_airbnb_url(id_str);
                        urls.push(full_url);
                        debug!("Found listing URL in JSON-LD @id: {}", id_str);
                    }
                }
            }

            // Recursively search in all object values
            for (_, v) in obj {
                extract_urls_from_json_value(v, urls);
            }
        }
        serde_json::Value::Array(arr) => {
            // Recursively search in all array elements
            for item in arr {
                extract_urls_from_json_value(item, urls);
            }
        }
        // Check if the string itself is a listing URL.
        serde_json::Value::String(s)
            if (s.contains("/rooms/") || s.contains("/homes/"))
                && (s.starts_with("http") || s.starts_with("/")) =>
        {
            let full_url = construct_airbnb_url(s);
            urls.push(full_url);
            debug!("Found listing URL in JSON-LD string: {}", s);
        }
        _ => {} // Ignore other types
    }
}

pub(crate) fn extract_json_ld(html: &str) -> Option<serde_json::Value> {
    use tracing::debug;

    // Find JSON-LD script tag - try multiple patterns
    let patterns = [
        r#"<script type="application/ld\+json">([^<]+)</script>"#,
        r#"<script type="application/ld\+json">\s*([^<]+)\s*</script>"#,
        r#"<script[^>]*type="application/ld\+json"[^>]*>([^<]+)</script>"#,
    ];

    for pattern in patterns {
        if let Ok(regex) = regex::Regex::new(pattern) {
            if let Some(captures) = regex.captures(html) {
                if let Some(json_str) = captures.get(1) {
                    let json_text = json_str.as_str().trim();
                    // Safely truncate for logging (handle multi-byte UTF-8 chars like emojis)
                    let preview: String = json_text.chars().take(200).collect();
                    debug!("Found JSON-LD content: {}", preview);

                    match serde_json::from_str::<serde_json::Value>(json_text) {
                        Ok(json_value) => {
                            debug!("Successfully parsed JSON-LD");
                            return Some(json_value);
                        }
                        Err(e) => {
                            debug!("Failed to parse JSON-LD: {}", e);
                        }
                    }
                }
            }
        }
    }

    debug!("No JSON-LD found in HTML");
    None
}

pub(crate) fn extract_region_from_url(url: &str) -> Option<String> {
    use tracing::debug;

    // Try to extract region from URL patterns or referrer
    // Common patterns in Airbnb search URLs or listing URLs
    if let Ok(parsed_url) = url::Url::parse(url) {
        // Check query parameters for location info
        for (key, value) in parsed_url.query_pairs() {
            if key == "location" || key == "place_id" || key == "region" {
                let location = value.to_string();
                debug!("Found region in URL parameter '{}': '{}'", key, location);

                // Extract city/region name from location string
                let parts: Vec<&str> = location.split(',').collect();
                if !parts.is_empty() {
                    let region = parts[0].trim().to_string();
                    if !region.is_empty() {
                        debug!("Extracted region from URL: '{}'", region);
                        return Some(region);
                    }
                }
            }
        }

        // Check if path contains location info
        let path = parsed_url.path();
        if path.contains("/s/") {
            // Pattern like /s/Toronto--ON--Canada/homes
            if let Some(start) = path.find("/s/") {
                let location_part = &path[start + 3..];
                if let Some(end) = location_part.find('/') {
                    let location = &location_part[..end];
                    let parts: Vec<&str> = location.split("--").collect();
                    if !parts.is_empty() {
                        let region = parts[0].replace('-', " ");
                        debug!("Extracted region from URL path: '{}'", region);
                        return Some(region);
                    }
                }
            }
        }
    }

    debug!("No region found in URL: '{}'", url);
    None
}

pub(crate) fn extract_region_country_from_source(html: &str) -> (Option<String>, Option<String>) {
    use tracing::debug;

    // Try to extract from JSON-LD address
    if let Some(json_ld) = extract_json_ld(html) {
        if let Some(address) = json_ld.get("address") {
            let region = address
                .get("addressRegion")
                .and_then(|v| v.as_str())
                .filter(|s| !s.trim().is_empty() && *s != "null" && *s != "undefined")
                .map(|s| s.trim().to_string());
            let country = address
                .get("addressCountry")
                .and_then(|v| v.as_str())
                .filter(|s| !s.trim().is_empty() && *s != "null" && *s != "undefined")
                .map(|s| s.trim().to_string());

            if region.is_some() || country.is_some() {
                debug!(
                    "Found region/country in JSON-LD: {:?}/{:?}",
                    region, country
                );
                return (region, country);
            }
        }
    }

    // Try to extract from meta og:title which might contain state/country info
    if let Some(title) = extract_meta_content(html, "og:title") {
        // Pattern: "Rental unit in Traverse City · ★4.89 · 1 bedroom · 3 beds · 1 bath"
        // Look for patterns like "City, State" or "City, Country"
        if let Some(pos) = title.find(" in ") {
            let location_part = &title[pos + 4..];
            if let Some(end_pos) = location_part.find(" ·") {
                let full_location = &location_part[..end_pos];

                // Split by comma to get city, state/region, country
                let parts: Vec<&str> = full_location
                    .split(',')
                    .map(|s| s.trim())
                    .filter(|s| !s.is_empty())
                    .collect();

                match parts.len() {
                    2 => {
                        // "City, State" or "City, Country"
                        let region_or_country = parts[1].to_string();
                        // Comprehensive US states and territories list
                        let us_states = [
                            "Alabama",
                            "Alaska",
                            "Arizona",
                            "Arkansas",
                            "California",
                            "Colorado",
                            "Connecticut",
                            "Delaware",
                            "Florida",
                            "Georgia",
                            "Hawaii",
                            "Idaho",
                            "Illinois",
                            "Indiana",
                            "Iowa",
                            "Kansas",
                            "Kentucky",
                            "Louisiana",
                            "Maine",
                            "Maryland",
                            "Massachusetts",
                            "Michigan",
                            "Minnesota",
                            "Mississippi",
                            "Missouri",
                            "Montana",
                            "Nebraska",
                            "Nevada",
                            "New Hampshire",
                            "New Jersey",
                            "New Mexico",
                            "New York",
                            "North Carolina",
                            "North Dakota",
                            "Ohio",
                            "Oklahoma",
                            "Oregon",
                            "Pennsylvania",
                            "Rhode Island",
                            "South Carolina",
                            "South Dakota",
                            "Tennessee",
                            "Texas",
                            "Utah",
                            "Vermont",
                            "Virginia",
                            "Washington",
                            "West Virginia",
                            "Wisconsin",
                            "Wyoming",
                            "DC",
                            "District of Columbia",
                            "AL",
                            "AK",
                            "AZ",
                            "AR",
                            "CA",
                            "CO",
                            "CT",
                            "DE",
                            "FL",
                            "GA",
                            "HI",
                            "ID",
                            "IL",
                            "IN",
                            "IA",
                            "KS",
                            "KY",
                            "LA",
                            "ME",
                            "MD",
                            "MA",
                            "MI",
                            "MN",
                            "MS",
                            "MO",
                            "MT",
                            "NE",
                            "NV",
                            "NH",
                            "NJ",
                            "NM",
                            "NY",
                            "NC",
                            "ND",
                            "OH",
                            "OK",
                            "OR",
                            "PA",
                            "RI",
                            "SC",
                            "SD",
                            "TN",
                            "TX",
                            "UT",
                            "VT",
                            "VA",
                            "WA",
                            "WV",
                            "WI",
                            "WY",
                        ];
                        if us_states.contains(&region_or_country.as_str()) {
                            return (Some(region_or_country), Some("United States".to_string()));
                        } else {
                            // Check for common countries
                            let countries = [
                                "Canada",
                                "Mexico",
                                "United Kingdom",
                                "France",
                                "Germany",
                                "Spain",
                                "Italy",
                                "Australia",
                                "Japan",
                            ];
                            if countries.contains(&region_or_country.as_str()) {
                                return (None, Some(region_or_country));
                            } else {
                                // Assume it's a region/state
                                return (Some(region_or_country), None);
                            }
                        }
                    }
                    3 => {
                        // "City, State, Country"
                        let region = parts[1].trim();
                        let country = parts[2].trim();
                        if !region.is_empty() && !country.is_empty() {
                            return (Some(region.to_string()), Some(country.to_string()));
                        }
                    }
                    _ => {}
                }
            }
        }
    }

    // Try to extract from page title
    if let Some(captures) = regex::Regex::new(r"<title>([^<]+)</title>")
        .unwrap()
        .captures(html)
    {
        if let Some(title) = captures.get(1) {
            let title_text = title.as_str();
            // Look for "in City, State, Country" pattern
            if title_text.contains("United States") {
                return (None, Some("United States".to_string()));
            }
            // Check for other countries
            let countries = [
                "Canada",
                "Mexico",
                "United Kingdom",
                "France",
                "Germany",
                "Spain",
                "Italy",
                "Australia",
                "Japan",
            ];
            for country in countries {
                if title_text.contains(country) {
                    return (None, Some(country.to_string()));
                }
            }
        }
    }

    debug!("No valid region/country found");
    (None, None)
}

pub(crate) fn extract_meta_content(html: &str, property: &str) -> Option<String> {
    // Try property="og:title" format
    let property_pattern = format!(
        r#"<meta property="{}" content="([^"]+)""#,
        regex::escape(property)
    );
    if let Some(captures) = regex::Regex::new(&property_pattern).unwrap().captures(html) {
        if let Some(content) = captures.get(1) {
            return Some(
                content
                    .as_str()
                    .replace("&quot;", "\"")
                    .replace("&amp;", "&"),
            );
        }
    }

    // Try name="description" format
    let name_pattern = format!(
        r#"<meta name="{}" content="([^"]+)""#,
        regex::escape(property)
    );
    if let Some(captures) = regex::Regex::new(&name_pattern).unwrap().captures(html) {
        if let Some(content) = captures.get(1) {
            return Some(
                content
                    .as_str()
                    .replace("&quot;", "\"")
                    .replace("&amp;", "&"),
            );
        }
    }

    None
}

// Extract data from script tag with id="data-deferred-state-0"
pub(crate) fn extract_airbnb_json_data(html: &str) -> Option<serde_json::Value> {
    use tracing::{debug, info};

    // Look for the script tag containing JSON data
    let pattern =
        r#"<script id="data-deferred-state-0"[^>]*type="application/json">([^<]+)</script>"#;
    if let Ok(regex) = regex::Regex::new(pattern) {
        if let Some(captures) = regex.captures(html) {
            if let Some(json_str) = captures.get(1) {
                let json_text = json_str.as_str().trim();
                debug!("Found Airbnb JSON data, length: {} chars", json_text.len());

                match serde_json::from_str::<serde_json::Value>(json_text) {
                    Ok(json_value) => {
                        info!("Successfully parsed Airbnb JSON data");
                        return Some(json_value);
                    }
                    Err(e) => {
                        debug!("Failed to parse Airbnb JSON data: {}", e);
                    }
                }
            }
        }
    }

    debug!("No Airbnb JSON data found in HTML");
    None
}

// Extract listing data from the Airbnb JSON structure
/// What one listing looks like when pulled straight out of Airbnb's embedded
/// JSON, before it becomes a `Listing`: title, price, rating, picture URL,
/// location, description, amenities.
pub(crate) type RawListingFields = (String, String, String, String, String, String, Vec<String>);

pub(crate) fn extract_listing_from_json(json_data: &serde_json::Value) -> Option<RawListingFields> {
    use tracing::{debug, info};

    // Navigate to the search results in the JSON structure
    let search_results = json_data
        .get("niobeClientData")?
        .get(1)?
        .get("data")?
        .get("presentation")?
        .get("staysSearch")?
        .get("results")?
        .get("searchResults")?
        .as_array()?;

    if search_results.is_empty() {
        debug!("No search results found in JSON data");
        return None;
    }

    // Get the first listing for now (in a full scraper, you'd iterate through all)
    let first_listing = &search_results[0];

    // Extract title
    let title = first_listing
        .get("title")?
        .as_str()
        .unwrap_or("")
        .to_string();

    // Extract price - try multiple possible price fields
    let price = first_listing
        .get("structuredDisplayPrice")?
        .get("primaryLine")?
        .get("price")
        .and_then(|p| p.as_str())
        .or_else(|| {
            // Try discounted price field
            first_listing
                .get("structuredDisplayPrice")?
                .get("primaryLine")?
                .get("discountedPrice")
                .and_then(|p| p.as_str())
        })
        .filter(|p| !p.trim().is_empty()) // Filter out empty strings
        .map(|p| p.to_string())
        .unwrap_or_else(|| {
            // If no price found in JSON structure, return empty to trigger fallback
            debug!("No price found in JSON structure, will use fallback extraction");
            String::new()
        });

    // Extract rating
    let rating = first_listing
        .get("avgRatingLocalized")?
        .as_str()
        .unwrap_or("")
        .to_string();

    // Extract location from title (e.g., "Condo in Traverse City")
    let location = if title.contains(" in ") {
        title.split(" in ").nth(1).unwrap_or("").to_string()
    } else {
        "".to_string()
    };

    // Extract property name/description
    let description = first_listing
        .get("demandStayListing")?
        .get("description")?
        .get("name")?
        .get("localizedStringWithTranslationPreference")?
        .as_str()
        .unwrap_or("")
        .to_string();

    // Extract picture URL
    let picture_url = first_listing
        .get("contextualPictures")?
        .get(0)?
        .get("picture")?
        .as_str()
        .unwrap_or("")
        .to_string();

    // Extract features (beds, baths, etc.)
    let mut features = Vec::new();

    // Add bed info
    if let Some(bed_info) = first_listing
        .get("structuredContent")?
        .get("primaryLine")?
        .get(0)?
        .get("body")?
        .as_str()
    {
        features.push(bed_info.to_string());
    }

    // Add badges (Superhost, Guest favorite, etc.)
    if let Some(badges) = first_listing.get("badges").and_then(|b| b.as_array()) {
        for badge in badges {
            if let Some(badge_text) = badge.get("text").and_then(|t| t.as_str()) {
                features.push(badge_text.to_string());
            }
        }
    }

    // Add payment messages (Free cancellation, etc.)
    if let Some(payment_msgs) = first_listing
        .get("paymentMessages")
        .and_then(|p| p.as_array())
    {
        for msg in payment_msgs {
            if let Some(msg_text) = msg.get("text").and_then(|t| t.as_str()) {
                features.push(msg_text.to_string());
            }
        }
    }

    info!(
        "Extracted from JSON: title='{}', price='{}', rating='{}', features={}",
        title,
        price,
        rating,
        features.len()
    );

    Some((
        title,
        description,
        price,
        rating,
        location,
        picture_url,
        features,
    ))
}

// Extract all listing URLs from the Airbnb JSON structure
pub(crate) fn extract_all_listing_urls_from_json(json_data: &serde_json::Value) -> Vec<String> {
    use tracing::{debug, info};

    let mut urls = Vec::new();

    // Navigate to the search results in the JSON structure
    if let Some(search_results) = json_data
        .get("niobeClientData")
        .and_then(|d| d.get(1))
        .and_then(|d| d.get("data"))
        .and_then(|d| d.get("presentation"))
        .and_then(|d| d.get("staysSearch"))
        .and_then(|d| d.get("results"))
        .and_then(|d| d.get("searchResults"))
        .and_then(|d| d.as_array())
    {
        info!("Found {} listings in JSON data", search_results.len());

        for (i, listing) in search_results.iter().enumerate() {
            // Try to extract the listing ID from various possible fields
            if let Some(listing_id) = listing
                .get("demandStayListing")
                .and_then(|l| l.get("id"))
                .and_then(|id| id.as_str())
            {
                // The ID is base64 encoded, we need to decode it to get the numeric ID
                if let Ok(decoded_bytes) =
                    base64::engine::general_purpose::STANDARD.decode(listing_id)
                {
                    if let Ok(decoded_str) = std::str::from_utf8(&decoded_bytes) {
                        // Extract numeric ID from decoded string like "DemandStayListing:633486763306530765"
                        if let Some(numeric_id) = decoded_str.split(':').nth(1) {
                            let url = format!("https://www.airbnb.com/rooms/{}", numeric_id);
                            debug!(
                                "Extracted URL {}: {} from listing {}",
                                urls.len() + 1,
                                url,
                                i + 1
                            );
                            urls.push(url);
                        } else {
                            debug!(
                                "Failed to split decoded string for listing {}: '{}'",
                                i + 1,
                                decoded_str
                            );
                        }
                    } else {
                        debug!("Failed to decode base64 to UTF-8 for listing {}", i + 1);
                    }
                } else {
                    debug!(
                        "Failed to decode base64 for listing {}: '{}'",
                        i + 1,
                        listing_id
                    );
                }
            } else {
                debug!("No demandStayListing.id found for listing {}", i + 1);
            }
        }

        if !urls.is_empty() {
            info!(
                "Successfully extracted {} listing URLs from JSON",
                urls.len()
            );
        } else {
            debug!("No valid listing URLs found in JSON structure");
        }
    } else {
        debug!("Could not navigate to search results in JSON structure");
    }

    urls
}

/// Helper function to extract listing ID from URL
pub(crate) fn extract_listing_id_from_url(url: &str) -> String {
    // Extract from /rooms/12345 pattern
    if let Some(start) = url.find("/rooms/") {
        let rest = &url[start + 7..];
        let end = rest
            .find(|c: char| !c.is_ascii_digit())
            .unwrap_or(rest.len());
        return rest[..end].to_string();
    }
    // Fallback
    "unknown".to_string()
}

/// Decode Airbnb's base64 "DemandStayListing:12345" id into the numeric id.
pub(crate) fn decode_listing_id(encoded: &str) -> Option<String> {
    let decoded = base64::engine::general_purpose::STANDARD
        .decode(encoded)
        .ok()?;
    let text = std::str::from_utf8(&decoded).ok()?;
    text.split(':').nth(1).map(|s| s.to_string())
}

/// Pull the first number (with optional decimal) out of a string like
/// "$1,234 night" -> 1234.0 or "4.95 (312)" -> 4.95. Commas are treated as
/// thousands separators.
pub(crate) fn first_number(text: &str) -> Option<f64> {
    let mut buf = String::new();
    let mut seen_dot = false;
    for c in text.chars() {
        if c.is_ascii_digit() {
            buf.push(c);
        } else if c == '.' && !seen_dot && !buf.is_empty() {
            seen_dot = true;
            buf.push(c);
        } else if c == ',' && !buf.is_empty() {
            continue; // thousands separator inside a number
        } else if !buf.is_empty() {
            break; // number ended
        }
    }
    buf.parse::<f64>().ok()
}

/// Build a full Listing from one entry of the search-results JSON array. Returns
/// None if the entry has no decodable id or title.
pub(crate) fn listing_from_search_result(entry: &serde_json::Value) -> Option<Listing> {
    let encoded_id = entry
        .get("demandStayListing")
        .and_then(|l| l.get("id"))
        .and_then(|id| id.as_str())?;
    let numeric_id = decode_listing_id(encoded_id)?;
    let url = format!("https://www.airbnb.com/rooms/{}", numeric_id);

    let title = entry
        .get("title")
        .and_then(|v| v.as_str())
        .unwrap_or_default()
        .to_string();
    if title.is_empty() {
        return None;
    }

    // Price: primaryLine.price, falling back to discountedPrice.
    let price = entry
        .get("structuredDisplayPrice")
        .and_then(|p| p.get("primaryLine"))
        .and_then(|p| {
            p.get("price")
                .and_then(|v| v.as_str())
                .or_else(|| p.get("discountedPrice").and_then(|v| v.as_str()))
        })
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .unwrap_or_default();
    let price_numeric = first_number(&price);

    // Rating: avgRatingLocalized looks like "4.95 (312)".
    let rating_localized = entry
        .get("avgRatingLocalized")
        .and_then(|v| v.as_str())
        .unwrap_or_default();
    let rating = rating_localized
        .split_whitespace()
        .next()
        .unwrap_or_default()
        .to_string();
    let rating_numeric = first_number(rating_localized);
    let reviews_count = rating_localized
        .split_once('(')
        .and_then(|(_, rest)| first_number(rest))
        .map(|n| n as i32);

    // Description / property name.
    let description = entry
        .get("demandStayListing")
        .and_then(|l| l.get("description"))
        .and_then(|d| d.get("name"))
        .and_then(|n| n.get("localizedStringWithTranslationPreference"))
        .and_then(|v| v.as_str())
        .unwrap_or_default()
        .to_string();

    // Location parsed from a "<Type> in <Place>" title.
    let location = title
        .split_once(" in ")
        .map(|(_, place)| place.to_string())
        .unwrap_or_default();

    // Pictures.
    let mut pictures = Vec::new();
    if let Some(pics) = entry.get("contextualPictures").and_then(|p| p.as_array()) {
        for pic in pics {
            if let Some(u) = pic.get("picture").and_then(|v| v.as_str()) {
                pictures.push(u.to_string());
            }
        }
    }
    let picture_url = pictures.first().cloned().unwrap_or_default();

    // Coordinates (used by the map-tiling pass). Current Airbnb nests these
    // under demandStayListing.location.coordinate; older shapes put a
    // coordinate/coordinates object at the entry top level.
    let coordinates = entry
        .get("demandStayListing")
        .and_then(|d| d.get("location"))
        .and_then(|l| l.get("coordinate"))
        .and_then(|c| {
            let lat = c.get("latitude").and_then(|v| v.as_f64())?;
            let lng = c.get("longitude").and_then(|v| v.as_f64())?;
            Some(Coordinates { lat, lng })
        })
        .or_else(|| {
            entry
                .get("coordinate")
                .or_else(|| entry.get("coordinates"))
                .and_then(|c| {
                    let lat = c
                        .get("latitude")
                        .or_else(|| c.get("lat"))
                        .and_then(|v| v.as_f64())?;
                    let lng = c
                        .get("longitude")
                        .or_else(|| c.get("lng"))
                        .and_then(|v| v.as_f64())?;
                    Some(Coordinates { lat, lng })
                })
        });

    // Features: bed/room summary + badges + payment messages.
    let mut features = Vec::new();
    if let Some(body) = entry
        .get("structuredContent")
        .and_then(|s| s.get("primaryLine"))
        .and_then(|p| p.get(0))
        .and_then(|p| p.get("body"))
        .and_then(|v| v.as_str())
    {
        if !body.is_empty() {
            features.push(body.to_string());
        }
    }
    if let Some(badges) = entry.get("badges").and_then(|b| b.as_array()) {
        for badge in badges {
            if let Some(t) = badge.get("text").and_then(|v| v.as_str()) {
                if !t.is_empty() {
                    features.push(t.to_string());
                }
            }
        }
    }
    if let Some(msgs) = entry.get("paymentMessages").and_then(|p| p.as_array()) {
        for msg in msgs {
            if let Some(t) = msg.get("text").and_then(|v| v.as_str()) {
                if !t.is_empty() {
                    features.push(t.to_string());
                }
            }
        }
    }

    Some(Listing {
        id: None,
        url,
        title,
        picture_url,
        pictures,
        description,
        price,
        price_numeric,
        rating,
        rating_numeric,
        reviews_count,
        location,
        coordinates,
        features,
        house_details: Vec::new(),
        host: None,
        region: None,
        country: None,
        property_type: None,
        created_at: None,
        scraped_at: Some(chrono::Utc::now()),
    })
}

/// Locate the staysSearch `searchResults` array inside the deferred-state JSON.
/// Airbnb has nested `niobeClientData` differently over time — as `[…, {data}]`
/// (older) and `[[…, {data}]]` (current) — so probe each container and its
/// immediate children rather than hard-coding one index path.
/// Locate the `staysSearch.results` node. Both the listing array and the
/// pagination cursor hang off it, so resolving it once keeps the two in sync —
/// a cursor read from a different node than the results it paginates is how
/// you silently re-harvest page one forever.
fn find_stays_search_results(json_data: &serde_json::Value) -> Option<&serde_json::Value> {
    let niobe = json_data.get("niobeClientData")?.as_array()?;
    let mut candidates: Vec<&serde_json::Value> = Vec::new();
    for item in niobe {
        candidates.push(item);
        if let Some(inner) = item.as_array() {
            for x in inner {
                candidates.push(x);
            }
        }
    }
    for c in candidates {
        let results = c
            .get("data")
            .and_then(|d| d.get("presentation"))
            .and_then(|d| d.get("staysSearch"))
            .and_then(|d| d.get("results"));
        // Only accept a node that actually carries the listing array; some
        // responses contain a skeleton `results` with no searchResults yet.
        if let Some(r) = results {
            if r.get("searchResults").and_then(|s| s.as_array()).is_some() {
                return Some(r);
            }
        }
    }
    None
}

pub(crate) fn find_search_results(
    json_data: &serde_json::Value,
) -> Option<&Vec<serde_json::Value>> {
    find_stays_search_results(json_data)?
        .get("searchResults")?
        .as_array()
}

/// Every page cursor for this search, in page order, index 0 being the page
/// that was just fetched.
///
/// Airbnb does NOT ship a chained `nextPageCursor` — it ships the whole list
/// up front under `paginationInfo.pageCursors`, each entry a base64 blob like
/// `{"section_offset":0,"items_offset":18,"version":1}`. A live search page
/// returns 15 of them (15 x 18 = 270 results), which is where
/// `TILE_SPLIT_THRESHOLD` comes from.
///
/// Having the full list means the caller knows the page count before it
/// starts, and never has to chain a cursor out of each response.
pub(crate) fn find_page_cursors(json_data: &serde_json::Value) -> Vec<String> {
    let Some(info) = find_stays_search_results(json_data).and_then(|r| r.get("paginationInfo"))
    else {
        return Vec::new();
    };
    info.get("pageCursors")
        .and_then(|c| c.as_array())
        .map(|arr| {
            arr.iter()
                .filter_map(|v| v.as_str())
                .filter(|s| !s.is_empty())
                .map(str::to_string)
                .collect()
        })
        .unwrap_or_default()
}

/// Phase 1: build listings for every result in the search-page JSON.
pub fn extract_all_listings_from_json(json_data: &serde_json::Value) -> Vec<Listing> {
    use tracing::{debug, info};
    let mut listings = Vec::new();
    match find_search_results(json_data) {
        Some(arr) => {
            for entry in arr {
                if let Some(listing) = listing_from_search_result(entry) {
                    listings.push(listing);
                }
            }
            info!("[FAST] Built {} listings from search JSON", listings.len());
        }
        None => debug!("[FAST] searchResults array not found in JSON"),
    }
    listings
}

#[cfg(test)]
mod extract_tests {
    use super::*;
    use serde_json::json;

    // These parsers are the app's contact surface with Airbnb's markup. They
    // all fail soft (None / empty vec), so a markup change breaks scraping
    // silently — pinning the shapes we rely on is the only early warning.

    fn ld(body: &str) -> String {
        format!(
            r#"<html><head><script type="application/ld+json">{}</script></head></html>"#,
            body
        )
    }

    // ---- extract_json_ld ----

    #[test]
    fn json_ld_parses_embedded_block() {
        let html = ld(r#"{"name":"Cosy cabin","description":"A place"}"#);
        let v = extract_json_ld(&html).expect("should parse");
        assert_eq!(v.get("name").and_then(|v| v.as_str()), Some("Cosy cabin"));
    }

    #[test]
    fn json_ld_returns_none_for_malformed_json() {
        let html = ld(r#"{"name": }"#);
        assert!(extract_json_ld(&html).is_none());
    }

    #[test]
    fn json_ld_returns_none_when_absent() {
        assert!(extract_json_ld("<html><body>nothing</body></html>").is_none());
    }

    #[test]
    fn json_ld_handles_attributes_before_type() {
        let html = r#"<script id="x" type="application/ld+json">{"name":"With attrs"}</script>"#;
        let v = extract_json_ld(html).expect("should parse despite extra attributes");
        assert_eq!(v.get("name").and_then(|v| v.as_str()), Some("With attrs"));
    }

    // ---- extract_meta_content ----

    #[test]
    fn meta_content_reads_property_form() {
        let html = r#"<meta property="og:title" content="Loft in Paris">"#;
        assert_eq!(
            extract_meta_content(html, "og:title"),
            Some("Loft in Paris".into())
        );
    }

    #[test]
    fn meta_content_reads_name_form() {
        let html = r#"<meta name="description" content="A nice place">"#;
        assert_eq!(
            extract_meta_content(html, "description"),
            Some("A nice place".into())
        );
    }

    #[test]
    fn meta_content_unescapes_entities() {
        let html = r#"<meta property="og:title" content="Bed &amp; Breakfast &quot;Sun&quot;">"#;
        assert_eq!(
            extract_meta_content(html, "og:title"),
            Some(r#"Bed & Breakfast "Sun""#.into())
        );
    }

    #[test]
    fn meta_content_missing_is_none() {
        assert!(extract_meta_content("<html></html>", "og:title").is_none());
    }

    // ---- extract_title_from_source ----

    #[test]
    fn title_prefers_json_ld_name() {
        let html = format!(
            "{}{}",
            ld(r#"{"name":"From JSON-LD"}"#),
            r#"<meta property="og:title" content="From og">"#
        );
        assert_eq!(
            extract_title_from_source(&html),
            Some("From JSON-LD".into())
        );
    }

    #[test]
    fn title_falls_back_to_og_then_title_tag() {
        let og = r#"<meta property="og:title" content="From og"><title>From title</title>"#;
        assert_eq!(extract_title_from_source(og), Some("From og".into()));

        let only_title = "<title>From title</title>";
        assert_eq!(
            extract_title_from_source(only_title),
            Some("From title".into())
        );
    }

    #[test]
    fn title_trims_apartments_suffix_from_title_tag() {
        let html = r#"<title>Sunny Loft - Apartments for Rent</title>"#;
        assert_eq!(extract_title_from_source(html), Some("Sunny Loft".into()));
    }

    #[test]
    fn title_ignores_placeholder_json_ld_name() {
        // A lone quote is Airbnb noise, not a title; we must fall through.
        let html = format!(
            "{}{}",
            ld(r#"{"name":"\""}"#),
            r#"<meta property="og:title" content="Real title">"#
        );
        assert_eq!(extract_title_from_source(&html), Some("Real title".into()));
    }

    #[test]
    fn title_none_when_nothing_present() {
        assert!(extract_title_from_source("<html><body></body></html>").is_none());
    }

    // ---- safe_truncate ----

    #[test]
    fn safe_truncate_shorter_than_limit_is_unchanged() {
        assert_eq!(safe_truncate("short", 100), "short");
    }

    #[test]
    fn safe_truncate_never_splits_a_utf8_char() {
        // This is the whole point of the function: naive slicing on a byte
        // index inside a multi-byte char panics.
        let s = "café🏖️beach";
        for limit in 0..s.len() + 5 {
            let out = safe_truncate(s, limit);
            assert!(s.starts_with(out), "truncation must be a prefix");
            assert!(out.len() <= limit);
        }
    }

    #[test]
    fn safe_truncate_emoji_only_string() {
        let s = "🏖️🏔️🌋";
        let out = safe_truncate(s, 5);
        assert!(s.starts_with(out));
        // Must not panic and must stay valid UTF-8 (guaranteed by &str).
        assert!(out.chars().count() <= 3);
    }

    // ---- extract_description_from_source ----

    #[test]
    fn description_from_json_ld() {
        let html = ld(r#"{"description":"A lovely cabin by the lake."}"#);
        assert_eq!(
            extract_description_from_source(&html),
            Some("A lovely cabin by the lake.".into())
        );
    }

    #[test]
    fn description_rejects_short_or_placeholder_values() {
        for bad in [
            r#"{"description":"null"}"#,
            r#"{"description":"undefined"}"#,
            r#"{"description":"tiny"}"#,
            r#"{"description":""}"#,
        ] {
            let html = ld(bad);
            assert!(
                extract_description_from_source(&html).is_none(),
                "should reject {bad}"
            );
        }
    }

    // ---- price extraction ----

    #[test]
    fn price_from_text_handles_formats_and_thousands() {
        assert_eq!(extract_price_from_text("$150 night"), Some("$150".into()));
        assert_eq!(
            extract_price_from_text("$1,500 total"),
            Some("$1,500".into())
        );
        assert_eq!(
            extract_price_from_text("120 per night"),
            Some("$120".into())
        );
        assert_eq!(extract_price_from_text("95/night"), Some("$95".into()));
    }

    #[test]
    fn price_from_text_rejects_out_of_range_values() {
        // Guards against picking up "$1" badges or absurd figures.
        assert_eq!(extract_price_from_text("$9 fee"), None);
        assert_eq!(extract_price_from_text("$99,999 nonsense"), None);
    }

    #[test]
    fn price_from_text_none_when_absent() {
        assert_eq!(extract_price_from_text("no prices here"), None);
    }

    #[test]
    fn price_from_title_variants() {
        assert_eq!(
            extract_price_from_title("Loft — $199 a night"),
            Some("$199".into())
        );
        assert_eq!(
            extract_price_from_title("199 per night"),
            Some("$199".into())
        );
        assert_eq!(extract_price_from_title("199/night"), Some("$199".into()));
        assert_eq!(extract_price_from_title("no price"), None);
    }

    // ---- rating / location ----

    #[test]
    fn rating_from_json_ld_aggregate() {
        let html = ld(r#"{"aggregateRating":{"ratingValue":4.89}}"#);
        assert_eq!(extract_rating_from_source(&html), Some("4.89".into()));
    }

    #[test]
    fn rating_from_og_title_star() {
        let html = r#"<meta property="og:title" content="Loft ★4.75 · 2 beds">"#;
        assert_eq!(extract_rating_from_source(html), Some("4.75".into()));
    }

    #[test]
    fn rating_none_when_absent() {
        assert!(extract_rating_from_source("<html></html>").is_none());
    }

    #[test]
    fn location_from_json_ld_address() {
        let html = ld(r#"{"address":{"addressLocality":"Lisbon"}}"#);
        assert_eq!(extract_location_from_source(&html), Some("Lisbon".into()));
    }

    #[test]
    fn location_from_og_title_between_in_and_separator() {
        let html = r#"<meta property="og:title" content="Cosy loft in Porto ·  Portugal">"#;
        assert_eq!(extract_location_from_source(html), Some("Porto".into()));
    }

    // ---- ids and numbers ----

    #[test]
    fn listing_id_from_url() {
        assert_eq!(
            extract_listing_id_from_url("https://airbnb.com/rooms/12345"),
            "12345"
        );
        assert_eq!(
            extract_listing_id_from_url("https://airbnb.com/rooms/98765?check_in=2026-01-01"),
            "98765"
        );
        assert_eq!(
            extract_listing_id_from_url("https://airbnb.com/experiences/1"),
            "unknown"
        );
    }

    #[test]
    fn decode_listing_id_from_base64() {
        use base64::Engine as _;
        let encoded = base64::engine::general_purpose::STANDARD.encode("DemandStayListing:54321");
        assert_eq!(decode_listing_id(&encoded), Some("54321".into()));
    }

    #[test]
    fn decode_listing_id_rejects_garbage() {
        assert_eq!(decode_listing_id("!!!not base64!!!"), None);
    }

    #[test]
    fn first_number_parses_prices_and_ratings() {
        assert_eq!(first_number("$1,234 night"), Some(1234.0));
        assert_eq!(first_number("4.95 (312)"), Some(4.95));
        assert_eq!(first_number("$1,234.56"), Some(1234.56));
        assert_eq!(first_number("no digits"), None);
        assert_eq!(first_number(""), None);
    }

    // ---- URL / region ----

    #[test]
    fn region_from_url_query_parameter() {
        let url = "https://www.airbnb.com/s/homes?location=Toronto%2C%20ON%2C%20Canada";
        assert_eq!(extract_region_from_url(url), Some("Toronto".into()));
    }

    #[test]
    fn region_from_url_none_for_unrelated() {
        assert!(extract_region_from_url("https://example.com/").is_none());
    }

    // ---- listing URL harvesting ----

    #[test]
    fn urls_from_json_value_collects_room_links() {
        let v = json!({
            "a": {"url": "https://www.airbnb.com/rooms/111"},
            "b": [{"url": "https://www.airbnb.com/rooms/222"}],
        });
        let mut urls = Vec::new();
        extract_urls_from_json_value(&v, &mut urls);
        assert!(urls.iter().any(|u| u.contains("111")), "got {urls:?}");
        assert!(urls.iter().any(|u| u.contains("222")), "got {urls:?}");
    }

    #[test]
    fn urls_from_source_finds_room_paths() {
        let html = r#"<a href="/rooms/12345">x</a><a href="/rooms/67890?x=1">y</a>"#;
        let urls = extract_listing_urls_from_source(html);
        assert!(urls.iter().any(|u| u.contains("12345")), "got {urls:?}");
    }

    #[test]
    fn urls_from_source_empty_when_none() {
        assert!(extract_listing_urls_from_source("<html>nothing</html>").is_empty());
    }
}

#[cfg(test)]
mod pagination_tests {
    use super::*;
    use serde_json::json;

    // Wrap a staysSearch `results` node in the niobeClientData envelope the
    // real page ships, so these tests exercise the same traversal as prod.
    fn envelope(results: serde_json::Value) -> serde_json::Value {
        json!({
            "niobeClientData": [[
                {"data": {"presentation": {"staysSearch": {"results": results}}}}
            ]]
        })
    }

    // Verbatim from a live search response. Airbnb ships the whole cursor list
    // under `pageCursors` — there is NO `nextPageCursor` field, which is what
    // an earlier version of this reader looked for, so it found nothing and
    // paginated exactly one page.
    #[test]
    fn reads_the_full_page_cursor_list() {
        let v = envelope(json!({
            "searchResults": [],
            "paginationInfo": {
                "__typename": "StaysSearchPaginationInfo",
                "pageCursors": [
                    "eyJzZWN0aW9uX29mZnNldCI6MCwiaXRlbXNfb2Zmc2V0IjowLCJ2ZXJzaW9uIjoxfQ==",
                    "eyJzZWN0aW9uX29mZnNldCI6MCwiaXRlbXNfb2Zmc2V0IjoxOCwidmVyc2lvbiI6MX0=",
                    "eyJzZWN0aW9uX29mZnNldCI6MCwiaXRlbXNfb2Zmc2V0IjozNiwidmVyc2lvbiI6MX0="
                ]
            }
        }));
        let cursors = find_page_cursors(&v);
        assert_eq!(cursors.len(), 3);
        // Index 0 is the page already fetched; the caller skips it.
        assert!(cursors[1].starts_with("eyJzZWN0aW9uX29mZnNldCI6MCwiaXRlbXNfb2Zmc2V0IjoxOCw"));
    }

    #[test]
    fn missing_pagination_info_yields_no_cursors() {
        let v = envelope(json!({"searchResults": []}));
        assert!(find_page_cursors(&v).is_empty());
    }

    // A single-page result set still lists its own cursor; that must not be
    // mistaken for "there is a page 2".
    #[test]
    fn single_page_yields_one_cursor() {
        let v = envelope(json!({
            "searchResults": [],
            "paginationInfo": {"pageCursors": ["eyJpdGVtc19vZmZzZXQiOjB9"]}
        }));
        assert_eq!(find_page_cursors(&v).len(), 1);
    }

    #[test]
    fn empty_entries_are_dropped() {
        let v = envelope(json!({
            "searchResults": [],
            "paginationInfo": {"pageCursors": ["a", "", "b", null]}
        }));
        assert_eq!(
            find_page_cursors(&v),
            vec!["a".to_string(), "b".to_string()]
        );
    }

    // A skeleton `results` node with no searchResults must not shadow the real
    // one; otherwise cursors come from a node that paginates nothing.
    #[test]
    fn skips_skeleton_results_node() {
        let v = json!({
            "niobeClientData": [[
                {"data": {"presentation": {"staysSearch": {"results": {
                    "paginationInfo": {"pageCursors": ["WRONG"]}
                }}}}},
                {"data": {"presentation": {"staysSearch": {"results": {
                    "searchResults": [],
                    "paginationInfo": {"pageCursors": ["RIGHT"]}
                }}}}}
            ]]
        });
        assert_eq!(find_page_cursors(&v), vec!["RIGHT".to_string()]);
        assert!(find_search_results(&v).is_some());
    }
}
