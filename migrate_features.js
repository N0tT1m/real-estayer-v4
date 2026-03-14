// Migration script to normalize features in existing listings

var normalizeFeature = function(feature) {
    var lower = feature.toLowerCase().trim();

    // Hot tub variations
    if (lower.includes("hot tub") || lower.includes("hottub") || lower.includes("hot-tub") ||
        lower.includes("jacuzzi") || lower.includes("whirlpool") || lower.includes("jetted tub") ||
        lower.includes("jet tub") || lower.includes("soaking tub") || lower.includes("spa tub") ||
        lower.includes("hydrotherapy") || lower.includes("plunge pool") ||
        (lower.includes("spa") && lower.indexOf("space") === -1)) {
        return "Hot Tub";
    }
    if (lower.includes("pool") || lower.includes("swimming")) return "Pool";
    if (lower.includes("waterfront") || lower.includes("lakefront") || lower.includes("beachfront") ||
        lower.includes("oceanfront") || lower.includes("lake view") || lower.includes("ocean view") ||
        lower.includes("water view") || lower.includes("beach view")) return "Waterfront";
    if (lower.includes("air conditioning") || lower.includes("aircon") || lower === "a/c" || lower === "ac") return "Air Conditioning";
    if (lower.includes("wifi") || lower.includes("wi-fi") || lower.includes("wireless") || lower.includes("internet")) return "WiFi";
    if (lower.includes("kitchen")) return "Kitchen";
    if (lower.includes("washer") || lower.includes("washing machine") || lower.includes("laundry")) return "Washer";
    if (lower.includes("dryer")) return "Dryer";
    if (lower.includes("workspace") || lower.includes("work space") || lower.includes("desk")) return "Workspace";
    if (lower.includes("parking") || lower.includes("garage")) return "Parking";
    if (lower.includes("gym") || lower.includes("fitness") || lower.includes("exercise")) return "Gym";
    if (lower.includes("heating") || lower.includes("heater") || lower === "heat") return "Heating";
    if (lower.includes("tv") || lower.includes("television")) return "TV";
    if (lower.includes("fireplace") || lower.includes("fire pit")) return "Fireplace";
    if (lower.includes("bbq") || lower.includes("grill") || lower.includes("barbecue")) return "BBQ Grill";
    if (lower.includes("sauna") || lower.includes("steam room")) return "Sauna";
    if (lower.includes("elevator") || lower.includes("lift")) return "Elevator";
    if (lower.includes("balcony") || lower.includes("patio") || lower.includes("deck") ||
        lower.includes("terrace") || lower.includes("garden") || lower.includes("yard")) return "Outdoor Space";

    // Keep badges as-is
    if (lower === "superhost" || lower === "guest favorite" || lower === "rare find" ||
        lower === "top rated" || lower === "highly rated") {
        return feature;
    }

    // Default: capitalize first letter of each word
    return feature.split(" ").map(function(w) {
        return w.charAt(0).toUpperCase() + w.slice(1).toLowerCase();
    }).join(" ");
};

var updated = 0;
db.listings.find({}).forEach(function(doc) {
    if (doc.features && doc.features.length > 0) {
        var normalized = [];
        var seen = {};
        doc.features.forEach(function(f) {
            var norm = normalizeFeature(f);
            if (!seen[norm]) {
                seen[norm] = true;
                normalized.push(norm);
            }
        });
        db.listings.updateOne({ _id: doc._id }, { $set: { features: normalized } });
        updated++;
    }
});
print("Updated " + updated + " listings");

// Show new counts
print("\nNew feature counts:");
db.listings.aggregate([
    { $unwind: "$features" },
    { $group: { _id: "$features", count: { $sum: 1 } } },
    { $sort: { count: -1 } }
]).forEach(function(f) {
    print(f.count + "x - " + f._id);
});
