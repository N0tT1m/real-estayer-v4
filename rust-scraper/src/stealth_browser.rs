// stealth_browser.rs - CDP-based browser control using chromiumoxide
// This bypasses WebDriver protocol entirely, making detection much harder

use anyhow::Result;
use chromiumoxide::cdp::browser_protocol::emulation::{
    SetUserAgentOverrideParams, UserAgentBrandVersion, UserAgentMetadata,
};
use chromiumoxide::{Browser, BrowserConfig, Page};
use futures::StreamExt;
use std::sync::Arc;
use tokio::sync::Mutex;
use tracing::info;

#[cfg(target_os = "windows")]
use std::os::windows::process::CommandExt;

// Anti-detection JavaScript, registered via
// Page.addScriptToEvaluateOnNewDocument so it runs before any page script in
// every frame. It used to be applied with page.evaluate() after goto() returned,
// which left a window where Airbnb's document-start scripts observed the
// unpatched values this is meant to hide.
const STEALTH_JS: &str = r#"
// Remove webdriver property
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
delete navigator.__proto__.webdriver;

// Mock realistic plugins
Object.defineProperty(navigator, 'plugins', {
    get: () => {
        const plugins = {
            0: { name: 'Chrome PDF Plugin', filename: 'internal-pdf-viewer', description: 'Portable Document Format' },
            1: { name: 'Chrome PDF Viewer', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai', description: '' },
            2: { name: 'Native Client', filename: 'internal-nacl-plugin', description: '' },
            length: 3
        };
        plugins.item = (index) => plugins[index] || null;
        plugins.namedItem = (name) => Object.values(plugins).find(p => p && p.name === name) || null;
        plugins.refresh = () => {};
        return plugins;
    }
});

// Mock realistic mimeTypes
Object.defineProperty(navigator, 'mimeTypes', {
    get: () => {
        const mimes = {
            0: { type: 'application/pdf', suffixes: 'pdf', description: 'Portable Document Format' },
            length: 1
        };
        mimes.item = (index) => mimes[index] || null;
        mimes.namedItem = (name) => Object.values(mimes).find(m => m && m.type === name) || null;
        return mimes;
    }
});

// Languages
Object.defineProperty(navigator, 'languages', { get: () => ['en-US', 'en'] });
Object.defineProperty(navigator, 'language', { get: () => 'en-US' });

// Hardware concurrency (realistic CPU cores)
Object.defineProperty(navigator, 'hardwareConcurrency', { get: () => 8 });

// Device memory
Object.defineProperty(navigator, 'deviceMemory', { get: () => 8 });

// navigator.platform is deliberately NOT patched here. It is set natively via
// Emulation.setUserAgentOverride in new_stealth_page(), which also drives
// navigator.userAgentData and the Sec-CH-UA-* request headers from the same
// profile. Patching it in JS could only contradict those, and left a
// Function.prototype.toString tell besides.

// Chrome object (many sites check for this)
window.chrome = {
    runtime: {},
    loadTimes: function() { return {}; },
    csi: function() { return {}; },
    app: { isInstalled: false, InstallState: { DISABLED: 'disabled', INSTALLED: 'installed', NOT_INSTALLED: 'not_installed' }, RunningState: { CANNOT_RUN: 'cannot_run', READY_TO_RUN: 'ready_to_run', RUNNING: 'running' } }
};

// Override permissions query
const originalQuery = window.navigator.permissions.query;
window.navigator.permissions.query = (parameters) => (
    parameters.name === 'notifications' ?
        Promise.resolve({ state: Notification.permission }) :
        originalQuery(parameters)
);

// Fake battery API
navigator.getBattery = () => Promise.resolve({
    charging: true,
    chargingTime: 0,
    dischargingTime: Infinity,
    level: 1
});

// Override toString to hide modifications
const originalToString = Function.prototype.toString;
Function.prototype.toString = function() {
    if (this === navigator.permissions.query) {
        return 'function query() { [native code] }';
    }
    return originalToString.call(this);
};
"#;

pub struct StealthBrowser {
    browser: Browser,
    handler_task: tokio::task::JoinHandle<()>,
}

/// Marker embedded in the `--user-data-dir` of every Chrome we launch, so
/// cleanup can target our own instances instead of every Chrome on the machine.
pub(crate) const SCRAPER_PROFILE_MARKER: &str = "chrome_scraper_";

impl StealthBrowser {
    /// Kill Chrome instances *this scraper* started.
    ///
    /// This used to be `pkill -f chrome`, which on Linux and macOS killed the
    /// operator's own browser along with any orphans. Every Chrome we launch
    /// carries `--user-data-dir=.../chrome_scraper_<pid>`, so matching on that
    /// marker scopes the kill to instances we are responsible for.
    fn kill_existing_chrome() {
        info!("[STEALTH] Killing orphaned scraper Chrome processes...");
        #[cfg(target_os = "windows")]
        {
            // taskkill has no command-line filter, so match the marker via WMI.
            let _ = std::process::Command::new("wmic")
                .args([
                    "process",
                    "where",
                    &format!("CommandLine like '%{}%'", SCRAPER_PROFILE_MARKER),
                    "delete",
                ])
                .creation_flags(0x08000000) // CREATE_NO_WINDOW
                .output();
        }
        #[cfg(not(target_os = "windows"))]
        {
            let _ = std::process::Command::new("pkill")
                .args(["-f", SCRAPER_PROFILE_MARKER])
                .output();
        }
        // Give processes time to die
        std::thread::sleep(std::time::Duration::from_millis(500));
    }

    /// Create a new stealth browser instance
    pub async fn new() -> Result<Self> {
        info!("[STEALTH] Creating new stealth browser with CDP...");

        // Kill any existing Chrome to avoid CDP conflicts
        Self::kill_existing_chrome();

        // Find Chrome binary
        let chrome_path = Self::find_chrome_binary()?;
        info!("[STEALTH] Using Chrome binary: {}", chrome_path);

        // Create a unique user data directory to avoid profile conflicts
        let user_data_dir =
            std::env::temp_dir().join(format!("{}{}", SCRAPER_PROFILE_MARKER, std::process::id()));
        let user_data_arg = format!("--user-data-dir={}", user_data_dir.display());
        info!("[STEALTH] Using temp profile: {}", user_data_dir.display());

        // Build browser config with stealth options
        let mut builder = BrowserConfig::builder()
            .chrome_executable(chrome_path)
            .window_size(1920, 1080)
            .request_timeout(std::time::Duration::from_secs(120))
            .with_head() // Run visible for debugging
            // Use isolated profile to avoid conflicts
            .arg(&user_data_arg)
            .arg("--incognito")
            // Anti-detection args
            .arg("--disable-blink-features=AutomationControlled")
            .arg("--no-sandbox")
            .arg("--disable-dev-shm-usage")
            .arg("--disable-infobars")
            .arg("--disable-extensions")
            .arg("--disable-plugins-discovery")
            .arg("--disable-default-apps")
            .arg("--disable-background-networking")
            .arg("--disable-sync")
            .arg("--disable-translate")
            .arg("--metrics-recording-only")
            .arg("--no-first-run")
            .arg("--safebrowsing-disable-auto-update")
            // User agent from the pinned profile. new_stealth_page() then calls
            // setUserAgentOverride with the matching platform and client-hint
            // metadata, so the UA, navigator.platform, navigator.userAgentData,
            // and the Sec-CH-UA-* headers all originate from one identity.
            .arg(format!(
                "--user-agent={}",
                crate::scraping::profile::active().user_agent
            ));

        // GPU stays enabled by default. Every real browser reports a hardware
        // WebGL renderer, so a missing one — or SwiftShader — is a strong bot
        // signal, and the renderer string is exactly the kind of hardware fact
        // that contradicts the platform this profile claims.
        // SCRAPER_DISABLE_GPU=1 restores the old behaviour for hosts where GPU
        // rendering genuinely breaks.
        if crate::routes::gpu_disabled() {
            info!("[STEALTH] SCRAPER_DISABLE_GPU set: disabling GPU (detectable)");
            builder = builder
                .arg("--disable-gpu")
                .arg("--disable-software-rasterizer");
        }

        // Same egress rotation the HTTP fast path uses.
        if let Some(proxy_url) = crate::scraping::proxy::next() {
            info!(
                "[STEALTH] Routing browser via {}",
                crate::scraping::proxy::redact(&proxy_url)
            );
            builder = builder.arg(format!("--proxy-server={}", proxy_url));
        }

        let config = builder
            .build()
            .map_err(|e| anyhow::anyhow!("Failed to build browser config: {}", e))?;

        // Launch browser
        let (browser, mut handler) = Browser::launch(config)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to launch browser: {}", e))?;

        // Spawn handler task (silently consume handler events - errors are non-fatal CDP parsing issues)
        let handler_task = tokio::spawn(async move { while handler.next().await.is_some() {} });

        info!("[STEALTH] Browser launched successfully");

        Ok(Self {
            browser,
            handler_task,
        })
    }

    /// Find Chrome binary path
    fn find_chrome_binary() -> Result<String> {
        // Check environment variable first
        if let Ok(path) = std::env::var("CHROME_BINARY_PATH") {
            if std::path::Path::new(&path).exists() {
                return Ok(path);
            }
        }

        // Windows paths
        let paths = [
            r"C:\Program Files\Google\Chrome\Application\chrome.exe",
            r"C:\Program Files (x86)\Google\Chrome\Application\chrome.exe",
        ];

        for path in &paths {
            if std::path::Path::new(path).exists() {
                return Ok(path.to_string());
            }
        }

        // Check LOCALAPPDATA
        if let Ok(local_app_data) = std::env::var("LOCALAPPDATA") {
            let chrome_path = format!(r"{}\Google\Chrome\Application\chrome.exe", local_app_data);
            if std::path::Path::new(&chrome_path).exists() {
                return Ok(chrome_path);
            }
        }

        Err(anyhow::anyhow!("Chrome binary not found"))
    }

    /// Create a new stealth page with anti-detection measures registered.
    ///
    /// Order matters. Both the UA override and the document-start script are
    /// registered while the page is still on about:blank, so they are already in
    /// effect for the first real navigation.
    pub async fn new_stealth_page(&self) -> Result<Page> {
        info!("[STEALTH] Creating new stealth page...");

        let page = self
            .browser
            .new_page("about:blank")
            .await
            .map_err(|e| anyhow::anyhow!("Failed to create new page: {}", e))?;

        Self::apply_user_agent_override(&page).await?;

        // Register the stealth script to run before any page script in every
        // frame, for every document this page loads. page.evaluate() would run
        // it against the *current* document only, and only once the call
        // resolves — too late for Airbnb's own document-start scripts.
        page.evaluate_on_new_document(STEALTH_JS)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to register stealth JS: {}", e))?;

        info!("[STEALTH] Stealth page created and configured");

        Ok(page)
    }

    /// Set the UA, `navigator.platform`, `navigator.userAgentData`, and the
    /// `Sec-CH-UA-*` request headers from the pinned profile in one CDP call.
    ///
    /// Doing this natively rather than in injected JS matters twice over: Chrome
    /// applies it below the JS layer, so there is no `Function.prototype
    /// .toString` tell and no ordering window, and it keeps `userAgentData`
    /// consistent with `navigator.platform`. Patching only `navigator.platform`
    /// in JS left `userAgentData.platform` reporting the true host OS — a
    /// one-line contradiction for any detector that reads both.
    async fn apply_user_agent_override(page: &Page) -> Result<()> {
        let profile = crate::scraping::profile::active();

        let mut builder = SetUserAgentOverrideParams::builder()
            .user_agent(&profile.user_agent)
            .accept_language(profile.accept_language)
            .platform(profile.navigator_platform);

        // Only Chromium exposes navigator.userAgentData. Attaching metadata to a
        // Gecko or WebKit identity would manufacture the very contradiction the
        // profile exists to avoid.
        if let Some(brands) = profile.brands.as_ref() {
            let brand_versions: Vec<UserAgentBrandVersion> = brands
                .iter()
                .map(|(brand, version)| UserAgentBrandVersion::new(brand, version))
                .collect();
            let full_versions: Vec<UserAgentBrandVersion> = brands
                .iter()
                .map(|(brand, _)| UserAgentBrandVersion::new(brand, &profile.full_version))
                .collect();

            let metadata = UserAgentMetadata::builder()
                .brands(brand_versions)
                .full_version_lists(full_versions)
                .platform(profile.ch_platform)
                .platform_version(profile.ch_platform_version)
                .architecture(profile.architecture)
                .bitness(profile.bitness)
                .model("")
                .mobile(false)
                .build()
                .map_err(|e| anyhow::anyhow!("Failed to build userAgentMetadata: {}", e))?;

            builder = builder.user_agent_metadata(metadata);
        }

        let params = builder
            .build()
            .map_err(|e| anyhow::anyhow!("Failed to build UA override: {}", e))?;

        // page.set_user_agent() takes the legacy Network-domain params, which
        // carry no userAgentMetadata; issue the Emulation command directly.
        page.execute(params)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to apply UA override: {}", e))?;

        info!(
            "[STEALTH] UA override applied: platform={} client_hints={}",
            profile.navigator_platform,
            profile.brands.is_some()
        );

        Ok(())
    }

    /// Navigate to a URL with stealth measures and retry logic
    pub async fn navigate(&self, page: &Page, url: &str) -> Result<()> {
        const MAX_RETRIES: u32 = 3;

        for attempt in 1..=MAX_RETRIES {
            info!(
                "[STEALTH] Navigating to: {} (attempt {}/{})",
                url, attempt, MAX_RETRIES
            );

            // Navigate to the URL with a 90 second timeout
            let nav_result =
                tokio::time::timeout(tokio::time::Duration::from_secs(90), page.goto(url)).await;

            match nav_result {
                Ok(Ok(_)) => {
                    // No re-injection here. The script is registered with
                    // addScriptToEvaluateOnNewDocument in new_stealth_page(), so
                    // it has already run before this document's own scripts.
                    // Re-running it post-load was strictly worse: it could not
                    // help anything that had already read the real values, and
                    // re-applying the Function.prototype.toString patch over an
                    // already-patched toString is how that trick gets noticed.

                    // Wait for page to be somewhat loaded
                    tokio::time::sleep(tokio::time::Duration::from_secs(2)).await;

                    info!("[STEALTH] Navigation complete");
                    return Ok(());
                }
                Ok(Err(e)) => {
                    if attempt == MAX_RETRIES {
                        return Err(anyhow::anyhow!("Failed to navigate to {}: {}", url, e));
                    }
                    info!("[STEALTH] Navigation failed, retrying in 5s: {}", e);
                    tokio::time::sleep(tokio::time::Duration::from_secs(5)).await;
                }
                Err(_) => {
                    if attempt == MAX_RETRIES {
                        return Err(anyhow::anyhow!(
                            "Navigation to {} timed out after {} attempts",
                            url,
                            MAX_RETRIES
                        ));
                    }
                    info!("[STEALTH] Navigation timed out, retrying in 5s...");
                    tokio::time::sleep(tokio::time::Duration::from_secs(5)).await;
                }
            }
        }

        Err(anyhow::anyhow!(
            "Navigation failed after {} attempts",
            MAX_RETRIES
        ))
    }

    /// Get page content
    pub async fn get_content(&self, page: &Page) -> Result<String> {
        let content = page
            .content()
            .await
            .map_err(|e| anyhow::anyhow!("Failed to get page content: {}", e))?;
        Ok(content)
    }

    /// Get current URL
    pub async fn current_url(&self, page: &Page) -> Result<String> {
        let url = page
            .url()
            .await
            .map_err(|e| anyhow::anyhow!("Failed to get current URL: {}", e))?
            .unwrap_or_default();
        Ok(url.to_string())
    }

    /// Execute JavaScript on the page
    pub async fn execute_js(&self, page: &Page, script: &str) -> Result<serde_json::Value> {
        let result = page
            .evaluate(script)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to execute JS: {}", e))?;
        Ok(result.into_value()?)
    }

    /// Find elements by CSS selector
    pub async fn find_elements(
        &self,
        page: &Page,
        selector: &str,
    ) -> Result<Vec<chromiumoxide::element::Element>> {
        let elements = page
            .find_elements(selector)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to find elements: {}", e))?;
        Ok(elements)
    }

    /// Find single element by CSS selector
    pub async fn find_element(
        &self,
        page: &Page,
        selector: &str,
    ) -> Result<chromiumoxide::element::Element> {
        let element = page
            .find_element(selector)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to find element: {}", e))?;
        Ok(element)
    }

    /// Click an element
    pub async fn click(&self, page: &Page, selector: &str) -> Result<()> {
        let element = self.find_element(page, selector).await?;
        element
            .click()
            .await
            .map_err(|e| anyhow::anyhow!("Failed to click element: {}", e))?;
        Ok(())
    }

    /// Scroll the page
    pub async fn scroll(&self, page: &Page, pixels: i32) -> Result<()> {
        page.evaluate(format!("window.scrollBy(0, {});", pixels))
            .await
            .map_err(|e| anyhow::anyhow!("Failed to scroll: {}", e))?;
        Ok(())
    }

    /// Check if page is still valid
    pub async fn is_page_valid(&self, page: &Page) -> bool {
        page.url().await.is_ok()
    }

    /// Close the browser and kill Chrome process
    pub async fn close(mut self) -> Result<()> {
        info!("[STEALTH] Closing browser...");
        self.handler_task.abort();
        // Actually close the browser which kills the Chrome process
        if let Err(e) = self.browser.close().await {
            info!("[STEALTH] Browser close returned: {}", e);
        }
        // Give it a moment to clean up
        tokio::time::sleep(tokio::time::Duration::from_millis(500)).await;
        info!("[STEALTH] Browser closed");
        Ok(())
    }
}

/// Wrapper to use StealthBrowser with existing code that expects WebDriver-like interface
pub struct StealthDriver {
    browser: Arc<StealthBrowser>,
    page: Arc<Mutex<Option<Page>>>,
}

impl StealthDriver {
    pub async fn new() -> Result<Self> {
        let browser = StealthBrowser::new().await?;
        let page = browser.new_stealth_page().await?;

        Ok(Self {
            browser: Arc::new(browser),
            page: Arc::new(Mutex::new(Some(page))),
        })
    }

    pub async fn goto(&self, url: &str) -> Result<()> {
        let page_guard = self.page.lock().await;
        if let Some(page) = page_guard.as_ref() {
            self.browser.navigate(page, url).await
        } else {
            Err(anyhow::anyhow!("No active page"))
        }
    }

    pub async fn current_url(&self) -> Result<String> {
        let page_guard = self.page.lock().await;
        if let Some(page) = page_guard.as_ref() {
            self.browser.current_url(page).await
        } else {
            Err(anyhow::anyhow!("No active page"))
        }
    }

    pub async fn page_source(&self) -> Result<String> {
        let page_guard = self.page.lock().await;
        if let Some(page) = page_guard.as_ref() {
            self.browser.get_content(page).await
        } else {
            Err(anyhow::anyhow!("No active page"))
        }
    }

    pub async fn execute_script(&self, script: &str) -> Result<serde_json::Value> {
        let page_guard = self.page.lock().await;
        if let Some(page) = page_guard.as_ref() {
            self.browser.execute_js(page, script).await
        } else {
            Err(anyhow::anyhow!("No active page"))
        }
    }

    pub async fn find_elements_by_css(
        &self,
        selector: &str,
    ) -> Result<Vec<chromiumoxide::element::Element>> {
        let page_guard = self.page.lock().await;
        if let Some(page) = page_guard.as_ref() {
            self.browser.find_elements(page, selector).await
        } else {
            Err(anyhow::anyhow!("No active page"))
        }
    }

    pub async fn is_valid(&self) -> bool {
        let page_guard = self.page.lock().await;
        if let Some(page) = page_guard.as_ref() {
            self.browser.is_page_valid(page).await
        } else {
            false
        }
    }

    pub async fn quit(self) -> Result<()> {
        info!("[STEALTH] Driver quit requested - closing browser...");

        // Drop page first
        {
            let mut page_guard = self.page.lock().await;
            *page_guard = None;
        }

        // Try to get ownership of the browser and close it properly
        match Arc::try_unwrap(self.browser) {
            Ok(browser) => {
                browser.close().await?;
            }
            Err(_) => {
                // Arc still has references, force kill Chrome processes
                info!("[STEALTH] Forcing Chrome process cleanup...");
                Self::kill_chrome_processes();
            }
        }

        info!("[STEALTH] Driver quit complete");
        Ok(())
    }

    /// Kill lingering Chrome processes *this scraper* started.
    ///
    /// Scoped by the `--user-data-dir` marker for the same reason as
    /// [`StealthBrowser::kill_existing_chrome`]: the previous `pkill -f chrome`
    /// took the operator's browser down with it.
    fn kill_chrome_processes() {
        #[cfg(target_os = "windows")]
        {
            let _ = std::process::Command::new("wmic")
                .args([
                    "process",
                    "where",
                    &format!("CommandLine like '%{}%'", SCRAPER_PROFILE_MARKER),
                    "delete",
                ])
                .output();
        }
        #[cfg(not(target_os = "windows"))]
        {
            let _ = std::process::Command::new("pkill")
                .args(["-f", SCRAPER_PROFILE_MARKER])
                .output();
        }
    }
}
