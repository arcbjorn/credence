//! Generate the tonic server stubs from `schemas/proto/risk.proto`.
//!
//! The proto is the single source for both ends: Go's stubs come from the
//! same file via `protoc-gen-go`, so the two cannot drift. Generating at
//! build time rather than committing the output keeps them that way --
//! a committed stub is a second copy that can fall behind the first,
//! which is the same rule `web/src/engine/` follows.

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto = "../../../schemas/proto/risk.proto";

    // Rebuild when the schema changes, and only then.
    println!("cargo:rerun-if-changed={proto}");

    tonic_prost_build::configure()
        // The server only. Rust never calls this service; the Go gateway
        // does, and its client comes from protoc-gen-go-grpc.
        .build_client(false)
        .build_server(true)
        .compile_protos(&[proto], &["../../../schemas/proto"])?;

    Ok(())
}
