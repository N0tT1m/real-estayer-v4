//! Egress proxy rotation.
//!
//! Request rate from a single IP is the strongest signal a target has, and it is
//! the one no amount of fingerprint work addresses. `MAX_CONCURRENT_SCRAPES = 1`
//! used to be the only mitigation, while `routes.rs` returned a note *advising*
//! proxy rotation that nothing implemented.
//!
//! Configure with `SCRAPER_PROXIES`, a comma-separated list:
//!
//! ```text
//! SCRAPER_PROXIES=http://user:pass@gw1.example:8080,http://user:pass@gw2.example:8080
//! ```
//!
//! Unset means direct connections, which is the previous behaviour, so this is
//! opt-in. Selection is round-robin rather than random: for a small pool,
//! sampling with replacement wastes addresses and clusters requests onto
//! whichever one luck favours, which is the pattern we are trying to avoid.

use once_cell::sync::Lazy;
use std::sync::atomic::{AtomicUsize, Ordering};

/// Proxy URLs from `SCRAPER_PROXIES`, parsed once.
static PROXIES: Lazy<Vec<String>> = Lazy::new(|| {
    let raw = std::env::var("SCRAPER_PROXIES").unwrap_or_default();
    let list = parse(&raw);
    if list.is_empty() {
        tracing::info!("[PROXY] SCRAPER_PROXIES unset; using direct connections");
    } else {
        tracing::info!(
            "[PROXY] {} proxy endpoint(s) configured: {}",
            list.len(),
            list.iter()
                .map(|u| redact(u))
                .collect::<Vec<_>>()
                .join(", ")
        );
    }
    list
});

static CURSOR: AtomicUsize = AtomicUsize::new(0);

/// Split and clean a `SCRAPER_PROXIES` value.
///
/// Kept pure and separate from the env lookup so the parsing rules are testable
/// without touching process state.
pub fn parse(raw: &str) -> Vec<String> {
    raw.split(',')
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .map(|s| s.to_string())
        .collect()
}

/// Number of configured proxies. Zero means direct connections.
pub fn count() -> usize {
    PROXIES.len()
}

/// The next proxy in the rotation, or `None` when none are configured.
pub fn next() -> Option<String> {
    let proxies = &*PROXIES;
    if proxies.is_empty() {
        return None;
    }
    let i = CURSOR.fetch_add(1, Ordering::Relaxed);
    Some(proxies[i % proxies.len()].clone())
}

/// Strip credentials from a proxy URL so it can be logged.
///
/// Proxy strings routinely carry `user:pass@`, and these URLs end up in log
/// files and in the SMTP run report.
pub fn redact(url: &str) -> String {
    // Only the authority segment can hold credentials; find the last '@' before
    // the first '/' after the scheme.
    let (scheme, rest) = match url.split_once("://") {
        Some((s, r)) => (Some(s), r),
        None => (None, url),
    };
    let authority_end = rest.find('/').unwrap_or(rest.len());
    let (authority, tail) = rest.split_at(authority_end);

    let redacted_authority = match authority.rsplit_once('@') {
        Some((_creds, host)) => format!("***@{host}"),
        None => authority.to_string(),
    };

    match scheme {
        Some(s) => format!("{s}://{redacted_authority}{tail}"),
        None => format!("{redacted_authority}{tail}"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_splits_and_trims() {
        assert_eq!(
            parse("http://a:8080, http://b:8080 ,http://c:8080"),
            vec!["http://a:8080", "http://b:8080", "http://c:8080"]
        );
    }

    #[test]
    fn parse_ignores_empty_entries() {
        assert!(parse("").is_empty());
        assert!(parse("   ").is_empty());
        assert!(parse(",,").is_empty());
        assert_eq!(parse("http://a:1,,http://b:2,").len(), 2);
    }

    /// Credentials must never reach a log line or the run report.
    #[test]
    fn redact_removes_credentials() {
        assert_eq!(
            redact("http://user:s3cret@gw.example:8080"),
            "http://***@gw.example:8080"
        );
        assert_eq!(
            redact("https://user:pass@gw.example:8080/path"),
            "https://***@gw.example:8080/path"
        );
        // A password containing '@' must still be fully removed.
        assert_eq!(
            redact("http://user:p@ss@gw.example:8080"),
            "http://***@gw.example:8080"
        );
    }

    #[test]
    fn redact_leaves_credential_free_urls_alone() {
        assert_eq!(redact("http://gw.example:8080"), "http://gw.example:8080");
        assert_eq!(redact("gw.example:8080"), "gw.example:8080");
    }

    /// A path segment containing '@' must not be mistaken for credentials.
    #[test]
    fn redact_only_touches_the_authority() {
        assert_eq!(
            redact("http://gw.example:8080/a@b"),
            "http://gw.example:8080/a@b"
        );
    }

    /// Round-robin, not sampling: every endpoint must be used before any repeat.
    #[test]
    fn rotation_is_round_robin() {
        let pool = parse("http://a:1,http://b:2,http://c:3");
        let cursor = AtomicUsize::new(0);
        let mut seen = Vec::new();
        for _ in 0..6 {
            let i = cursor.fetch_add(1, Ordering::Relaxed);
            seen.push(pool[i % pool.len()].clone());
        }
        assert_eq!(
            seen,
            vec![
                "http://a:1",
                "http://b:2",
                "http://c:3",
                "http://a:1",
                "http://b:2",
                "http://c:3"
            ]
        );
    }

    /// With nothing configured we must return None rather than a bogus URL, so
    /// callers fall back to direct connections.
    #[test]
    fn no_proxies_means_direct() {
        assert!(parse("").is_empty());
        if count() == 0 {
            assert!(next().is_none());
        }
    }
}
