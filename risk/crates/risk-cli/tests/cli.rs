//! CLI behaviour. Runs the built binary against the committed fixture pack,
//! so a change that breaks the replay demo fails here rather than on stage.

use std::path::PathBuf;
use std::process::Command;

fn repo_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(3)
        .unwrap()
        .to_path_buf()
}

fn bin() -> PathBuf {
    PathBuf::from(env!("CARGO_BIN_EXE_credence-risk"))
}

fn fixture(name: &str) -> PathBuf {
    repo_root().join("fixtures/performer").join(name)
}

fn run(args: &[&str]) -> (bool, String, String) {
    let out = Command::new(bin()).args(args).output().expect("run cli");
    (
        out.status.success(),
        String::from_utf8_lossy(&out.stdout).to_string(),
        String::from_utf8_lossy(&out.stderr).to_string(),
    )
}

/// Demo beat 8: a retained assessment replays to an exact match.
#[test]
fn replay_of_retained_assessment_matches() {
    let (ok, stdout, _) = run(&[
        "replay",
        "--assessment",
        fixture("assessment.json").to_str().unwrap(),
        "--input",
        fixture("input.json").to_str().unwrap(),
        "--policy",
        fixture("policy.json").to_str().unwrap(),
    ]);
    assert!(ok, "replay must succeed");
    assert!(stdout.contains("MATCH"), "expected MATCH, got: {stdout}");
}

/// A replay that always matched would prove nothing. Altering the evidence
/// must produce a difference and a non-zero exit.
#[test]
fn replay_detects_altered_evidence() {
    let dir = std::env::temp_dir().join("credence-cli-test");
    std::fs::create_dir_all(&dir).unwrap();
    let altered = dir.join("altered.json");

    let raw = std::fs::read_to_string(fixture("input.json")).unwrap();
    let mut v: serde_json::Value = serde_json::from_str(&raw).unwrap();
    v["account"]["value"]["settlement_balance"] = serde_json::json!("1000000000");
    std::fs::write(&altered, serde_json::to_string(&v).unwrap()).unwrap();

    let (ok, stdout, _) = run(&[
        "replay",
        "--assessment",
        fixture("assessment.json").to_str().unwrap(),
        "--input",
        altered.to_str().unwrap(),
        "--policy",
        fixture("policy.json").to_str().unwrap(),
    ]);
    assert!(!ok, "an altered replay must fail");
    assert!(stdout.contains("DIFFERENCE"));
    // The diff must name what changed, not merely report inequality.
    assert!(
        stdout.contains("input_hash"),
        "diff must name the fields: {stdout}"
    );
}

/// A replay under a different model is not a replay. Refuse it rather than
/// reporting a difference with an obvious cause.
#[test]
fn replay_refuses_model_mismatch() {
    let dir = std::env::temp_dir().join("credence-cli-test");
    std::fs::create_dir_all(&dir).unwrap();
    let wrong = dir.join("wrong-model.json");

    let raw = std::fs::read_to_string(fixture("input.json")).unwrap();
    let mut v: serde_json::Value = serde_json::from_str(&raw).unwrap();
    v["model_policy"]["model_hash"] = serde_json::json!("different-model");
    std::fs::write(&wrong, serde_json::to_string(&v).unwrap()).unwrap();

    let (ok, _, stderr) = run(&[
        "replay",
        "--assessment",
        fixture("assessment.json").to_str().unwrap(),
        "--input",
        wrong.to_str().unwrap(),
        "--policy",
        fixture("policy.json").to_str().unwrap(),
    ]);
    assert!(!ok);
    assert!(stderr.contains("model mismatch"), "got: {stderr}");
}

/// `assess` prints canonical JSON, so its output is directly hashable.
#[test]
fn assess_emits_canonical_json() {
    let (ok, stdout, _) = run(&[
        "assess",
        "--input",
        fixture("input.json").to_str().unwrap(),
        "--policy",
        fixture("policy.json").to_str().unwrap(),
    ]);
    assert!(ok);
    let trimmed = stdout.trim();
    assert!(trimmed.starts_with('{') && trimmed.ends_with('}'));
    // Canonical output has no whitespace padding between tokens.
    assert!(
        !trimmed.contains(": "),
        "canonical output must not be pretty-printed"
    );
    serde_json::from_str::<serde_json::Value>(trimmed).expect("valid json");
}

#[test]
fn inspect_reports_identity_and_structure() {
    let (ok, stdout, _) = run(&[
        "inspect",
        "--input",
        fixture("input.json").to_str().unwrap(),
    ]);
    assert!(ok);
    for field in ["input_hash", "assessment_id", "model_hash", "structure"] {
        assert!(stdout.contains(field), "missing {field} in: {stdout}");
    }
    assert!(stdout.contains("valid"));
}

#[test]
fn missing_arguments_fail_cleanly() {
    let (ok, _, stderr) = run(&["assess"]);
    assert!(!ok);
    assert!(stderr.contains("missing --input"), "got: {stderr}");
}

#[test]
fn unknown_command_fails() {
    let (ok, _, stderr) = run(&["frobnicate"]);
    assert!(!ok);
    assert!(stderr.contains("unknown command"));
}
