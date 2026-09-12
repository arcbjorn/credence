//! Canonical encoding — the Rust half of the cross-language contract.
//!
//! `docs/04`: UTF-8, deterministic key ordering, fixed decimal strings, no
//! non-finite values, deterministic ordering of semantically unordered
//! collections. Must produce byte-identical output to the Go implementation
//! in `gateway/canonical`; `schemas/v1/canonical-vectors.json` proves it.

use serde_json::Value;
use sha2::{Digest, Sha256};

#[derive(Debug, PartialEq, Eq)]
pub enum Error {
    /// Floats carry no authorization amount and have no canonical decimal
    /// form both languages agree on. Amounts are decimal strings.
    Float,
    NonFinite,
}

/// Encode a JSON value canonically.
pub fn encode(value: &Value) -> Result<Vec<u8>, Error> {
    let mut out = Vec::new();
    write_value(&mut out, value)?;
    Ok(out)
}

/// SHA-256 of the canonical encoding, lowercase hex.
pub fn hash_hex(value: &Value) -> Result<String, Error> {
    let bytes = encode(value)?;
    let digest = Sha256::digest(&bytes);
    Ok(digest.iter().map(|b| format!("{b:02x}")).collect())
}

fn write_value(out: &mut Vec<u8>, value: &Value) -> Result<(), Error> {
    match value {
        Value::Null => out.extend_from_slice(b"null"),
        Value::Bool(true) => out.extend_from_slice(b"true"),
        Value::Bool(false) => out.extend_from_slice(b"false"),
        Value::String(s) => write_string(out, s),
        Value::Number(n) => {
            // With arbitrary_precision the literal text is preserved, so an
            // integer beyond f64 range round-trips exactly. A value that
            // parses as a float is rejected rather than rounded.
            let text = n.to_string();
            if text.contains('.') || text.contains('e') || text.contains('E') {
                return Err(Error::Float);
            }
            out.extend_from_slice(text.as_bytes());
        }
        Value::Array(items) => {
            out.push(b'[');
            for (i, item) in items.iter().enumerate() {
                if i > 0 {
                    out.push(b',');
                }
                write_value(out, item)?;
            }
            out.push(b']');
        }
        Value::Object(map) => {
            // Byte-wise key sort, matching Go's sort.Strings on UTF-8.
            let mut keys: Vec<&String> = map.keys().collect();
            keys.sort_by(|a, b| a.as_bytes().cmp(b.as_bytes()));

            out.push(b'{');
            for (i, key) in keys.iter().enumerate() {
                if i > 0 {
                    out.push(b',');
                }
                write_string(out, key);
                out.push(b':');
                write_value(out, &map[*key])?;
            }
            out.push(b'}');
        }
    }
    Ok(())
}

fn write_string(out: &mut Vec<u8>, s: &str) {
    out.push(b'"');
    for ch in s.chars() {
        match ch {
            '"' => out.extend_from_slice(b"\\\""),
            '\\' => out.extend_from_slice(b"\\\\"),
            '\n' => out.extend_from_slice(b"\\n"),
            '\r' => out.extend_from_slice(b"\\r"),
            '\t' => out.extend_from_slice(b"\\t"),
            '\u{08}' => out.extend_from_slice(b"\\b"),
            '\u{0c}' => out.extend_from_slice(b"\\f"),
            c if (c as u32) < 0x20 => {
                out.extend_from_slice(format!("\\u{:04x}", c as u32).as_bytes());
            }
            c => {
                let mut buf = [0u8; 4];
                out.extend_from_slice(c.encode_utf8(&mut buf).as_bytes());
            }
        }
    }
    out.push(b'"');
}
