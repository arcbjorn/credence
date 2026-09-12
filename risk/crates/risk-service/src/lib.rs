//! HTTP boundary for the risk engine.
//!
//! `docs/01`: this crate owns the service boundary, schema validation, and
//! resource limits. It owns **no database state and no provider
//! orchestration** — those belong to the Go gateway.
//!
//! Deliberately built on the standard library. The engine is a pure
//! function and the surface is three endpoints; a web framework would add
//! dependency surface without removing any of the work that actually
//! matters here, which is bounding input and refusing malformed requests
//! before they reach the engine.

pub mod handler;
pub mod server;

pub use handler::{handle, Request, Response};
