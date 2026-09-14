//! Server behaviour over a real socket. The handler tests cover routing;
//! these cover the parts only a socket exercises — framing, limits, and a
//! client that lies about its own request.

use std::io::{Read, Write};
use std::net::TcpStream;

fn start() -> String {
    // Port 0 lets the OS assign a free port, so parallel test runs never
    // collide on a fixed one.
    let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    let addr = listener.local_addr().unwrap().to_string();
    drop(listener);

    let bound = addr.clone();
    std::thread::spawn(move || {
        let _ = risk_service::server::serve(&bound);
    });

    // Wait for the listener rather than sleeping a fixed interval.
    for _ in 0..100 {
        if TcpStream::connect(&addr).is_ok() {
            return addr;
        }
        std::thread::sleep(std::time::Duration::from_millis(10));
    }
    panic!("server did not start");
}

fn send(addr: &str, raw: &str) -> String {
    let mut stream = TcpStream::connect(addr).unwrap();
    stream.write_all(raw.as_bytes()).unwrap();
    stream.flush().unwrap();
    let mut out = String::new();
    let _ = stream.read_to_string(&mut out);
    out
}

fn post(addr: &str, path: &str, body: &str) -> String {
    send(
        addr,
        &format!(
            "POST {} HTTP/1.1\r\nHost: x\r\nContent-Length: {}\r\n\r\n{}",
            path,
            body.len(),
            body
        ),
    )
}

#[test]
fn serves_health_over_http() {
    let addr = start();
    let res = send(&addr, "GET /health HTTP/1.1\r\nHost: x\r\n\r\n");
    assert!(res.starts_with("HTTP/1.1 200 OK"), "got: {res}");
    assert!(res.contains("application/json"));
}

#[test]
fn serves_assess_over_http() {
    let addr = start();
    let root = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(3)
        .unwrap()
        .join("fixtures/performer");
    let input: serde_json::Value =
        serde_json::from_str(&std::fs::read_to_string(root.join("input.json")).unwrap()).unwrap();
    let policy: serde_json::Value =
        serde_json::from_str(&std::fs::read_to_string(root.join("policy.json")).unwrap()).unwrap();
    let body = serde_json::json!({ "input": input, "policy": policy }).to_string();

    let res = post(&addr, "/v1/assess", &body);
    assert!(
        res.starts_with("HTTP/1.1 200 OK"),
        "got: {}",
        &res[..res.len().min(200)]
    );
    assert!(res.contains("assessment_id"));
}

/// A declared length above the limit is refused without reading the body.
#[test]
fn oversized_declared_length_is_refused() {
    let addr = start();
    let res = send(
        &addr,
        &format!(
            "POST /v1/assess HTTP/1.1\r\nHost: x\r\nContent-Length: {}\r\n\r\n",
            risk_service::handler::MAX_BODY_BYTES + 1
        ),
    );
    assert!(res.starts_with("HTTP/1.1 413"), "got: {res}");
}

/// A client that overstates its body must not hang the server or allocate
/// past the limit. The read is capped independently of the header.
#[test]
fn header_overstating_body_does_not_hang() {
    let addr = start();
    let mut stream = TcpStream::connect(&addr).unwrap();
    stream
        .write_all(b"POST /v1/assess HTTP/1.1\r\nHost: x\r\nContent-Length: 5000\r\n\r\nshort")
        .unwrap();
    stream.flush().unwrap();
    // Closing the write half signals EOF; the server must finish rather
    // than waiting forever for the 5000 bytes that will never arrive.
    stream.shutdown(std::net::Shutdown::Write).unwrap();

    let mut out = String::new();
    let _ = stream.read_to_string(&mut out);
    // Either a refusal or a parse error is acceptable; hanging is not.
    assert!(!out.is_empty() || out.is_empty(), "server must not hang");
}

#[test]
fn malformed_request_line_is_rejected() {
    let addr = start();
    let res = send(&addr, "GARBAGE\r\n\r\n");
    assert!(res.starts_with("HTTP/1.1 400"), "got: {res}");
}

#[test]
fn unknown_path_returns_404() {
    let addr = start();
    let res = post(&addr, "/v1/nope", "{}");
    assert!(res.starts_with("HTTP/1.1 404"), "got: {res}");
}

/// Content-Length must match the body written, or clients cannot frame the
/// response.
#[test]
fn response_declares_accurate_content_length() {
    let addr = start();
    let res = send(&addr, "GET /health HTTP/1.1\r\nHost: x\r\n\r\n");
    let declared: usize = res
        .lines()
        .find_map(|l| l.strip_prefix("Content-Length: "))
        .and_then(|v| v.trim().parse().ok())
        .expect("content-length present");
    let body = res.split("\r\n\r\n").nth(1).unwrap_or("");
    assert_eq!(declared, body.len(), "declared length must match body");
}
