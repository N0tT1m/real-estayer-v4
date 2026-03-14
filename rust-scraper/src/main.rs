mod models;
mod database;
mod stealth_browser;  // CDP-based stealth browser
mod routes;
mod scraping;
mod watchlist;

use tracing_appender::rolling::{RollingFileAppender, Rotation};
use tracing_subscriber::{fmt, prelude::*, EnvFilter};
use axum::{routing::get, Router};
use tower_http::cors::{Any, CorsLayer};

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
    let file_filter = EnvFilter::new(
        "off,rust_scraper=debug"
    );

    // Console filter: Only show our app logs, nothing else
    let console_filter = EnvFilter::new(
        "off,rust_scraper=info"
    );

    // File appender for detailed logs
    let file_appender = RollingFileAppender::builder()
        .rotation(Rotation::DAILY)
        .filename_prefix("scraper")
        .filename_suffix("log")
        .build("logs")
        .expect("failed to initialize rolling file appender");

    let subscriber = tracing_subscriber::registry()
        // File layer: more detail, structured
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
                .with_filter(file_filter)
        )
        // Console layer: clean, human-readable
        .with(
            fmt::Layer::new()
                .with_file(false)
                .with_line_number(false)
                .with_thread_ids(false)
                .with_target(false)
                .with_ansi(true)
                .compact()
                .with_filter(console_filter)
        );

    if let Err(e) = tracing::subscriber::set_global_default(subscriber) {
        eprintln!("Failed to set up logging: {}", e);
    }

    eprintln!("Logging initialized - file: logs/scraper.YYYY-MM-DD.log");
}

#[tokio::main]
async fn main() {
    // Load environment variables from .env file
    dotenv::dotenv().ok();
    
    // Initialize default crypto provider for rustls (using ring - no cmake/NASM required on Windows)
    let _ = rustls::crypto::ring::default_provider().install_default();
    
    setup_logging();
    tracing::info!("Starting application...");

    let cors = CorsLayer::new()
        .allow_origin(Any)
        .allow_methods(Any)
        .allow_headers(Any);

    let app = Router::new()
        // Comprehensive scraping endpoints
        .route("/scrape-north-america", get(routes::scrape_north_america))
        .route("/scrape-city-data", get(routes::scrape_city_data))
        .route("/scrape/status", get(routes::scrape_status))

        // Listing retrieval endpoints
        .route("/get-listings", get(routes::get_listings_without_limit))
        .route("/get-all-listings", get(routes::get_all_listings))
        .route("/filters", get(routes::filters))
        .route("/get-listings/{city}/{limit}", get(routes::get_listings))
        .route("/get-listings/{city}", get(routes::get_listings_without_limit))
        .route("/get-listing/{listing_id}", get(routes::get_listing))
        
        // System endpoints
        .route("/info", get(routes::info))
        .route("/health", get(routes::health))
        .route("/test-email", get(routes::test_email))
        .route("/test-stealth", get(routes::test_stealth_browser))
        
        // Watchlist routes - temporarily commented out
        // .route("/watchlist", post(routes::add_to_watchlist))
        // .route("/watchlist/:user_id", get(routes::get_watchlist))
        // .route("/watchlist/:user_id/:listing_id", delete(routes::remove_from_watchlist))
        // .route("/watchlist/check-prices", post(routes::check_price_updates))
        // .route("/watchlist/price-history/:listing_id", get(routes::get_price_history))
        .layer(cors);

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3001")
        .await
        .expect("Failed to start server.");
    tracing::info!("Server listening on {}", listener.local_addr().unwrap());

    // Graceful shutdown with Ctrl+C handling
    let server = axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal());

    if let Err(e) = server.await {
        tracing::error!("Server error: {}", e);
    }

    // Cleanup Chrome processes on shutdown
    cleanup_browser_processes();
    tracing::info!("Server shutdown complete");
}

async fn shutdown_signal() {
    tokio::signal::ctrl_c()
        .await
        .expect("Failed to install Ctrl+C handler");
    tracing::info!("Shutdown signal received, cleaning up...");
    // Kill Chrome immediately - don't wait for graceful shutdown
    cleanup_browser_processes();

    // Force exit after 2 seconds if server doesn't shut down gracefully
    tokio::spawn(async {
        tokio::time::sleep(tokio::time::Duration::from_secs(2)).await;
        tracing::info!("Force exiting...");
        std::process::exit(0);
    });
}

fn cleanup_browser_processes() {
    tracing::info!("Cleaning up browser processes...");
    #[cfg(target_os = "windows")]
    {
        // Kill Chrome processes on Windows
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
}