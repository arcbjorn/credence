//! Protobuf wire encoding for the risk boundary, in ~200 lines of
//! dependency-free Rust.
//!
//! **Why not `tonic`/`prost`.** `docs/10` gates the transport change on
//! passing the acceptance matrix *without changing frozen artifacts*, and
//! `docs/01` calls the engine the part "whose output is hashed and
//! signed". `risk-service` today pulls **31 crates** and opens its socket
//! with `std::net`; `tonic` would bring tokio, hyper and roughly a
//! hundred more into that component. Adding them to pass a transport
//! gate would change the thing the gate exists to protect.
//!
//! The wire format here is real protobuf, matching
//! `schemas/proto/risk.proto`. What it is NOT is gRPC: there is no HTTP/2
//! framing, no streaming, no interceptors. `docs/01` says the gain is a
//! wire format, and this delivers exactly that and claims nothing more.
//!
//! **Every payload is `bytes`.** The engine hashes the snapshot and the
//! policy as the gateway handed them over, so decomposing them into
//! protobuf fields would re-serialize on the way out and change the bytes
//! being hashed -- a retained assessment would stop reproducing. The
//! canonical encoding stays JSON and stays frozen; this carries it.

/// Field numbers, from `schemas/proto/risk.proto`. Wrong numbers produce
/// a message that decodes to empty rather than an error, which is why
/// they are named here rather than written inline.
pub mod field {
    pub const ASSESS_INPUT: u64 = 1;
    pub const ASSESS_POLICY: u64 = 2;
    pub const ASSESS_ASSESSMENT: u64 = 1;

    pub const REPLAY_ASSESSMENT: u64 = 1;
    pub const REPLAY_INPUT: u64 = 2;
    pub const REPLAY_POLICY: u64 = 3;

    pub const REPLAY_MATCHED: u64 = 1;
    pub const REPLAY_ID: u64 = 2;
    pub const REPLAY_HASH: u64 = 3;
    pub const REPLAY_DIFFERENCES: u64 = 4;

    pub const HEALTH_MODEL_HASH: u64 = 1;
}

/// Wire types. Only the two this schema uses are represented: a schema
/// that grows a float or a fixed-width integer must add its own, rather
/// than silently mis-decoding as one of these.
const WIRE_VARINT: u64 = 0;
const WIRE_LEN: u64 = 2;

/// Append a base-128 varint.
pub fn put_varint(out: &mut Vec<u8>, mut value: u64) {
    loop {
        let byte = (value & 0x7f) as u8;
        value >>= 7;
        if value == 0 {
            out.push(byte);
            return;
        }
        out.push(byte | 0x80);
    }
}

/// Append a length-delimited field.
pub fn put_bytes(out: &mut Vec<u8>, field: u64, value: &[u8]) {
    put_varint(out, (field << 3) | WIRE_LEN);
    put_varint(out, value.len() as u64);
    out.extend_from_slice(value);
}

/// Append a bool. Proto3 omits a false: an absent field and an explicit
/// `false` decode identically, which is the encoding the spec requires.
pub fn put_bool(out: &mut Vec<u8>, field: u64, value: bool) {
    if !value {
        return;
    }
    put_varint(out, (field << 3) | WIRE_VARINT);
    put_varint(out, 1);
}

/// One decoded field: its number and its payload.
pub enum Value<'a> {
    Varint(u64),
    Bytes(&'a [u8]),
}

/// Read a varint, returning the value and the bytes consumed.
fn take_varint(buf: &[u8]) -> Option<(u64, usize)> {
    let mut value: u64 = 0;
    let mut shift = 0;
    for (i, byte) in buf.iter().enumerate() {
        // Ten groups of seven bits exceed u64. Refusing here stops a
        // crafted message from shifting into undefined behaviour.
        if shift >= 64 {
            return None;
        }
        value |= u64::from(byte & 0x7f) << shift;
        if byte & 0x80 == 0 {
            return Some((value, i + 1));
        }
        shift += 7;
    }
    None
}

/// Walk every field in a message.
///
/// Unknown field numbers are SKIPPED, not rejected: proto3 requires a
/// reader to tolerate fields a newer writer added, and a decoder that
/// errored would make the two ends impossible to deploy independently.
/// Unknown WIRE types are a different matter and do error, because the
/// length cannot be determined and the rest of the message would be
/// misread as garbage.
pub fn each_field<F>(mut buf: &[u8], mut visit: F) -> Result<(), String>
where
    F: FnMut(u64, Value<'_>),
{
    while !buf.is_empty() {
        let (key, used) = take_varint(buf).ok_or("truncated field key")?;
        buf = &buf[used..];
        let field = key >> 3;
        match key & 0x7 {
            WIRE_VARINT => {
                let (value, used) = take_varint(buf).ok_or("truncated varint")?;
                buf = &buf[used..];
                visit(field, Value::Varint(value));
            }
            WIRE_LEN => {
                let (len, used) = take_varint(buf).ok_or("truncated length")?;
                buf = &buf[used..];
                let len = usize::try_from(len).map_err(|_| "length overflows usize")?;
                if buf.len() < len {
                    return Err("length exceeds message".into());
                }
                visit(field, Value::Bytes(&buf[..len]));
                buf = &buf[len..];
            }
            other => return Err(format!("unsupported wire type {other}")),
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn varints_round_trip() {
        for value in [0u64, 1, 127, 128, 300, u64::MAX] {
            let mut buf = Vec::new();
            put_varint(&mut buf, value);
            let (decoded, used) = take_varint(&buf).expect("decodes");
            assert_eq!(decoded, value);
            assert_eq!(used, buf.len());
        }
    }

    /// The payloads are the bytes the engine hashes, so the codec must
    /// return them unchanged -- not merely equal-looking JSON.
    #[test]
    fn bytes_survive_exactly() {
        let payload = b"{\"a\":1,\"b\":\"\\u00e9\"}";
        let mut buf = Vec::new();
        put_bytes(&mut buf, field::ASSESS_INPUT, payload);

        let mut seen: Option<Vec<u8>> = None;
        each_field(&buf, |f, v| {
            if f == field::ASSESS_INPUT {
                if let Value::Bytes(b) = v {
                    seen = Some(b.to_vec());
                }
            }
        })
        .expect("decodes");
        assert_eq!(seen.as_deref(), Some(&payload[..]));
    }

    /// Proto3 omits a false, and an absent bool reads as false.
    #[test]
    fn false_is_omitted() {
        let mut buf = Vec::new();
        put_bool(&mut buf, field::REPLAY_MATCHED, false);
        assert!(buf.is_empty());

        let mut buf = Vec::new();
        put_bool(&mut buf, field::REPLAY_MATCHED, true);
        assert!(!buf.is_empty());
    }

    /// A newer writer's extra field must not break an older reader.
    #[test]
    fn unknown_fields_are_skipped() {
        let mut buf = Vec::new();
        put_bytes(&mut buf, field::ASSESS_INPUT, b"kept");
        put_bytes(&mut buf, 99, b"from a newer writer");

        let mut kept = None;
        each_field(&buf, |f, v| {
            if f == field::ASSESS_INPUT {
                if let Value::Bytes(b) = v {
                    kept = Some(b.to_vec());
                }
            }
        })
        .expect("tolerates unknown fields");
        assert_eq!(kept.as_deref(), Some(&b"kept"[..]));
    }

    /// A truncated message must error rather than return a short read:
    /// silently accepting half a snapshot would hand the engine input the
    /// caller never sent.
    #[test]
    fn truncation_is_refused() {
        let mut buf = Vec::new();
        put_bytes(&mut buf, field::ASSESS_INPUT, b"a long enough payload");
        buf.truncate(buf.len() - 4);
        assert!(each_field(&buf, |_, _| {}).is_err());
    }

    /// An unknown wire type cannot be skipped -- its length is unknown --
    /// so it must stop the decode rather than desynchronise it.
    #[test]
    fn unknown_wire_type_is_refused() {
        // Field 1, wire type 3 (deprecated group start).
        let buf = vec![(1u8 << 3) | 3];
        assert!(each_field(&buf, |_, _| {}).is_err());
    }
}
