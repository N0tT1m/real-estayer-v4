// Split out of the former monolithic scraping.rs. Code is unchanged;
// only visibility was widened so cross-module calls resolve.

use super::*;

/// Normalize an amenity name to a canonical form
/// Uses contains-based matching to handle Airbnb's verbose amenity names
/// e.g., "Shared hot tub - available all year, open specific hours" -> "Hot Tub"
pub(crate) fn normalize_amenity(amenity: &str) -> String {
    let lower = amenity.to_lowercase();
    let lower = lower.trim();

    // Check contains for each amenity type (order matters - more specific first)

    // Hot tub variations - all the slang and brand names
    if lower.contains("hot tub") || lower.contains("hottub") || lower.contains("hot-tub")
        || lower.contains("jacuzzi") || lower.contains("jaccuzi") || lower.contains("jacuzi")
        || lower.contains("whirlpool") || lower.contains("jetted tub") || lower.contains("jet tub")
        || lower.contains("soaking tub") || lower.contains("spa tub") || lower.contains("bubble tub")
        || lower.contains("hydrotherapy") || lower.contains("plunge pool")
        || (lower.contains("spa") && !lower.contains("space")) // "spa" but not "workspace"
    {
        return "Hot Tub".to_string();
    }

    // Pool variations (after hot tub to avoid "plunge pool" matching pool)
    if lower.contains("pool") || lower.contains("swimming") {
        return "Pool".to_string();
    }

    // Waterfront/views - check before beach access
    if lower.contains("waterfront") || lower.contains("lakefront") || lower.contains("beachfront")
        || lower.contains("oceanfront") || lower.contains("riverfront") || lower.contains("seafront")
        || lower.contains("lake view") || lower.contains("ocean view") || lower.contains("sea view")
        || lower.contains("water view") || lower.contains("beach view") || lower.contains("bay view")
        || lower.contains("harbor view") || lower.contains("marina view")
    {
        return "Waterfront".to_string();
    }

    // Beach/water access
    if lower.contains("beach access") || lower.contains("lake access") || lower.contains("private beach")
        || lower.contains("dock") || lower.contains("pier") || lower.contains("boat slip")
        || lower.contains("kayak") || lower.contains("canoe")
    {
        return "Beach Access".to_string();
    }

    // Air conditioning
    if lower.contains("air conditioning") || lower.contains("air-conditioning") || lower.contains("aircon")
        || lower.contains("central air") || lower.contains("mini split") || lower.contains("climate control")
        || lower == "a/c" || lower == "ac"
    {
        return "Air Conditioning".to_string();
    }

    // WiFi/Internet
    if lower.contains("wifi") || lower.contains("wi-fi") || lower.contains("wi fi")
        || lower.contains("wireless") || lower.contains("internet") || lower.contains("broadband")
    {
        return "WiFi".to_string();
    }

    // Kitchen (check before workspace to avoid "kitchenette" issues)
    if lower.contains("kitchen") || lower.contains("kitchenette") || lower.contains("cooking")
        || lower.contains("stove") || lower.contains("oven") || lower.contains("microwave")
        || lower.contains("refrigerator") || lower.contains("fridge")
    {
        return "Kitchen".to_string();
    }

    // Washer. The dishwasher guard matters: "dishwasher" contains "washer",
    // so without it the Dishwasher branch below is unreachable and every
    // dishwasher is mislabelled as laundry.
    if (lower.contains("washer") || lower.contains("washing machine") || lower.contains("laundry")
        || lower.contains("clothes washer"))
        && !lower.contains("dishwasher")
    {
        return "Washer".to_string();
    }

    // Dryer. Same shape as above: "hair dryer" contains "dryer", which would
    // otherwise shadow the Hair Dryer branch and label it a clothes dryer.
    if (lower.contains("dryer") || lower.contains("tumble dry") || lower.contains("clothes dry"))
        && !lower.contains("hair dryer")
        && !lower.contains("hairdryer")
        && !lower.contains("blow dryer")
    {
        return "Dryer".to_string();
    }

    // Workspace/office
    if lower.contains("workspace") || lower.contains("work space") || lower.contains("desk")
        || lower.contains("office") || lower.contains("work from home")
    {
        return "Workspace".to_string();
    }

    // Parking
    if lower.contains("parking") || lower.contains("garage") || lower.contains("carport")
        || lower.contains("driveway") || lower.contains("car park")
    {
        return "Parking".to_string();
    }

    // Gym/Fitness
    if lower.contains("gym") || lower.contains("fitness") || lower.contains("exercise")
        || lower.contains("workout") || lower.contains("weights") || lower.contains("treadmill")
    {
        return "Gym".to_string();
    }

    // Fireplace
    if lower.contains("fireplace") || lower.contains("fire place") || lower.contains("wood burning")
        || lower.contains("gas fire") || lower.contains("fire pit")
    {
        return "Fireplace".to_string();
    }

    // BBQ/Grill
    if lower.contains("bbq") || lower.contains("grill") || lower.contains("barbecue")
        || lower.contains("outdoor kitchen") || lower.contains("smoker")
    {
        return "BBQ Grill".to_string();
    }

    // EV Charger
    if lower.contains("ev charger") || lower.contains("ev charging") || lower.contains("electric vehicle")
        || lower.contains("tesla charger") || lower.contains("charging station")
    {
        return "EV Charger".to_string();
    }

    // Pets
    if lower.contains("pets allowed") || lower.contains("pet friendly") || lower.contains("pet-friendly")
        || lower.contains("pets ok") || lower.contains("dog friendly") || lower.contains("cat friendly")
        || lower.contains("dogs allowed") || lower.contains("cats allowed")
    {
        return "Pets Allowed".to_string();
    }

    // Heating
    if lower.contains("heating") || lower.contains("heater") || lower.contains("furnace")
        || lower.contains("radiant heat") || lower.contains("central heat")
        || (lower.contains("heated") && !lower.contains("heated pool"))
        || lower == "heat"
    {
        return "Heating".to_string();
    }

    // TV/Entertainment
    if lower.contains("tv") || lower.contains("television") || lower.contains("smart tv")
        || lower.contains("cable") || lower.contains("netflix") || lower.contains("streaming")
        || lower.contains("home theater") || lower.contains("projector") || lower.contains("roku")
        || lower.contains("apple tv") || lower.contains("chromecast")
    {
        return "TV".to_string();
    }

    // Sauna/Steam
    if lower.contains("sauna") || lower.contains("steam room") || lower.contains("steam shower") {
        return "Sauna".to_string();
    }

    // Elevator/Accessibility
    if lower.contains("elevator") || lower.contains("lift") || lower.contains("wheelchair")
        || lower.contains("accessible") || lower.contains("step-free")
    {
        return "Elevator".to_string();
    }

    // Security
    if lower.contains("security") || lower.contains("alarm") || lower.contains("safe")
        || lower.contains("lockbox") || lower.contains("doorman") || lower.contains("concierge")
        || lower.contains("gated") || lower.contains("security camera")
    {
        return "Security".to_string();
    }

    // Outdoor space
    if lower.contains("balcony") || lower.contains("patio") || lower.contains("deck")
        || lower.contains("terrace") || lower.contains("garden") || lower.contains("yard")
        || lower.contains("porch") || lower.contains("veranda") || lower.contains("rooftop")
    {
        return "Outdoor Space".to_string();
    }

    // Game room
    if lower.contains("game room") || lower.contains("pool table") || lower.contains("billiards")
        || lower.contains("ping pong") || lower.contains("foosball") || lower.contains("arcade")
    {
        return "Game Room".to_string();
    }

    // Crib/baby bed
    if lower.contains("crib") || lower.contains("baby bed") || lower.contains("pack 'n play")
        || lower.contains("pack n play") || lower.contains("travel crib") || lower.contains("bassinet")
    {
        return "Crib".to_string();
    }

    // High chair
    if lower.contains("high chair") || lower.contains("highchair") || lower.contains("booster seat") {
        return "High Chair".to_string();
    }

    // Bathtub (regular, not hot tub - soaking tub is handled in Hot Tub above)
    if lower.contains("bathtub") || lower.contains("bath tub")
        || lower.contains("clawfoot tub") || lower.contains("garden tub")
    {
        return "Bathtub".to_string();
    }

    // Coffee maker
    if lower.contains("coffee maker") || lower.contains("coffee machine") || lower.contains("espresso")
        || lower.contains("keurig") || lower.contains("nespresso") || lower.contains("french press")
    {
        return "Coffee Maker".to_string();
    }

    // Dishwasher
    if lower.contains("dishwasher") {
        return "Dishwasher".to_string();
    }

    // Self check-in
    if lower.contains("self check-in") || lower.contains("self checkin") || lower.contains("keyless")
        || lower.contains("smart lock") || lower.contains("keypad") || lower.contains("lockbox")
    {
        return "Self Check-in".to_string();
    }

    // Hair dryer
    if lower.contains("hair dryer") || lower.contains("hairdryer") || lower.contains("blow dryer") {
        return "Hair Dryer".to_string();
    }

    // Iron
    if lower.contains("iron") && !lower.contains("ironing board") {
        return "Iron".to_string();
    }

    // Long term stays
    if lower.contains("long term") || lower.contains("monthly") {
        return "Long Term Stays".to_string();
    }

    // Default: Capitalize first letter of each word
    amenity.split_whitespace()
        .map(|word| {
            let mut chars = word.chars();
            match chars.next() {
                Some(first) => first.to_uppercase().chain(chars).collect(),
                None => String::new(),
            }
        })
        .collect::<Vec<_>>()
        .join(" ")
}

/// Extract amenities from page source
pub(crate) fn extract_amenities_from_source(html: &str) -> Vec<String> {
    use tracing::info;
    let mut amenities = Vec::new();

    info!("[AMENITIES] Starting amenity extraction from page source ({} bytes)", html.len());

    // Try to find amenities in the Airbnb JSON data
    if let Some(json_data) = extract_airbnb_json_data(html) {
        info!("[AMENITIES] Found embedded JSON data, searching for amenities...");
        // Helper to extract amenity title from an amenity object
        fn extract_amenity_title(amenity: &serde_json::Value, amenities: &mut Vec<String>) {
            // Only add if available (or if available field doesn't exist)
            let is_available = amenity.get("available").and_then(|v| v.as_bool()).unwrap_or(true);
            if !is_available {
                return;
            }

            if let Some(title) = amenity.get("title").and_then(|t| t.as_str()) {
                if !title.is_empty() && !amenities.contains(&title.to_string()) {
                    amenities.push(title.to_string());
                }
            }
        }

        // Helper to process amenity groups (works for both old and new Airbnb structures)
        fn process_amenity_groups(groups: &serde_json::Value, amenities: &mut Vec<String>) {
            if let Some(arr) = groups.as_array() {
                for group in arr {
                    // Try "amenities" array (old structure)
                    if let Some(amenity_list) = group.get("amenities").and_then(|a| a.as_array()) {
                        for amenity in amenity_list {
                            extract_amenity_title(amenity, amenities);
                        }
                    }
                    // Try "amenities" as direct items
                    if let Some(items) = group.get("items").and_then(|i| i.as_array()) {
                        for item in items {
                            extract_amenity_title(item, amenities);
                        }
                    }
                    // The group itself might have title (category name - skip these)
                }
            }
        }

        // Helper to recursively find amenity-related data
        fn find_amenities_in_json(value: &serde_json::Value, amenities: &mut Vec<String>, depth: usize) {
            // Increased limits to capture all amenities
            if depth > 25 || amenities.len() > 200 {
                return;
            }

            match value {
                serde_json::Value::Object(map) => {
                    // NEW: Look for seeAllAmenitiesGroups (Airbnb's current structure)
                    if let Some(groups) = map.get("seeAllAmenitiesGroups") {
                        process_amenity_groups(groups, amenities);
                    }

                    // NEW: Look for previewAmenitiesGroups
                    if let Some(groups) = map.get("previewAmenitiesGroups") {
                        process_amenity_groups(groups, amenities);
                    }

                    // Look for amenityGroups (older structure)
                    if let Some(groups) = map.get("amenityGroups") {
                        process_amenity_groups(groups, amenities);
                    }

                    // Look for previewAmenities (array of amenity objects)
                    if let Some(preview) = map.get("previewAmenities").and_then(|p| p.as_array()) {
                        for amenity in preview {
                            extract_amenity_title(amenity, amenities);
                        }
                    }

                    // Look for highlightedAmenities
                    if let Some(highlighted) = map.get("highlightedAmenities").and_then(|h| h.as_array()) {
                        for amenity in highlighted {
                            extract_amenity_title(amenity, amenities);
                        }
                    }

                    // Look for amenity sections by title
                    if let Some(title) = map.get("title").and_then(|t| t.as_str()) {
                        if title.to_lowercase().contains("amenities") ||
                           title.to_lowercase().contains("offers") ||
                           title.to_lowercase().contains("what this place") {
                            if let Some(items) = map.get("items").and_then(|i| i.as_array()) {
                                for item in items {
                                    extract_amenity_title(item, amenities);
                                }
                            }
                        }
                    }

                    // Look for badges (Superhost, Guest favorite, etc.)
                    if let Some(badges) = map.get("badges").and_then(|b| b.as_array()) {
                        for badge in badges {
                            if let Some(text) = badge.get("text").and_then(|t| t.as_str()) {
                                if !amenities.contains(&text.to_string()) {
                                    amenities.push(text.to_string());
                                }
                            }
                            if let Some(title) = badge.get("title").and_then(|t| t.as_str()) {
                                if !amenities.contains(&title.to_string()) {
                                    amenities.push(title.to_string());
                                }
                            }
                        }
                    }

                    // Check for Superhost flag
                    if map.get("isSuperhost").and_then(|v| v.as_bool()).unwrap_or(false)
                        && !amenities.contains(&"Superhost".to_string()) {
                            amenities.push("Superhost".to_string());
                        }

                    // Look for host badge/tier
                    if let Some(host) = map.get("host") {
                        if host.get("isSuperhost").and_then(|v| v.as_bool()).unwrap_or(false)
                            && !amenities.contains(&"Superhost".to_string()) {
                                amenities.push("Superhost".to_string());
                            }
                    }

                    // Look for Guest favorite / highly rated indicators
                    if let Some(badge_type) = map.get("badgeType").and_then(|b| b.as_str()) {
                        let badge_str = badge_type.to_string();
                        if !amenities.contains(&badge_str) {
                            amenities.push(badge_str);
                        }
                    }

                    // Recurse into nested objects
                    for (_key, val) in map {
                        find_amenities_in_json(val, amenities, depth + 1);
                    }
                }
                serde_json::Value::Array(arr) => {
                    for item in arr {
                        find_amenities_in_json(item, amenities, depth + 1);
                    }
                }
                _ => {}
            }
        }

        find_amenities_in_json(&json_data, &mut amenities, 0);

        if !amenities.is_empty() {
            info!("[AMENITIES] === RAW AMENITIES FROM JSON ({}) ===", amenities.len());
            for (i, amenity) in amenities.iter().enumerate() {
                info!("[AMENITIES]   [{}] {:?}", i + 1, amenity);
            }
            // Normalize and deduplicate
            let normalized: Vec<String> = amenities.iter()
                .map(|a| normalize_amenity(a))
                .collect::<std::collections::HashSet<_>>()
                .into_iter()
                .collect();
            info!("[AMENITIES] === NORMALIZED AMENITIES ({}) ===", normalized.len());
            for (i, amenity) in normalized.iter().enumerate() {
                info!("[AMENITIES]   [{}] {}", i + 1, amenity);
            }
            return normalized;
        } else {
            info!("[AMENITIES] No amenities found in JSON data");
        }
    } else {
        info!("[AMENITIES] No embedded JSON data found in page source");
    }

    // Fallback: Try regex patterns for common amenities (comprehensive list)
    info!("[AMENITIES] Falling back to regex-based amenity detection");
    let common_amenities = [
        // WiFi variations
        "wifi", "Wi-Fi", "wi fi", "wireless", "internet", "broadband",
        // Kitchen variations
        "kitchen", "kitchenette", "full kitchen", "cooking", "stove", "oven", "microwave",
        // Parking variations
        "parking", "Free parking", "garage", "carport", "driveway", "car park",
        // Laundry variations
        "washer", "washing machine", "laundry", "clothes washer",
        "dryer", "tumble dryer", "clothes dryer",
        // Climate control
        "air conditioning", "AC", "A/C", "aircon", "climate control", "central air", "mini split",
        "heating", "heater", "furnace", "radiant heat", "central heating", "heated",
        // Pool variations
        "pool", "swimming", "indoor pool", "outdoor pool", "private pool", "shared pool", "infinity pool",
        // Hot tub/spa variations
        "hot tub", "hottub", "jacuzzi", "jaccuzi", "whirlpool", "jetted tub", "jet tub",
        "soaking tub", "spa tub", "hydrotherapy", "plunge pool", "spa",
        // Gym/fitness variations
        "gym", "fitness", "exercise", "workout", "weights", "treadmill", "home gym",
        // TV/entertainment
        "TV", "television", "smart tv", "cable", "netflix", "streaming", "home theater", "projector",
        // Workspace variations
        "workspace", "dedicated workspace", "work space", "desk", "office", "work from home", "home office",
        // Waterfront variations
        "waterfront", "lakefront", "beachfront", "oceanfront", "riverfront", "seafront",
        "lake view", "ocean view", "sea view", "water view", "beach view", "bay view",
        // Beach/water access
        "beach access", "lake access", "private beach", "beach", "dock", "boat dock", "pier",
        // Fireplace variations
        "fireplace", "fire place", "indoor fireplace", "wood burning", "gas fireplace", "fire pit",
        // BBQ/outdoor cooking
        "bbq", "grill", "barbecue", "outdoor kitchen", "smoker",
        // EV charging
        "ev charger", "ev charging", "electric vehicle", "tesla charger", "charging station",
        // Pets
        "pet friendly", "pets allowed", "pets ok", "dog friendly", "cat friendly",
        // Sauna/steam
        "sauna", "steam room", "steam shower",
        // Elevator/accessibility
        "elevator", "lift", "wheelchair", "accessible",
        // Security
        "security", "alarm", "safe", "lockbox", "doorman", "concierge", "gated",
        // Outdoor space
        "balcony", "patio", "deck", "terrace", "garden", "yard", "outdoor space", "porch",
    ];

    let html_lower = html.to_lowercase();
    for amenity in common_amenities {
        if html_lower.contains(&amenity.to_lowercase()) {
            let normalized = normalize_amenity(amenity);
            if !amenities.contains(&normalized) {
                amenities.push(normalized);
            }
        }
    }

    // Extract badges from HTML using the specific badge CSS class
    // Airbnb uses class="t1qa5xaj" for badge text elements
    use regex::Regex;
    if let Ok(badge_regex) = Regex::new(r#"class="[^"]*t1qa5xaj[^"]*"[^>]*>([^<]+)<"#) {
        for cap in badge_regex.captures_iter(html) {
            if let Some(badge_text) = cap.get(1) {
                let badge = badge_text.as_str().trim().to_string();
                if !badge.is_empty() && !amenities.contains(&badge) {
                    tracing::info!("[BADGE] Found badge from CSS class: '{}'", badge);
                    amenities.push(badge);
                }
            }
        }
    }

    // Also try a simpler pattern - look for the class followed by text
    if let Ok(badge_regex2) = Regex::new(r#"t1qa5xaj[^>]*>([^<]{2,50})<"#) {
        for cap in badge_regex2.captures_iter(html) {
            if let Some(badge_text) = cap.get(1) {
                let badge = badge_text.as_str().trim().to_string();
                // Only add if it looks like a badge (contains known badge keywords)
                let badge_lower = badge.to_lowercase();
                if (badge_lower.contains("superhost") ||
                    badge_lower.contains("guest fav") ||
                    badge_lower.contains("rare find") ||
                    badge_lower.contains("highly rated")) &&
                   !amenities.contains(&badge) {
                    tracing::info!("[BADGE] Found badge: '{}'", badge);
                    amenities.push(badge);
                }
            }
        }
    }

    info!("[AMENITIES] === REGEX FALLBACK AMENITIES ({}) ===", amenities.len());
    for (i, amenity) in amenities.iter().enumerate() {
        info!("[AMENITIES]   [{}] {}", i + 1, amenity);
    }

    amenities
}

/// Extract house details from page source
pub(crate) fn extract_house_details_from_source(html: &str) -> Vec<String> {
    use tracing::debug;
    let mut details = Vec::new();

    // Try Airbnb JSON data first
    if let Some(json_data) = extract_airbnb_json_data(html) {
        // Helper to find house details in JSON
        fn find_house_details_in_json(value: &serde_json::Value, details: &mut Vec<String>, depth: usize) {
            if depth > 15 || details.len() > 20 {
                return;
            }

            match value {
                serde_json::Value::Object(map) => {
                    // Look for sharingConfig which contains room type info
                    if let Some(sharing) = map.get("sharingConfig") {
                        if let Some(property_type) = sharing.get("propertyType").and_then(|t| t.as_str()) {
                            if !details.contains(&property_type.to_string()) {
                                details.push(property_type.to_string());
                            }
                        }
                    }

                    // Look for overview section with room counts
                    if let Some(overview_items) = map.get("overviewItems").and_then(|o| o.as_array()) {
                        for item in overview_items {
                            if let Some(title) = item.get("title").and_then(|t| t.as_str()) {
                                if !details.contains(&title.to_string()) {
                                    details.push(title.to_string());
                                }
                            }
                        }
                    }

                    // Look for listing details section
                    if let Some(section_type) = map.get("sectionComponentType").and_then(|t| t.as_str()) {
                        if section_type.contains("OVERVIEW") || section_type.contains("ROOM") {
                            if let Some(items) = map.get("items").and_then(|i| i.as_array()) {
                                for item in items {
                                    if let Some(title) = item.get("title").and_then(|t| t.as_str()) {
                                        if !details.contains(&title.to_string()) {
                                            details.push(title.to_string());
                                        }
                                    }
                                }
                            }
                        }
                    }

                    // Look for structuredContent with property info
                    if let Some(content) = map.get("structuredContent") {
                        if let Some(primary) = content.get("primaryLine").and_then(|p| p.as_array()) {
                            for line in primary {
                                if let Some(body) = line.get("body").and_then(|b| b.as_str()) {
                                    if !details.contains(&body.to_string()) {
                                        details.push(body.to_string());
                                    }
                                }
                            }
                        }
                        if let Some(secondary) = content.get("secondaryLine").and_then(|s| s.as_array()) {
                            for line in secondary {
                                if let Some(body) = line.get("body").and_then(|b| b.as_str()) {
                                    if !details.contains(&body.to_string()) {
                                        details.push(body.to_string());
                                    }
                                }
                            }
                        }
                    }

                    // Look for previewAmenityGroups which sometimes contains room info
                    if let Some(preview_groups) = map.get("previewAmenityGroups").and_then(|g| g.as_array()) {
                        for group in preview_groups {
                            if let Some(group_title) = group.get("title").and_then(|t| t.as_str()) {
                                if group_title.to_lowercase().contains("bedroom") ||
                                   group_title.to_lowercase().contains("bathroom") ||
                                   group_title.to_lowercase().contains("space") {
                                    if let Some(amenities) = group.get("amenities").and_then(|a| a.as_array()) {
                                        for amenity in amenities {
                                            if let Some(title) = amenity.get("title").and_then(|t| t.as_str()) {
                                                if !details.contains(&title.to_string()) {
                                                    details.push(title.to_string());
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }

                    // Recurse into nested objects
                    for (_key, val) in map {
                        find_house_details_in_json(val, details, depth + 1);
                    }
                }
                serde_json::Value::Array(arr) => {
                    for item in arr {
                        find_house_details_in_json(item, details, depth + 1);
                    }
                }
                _ => {}
            }
        }

        find_house_details_in_json(&json_data, &mut details, 0);

        if !details.is_empty() {
            debug!("Found {} house details from JSON", details.len());
            return details;
        }
    }

    // Try JSON-LD fallback
    if let Some(json_ld) = extract_json_ld(html) {
        // Try to get number of guests
        if let Some(occupancy) = json_ld.get("occupancy").and_then(|o| o.as_u64()) {
            details.push(format!("{} guests", occupancy));
        }
        // Try to get number of beds
        if let Some(beds) = json_ld.get("numberOfBeds").and_then(|b| b.as_u64()) {
            details.push(format!("{} beds", beds));
        }
    }

    // Try regex for common patterns
    let patterns = [
        (r"(\d+)\s*guests?", "guests"),
        (r"(\d+)\s*bedrooms?", "bedrooms"),
        (r"(\d+)\s*beds?", "beds"),
        (r"(\d+)\s*baths?", "baths"),
        (r"(\d+)\s*bathrooms?", "bathrooms"),
    ];

    for (pattern, suffix) in patterns {
        if let Ok(re) = regex::Regex::new(pattern) {
            if let Some(caps) = re.captures(html) {
                if let Some(num) = caps.get(1) {
                    let detail = format!("{} {}", num.as_str(), suffix);
                    if !details.contains(&detail) {
                        details.push(detail);
                    }
                }
            }
        }
    }

    details
}

// ============================================================================
// FAST PATH — search-JSON harvest, parallel HTTP enrichment, map-tile coverage
// ============================================================================
//
// The legacy stealth path navigates a browser to every /rooms/<id> page and
// re-scrapes fields the search-results JSON already contains. These functions
// instead:
//   Phase 1  build listings directly from the search JSON already downloaded,
//   Phase 2  enrich the few missing fields (amenities, house details, region)
//            over bounded parallel HTTP instead of one browser at a time,
//   Phase 3  subdivide the search area by map bounding-box so we break past
//            Airbnb's ~300-results-per-query ceiling.

#[cfg(test)]
mod amenity_tests {
    use super::*;

    // normalize_amenity is a long if-chain where ORDER is load-bearing: an
    // earlier branch wins even when a later one also matches. These tests pin
    // the cases where that ordering is doing real work, because reordering the
    // chain silently reclassifies listings.

    #[test]
    fn hot_tub_variants_and_misspellings() {
        for input in [
            "Hot tub", "hottub", "hot-tub", "Jacuzzi", "jaccuzi", "jacuzi",
            "Whirlpool bath", "jetted tub", "jet tub", "soaking tub",
            "spa tub", "bubble tub", "hydrotherapy pool",
        ] {
            assert_eq!(normalize_amenity(input), "Hot Tub", "input: {input}");
        }
    }

    #[test]
    fn plunge_pool_is_a_hot_tub_not_a_pool() {
        // Hot Tub is checked before Pool precisely so this doesn't become
        // "Pool". Flipping the branch order breaks it.
        assert_eq!(normalize_amenity("Plunge pool"), "Hot Tub");
    }

    #[test]
    fn spa_matches_hot_tub_but_workspace_does_not() {
        // The `!lower.contains("space")` guard exists for exactly this.
        assert_eq!(normalize_amenity("Spa"), "Hot Tub");
        assert_eq!(normalize_amenity("Dedicated workspace"), "Workspace");
    }

    #[test]
    fn pool_variants() {
        for input in ["Swimming pool", "Private pool", "Shared pool", "swimming"] {
            assert_eq!(normalize_amenity(input), "Pool", "input: {input}");
        }
    }

    #[test]
    fn waterfront_beats_beach_access() {
        // Waterfront is checked first; "Beachfront" must not become
        // "Beach Access".
        assert_eq!(normalize_amenity("Beachfront"), "Waterfront");
        assert_eq!(normalize_amenity("Lakefront"), "Waterfront");
        assert_eq!(normalize_amenity("Ocean view"), "Waterfront");
    }

    #[test]
    fn common_amenities_map_to_canonical_names() {
        let cases = [
            ("Wifi", "WiFi"),
            ("Free wifi", "WiFi"),
            ("Air conditioning", "Air Conditioning"),
            ("Full kitchen", "Kitchen"),
            ("Free parking on premises", "Parking"),
            ("Indoor fireplace", "Fireplace"),
            ("BBQ grill", "BBQ Grill"),
            ("Pets allowed", "Pets Allowed"),
            ("Sauna", "Sauna"),
            ("Elevator", "Elevator"),
            ("Dishwasher", "Dishwasher"),
            ("Self check-in", "Self Check-in"),
        ];
        for (input, want) in cases {
            assert_eq!(normalize_amenity(input), want, "input: {input}");
        }
    }

    #[test]
    fn washer_and_dryer_are_distinct() {
        assert_eq!(normalize_amenity("Washer"), "Washer");
        assert_eq!(normalize_amenity("Dryer"), "Dryer");
    }

    #[test]
    fn iron_excludes_ironing_board() {
        assert_eq!(normalize_amenity("Iron"), "Iron");
        // An ironing board is a different amenity and must fall through to the
        // title-case default rather than collapsing into "Iron".
        assert_ne!(normalize_amenity("Ironing board"), "Iron");
    }

    #[test]
    fn unknown_amenities_fall_back_to_title_case() {
        assert_eq!(normalize_amenity("piano lounge"), "Piano Lounge");
        assert_eq!(normalize_amenity("HELIPAD"), "HELIPAD");
    }

    #[test]
    fn normalization_is_case_and_whitespace_insensitive() {
        assert_eq!(normalize_amenity("  HOT TUB  "), "Hot Tub");
        assert_eq!(normalize_amenity("wIfI"), "WiFi");
    }

    #[test]
    fn empty_input_does_not_panic() {
        assert_eq!(normalize_amenity(""), "");
        assert_eq!(normalize_amenity("   "), "");
    }

    #[test]
    fn normalization_is_idempotent() {
        // Output must be stable when fed back in, or repeated normalisation
        // passes (e.g. the Go-side migration) would drift.
        for input in ["Hot tub", "Swimming pool", "Free wifi", "Beachfront", "piano lounge"] {
            let once = normalize_amenity(input);
            let twice = normalize_amenity(&once);
            assert_eq!(once, twice, "not idempotent for {input}: {once} -> {twice}");
        }
    }

    // ---- extraction from page source ----

    #[test]
    fn amenities_from_source_empty_when_absent() {
        assert!(extract_amenities_from_source("<html>nothing</html>").is_empty());
    }

    #[test]
    fn house_details_from_source_empty_when_absent() {
        assert!(extract_house_details_from_source("<html>nothing</html>").is_empty());
    }
}

#[cfg(test)]
mod amenity_reachability {
    use super::*;

    /// Every canonical category must be reachable by at least one plausible
    /// input. An unreachable category means an earlier `contains` branch is
    /// swallowing it — the class of bug that made "Dishwasher" return "Washer".
    #[test]
    fn every_category_is_reachable() {
        let probes = [
            ("Hot tub", "Hot Tub"),
            ("Swimming pool", "Pool"),
            ("Waterfront", "Waterfront"),
            ("Beach access", "Beach Access"),
            ("Air conditioning", "Air Conditioning"),
            ("Wifi", "WiFi"),
            ("Kitchen", "Kitchen"),
            ("Washer", "Washer"),
            ("Dryer", "Dryer"),
            ("Dedicated workspace", "Workspace"),
            ("Free parking", "Parking"),
            ("Gym", "Gym"),
            ("Fireplace", "Fireplace"),
            ("BBQ grill", "BBQ Grill"),
            ("EV charger", "EV Charger"),
            ("Pets allowed", "Pets Allowed"),
            ("Heating", "Heating"),
            ("TV", "TV"),
            ("Sauna", "Sauna"),
            ("Elevator", "Elevator"),
            ("Security cameras", "Security"),
            ("Patio", "Outdoor Space"),
            ("Game room", "Game Room"),
            ("Crib", "Crib"),
            ("High chair", "High Chair"),
            ("Bathtub", "Bathtub"),
            ("Coffee maker", "Coffee Maker"),
            ("Dishwasher", "Dishwasher"),
            ("Self check-in", "Self Check-in"),
            ("Hair dryer", "Hair Dryer"),
            ("Iron", "Iron"),
            ("Long term stays", "Long Term Stays"),
        ];
        let mut unreachable = Vec::new();
        for (input, want) in probes {
            let got = normalize_amenity(input);
            if got != want {
                unreachable.push(format!("{input:?} -> {got:?} (want {want:?})"));
            }
        }
        assert!(
            unreachable.is_empty(),
            "categories shadowed by an earlier branch:\n  {}",
            unreachable.join("\n  ")
        );
    }
}
