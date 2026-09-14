//! Handler behaviour, exercised without opening a socket.

use risk_service::{handle, Request};

fn repo_root() -> std::path::PathBuf {
    std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(3)
        .unwrap()
        .to_path_buf()
}

fn fixture(name: &str) -> serde_json::Value {
    let path = repo_root().join("fixtures/performer").join(name);
    let raw = std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("read {path:?}: {e}"));
    serde_json::from_str(&raw).expect("valid fixture")
}

fn post(path: &str, body: serde_json::Value) -> risk_service::Response {
    handle(&Request {
        method: "POST".into(),
        path: path.into(),
        body: serde_json::to_vec(&body).unwrap(),
    })
}

#[test]
fn health_responds() {
    let res = handle(&Request {
        method: "GET".into(),
        path: "/health".into(),
        body: vec![],
    });
    assert_eq!(res.status, 200);
    assert!(res.body.contains("ok"));
}

#[test]
fn assess_returns_an_assessment() {
    let res = post(
        "/v1/assess",
        serde_json::json!({ "input": fixture("input.json"), "policy": fixture("policy.json") }),
    );
    assert_eq!(res.status, 200, "body: {}", res.body);

    let v: serde_json::Value = serde_json::from_str(&res.body).unwrap();
    assert!(v["assessment_id"].as_str().is_some_and(|s| !s.is_empty()));
    assert_eq!(v["outcome"]["decision"], "ApprovedOrigination");
}

/// The service must return canonical JSON, so its response is directly
/// hashable by the caller.
#[test]
fn responses_are_canonical() {
    let res = post(
        "/v1/assess",
        serde_json::json!({ "input": fixture("input.json"), "policy": fixture("policy.json") }),
    );
    assert!(
        !res.body.contains(": "),
        "response must not be pretty-printed"
    );

    // Keys are sorted, so the same result always serializes identically.
    let again = post(
        "/v1/assess",
        serde_json::json!({ "input": fixture("input.json"), "policy": fixture("policy.json") }),
    );
    assert_eq!(res.body, again.body);
}

/// Demo beat 8 over HTTP.
#[test]
fn replay_reports_a_match() {
    let res = post(
        "/v1/replay",
        serde_json::json!({
            "assessment": fixture("assessment.json"),
            "input": fixture("input.json"),
            "policy": fixture("policy.json"),
        }),
    );
    assert_eq!(res.status, 200, "body: {}", res.body);
    let v: serde_json::Value = serde_json::from_str(&res.body).unwrap();
    assert_eq!(v["matched"], true, "body: {}", res.body);
    assert_eq!(v["differences"].as_array().unwrap().len(), 0);
}

/// A replay that always matched would prove nothing.
#[test]
fn replay_detects_altered_evidence() {
    let mut input = fixture("input.json");
    input["account"]["value"]["settlement_balance"] = serde_json::json!("1000000000");

    let res = post(
        "/v1/replay",
        serde_json::json!({
            "assessment": fixture("assessment.json"),
            "input": input,
            "policy": fixture("policy.json"),
        }),
    );
    assert_eq!(res.status, 200);
    let v: serde_json::Value = serde_json::from_str(&res.body).unwrap();
    assert_eq!(v["matched"], false);
    assert!(!v["differences"].as_array().unwrap().is_empty());
}

/// A replay under different pins is refused, not diffed.
#[test]
fn replay_refuses_pin_mismatch() {
    let mut input = fixture("input.json");
    input["model_policy"]["model_hash"] = serde_json::json!("other-model");

    let res = post(
        "/v1/replay",
        serde_json::json!({
            "assessment": fixture("assessment.json"),
            "input": input,
            "policy": fixture("policy.json"),
        }),
    );
    assert_eq!(res.status, 409);
    assert!(res.body.contains("model_mismatch"));
}

/// Schema validation happens at the boundary, so the engine never sees a
/// malformed request.
#[test]
fn malformed_body_is_rejected() {
    let res = handle(&Request {
        method: "POST".into(),
        path: "/v1/assess".into(),
        body: b"{\"input\": \"not a snapshot\"}".to_vec(),
    });
    assert_eq!(res.status, 400);
    assert!(res.body.contains("invalid_body"));
}

/// `docs/04`: bounded input sizes, so a replay endpoint cannot exhaust the
/// demo service.
#[test]
fn oversized_body_is_rejected() {
    let res = handle(&Request {
        method: "POST".into(),
        path: "/v1/assess".into(),
        body: vec![b'x'; risk_service::handler::MAX_BODY_BYTES + 1],
    });
    assert_eq!(res.status, 413);
}

#[test]
fn unknown_path_is_not_found() {
    let res = post("/v1/nope", serde_json::json!({}));
    assert_eq!(res.status, 404);
}

#[test]
fn unsupported_method_is_rejected() {
    let res = handle(&Request {
        method: "DELETE".into(),
        path: "/v1/assess".into(),
        body: vec![],
    });
    assert_eq!(res.status, 405);
}

/// The handler is pure, like the engine beneath it.
#[test]
fn handling_is_deterministic() {
    let body = serde_json::json!({
        "input": fixture("input.json"),
        "policy": fixture("policy.json"),
    });
    let first = post("/v1/assess", body.clone());
    for _ in 0..10 {
        assert_eq!(post("/v1/assess", body.clone()), first);
    }
}
