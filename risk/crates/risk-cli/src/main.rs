//! `credence-risk` — offline assessment and replay.
//!
//! `docs/04` names these commands as the interface that makes replay
//! demonstrable. The CLI needs no network and no database: it reads a
//! snapshot and a policy from disk, runs the pure engine, and prints the
//! canonical result. That is the whole point — a judge can reproduce a
//! historical decision from retained evidence with nothing running.
//!
//!   credence-risk assess --input <snapshot.json> --policy <policy.json>
//!   credence-risk replay --assessment <assessment.json> --input <snapshot.json> --policy <policy.json>
//!   credence-risk inspect --input <snapshot.json>

use std::fs;
use std::process::ExitCode;

use risk_core::assessment::RiskAssessment;
use risk_core::engine::{evaluate, Policy};
use risk_core::input::RiskInput;
use risk_core::snapshot;

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.is_empty() {
        usage();
        return ExitCode::FAILURE;
    }

    let result = match args[0].as_str() {
        "assess" => cmd_assess(&args[1..]),
        "replay" => cmd_replay(&args[1..]),
        "inspect" => cmd_inspect(&args[1..]),
        "--help" | "-h" | "help" => {
            usage();
            return ExitCode::SUCCESS;
        }
        other => Err(format!("unknown command: {other}")),
    };

    match result {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            eprintln!("error: {e}");
            ExitCode::FAILURE
        }
    }
}

fn usage() {
    eprintln!(
        "credence-risk — offline assessment and replay

  assess  --input <file> --policy <file>
          Evaluate a snapshot and print the canonical assessment.

  replay  --assessment <file> --input <file> --policy <file>
          Re-evaluate and compare against a retained assessment.
          Reports MATCH or the fields responsible for a difference.

  inspect --input <file>
          Print the snapshot's identity without evaluating it."
    );
}

fn flag(args: &[String], name: &str) -> Result<String, String> {
    let idx = args
        .iter()
        .position(|a| a == name)
        .ok_or_else(|| format!("missing {name}"))?;
    args.get(idx + 1)
        .cloned()
        .ok_or_else(|| format!("{name} needs a value"))
}

fn load<T: serde::de::DeserializeOwned>(path: &str) -> Result<T, String> {
    let raw = fs::read_to_string(path).map_err(|e| format!("read {path}: {e}"))?;
    serde_json::from_str(&raw).map_err(|e| format!("parse {path}: {e}"))
}

fn cmd_assess(args: &[String]) -> Result<(), String> {
    let input: RiskInput = load(&flag(args, "--input")?)?;
    let policy: Policy = load(&flag(args, "--policy")?)?;

    let assessment = evaluate(&input, &policy);
    print_canonical(&assessment)
}

fn cmd_inspect(args: &[String]) -> Result<(), String> {
    let input: RiskInput = load(&flag(args, "--input")?)?;
    let identity =
        snapshot::identify(&input).map_err(|e| format!("identify: {e:?}"))?;

    println!("input_hash    {}", identity.input_hash);
    println!("assessment_id {}", identity.assessment_id);
    println!("model_hash    {}", input.model_policy.model_hash);
    println!("policy_hash   {}", input.model_policy.policy_hash);
    match input.validate() {
        Ok(()) => println!("structure     valid"),
        Err(e) => println!("structure     INVALID: {e:?}"),
    }
    Ok(())
}

/// Re-evaluate and compare, reporting the fields responsible for any
/// difference rather than a bare mismatch (`docs/04`).
fn cmd_replay(args: &[String]) -> Result<(), String> {
    let original: RiskAssessment = load(&flag(args, "--assessment")?)?;
    let input: RiskInput = load(&flag(args, "--input")?)?;
    let policy: Policy = load(&flag(args, "--policy")?)?;

    // A replay under a different model or policy is not a replay. Say so
    // rather than reporting a difference that has an obvious cause.
    if original.model_hash != input.model_policy.model_hash {
        return Err(format!(
            "model mismatch: assessment used {}, input pins {}",
            original.model_hash, input.model_policy.model_hash
        ));
    }
    if original.policy_hash != input.model_policy.policy_hash {
        return Err(format!(
            "policy mismatch: assessment used {}, input pins {}",
            original.policy_hash, input.model_policy.policy_hash
        ));
    }

    let replayed = evaluate(&input, &policy);
    let differences = diff(&original, &replayed);

    if differences.is_empty() {
        println!("MATCH");
        println!("  assessment_id {}", replayed.assessment_id);
        println!("  input_hash    {}", replayed.input_hash);
        Ok(())
    } else {
        println!("DIFFERENCE");
        for d in &differences {
            println!("  {d}");
        }
        Err(format!("{} field(s) differ", differences.len()))
    }
}

/// Field-level comparison. A bare "not equal" would leave the operator
/// guessing which input drifted.
fn diff(a: &RiskAssessment, b: &RiskAssessment) -> Vec<String> {
    let mut out = Vec::new();
    let mut cmp = |name: &str, x: String, y: String| {
        if x != y {
            out.push(format!("{name}: retained={x} replayed={y}"));
        }
    };

    cmp("assessment_id", a.assessment_id.clone(), b.assessment_id.clone());
    cmp("input_hash", a.input_hash.clone(), b.input_hash.clone());
    cmp("schema_version", a.schema_version.clone(), b.schema_version.clone());
    cmp("sequence", a.sequence.to_string(), b.sequence.to_string());
    cmp(
        "authorization_class",
        format!("{:?}", a.authorization_class),
        format!("{:?}", b.authorization_class),
    );
    // Compare the outcome by its decisive fields rather than dumping the
    // whole struct — an operator needs to see WHAT differs, not scroll past
    // two near-identical debug renderings.
    cmp("decision", decision_label(a), decision_label(b));
    cmp("granted", granted(a), granted(b));
    cmp("apr_bps", apr(a), apr(b));
    cmp("agent_capacity", agent_capacity(a), agent_capacity(b));
    cmp("binding_constraint", binding(a), binding(b));
    cmp("score", score(a), score(b));
    cmp(
        "synthetic_used",
        a.synthetic_used.to_string(),
        b.synthetic_used.to_string(),
    );

    if out.is_empty() {
        // Catch anything the named fields miss, so a silent divergence in
        // an unlisted field still surfaces.
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

fn decision_label(a: &RiskAssessment) -> String {
    use risk_core::assessment::Outcome::*;
    match &a.outcome {
        ApprovedOrigination { .. } => "approved".into(),
        Denied { .. } => "denied".into(),
        Unavailable => "unavailable".into(),
        MonitoringAllowed { .. } => "monitoring_allowed".into(),
        MonitoringBlocked { .. } => "monitoring_blocked".into(),
    }
}

fn granted(a: &RiskAssessment) -> String {
    match &a.outcome {
        risk_core::assessment::Outcome::ApprovedOrigination { approved_principal, .. } => {
            approved_principal.0.to_string()
        }
        _ => "-".into(),
    }
}

fn apr(a: &RiskAssessment) -> String {
    a.outcome.terms().map_or("-".into(), |t| t.apr_bps.0.to_string())
}

fn capacity_view(a: &RiskAssessment) -> Option<&risk_core::assessment::CapacityView> {
    use risk_core::assessment::Outcome::*;
    match &a.outcome {
        ApprovedOrigination { capacity, .. } => Some(capacity),
        Denied { capacity, .. } => capacity.as_ref(),
        _ => None,
    }
}

fn agent_capacity(a: &RiskAssessment) -> String {
    capacity_view(a).map_or("-".into(), |c| c.agent_capacity.0.to_string())
}

fn binding(a: &RiskAssessment) -> String {
    capacity_view(a).map_or("-".into(), |c| format!("{:?}", c.agent_binding))
}

fn score(a: &RiskAssessment) -> String {
    capacity_view(a)
        .and_then(|c| c.score)
        .map_or("-".into(), |s| s.to_string())
}

fn print_canonical<T: serde::Serialize>(value: &T) -> Result<(), String> {
    let json = serde_json::to_value(value).map_err(|e| format!("serialize: {e}"))?;
    let bytes = risk_core::canonical::encode(&json).map_err(|e| format!("encode: {e:?}"))?;
    println!(
        "{}",
        String::from_utf8(bytes).map_err(|e| format!("utf8: {e}"))?
    );
    Ok(())
}
