// stealth_browser.rs - CDP-based browser control using chromiumoxide
// This bypasses WebDriver protocol entirely, making detection much harder

use anyhow::Result;
use chromiumoxide::{Browser, BrowserConfig, Page};
use futures::StreamExt;
use std::sync::Arc;
use tokio::sync::Mutex;
use tracing::info;

#[cfg(target_os = "windows")]
use std::os::windows::process::CommandExt;

// Anti-detection JavaScript to inject before any page loads
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

// Platform - match Windows since that's where this runs
Object.defineProperty(navigator, 'platform', { get: () => 'Win32' });

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

impl StealthBrowser {
    /// Kill any existing Chrome processes to avoid conflicts
    fn kill_existing_chrome() {
        info!("[STEALTH] Killing any existing Chrome processes...");
        #[cfg(target_os = "windows")]
        {
            let _ = std::process::Command::new("taskkill")
                .args(["/IM", "chrome.exe", "/F"])
                .creation_flags(0x08000000) // CREATE_NO_WINDOW
                .output();
        }
        #[cfg(not(target_os = "windows"))]
        {
            let _ = std::process::Command::new("pkill")
                .args(["-f", "chrome"])
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
            std::env::temp_dir().join(format!("chrome_scraper_{}", std::process::id()));
        let user_data_arg = format!("--user-data-dir={}", user_data_dir.display());
        info!("[STEALTH] Using temp profile: {}", user_data_dir.display());

        // Build browser config with stealth options
        let config = BrowserConfig::builder()
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
            // Disable GPU to avoid rendering issues
            .arg("--disable-gpu")
            .arg("--disable-software-rasterizer")
            // Randomized user agent
            .arg("--user-agent=Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
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

    /// Create a new stealth page with anti-detection scripts injected
    pub async fn new_stealth_page(&self) -> Result<Page> {
        info!("[STEALTH] Creating new stealth page...");

        let page = self
            .browser
            .new_page("about:blank")
            .await
            .map_err(|e| anyhow::anyhow!("Failed to create new page: {}", e))?;

        // Inject stealth JavaScript before any navigation
        page.evaluate(STEALTH_JS)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to inject stealth JS: {}", e))?;

        info!("[STEALTH] Stealth page created and configured");

        Ok(page)
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
                    // Re-inject stealth JS after navigation (some sites check after load)
                    page.evaluate(STEALTH_JS).await.ok(); // Ignore errors here

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

    /// Kill any lingering Chrome processes (platform-specific)
    fn kill_chrome_processes() {
        #[cfg(target_os = "windows")]
        {
            let _ = std::process::Command::new("taskkill")
                .args(["/IM", "chrome.exe", "/F"])
                .output();
        }
        #[cfg(not(target_os = "windows"))]
        {
            let _ = std::process::Command::new("pkill")
                .args(["-f", "chrome"])
                .output();
        }
    }
}
