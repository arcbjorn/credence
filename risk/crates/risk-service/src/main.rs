//! Entry point. Address comes from the environment so the demo deployment
//! and the test harness bind differently without a code change.
//!
//! **Two transports, one handler.** `docs/10` gates the gRPC change on
//! passing the same acceptance matrix as JSON without changing frozen
//! artifacts, which is only checkable if both can run against the same
//! evidence. `RISK_SERVICE_GRPC_ADDR` starts the gRPC listener; absent,
//! the service is exactly what it was before.
//!
//! Both listeners delegate to `handler`, so neither can validate
//! differently from the other. A transport that accepted what the other
//! refused would make "the matrix passes on both" a weaker claim than it
//! sounds.

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let http_addr = std::env::var("RISK_SERVICE_ADDR").unwrap_or_else(|_| "127.0.0.1:8081".into());
    let grpc_addr = std::env::var("RISK_SERVICE_GRPC_ADDR").ok();

    let Some(grpc_addr) = grpc_addr else {
        // The default path: HTTP only, no async runtime started.
        risk_service::server::serve(&http_addr)?;
        return Ok(());
    };

    // gRPC requested. HTTP moves to its own thread so both serve at once
    // -- the acceptance matrix runs against one while the other stays
    // reachable, which is how the two are compared.
    let http = std::thread::spawn(move || {
        if let Err(e) = risk_service::server::serve(&http_addr) {
            eprintln!("http listener stopped: {e}");
        }
    });

    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()?;

    runtime.block_on(async move {
        let addr = grpc_addr.parse()?;
        eprintln!("grpc listening on {addr}");
        tonic::transport::Server::builder()
            .add_service(risk_service::grpc::server())
            .serve(addr)
            .await?;
        Ok::<(), Box<dyn std::error::Error>>(())
    })?;

    // Unreachable while the gRPC server runs; joined so a future shutdown
    // path does not leak the thread.
    let _ = http.join();
    Ok(())
}
