// Split out of the former monolithic scraping.rs. Code is unchanged;
// only visibility was widened so cross-module calls resolve.

use anyhow::Result;
use mail_send::mail_builder::MessageBuilder;
use mail_send::SmtpClientBuilder;

// Send email once scraper completes. All configuration comes from environment
// variables so the binary ships no credentials. If SMTP_HOST or SMTP_USER is
// missing the function no-ops so development can run without an SMTP setup.
pub async fn send_email() -> Result<()> {
    let host = match std::env::var("SMTP_HOST") {
        Ok(v) if !v.is_empty() => v,
        _ => {
            tracing::info!("SMTP_HOST not set; skipping completion email");
            return Ok(());
        }
    };
    let port: u16 = std::env::var("SMTP_PORT")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(587);
    let implicit_tls = std::env::var("SMTP_IMPLICIT_TLS")
        .ok()
        .map(|v| v == "1" || v.eq_ignore_ascii_case("true"))
        .unwrap_or(port == 465);

    let user = std::env::var("SMTP_USER").unwrap_or_default();
    let password = std::env::var("SMTP_PASSWORD").unwrap_or_default();
    if user.is_empty() || password.is_empty() {
        tracing::info!("SMTP credentials not set; skipping completion email");
        return Ok(());
    }

    let from_address = std::env::var("SMTP_FROM").unwrap_or_else(|_| user.clone());
    let to_addresses: Vec<(String, String)> = std::env::var("SMTP_TO")
        .unwrap_or_default()
        .split(',')
        .filter_map(|raw| {
            let trimmed = raw.trim();
            if trimmed.is_empty() {
                None
            } else {
                Some(("Recipient".to_string(), trimmed.to_string()))
            }
        })
        .collect();
    if to_addresses.is_empty() {
        tracing::info!("SMTP_TO not set; skipping completion email");
        return Ok(());
    }

    let to_refs: Vec<(&str, &str)> = to_addresses
        .iter()
        .map(|(n, a)| (n.as_str(), a.as_str()))
        .collect();

    let message = MessageBuilder::new()
        .from(("Automation", from_address.as_str()))
        .to(to_refs)
        .subject("Scraping Complete")
        .html_body("<h1>Scraping has completed</h1>")
        .text_body("Scraping has completed, you should now see new listing available.");

    tracing::info!(
        "Connecting to SMTP {}:{} (implicit_tls: {})",
        host,
        port,
        implicit_tls
    );

    let mut client = SmtpClientBuilder::new(host.as_str(), port)
        .implicit_tls(implicit_tls)
        .credentials((user.as_str(), password.as_str()))
        .connect()
        .await?;

    client.send(message).await.map_err(|e| e.into())
}
