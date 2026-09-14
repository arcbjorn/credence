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
        "policy-hash" => cmd_policy_hash(&args[1..]),
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

/// Prints the canonical content hash of a policy file.
///
/// An executable result requires the snapshot's declared `policy_hash` to
/// equal this value, so a snapshot author needs a way to compute it
/// rather than inventing a label.
fn cmd_policy_hash(args: &[String]) -> Result<(), String> {
    let policy: Policy = load(&flag(args, "--policy")?)?;
    let value = serde_json::to_value(policy).map_err(|e| format!("serialize policy: {e}"))?;
    let hash = risk_core::canonical::hash_hex(&value).map_err(|e| format!("hash policy: {e:?}"))?;
    println!("{hash}");
    Ok(())
}

fn cmd_inspect(args: &[String]) -> Result<(), String> {
    let input: RiskInput = load(&flag(args, "--input")?)?;
    let identity = snapshot::identify(&input).map_err(|e| format!("identify: {e:?}"))?;

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

    // The comparison lives in `risk_core::replay`, shared with the browser
    // viewer. Two implementations of "did this reproduce?" is the drift a
    // replay exists to rule out.
    let result = risk_core::replay::replay(&original, &input, &policy)
        .map_err(|e| e.to_string())?;

    if result.is_match() {
        println!("MATCH");
        println!("  assessment_id {}", result.replayed.assessment_id);
        println!("  input_hash    {}", result.replayed.input_hash);
        Ok(())
    } else {
        println!("DIFFERENCE");
        for d in &result.differences {
            println!("  {d}");
        }
        Err(format!("{} field(s) differ", result.differences.len()))
    }
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
