//! The gRPC boundary, over tonic.
//!
//! `docs/10` gates an isolated transport change: it must pass the same
//! reference vectors and acceptance matrix **without changing contracts,
//! schemas, model or fixtures**. `docs/01`: "the boundary is
//! architectural and permanent; only its wire format changes."
//!
//! **This adds no logic.** Every method decodes bytes, hands them to the
//! same `handler` the HTTP path uses, and encodes the reply. A transport
//! that validated differently from the one beside it would make the gate
//! meaningless -- the point is that both carry identical decisions.
//!
//! **Every payload is `bytes`, never a structured message.** The engine
//! hashes the snapshot and the policy exactly as the caller handed them
//! over, so a protobuf schema that decomposed them into fields would
//! re-serialize on the way out and change the bytes being hashed: a
//! retained assessment would stop reproducing. The canonical encoding
//! lives in `schemas/v1/`, it is JSON, and it is frozen. gRPC carries
//! it; it does not redefine it.

use tonic::{Request, Response, Status};

use crate::handler;

/// The generated types and service trait, from `schemas/proto/risk.proto`.
pub mod pb {
    // Generated at build time by `build.rs` rather than committed: a
    // committed stub is a second copy that can fall behind the schema.
    tonic::include_proto!("credence.risk.v1");
}

use pb::risk_server::{Risk, RiskServer};
use pb::{
    AssessRequest, AssessResponse, HealthRequest, HealthResponse, ReplayRequest, ReplayResponse,
};

/// Stateless, like the HTTP handler. The engine owns no state, and a
/// service that did would break replay.
#[derive(Debug, Default, Clone)]
pub struct RiskService;

/// Build a server ready to add to a tonic router.
pub fn server() -> RiskServer<RiskService> {
    RiskServer::new(RiskService)
}

/// Map a handler response onto a gRPC result.
///
/// The status codes are chosen so a caller can tell a BAD REQUEST from a
/// service fault without parsing prose: 400 becomes `InvalidArgument`,
/// 413 becomes `ResourceExhausted`, and anything else `Internal`. The
/// body is carried through verbatim, because it is the same canonical
/// JSON error the HTTP path returns and the two must not diverge.
fn into_result(response: handler::Response) -> Result<Vec<u8>, Status> {
    match response.status {
        200 => Ok(response.body.into_bytes()),
        400 => Err(Status::invalid_argument(response.body)),
        413 => Err(Status::resource_exhausted(response.body)),
        404 => Err(Status::not_found(response.body)),
        _ => Err(Status::internal(response.body)),
    }
}

/// Reject an oversized payload before it reaches the engine.
///
/// tonic enforces its own limit, but this mirrors
/// `handler::MAX_BODY_BYTES` so both transports refuse the same input at
/// the same size. A transport that accepted what the other rejected
/// would make "the acceptance matrix passes on both" a weaker claim than
/// it sounds.
fn check_size(bytes: &[u8], field: &str) -> Result<(), Status> {
    if bytes.len() > handler::MAX_BODY_BYTES {
        return Err(Status::resource_exhausted(format!(
            "{field} exceeds {} bytes",
            handler::MAX_BODY_BYTES
        )));
    }
    Ok(())
}

#[tonic::async_trait]
impl Risk for RiskService {
    async fn assess(
        &self,
        request: Request<AssessRequest>,
    ) -> Result<Response<AssessResponse>, Status> {
        let req = request.into_inner();
        check_size(&req.input, "input")?;
        check_size(&req.policy, "policy")?;

        // The same JSON envelope the HTTP path posts. Built here rather
        // than in the handler so the handler stays transport-independent
        // and one code path evaluates both transports' requests.
        let body = envelope(&[("input", &req.input), ("policy", &req.policy)])?;
        let assessment = into_result(handler::handle(&handler::Request {
            method: "POST".into(),
            path: "/v1/assess".into(),
            body,
        }))?;

        Ok(Response::new(AssessResponse { assessment }))
    }

    async fn replay(
        &self,
        request: Request<ReplayRequest>,
    ) -> Result<Response<ReplayResponse>, Status> {
        let req = request.into_inner();
        check_size(&req.assessment, "assessment")?;
        check_size(&req.input, "input")?;
        check_size(&req.policy, "policy")?;

        let body = envelope(&[
            ("assessment", &req.assessment),
            ("input", &req.input),
            ("policy", &req.policy),
        ])?;
        let raw = into_result(handler::handle(&handler::Request {
            method: "POST".into(),
            path: "/v1/replay".into(),
            body,
        }))?;

        // The verdict is a small, fixed shape, so it DOES become typed
        // fields -- unlike the payloads above, nothing hashes it.
        let value: serde_json::Value = serde_json::from_slice(&raw)
            .map_err(|e| Status::internal(format!("decode replay verdict: {e}")))?;

        Ok(Response::new(ReplayResponse {
            matched: value["matched"].as_bool().unwrap_or(false),
            assessment_id: value["assessment_id"]
                .as_str()
                .unwrap_or_default()
                .to_string(),
            input_hash: value["input_hash"].as_str().unwrap_or_default().to_string(),
            differences: value["differences"]
                .as_array()
                .map(|a| {
                    a.iter()
                        .map(|d| d.as_str().unwrap_or_default().to_string())
                        .collect()
                })
                .unwrap_or_default(),
        }))
    }

    async fn health(
        &self,
        _request: Request<HealthRequest>,
    ) -> Result<Response<HealthResponse>, Status> {
        // The model hash, not a bare OK. A healthy service running a
        // different engine is worse than one that is down: the caller
        // would get assessments it could not reproduce.
        Ok(Response::new(HealthResponse {
            model_hash: risk_core::model_hash(),
        }))
    }
}

/// Wrap raw JSON payloads in the envelope the handler expects.
///
/// Assembled by concatenation rather than through `serde_json`, because
/// re-encoding would normalise the caller's bytes and change what the
/// engine hashes. Each payload must already be valid JSON; a caller that
/// sends garbage gets the handler's own parse error, which is the same
/// error the HTTP path returns for the same input.
fn envelope(parts: &[(&str, &Vec<u8>)]) -> Result<Vec<u8>, Status> {
    let mut body = Vec::with_capacity(parts.iter().map(|(_, v)| v.len() + 16).sum::<usize>() + 2);
    body.push(b'{');
    for (i, (name, value)) in parts.iter().enumerate() {
        if i > 0 {
            body.push(b',');
        }
        body.push(b'"');
        body.extend_from_slice(name.as_bytes());
        body.extend_from_slice(b"\":");
        if value.is_empty() {
            return Err(Status::invalid_argument(format!("{name} is empty")));
        }
        body.extend_from_slice(value);
    }
    body.push(b'}');
    Ok(body)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The envelope must carry the caller's bytes VERBATIM. Re-encoding
    /// through serde would normalise escapes and change the hash.
    #[test]
    fn envelope_does_not_reencode() {
        let input = b"{\"e\":\"\\u00e9\",\"n\":1.0}".to_vec();
        let policy = br#"{"p":2}"#.to_vec();
        let body = envelope(&[("input", &input), ("policy", &policy)]).expect("builds");

        let text = String::from_utf8(body).expect("utf8");
        assert!(text.contains(r"\u00e9"), "escape was rewritten: {text}");
        assert!(text.contains("1.0"), "number was normalised: {text}");
        assert_eq!(text, r#"{"input":{"e":"\u00e9","n":1.0},"policy":{"p":2}}"#);
    }

    /// An empty payload is refused rather than spliced in, which would
    /// produce `{"input":}` and a confusing parse error downstream.
    #[test]
    fn empty_payload_is_refused() {
        let empty = Vec::new();
        let policy = br#"{"p":2}"#.to_vec();
        assert!(envelope(&[("input", &empty), ("policy", &policy)]).is_err());
    }

    /// Both transports must refuse the same size, or "the matrix passes
    /// on both" means less than it sounds.
    #[test]
    fn oversize_is_refused_at_the_same_limit() {
        let ok = vec![b'x'; handler::MAX_BODY_BYTES];
        assert!(check_size(&ok, "input").is_ok());

        let too_big = vec![b'x'; handler::MAX_BODY_BYTES + 1];
        let err = check_size(&too_big, "input").expect_err("refuses");
        assert_eq!(err.code(), tonic::Code::ResourceExhausted);
    }

    /// A handler error must keep its distinction: a malformed request is
    /// the caller's fault, and a caller cannot fix an Internal.
    #[test]
    fn status_codes_distinguish_caller_from_service() {
        let bad = handler::Response {
            status: 400,
            body: "{}".into(),
        };
        assert_eq!(
            into_result(bad).expect_err("errors").code(),
            tonic::Code::InvalidArgument
        );

        let oversize = handler::Response {
            status: 413,
            body: "{}".into(),
        };
        assert_eq!(
            into_result(oversize).expect_err("errors").code(),
            tonic::Code::ResourceExhausted
        );
    }
}
