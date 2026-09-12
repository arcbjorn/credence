//! Entry point. Address comes from the environment so the demo deployment
//! and the test harness bind differently without a code change.

fn main() -> std::io::Result<()> {
    let addr = std::env::var("RISK_SERVICE_ADDR").unwrap_or_else(|_| "127.0.0.1:8081".into());
    risk_service::server::serve(&addr)
}
