//! Derive a listing's region and country from its free-text location.
//!
//! `extract_region_country_from_source` reads Airbnb's JSON-LD / og:title,
//! which the fast path's HTTP enrichment usually does not receive — 280 of 366
//! listings ended up with neither field, and the app's State filter offered a
//! single state for a database spanning Indiana, Maryland, Texas and Ontario.
//!
//! The location string ("Indianapolis, Indiana", "Toronto, Canada") is already
//! recorded for display and is enough to recover both, with no extra request
//! and no dependency on Airbnb markup. This mirrors `internal/listing/place.go`
//! on the Go side, which backfilled the existing rows; keeping the two in step
//! is what stops new scrapes from re-introducing the gap. The country table
//! itself is literally the same file, included from both.

use once_cell::sync::Lazy;
use std::collections::HashMap;

/// Lowercased US state name or postal abbreviation -> canonical name.
/// Both spellings occur ("Ocean City, Maryland", "Austin, TX").
const US_STATES: &[(&str, &str)] = &[
    ("alabama", "Alabama"),
    ("al", "Alabama"),
    ("alaska", "Alaska"),
    ("ak", "Alaska"),
    ("arizona", "Arizona"),
    ("az", "Arizona"),
    ("arkansas", "Arkansas"),
    ("ar", "Arkansas"),
    ("california", "California"),
    ("ca", "California"),
    ("colorado", "Colorado"),
    ("co", "Colorado"),
    ("connecticut", "Connecticut"),
    ("ct", "Connecticut"),
    ("delaware", "Delaware"),
    ("de", "Delaware"),
    ("florida", "Florida"),
    ("fl", "Florida"),
    ("georgia", "Georgia"),
    ("ga", "Georgia"),
    ("hawaii", "Hawaii"),
    ("hi", "Hawaii"),
    ("idaho", "Idaho"),
    ("id", "Idaho"),
    ("illinois", "Illinois"),
    ("il", "Illinois"),
    ("indiana", "Indiana"),
    ("in", "Indiana"),
    ("iowa", "Iowa"),
    ("ia", "Iowa"),
    ("kansas", "Kansas"),
    ("ks", "Kansas"),
    ("kentucky", "Kentucky"),
    ("ky", "Kentucky"),
    ("louisiana", "Louisiana"),
    ("la", "Louisiana"),
    ("maine", "Maine"),
    ("me", "Maine"),
    ("maryland", "Maryland"),
    ("md", "Maryland"),
    ("massachusetts", "Massachusetts"),
    ("ma", "Massachusetts"),
    ("michigan", "Michigan"),
    ("mi", "Michigan"),
    ("minnesota", "Minnesota"),
    ("mn", "Minnesota"),
    ("mississippi", "Mississippi"),
    ("ms", "Mississippi"),
    ("missouri", "Missouri"),
    ("mo", "Missouri"),
    ("montana", "Montana"),
    ("mt", "Montana"),
    ("nebraska", "Nebraska"),
    ("ne", "Nebraska"),
    ("nevada", "Nevada"),
    ("nv", "Nevada"),
    ("new hampshire", "New Hampshire"),
    ("nh", "New Hampshire"),
    ("new jersey", "New Jersey"),
    ("nj", "New Jersey"),
    ("new mexico", "New Mexico"),
    ("nm", "New Mexico"),
    ("new york", "New York"),
    ("ny", "New York"),
    ("north carolina", "North Carolina"),
    ("nc", "North Carolina"),
    ("north dakota", "North Dakota"),
    ("nd", "North Dakota"),
    ("ohio", "Ohio"),
    ("oh", "Ohio"),
    ("oklahoma", "Oklahoma"),
    ("ok", "Oklahoma"),
    ("oregon", "Oregon"),
    ("or", "Oregon"),
    ("pennsylvania", "Pennsylvania"),
    ("pa", "Pennsylvania"),
    ("rhode island", "Rhode Island"),
    ("ri", "Rhode Island"),
    ("south carolina", "South Carolina"),
    ("sc", "South Carolina"),
    ("south dakota", "South Dakota"),
    ("sd", "South Dakota"),
    ("tennessee", "Tennessee"),
    ("tn", "Tennessee"),
    ("texas", "Texas"),
    ("tx", "Texas"),
    ("utah", "Utah"),
    ("ut", "Utah"),
    ("vermont", "Vermont"),
    ("vt", "Vermont"),
    ("virginia", "Virginia"),
    ("va", "Virginia"),
    ("washington", "Washington"),
    ("wa", "Washington"),
    ("west virginia", "West Virginia"),
    ("wv", "West Virginia"),
    ("wisconsin", "Wisconsin"),
    ("wi", "Wisconsin"),
    ("wyoming", "Wyoming"),
    ("wy", "Wyoming"),
    ("district of columbia", "District of Columbia"),
    ("dc", "District of Columbia"),
    ("puerto rico", "Puerto Rico"),
    ("pr", "Puerto Rico"),
];

/// Canadian equivalent. Separate from `US_STATES` so a match also determines
/// the country.
const CA_PROVINCES: &[(&str, &str)] = &[
    ("alberta", "Alberta"),
    ("ab", "Alberta"),
    ("british columbia", "British Columbia"),
    ("bc", "British Columbia"),
    ("manitoba", "Manitoba"),
    ("mb", "Manitoba"),
    ("new brunswick", "New Brunswick"),
    ("nb", "New Brunswick"),
    ("newfoundland and labrador", "Newfoundland and Labrador"),
    ("nl", "Newfoundland and Labrador"),
    ("nova scotia", "Nova Scotia"),
    ("ns", "Nova Scotia"),
    ("ontario", "Ontario"),
    ("on", "Ontario"),
    ("prince edward island", "Prince Edward Island"),
    ("pe", "Prince Edward Island"),
    ("quebec", "Quebec"),
    ("québec", "Quebec"),
    ("qc", "Quebec"),
    ("saskatchewan", "Saskatchewan"),
    ("sk", "Saskatchewan"),
    ("yukon", "Yukon"),
    ("yt", "Yukon"),
    ("northwest territories", "Northwest Territories"),
    ("nt", "Northwest Territories"),
    ("nunavut", "Nunavut"),
    ("nu", "Nunavut"),
];

/// The country table, shared verbatim with `internal/listing/place.go`.
/// Embedding the same file is what stops the scraper and the app from
/// disagreeing about whether "Holland" is Netherlands or "Türkiye" is Turkey.
///
/// It carries NO two-letter ISO codes: a two-letter trailing segment in an
/// Airbnb location is overwhelmingly a US state, and CA/IN/ID/LA/MA/MD/ME/MO/
/// MS/MT/NE/PA/SC/SD/VA and others all collide with one.
const COUNTRIES_TABLE: &str = include_str!("../../../internal/listing/countries.txt");

static COUNTRIES: Lazy<HashMap<String, String>> = Lazy::new(|| {
    let mut m = HashMap::with_capacity(512);
    for line in COUNTRIES_TABLE.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let (canon, aliases) = line.split_once('|').unwrap_or((line, ""));
        let canon = canon.trim();
        if canon.is_empty() {
            continue;
        }
        m.insert(canon.to_lowercase(), canon.to_string());
        for a in aliases.split(',') {
            let a = a.trim();
            if !a.is_empty() {
                m.insert(a.to_lowercase(), canon.to_string());
            }
        }
    }
    m
});

fn lookup(table: &[(&str, &str)], key: &str) -> Option<String> {
    table
        .iter()
        .find(|(k, _)| *k == key)
        .map(|(_, v)| (*v).to_string())
}

/// Derive `(region, country)` from a location string.
///
/// Either component is `None` when the string does not determine it. A bare
/// neighbourhood ("East Austin", "Zilker") yields `(None, None)` on purpose:
/// the text genuinely does not say which state it is in, and inferring one
/// from surrounding context would misfile any listing whose neighbourhood name
/// is not unique.
pub fn parse_place(location: &str) -> (Option<String>, Option<String>) {
    let parts: Vec<&str> = location
        .split(',')
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .collect();
    if parts.len() < 2 {
        return (None, None);
    }

    // Airbnb orders these coarsest-last: "Ocean City, Maryland",
    // "Toronto, Ontario, Canada".
    let last = parts[parts.len() - 1].to_lowercase();

    if let Some(country) = COUNTRIES.get(&last).cloned() {
        // "Toronto, Ontario, Canada" / "Barcelona, Catalonia, Spain" — the
        // middle segment is the subdivision. Canonicalise where we have a
        // table (US/CA, where Airbnb also abbreviates); elsewhere keep it as
        // written, since no table can enumerate every country's regions and
        // Airbnb's own string beats discarding it.
        let region = if parts.len() >= 3 {
            let raw = parts[parts.len() - 2];
            let mid = raw.to_lowercase();
            match country.as_str() {
                "United States" => lookup(US_STATES, &mid),
                "Canada" => lookup(CA_PROVINCES, &mid),
                _ => Some(raw.to_string()),
            }
        } else {
            None
        };
        return (region, Some(country));
    }

    if let Some(region) = lookup(US_STATES, &last) {
        return (Some(region), Some("United States".to_string()));
    }
    if let Some(region) = lookup(CA_PROVINCES, &last) {
        return (Some(region), Some("Canada".to_string()));
    }

    // Trailing segment is neither a known country nor a US/CA subdivision.
    // Record nothing rather than the raw string: the app's filters match a
    // fixed variant list, so a stray value becomes a dead dropdown entry.
    (None, None)
}

/// Fill a listing's region/country from its location when they are still
/// unset. Never overwrites a value the page markup supplied — that source is
/// authoritative, this one is inference from display text.
pub fn fill_place(location: &str, region: &mut Option<String>, country: &mut Option<String>) {
    if region.is_some() && country.is_some() {
        return;
    }
    let (parsed_region, parsed_country) = parse_place(location);
    if region.is_none() {
        *region = parsed_region;
    }
    if country.is_none() {
        *country = parsed_country;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn p(s: &str) -> (Option<String>, Option<String>) {
        parse_place(s)
    }
    fn some(a: &str, b: &str) -> (Option<String>, Option<String>) {
        (Some(a.to_string()), Some(b.to_string()))
    }

    // The shapes actually present in the listings that shipped with no region.
    #[test]
    fn parses_the_real_location_shapes() {
        assert_eq!(p("Indianapolis, Indiana"), some("Indiana", "United States"));
        assert_eq!(p("Ocean City, Maryland"), some("Maryland", "United States"));
        assert_eq!(p("Ann Arbor, Michigan"), some("Michigan", "United States"));
        assert_eq!(p("Austin, TX"), some("Texas", "United States"));
        assert_eq!(p("Toronto, Ontario, Canada"), some("Ontario", "Canada"));
        assert_eq!(p(" Denver , Colorado "), some("Colorado", "United States"));
    }

    // "Toronto, Canada" names no province; the country is still knowable.
    #[test]
    fn country_without_region() {
        assert_eq!(p("Toronto, Canada"), (None, Some("Canada".to_string())));
    }

    // CA is California. Countries are matched before states, so an alias for
    // Canada here would misfile every Californian listing.
    #[test]
    fn ca_is_california_not_canada() {
        assert_eq!(p("San Diego, CA"), some("California", "United States"));
        assert_eq!(p("Vancouver, BC"), some("British Columbia", "Canada"));
    }

    // Neighbourhood-only strings must stay unresolved rather than be guessed.
    #[test]
    fn neighbourhoods_resolve_to_nothing() {
        for s in ["East Austin", "Zilker", "Travis Heights", "", "   "] {
            assert_eq!(p(s), (None, None), "input {s:?}");
        }
    }

    // The scraper targets every region, so any recognised country resolves.
    #[test]
    fn resolves_countries_worldwide() {
        assert_eq!(p("Paris, France"), (None, Some("France".into())));
        assert_eq!(p("Kyoto, Japan"), (None, Some("Japan".into())));
        assert_eq!(p("Amsterdam, Holland"), (None, Some("Netherlands".into())));
        assert_eq!(p("Istanbul, Türkiye"), (None, Some("Turkey".into())));
        assert_eq!(
            p("Edinburgh, Scotland"),
            (None, Some("United Kingdom".into()))
        );
    }

    // Subdivisions outside US/CA are preserved as written.
    #[test]
    fn foreign_subdivisions_are_kept() {
        assert_eq!(p("Barcelona, Catalonia, Spain"), some("Catalonia", "Spain"));
    }

    #[test]
    fn unknown_trailing_segment_records_nothing() {
        assert_eq!(p("Somewhere, Atlantis"), (None, None));
    }

    // The table is shared with Go; if the file moves or empties, every
    // location silently stops resolving its country.
    #[test]
    fn shared_country_table_loaded() {
        assert!(COUNTRIES.len() > 200, "got {} entries", COUNTRIES.len());
        assert_eq!(
            COUNTRIES.get("usa").map(String::as_str),
            Some("United States")
        );
        // And no two-letter ISO codes leaked in to shadow a US state.
        assert!(COUNTRIES.get("ca").is_none(), "ca must remain California");
    }

    // Markup-derived values win; inference only fills gaps.
    #[test]
    fn fill_place_does_not_overwrite() {
        let mut region = Some("Ontario".to_string());
        let mut country = None;
        fill_place("Indianapolis, Indiana", &mut region, &mut country);
        assert_eq!(region.as_deref(), Some("Ontario"));
        assert_eq!(country.as_deref(), Some("United States"));
    }

    #[test]
    fn fill_place_fills_both_when_empty() {
        let (mut region, mut country) = (None, None);
        fill_place("Ocean City, Maryland", &mut region, &mut country);
        assert_eq!(region.as_deref(), Some("Maryland"));
        assert_eq!(country.as_deref(), Some("United States"));
    }
}
