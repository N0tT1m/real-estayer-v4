//! Coherent browser identities.
//!
//! Fingerprinting works by cross-referencing signals, so the risk is not "which
//! browser do we claim" but "do our claims contradict each other, or the machine
//! we actually run on". Three contradictions used to be live here:
//!
//!  * The user agent, `Sec-CH-UA`, and `Sec-CH-UA-Platform` were drawn from
//!    three independent lists, so a Firefox UA could ship with Chrome client
//!    hints — and Firefox does not send `Sec-CH-UA` at all.
//!  * `navigator.platform` was hardcoded (`Win32` on the CDP path, `MacIntel` on
//!    the WebDriver path) regardless of the host OS, while the WebGL renderer
//!    string, font metrics, and timezone still reported the truth.
//!  * The claimed browser version was a hardcoded table that drifted out of date
//!    the moment Chrome updated. A stale user agent is itself a signal, and it
//!    contradicted the real browser doing the rendering.
//!
//! A [`BrowserProfile`] bundles every claim that can be cross-checked into one
//! value, and [`active`] pins a single profile for the process lifetime. That
//! last part matters independently: a real browser does not change its user
//! agent between requests in a session, so rotating per-request was itself a
//! signal. Rotation happens across runs instead.
//!
//! Two deliberate constraints:
//!
//!  * **Chrome only.** This scraper drives Chrome — the CDP transport only
//!    speaks to Chromium, `STEALTH_JS` installs a `window.chrome` object, and
//!    `navigator.userAgentData` exists nowhere else. A Firefox or Safari user
//!    agent would contradict all of that, so those profiles were removed rather
//!    than maintained.
//!  * **Version comes from the installed binary.** [`detect_chrome_version`]
//!    reads the real Chrome version and every version-bearing field is derived
//!    from it, so the claim tracks reality without anyone editing a table.
//!
//! Profiles are still filtered to the host OS, because no amount of internal
//! consistency hides a `Win32` claim from a Linux WebGL renderer.

use once_cell::sync::Lazy;
use std::process::Command;

/// Host operating system families we have profiles for.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OsFamily {
    Windows,
    MacOs,
    Linux,
}

/// The OS this binary is running on. Profile selection prefers matching profiles
/// so our claims agree with the signals we cannot fake cheaply.
pub fn host_os() -> OsFamily {
    if cfg!(target_os = "windows") {
        OsFamily::Windows
    } else if cfg!(target_os = "macos") {
        OsFamily::MacOs
    } else {
        OsFamily::Linux
    }
}

/// Used only when Chrome cannot be interrogated (no binary on PATH, permission
/// error). Deliberately a plausible recent release rather than a placeholder:
/// this value goes on the wire.
const FALLBACK_FULL_VERSION: &str = "131.0.6778.86";

/// Greased brand entry. Chrome varies this per release; the value matters far
/// less than it being present and consistent between the header and metadata.
const GREASED_BRAND: (&str, &str) = ("Not_A Brand", "24");

/// The version-independent half of a profile.
struct ProfileTemplate {
    os: OsFamily,
    /// `{major}` is replaced with the detected Chrome major version.
    ua_template: &'static str,
    navigator_platform: &'static str,
    ch_platform: &'static str,
    ch_platform_version: &'static str,
    accept: &'static str,
    accept_language: &'static str,
    architecture: &'static str,
    bitness: &'static str,
}

const CHROME_ACCEPT: &str = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7";

/// The template pool.
///
/// Note the Windows entries claim `Windows NT 10.0`: real Chrome on Windows 11
/// still reports NT 10.0 in the UA string and distinguishes 11 from 10 only via
/// `Sec-CH-UA-Platform-Version` (>= 13.0.0). A `Windows NT 11.0` user agent does
/// not exist in the wild and was itself a tell.
const TEMPLATES: &[ProfileTemplate] = &[
    ProfileTemplate {
        os: OsFamily::Windows,
        ua_template: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{major}.0.0.0 Safari/537.36",
        navigator_platform: "Win32",
        ch_platform: "Windows",
        ch_platform_version: "10.0.0",
        accept: CHROME_ACCEPT,
        accept_language: "en-US,en;q=0.9",
        architecture: "x86",
        bitness: "64",
    },
    ProfileTemplate {
        os: OsFamily::Windows,
        ua_template: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{major}.0.0.0 Safari/537.36",
        navigator_platform: "Win32",
        // Same UA; Windows 11 differs only in the client hint.
        ch_platform: "Windows",
        ch_platform_version: "15.0.0",
        accept: CHROME_ACCEPT,
        accept_language: "en-US,en;q=0.9",
        architecture: "x86",
        bitness: "64",
    },
    ProfileTemplate {
        os: OsFamily::MacOs,
        ua_template: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{major}.0.0.0 Safari/537.36",
        navigator_platform: "MacIntel",
        ch_platform: "macOS",
        // The UA freezes at 10_15_7; the client hint carries the real version.
        ch_platform_version: "14.6.1",
        accept: CHROME_ACCEPT,
        accept_language: "en-US,en;q=0.9",
        architecture: "x86",
        bitness: "64",
    },
    ProfileTemplate {
        os: OsFamily::Linux,
        ua_template: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{major}.0.0.0 Safari/537.36",
        navigator_platform: "Linux x86_64",
        ch_platform: "Linux",
        // Chrome on Linux genuinely sends an empty platform version.
        ch_platform_version: "",
        accept: CHROME_ACCEPT,
        accept_language: "en-US,en;q=0.9",
        architecture: "x86",
        bitness: "64",
    },
];

/// One self-consistent browser identity.
///
/// Every field is a claim a fingerprinting check can compare against another
/// field, so they are only ever read together — never mixed between profiles.
#[derive(Debug, Clone)]
pub struct BrowserProfile {
    pub os: OsFamily,
    pub user_agent: String,
    /// What `navigator.platform` must return for this UA.
    pub navigator_platform: &'static str,
    /// `Sec-CH-UA-Platform` / `userAgentMetadata.platform`.
    pub ch_platform: &'static str,
    /// `Sec-CH-UA-Platform-Version`. Chrome on Linux genuinely sends an empty
    /// string, so an empty value is meaningful rather than missing.
    pub ch_platform_version: &'static str,
    /// `Sec-CH-UA`, or `None` for engines that do not implement client hints.
    /// Every current profile is Chromium so this is always `Some`; the option is
    /// kept so the rule stays expressed and enforced if that ever changes.
    pub sec_ch_ua: Option<String>,
    /// Brand list for `userAgentMetadata`, mirroring `sec_ch_ua`.
    pub brands: Option<Vec<(String, String)>>,
    /// Full browser version for `userAgentMetadata.fullVersionList`.
    pub full_version: String,
    /// Engine-specific `Accept` for navigation requests.
    pub accept: &'static str,
    pub accept_language: &'static str,
    pub architecture: &'static str,
    pub bitness: &'static str,
}

impl BrowserProfile {
    /// True when this profile's engine implements UA client hints.
    pub fn sends_client_hints(&self) -> bool {
        self.sec_ch_ua.is_some()
    }

    /// Major version as claimed by the user agent.
    pub fn major_version(&self) -> &str {
        self.full_version.split('.').next().unwrap_or("")
    }

    fn from_template(t: &ProfileTemplate, full_version: &str) -> Self {
        let major = full_version.split('.').next().unwrap_or(full_version);
        let brands = vec![
            ("Google Chrome".to_string(), major.to_string()),
            ("Chromium".to_string(), major.to_string()),
            (GREASED_BRAND.0.to_string(), GREASED_BRAND.1.to_string()),
        ];
        // Header and metadata are rendered from the same brand list, so they
        // cannot drift apart.
        let sec_ch_ua = brands
            .iter()
            .map(|(brand, version)| format!("\"{brand}\";v=\"{version}\""))
            .collect::<Vec<_>>()
            .join(", ");

        Self {
            os: t.os,
            user_agent: t.ua_template.replace("{major}", major),
            navigator_platform: t.navigator_platform,
            ch_platform: t.ch_platform,
            ch_platform_version: t.ch_platform_version,
            sec_ch_ua: Some(sec_ch_ua),
            brands: Some(brands),
            full_version: full_version.to_string(),
            accept: t.accept,
            accept_language: t.accept_language,
            architecture: t.architecture,
            bitness: t.bitness,
        }
    }
}

/// Read the installed Chrome's version, e.g. `"141.0.7390.55"`.
///
/// Deriving the claimed version from the real binary is what keeps the user
/// agent from going stale, and it removes the contradiction between the UA we
/// send and the engine actually rendering the page.
pub fn detect_chrome_version() -> Option<String> {
    // Honour an explicit override first; useful for reproducing a run and for
    // environments where the binary cannot be executed at startup.
    if let Ok(v) = std::env::var("CHROME_VERSION_OVERRIDE") {
        let v = v.trim().to_string();
        if !v.is_empty() {
            return Some(v);
        }
    }

    let output = if cfg!(target_os = "windows") {
        Command::new("reg")
            .args([
                "query",
                "HKEY_CURRENT_USER\\Software\\Google\\Chrome\\BLBeacon",
                "/v",
                "version",
            ])
            .output()
            .ok()
    } else {
        let mut candidates: Vec<String> = Vec::new();
        if let Ok(p) = std::env::var("CHROME_BINARY_PATH") {
            if !p.trim().is_empty() {
                candidates.push(p);
            }
        }
        if cfg!(target_os = "macos") {
            candidates
                .push("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome".to_string());
        }
        candidates.extend(
            ["google-chrome", "google-chrome-stable", "chromium"]
                .iter()
                .map(|s| s.to_string()),
        );

        candidates
            .iter()
            .find_map(|bin| Command::new(bin).arg("--version").output().ok())
    };

    let out = output?;
    parse_version_output(&String::from_utf8_lossy(&out.stdout))
}

/// Pull a dotted version out of a `--version` / `reg query` output.
///
/// Kept pure so the accepted formats are pinned by tests rather than discovered
/// in production: this value now determines the user agent we send, so a silent
/// parse failure means falling back to a stale version on every request.
pub fn parse_version_output(text: &str) -> Option<String> {
    text.split_whitespace()
        .find(|s| {
            // A version starts with a digit and has at least one dot. The digit
            // check rejects `REG_SZ`; the dot check rejects a bare build number.
            s.chars().next().is_some_and(|c| c.is_ascii_digit())
                && s.contains('.')
                && s.chars().all(|c| c.is_ascii_digit() || c == '.')
        })
        .map(|s| s.to_string())
}

/// The Chrome version every profile claims this run.
static FULL_VERSION: Lazy<String> = Lazy::new(|| match detect_chrome_version() {
    Some(v) => {
        tracing::info!(
            "[PROFILE] Detected Chrome {}; deriving user agent from it",
            v
        );
        v
    }
    None => {
        tracing::warn!(
            "[PROFILE] Could not detect Chrome version; falling back to {}. \
             Set CHROME_VERSION_OVERRIDE to pin one.",
            FALLBACK_FULL_VERSION
        );
        FALLBACK_FULL_VERSION.to_string()
    }
});

/// Every profile, resolved at the detected Chrome version.
pub fn all_profiles() -> Vec<BrowserProfile> {
    TEMPLATES
        .iter()
        .map(|t| BrowserProfile::from_template(t, &FULL_VERSION))
        .collect()
}

/// Profiles whose claimed OS matches the host.
///
/// Falls back to the whole pool if we have none for this OS, since a
/// cross-OS-but-internally-consistent identity still beats an incoherent one.
pub fn host_profiles() -> Vec<BrowserProfile> {
    let host = host_os();
    let matching: Vec<_> = all_profiles()
        .into_iter()
        .filter(|p| p.os == host)
        .collect();
    if matching.is_empty() {
        all_profiles()
    } else {
        matching
    }
}

/// Pick a random host-matching profile. Prefer [`active`] for anything that
/// touches the network; this exists for tests and for callers that genuinely
/// want a fresh identity.
pub fn random_profile() -> BrowserProfile {
    let candidates = host_profiles();
    candidates[fastrand::usize(0..candidates.len())].clone()
}

/// The identity for this process, chosen once at first use.
///
/// Pinned deliberately: a browser that changes its user agent between requests
/// inside one session is more suspicious than one that never does. Rotation
/// happens across process restarts.
static ACTIVE: Lazy<BrowserProfile> = Lazy::new(|| {
    let p = random_profile();
    tracing::info!(
        "[PROFILE] Active browser identity: {} (platform={}, client_hints={})",
        p.user_agent,
        p.navigator_platform,
        p.sends_client_hints()
    );
    p
});

/// The pinned profile for this process. Every UA, client hint, and
/// `navigator.platform` claim must come from here.
pub fn active() -> &'static BrowserProfile {
    &ACTIVE
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The bug this module exists to prevent: client-hint headers paired with a
    /// UA whose engine does not implement client hints.
    #[test]
    fn client_hints_only_on_chromium_profiles() {
        for p in all_profiles() {
            let looks_chromium = p.user_agent.contains("Chrome/");
            assert_eq!(
                p.sends_client_hints(),
                looks_chromium,
                "client-hint support disagrees with the UA engine: {}",
                p.user_agent
            );
        }
    }

    /// We only ever drive Chrome, so a non-Chromium identity would contradict
    /// the CDP transport, `window.chrome`, and `navigator.userAgentData`.
    #[test]
    fn every_profile_is_chromium() {
        for p in all_profiles() {
            assert!(p.user_agent.contains("Chrome/"), "{}", p.user_agent);
            assert!(!p.user_agent.contains("Firefox/"), "{}", p.user_agent);
        }
    }

    #[test]
    fn sec_ch_ua_and_brands_agree() {
        for p in all_profiles() {
            assert_eq!(p.sec_ch_ua.is_some(), p.brands.is_some());
            let (header, brands) = (p.sec_ch_ua.unwrap(), p.brands.unwrap());
            for (brand, version) in &brands {
                assert!(
                    header.contains(brand) && header.contains(version),
                    "brand {brand}/{version} missing from Sec-CH-UA {header:?}"
                );
            }
        }
    }

    /// The whole point of #6: the version in the UA must be the version we
    /// detected, not a value baked into the source.
    #[test]
    fn user_agent_version_tracks_the_detected_version() {
        for p in all_profiles() {
            let major = p.major_version();
            assert!(
                p.user_agent.contains(&format!("Chrome/{major}.")),
                "UA {} does not carry detected major {major}",
                p.user_agent
            );
            assert!(
                p.sec_ch_ua
                    .as_ref()
                    .unwrap()
                    .contains(&format!("v=\"{major}\"")),
                "Sec-CH-UA does not carry detected major {major}"
            );
        }
    }

    /// Rendering a template at an explicit version must touch every
    /// version-bearing field together.
    #[test]
    fn from_template_threads_one_version_everywhere() {
        let p = BrowserProfile::from_template(&TEMPLATES[0], "999.0.1234.5");
        assert_eq!(p.major_version(), "999");
        assert!(
            p.user_agent.contains("Chrome/999.0.0.0"),
            "{}",
            p.user_agent
        );
        assert!(p.sec_ch_ua.as_ref().unwrap().contains("v=\"999\""));
        assert_eq!(p.full_version, "999.0.1234.5");
        for (brand, version) in p.brands.as_ref().unwrap() {
            if brand != GREASED_BRAND.0 {
                assert_eq!(version, "999", "brand {brand} carries the wrong version");
            }
        }
    }

    /// No `{major}` placeholder may survive into a rendered profile.
    #[test]
    fn templates_are_fully_rendered() {
        for p in all_profiles() {
            assert!(!p.user_agent.contains("{major}"), "{}", p.user_agent);
        }
    }

    #[test]
    fn navigator_platform_matches_claimed_os() {
        for p in all_profiles() {
            let expected = match p.os {
                OsFamily::Windows => "Win32",
                OsFamily::MacOs => "MacIntel",
                OsFamily::Linux => "Linux x86_64",
            };
            assert_eq!(p.navigator_platform, expected, "for {}", p.user_agent);
        }
    }

    #[test]
    fn user_agent_string_matches_claimed_os() {
        for p in all_profiles() {
            let ua = &p.user_agent;
            match p.os {
                OsFamily::Windows => assert!(ua.contains("Windows NT"), "{ua}"),
                OsFamily::MacOs => assert!(ua.contains("Macintosh"), "{ua}"),
                OsFamily::Linux => assert!(ua.contains("Linux"), "{ua}"),
            }
        }
    }

    /// `Windows NT 11.0` does not exist in real user agents; Windows 11 is
    /// signalled through Sec-CH-UA-Platform-Version instead.
    #[test]
    fn no_fictional_windows_nt_11() {
        for p in all_profiles() {
            assert!(
                !p.user_agent.contains("Windows NT 11.0"),
                "{}",
                p.user_agent
            );
        }
    }

    #[test]
    fn ch_platform_matches_claimed_os() {
        for p in all_profiles() {
            let expected = match p.os {
                OsFamily::Windows => "Windows",
                OsFamily::MacOs => "macOS",
                OsFamily::Linux => "Linux",
            };
            assert_eq!(p.ch_platform, expected, "for {}", p.user_agent);
        }
    }

    #[test]
    fn host_profiles_are_all_host_os_when_available() {
        let host = host_os();
        if all_profiles().iter().any(|p| p.os == host) {
            for p in host_profiles() {
                assert_eq!(p.os, host, "cross-OS profile leaked: {}", p.user_agent);
            }
        }
    }

    #[test]
    fn active_profile_is_stable() {
        assert_eq!(active().user_agent, active().user_agent);
        assert!(std::ptr::eq(active(), active()));
    }

    /// Real `--version` output, per platform. These strings are what the
    /// detector actually receives.
    #[test]
    fn parses_real_version_output() {
        assert_eq!(
            parse_version_output("Google Chrome 141.0.7390.55 \n").as_deref(),
            Some("141.0.7390.55")
        );
        assert_eq!(
            parse_version_output("Chromium 130.0.6723.116 snap").as_deref(),
            Some("130.0.6723.116")
        );
        // Windows `reg query` form — REG_SZ must not be mistaken for a version.
        assert_eq!(
            parse_version_output(
                "HKEY_CURRENT_USER\\Software\\Google\\Chrome\\BLBeacon\n    version    REG_SZ    141.0.7390.55\n"
            )
            .as_deref(),
            Some("141.0.7390.55")
        );
    }

    #[test]
    fn rejects_output_without_a_version() {
        assert_eq!(parse_version_output(""), None);
        assert_eq!(parse_version_output("command not found"), None);
        // A bare build number is not a version.
        assert_eq!(parse_version_output("Chrome 12345"), None);
    }

    /// A failed parse must fall through to the fallback, not to an empty UA.
    #[test]
    fn unparseable_output_yields_no_version() {
        assert!(parse_version_output("Google Chrome unknown").is_none());
    }

    /// The fallback must be a usable version string, since it goes on the wire
    /// whenever Chrome cannot be interrogated.
    #[test]
    fn fallback_version_is_plausible() {
        let major = FALLBACK_FULL_VERSION.split('.').next().unwrap();
        assert!(major.parse::<u32>().unwrap() >= 100);
        assert_eq!(FALLBACK_FULL_VERSION.split('.').count(), 4);
    }
}
