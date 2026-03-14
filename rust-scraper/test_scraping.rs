// test_scraping.rs - Simple test to verify scraping functionality
use std::process::Command;
use reqwest;
use tokio;
use std::time::Duration;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    println!("Testing Rust scraper functionality...");
    
    // Start the server
    println!("Starting Rust scraper server...");
    let mut server = Command::new("cargo")
        .arg("run")
        .spawn()
        .expect("Failed to start server");
    
    // Wait for server to start
    tokio::time::sleep(Duration::from_secs(5)).await;
    
    // Test health endpoint first
    println!("Testing health endpoint...");
    match reqwest::get("http://localhost:3001/health").await {
        Ok(response) => {
            if response.status().is_success() {
                println!("✓ Server is running!");
                let body = response.text().await?;
                println!("Health response: {}", body);
            } else {
                println!("✗ Server health check failed: {}", response.status());
            }
        }
        Err(e) => {
            println!("✗ Failed to connect to server: {}", e);
            server.kill().ok();
            return Ok(());
        }
    }
    
    // Test scraping Montreal listings
    println!("\nTesting scraping Montreal listings...");
    match reqwest::get("http://localhost:3001/scrape-city-data?city=Montreal").await {
        Ok(response) => {
            if response.status().is_success() {
                println!("✓ Scraping request accepted!");
                let body = response.text().await?;
                println!("Scraping response: {}", body);
            } else {
                println!("✗ Scraping failed: {}", response.status());
                let body = response.text().await?;
                println!("Error response: {}", body);
            }
        }
        Err(e) => {
            println!("✗ Failed to scrape: {}", e);
        }
    }
    
    // Test getting all listings to see total count
    println!("\nTesting get all listings...");
    match reqwest::get("http://localhost:3001/get-all-listings").await {
        Ok(response) => {
            if response.status().is_success() {
                let body = response.text().await?;
                println!("Total listings response length: {} characters", body.len());
                
                // Parse JSON to count listings
                if let Ok(json_value) = serde_json::from_str::<serde_json::Value>(&body) {
                    if let Some(array) = json_value.as_array() {
                        println!("✓ Total listings in database: {}", array.len());
                    }
                }
            } else {
                println!("✗ Failed to get listings: {}", response.status());
            }
        }
        Err(e) => {
            println!("✗ Failed to get listings: {}", e);
        }
    }
    
    // Clean up
    println!("\nShutting down server...");
    server.kill().ok();
    println!("✓ Test completed!");
    
    Ok(())
}