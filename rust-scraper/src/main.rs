use axum::http::HeaderValue;
use tracing_appender::rolling::{RollingFileAppender, Rotation};
use tracing_subscriber::{fmt, prelude::*, EnvFilter};

use rust_scraper::{build_app, ApiKey};

#[cfg(target_os = "windows")]
use std::os::windows::process::CommandExt;

// Enhanced logging function with file output and detailed tracing
pub fn setup_logging() {
    use std::fs;

    // Bridge the log crate to tracing (for chromiumoxide and other libraries)
    tracing_log::LogTracer::init().ok();

    // Create logs directory if it doesn't exist
    if let Err(e) = fs::create_dir_all("logs") {
        eprintln!("Failed to create logs directory: {}", e);
    }

    // Filter: Show only our app logs, completely suppress all library noise
    let file_filter = EnvFilter::new("off,rust_scraper=debug");

    // Console filter: Only show our app logs, nothing else
    let console_filter = EnvFilter::new("off,rust_scraper=info");

    // File appender for detailed logs
    let file_appender = RollingFileAppender::builder()
        .rotation(Rotation::DAILY)
        .filename_prefix("scraper")
        .filename_suffix("log")
        .build("logs")
        .expect("failed to initialize rolling file appender");

    let subscriber = tracing_subscriber::registry()
        .with(
            fmt::Layer::new()
                .with_file(false)
                .with_line_number(false)
                .with_thread_ids(false)
                .with_thread_names(false)
                .with_writer(file_appender)
                .with_ansi(false)
                .with_target(true)
                .with_level(true)
                .with_filter(file_filter),
        )
        .with(
            fmt::Layer::new()
                .with_file(false)
                .with_line_number(false)
                .with_thread_ids(false)
                .with_target(false)
                .with_ansi(true)
                .compact()
                .with_filter(console_filter),
        );

    if let Err(e) = tracing::subscriber::set_global_default(subscriber) {
        eprintln!("Failed to set up logging: {}", e);
    }

    eprintln!("Logging initialized - file: logs/scraper.YYYY-MM-DD.log");
}

#[tokio::main]
async fn main() {
    dotenv::dotenv().ok();

    // rustls crypto provider (ring; no cmake/NASM needed on Windows)
    let _ = rustls::crypto::ring::default_provider().install_default();

    setup_logging();
    tracing::info!("Starting application...");

    // Trim whitespace so a CRLF-saved .env (`KEY=value\r\n`) or an accidental
    // trailing space doesn't drift us off by one byte and cause mystery 401s.
    let api_key = std::env::var("SCRAPER_API_KEY")
        .unwrap_or_default()
        .trim()
        .to_string();
    if api_key.is_empty() {
        tracing::error!(
            "SCRAPER_API_KEY is not set. Refusing to start because unauthenticated scraper access would expose write/trigger endpoints."
        );
        std::process::exit(1);
    }
    // Log a fingerprint (length + first/last 4 chars) so operators can
    // verify the running key matches the one they intended without leaking
    // the full value to logs.
    let head = api_key.chars().take(4).collect::<String>();
    let tail: String = api_key
        .chars()
        .rev()
        .take(4)
        .collect::<Vec<_>>()
        .into_iter()
        .rev()
        .collect();
    tracing::info!(
        "SCRAPER_API_KEY loaded: len={} head={} tail={}",
        api_key.len(),
        head,
        tail
    );

    let allowed_origins: Vec<HeaderValue> = std::env::var("ALLOWED_ORIGINS")
        .unwrap_or_default()
        .split(',')
        .filter_map(|o| HeaderValue::from_str(o.trim()).ok())
        .filter(|v| !v.is_empty())
        .collect();

    let app = build_app(ApiKey(api_key), allowed_origins);

    let bind_addr = std::env::var("BIND_ADDR").unwrap_or_else(|_| "127.0.0.1:3001".to_string());
    let listener = match tokio::net::TcpListener::bind(&bind_addr).await {
        Ok(l) => l,
        Err(e) => {
            tracing::error!("Failed to bind {}: {}", bind_addr, e);
            std::process::exit(1);
        }
    };
    tracing::info!("Server listening on {}", listener.local_addr().unwrap());

    let server = axum::serve(listener, app).with_graceful_shutdown(shutdown_signal());

    if let Err(e) = server.await {
        tracing::error!("Server error: {}", e);
    }

    cleanup_browser_processes();
    tracing::info!("Server shutdown complete");
}

async fn shutdown_signal() {
    if let Err(e) = tokio::signal::ctrl_c().await {
        tracing::error!("Failed to install Ctrl+C handler: {}", e);
        return;
    }
    tracing::info!("Shutdown signal received, cleaning up...");
    cleanup_browser_processes();

    tokio::spawn(async {
        tokio::time::sleep(tokio::time::Duration::from_secs(2)).await;
        tracing::info!("Force exiting...");
        std::process::exit(0);
    });
}

fn cleanup_browser_processes() {
    // Only kill Chrome processes we spawned — the ones whose command line
    // references our scraper-specific user-data-dir pattern
    // `chrome_scraper_<our_pid>`. Matching the whole host's chrome instances
    // with `pkill -f chrome` would nuke unrelated browser windows, which has
    // happened on shared dev machines before.
    let marker = format!("chrome_scraper_{}", std::process::id());
    tracing::info!("Cleaning up browser processes matching {marker}");

    #[cfg(target_os = "windows")]
    {
        // /FI "COMMANDLINE eq *marker*" would be ideal but taskkill doesn't
        // support COMMANDLINE filters; fall back to WMIC which does.
        let where_clause = format!("CommandLine like '%%{}%%' and Name='chrome.exe'", marker);
        let _ = std::process::Command::new("wmic")
            .args(["process", "where", &where_clause, "call", "terminate"])
            .creation_flags(0x08000000)
            .output();
    }
    #[cfg(not(target_os = "windows"))]
    {
        let _ = std::process::Command::new("pkill")
            .args(["-f", &marker])
            .output();
    }
}
