//! Request routing and validation. Transport-independent, so the same
//! logic is exercised by tests without opening a socket.

use risk_core::assessment::RiskAssessment;
use risk_core::engine::{evaluate, Policy};
use risk_core::input::RiskInput;
use risk_core::snapshot;
use serde::{Deserialize, Serialize};

/// `docs/04`: bounded input sizes, so a replay endpoint cannot exhaust the
/// demo service. One megabyte is far above any legitimate snapshot and far
/// below anything that threatens the process.
pub const MAX_BODY_BYTES: usize = 1_048_576;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Request {
    pub method: String,
    pub path: String,
    pub body: Vec<u8>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Response {
    pub status: u16,
    pub body: String,
}

impl Response {
    fn json(status: u16, value: &serde_json::Value) -> Self {
        let body = risk_core::canonical::encode(value)
            .ok()
            .and_then(|b| String::from_utf8(b).ok())
            .unwrap_or_else(|| "{}".into());
        Response { status, body }
    }

    fn error(status: u16, code: &str, detail: &str) -> Self {
        Response::json(
            status,
            &serde_json::json!({ "error": code, "detail": detail }),
        )
    }
}

#[derive(Debug, Deserialize)]
struct AssessBody {
    input: RiskInput,
    policy: Policy,
}

#[derive(Debug, Deserialize)]
struct ReplayBody {
    assessment: RiskAssessment,
    input: RiskInput,
    policy: Policy,
}

#[derive(Debug, Serialize)]
struct ReplayResult {
    matched: bool,
    assessment_id: String,
    input_hash: String,
    differences: Vec<String>,
}

/// Route one request. Pure: same request, same response.
pub fn handle(req: &Request) -> Response {
    if req.body.len() > MAX_BODY_BYTES {
        return Response::error(
            413,
            "body_too_large",
            &format!("limit {MAX_BODY_BYTES} bytes"),
        );
    }

    match (req.method.as_str(), req.path.as_str()) {
        ("GET", "/health") => Response::json(200, &serde_json::json!({ "status": "ok" })),
        ("POST", "/v1/assess") => assess(&req.body),
        ("POST", "/v1/replay") => replay(&req.body),
        ("POST", _) | ("GET", _) => Response::error(404, "not_found", &req.path),
        _ => Response::error(405, "method_not_allowed", &req.method),
    }
}

fn assess(body: &[u8]) -> Response {
    let parsed: AssessBody = match serde_json::from_slice(body) {
        Ok(v) => v,
        // Schema validation happens here, at the boundary. A malformed
        // request is rejected before the engine sees it, so the engine
        // never has to represent "not a snapshot".
        Err(e) => return Response::error(400, "invalid_body", &e.to_string()),
    };

    let assessment = evaluate(&parsed.input, &parsed.policy);
    match serde_json::to_value(&assessment) {
        Ok(v) => Response::json(200, &v),
        Err(e) => Response::error(500, "serialize_failed", &e.to_string()),
    }
}

fn replay(body: &[u8]) -> Response {
    let parsed: ReplayBody = match serde_json::from_slice(body) {
        Ok(v) => v,
        Err(e) => return Response::error(400, "invalid_body", &e.to_string()),
    };

    // A replay under different pins is not a replay (`docs/04`). Refusing
    // is more useful than reporting a difference with an obvious cause.
    if parsed.assessment.model_hash != parsed.input.model_policy.model_hash {
        return Response::error(
            409,
            "model_mismatch",
            "assessment and input pin different models",
        );
    }
    if parsed.assessment.policy_hash != parsed.input.model_policy.policy_hash {
        return Response::error(
            409,
            "policy_mismatch",
            "assessment and input pin different policies",
        );
    }

    let replayed = evaluate(&parsed.input, &parsed.policy);
    let differences = diff(&parsed.assessment, &replayed);

    let result = ReplayResult {
        matched: differences.is_empty(),
        assessment_id: replayed.assessment_id.clone(),
        input_hash: replayed.input_hash.clone(),
        differences,
    };
    match serde_json::to_value(&result) {
        Ok(v) => Response::json(200, &v),
        Err(e) => Response::error(500, "serialize_failed", &e.to_string()),
    }
}

/// Compare by decisive fields, falling back to the output hash so an
/// unlisted field cannot diverge silently.
fn diff(a: &RiskAssessment, b: &RiskAssessment) -> Vec<String> {
    let mut out = Vec::new();
    if a.assessment_id != b.assessment_id {
        out.push(format!(
            "assessment_id: retained={} replayed={}",
            a.assessment_id, b.assessment_id
        ));
    }
    if a.input_hash != b.input_hash {
        out.push(format!(
            "input_hash: retained={} replayed={}",
            a.input_hash, b.input_hash
        ));
    }
    if a.authorization_class != b.authorization_class {
        out.push(format!(
            "authorization_class: retained={:?} replayed={:?}",
            a.authorization_class, b.authorization_class
        ));
    }

    if out.is_empty() {
        let ja = serde_json::to_value(a).unwrap_or_default();
        let jb = serde_json::to_value(b).unwrap_or_default();
        if let (Ok(ha), Ok(hb)) = (snapshot::output_hash(&ja), snapshot::output_hash(&jb)) {
            if ha != hb {
                out.push(format!("output_hash: retained={ha} replayed={hb}"));
            }
        }
    }
    out
}
