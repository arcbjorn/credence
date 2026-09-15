//! Minimal HTTP/1.1 server.
//!
//! Requests are bounded before they are buffered: `Content-Length` is
//! checked against the limit, and the body read is capped regardless of
//! what the header claimed. A header that lies must not be able to allocate
//! more than the limit.

use std::io::{BufRead, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::time::Duration;

use crate::handler::{handle, Request, Response, MAX_BODY_BYTES};

/// Cap on the request line and headers, so a client cannot stream headers
/// forever and exhaust memory before the body limit applies.
const MAX_HEADER_BYTES: usize = 16 * 1024;

/// How long a connection may take to send its request, and to accept its
/// response.
///
/// Without these, a peer that opens a socket and sends nothing holds the
/// worker forever. Generous, because the engine answers in milliseconds
/// and a slow LAN is not an attack.
const IO_TIMEOUT: Duration = Duration::from_secs(15);

/// Connections served at once.
///
/// Bounded rather than unbounded: the engine is pure and CPU-bound, so
/// more threads than this buy nothing and a connection flood must not be
/// able to spawn without limit. A connection beyond the bound is answered
/// 503 and closed, which is a true statement about capacity rather than a
/// silent queue.
const MAX_CONNECTIONS: usize = 32;

pub fn serve(addr: &str) -> std::io::Result<()> {
    let listener = TcpListener::bind(addr)?;
    eprintln!("risk-service listening on {addr}");

    // Bounded concurrency, and every connection on a deadline.
    //
    // This loop used to serve ONE connection at a time with no timeouts,
    // on the reasoning that the engine is pure and the demo is
    // single-client. Both halves were wrong by 15 September: a peer that
    // connects and sends nothing blocked every other request until it
    // disconnected -- verified by opening one idle socket and watching
    // `/health` hang -- and the service now answers `credence-assessd`,
    // which N CRE nodes query independently.
    //
    // Reported by review, 15 September.
    let live = Arc::new(AtomicUsize::new(0));

    for stream in listener.incoming() {
        match stream {
            Ok(s) => {
                let in_flight = live.fetch_add(1, Ordering::SeqCst) + 1;
                if in_flight > MAX_CONNECTIONS {
                    live.fetch_sub(1, Ordering::SeqCst);
                    // Say so rather than queueing silently. A caller that
                    // cannot be served must not mistake a stalled socket
                    // for a slow assessment.
                    let _ = refuse_overloaded(s);
                    continue;
                }
                let counter = Arc::clone(&live);
                let spawned =
                    std::thread::Builder::new()
                        .name("risk-conn".into())
                        .spawn(move || {
                            if let Err(e) = handle_connection(s) {
                                eprintln!("connection error: {e}");
                            }
                            counter.fetch_sub(1, Ordering::SeqCst);
                        });
                if let Err(e) = spawned {
                    // A thread that cannot start is not a request that can
                    // be served; the count must not leak either way.
                    live.fetch_sub(1, Ordering::SeqCst);
                    eprintln!("spawn error: {e}");
                }
            }
            Err(e) => eprintln!("accept error: {e}"),
        }
    }
    Ok(())
}

/// Answer 503 and close, for a connection beyond the concurrency bound.
fn refuse_overloaded(mut stream: TcpStream) -> std::io::Result<()> {
    let _ = stream.set_write_timeout(Some(IO_TIMEOUT));
    write_response(
        &mut stream,
        &Response {
            status: 503,
            body: r#"{"detail":"too many connections","error":"overloaded"}"#.into(),
        },
    )
}

fn handle_connection(mut stream: TcpStream) -> std::io::Result<()> {
    // A peer that stalls mid-request releases the worker on its own.
    stream.set_read_timeout(Some(IO_TIMEOUT))?;
    stream.set_write_timeout(Some(IO_TIMEOUT))?;

    let response = match read_request(&mut stream) {
        Ok(req) => handle(&req),
        Err(RequestError::TooLarge) => Response {
            status: 413,
            body: r#"{"detail":"request too large","error":"body_too_large"}"#.into(),
        },
        Err(RequestError::Malformed) => Response {
            status: 400,
            body: r#"{"detail":"malformed request","error":"bad_request"}"#.into(),
        },
        Err(RequestError::Io(e)) => return Err(e),
    };
    write_response(&mut stream, &response)
}

enum RequestError {
    TooLarge,
    Malformed,
    Io(std::io::Error),
}

impl From<std::io::Error> for RequestError {
    fn from(e: std::io::Error) -> Self {
        RequestError::Io(e)
    }
}

fn read_request(stream: &mut TcpStream) -> Result<Request, RequestError> {
    let mut reader = BufReader::new(stream.try_clone()?);

    let mut request_line = String::new();
    reader.read_line(&mut request_line)?;
    let mut parts = request_line.split_whitespace();
    let method = parts.next().ok_or(RequestError::Malformed)?.to_string();
    let path = parts.next().ok_or(RequestError::Malformed)?.to_string();

    let mut content_length = 0usize;
    let mut header_bytes = request_line.len();
    loop {
        let mut line = String::new();
        let n = reader.read_line(&mut line)?;
        if n == 0 || line == "\r\n" || line == "\n" {
            break;
        }
        header_bytes += n;
        if header_bytes > MAX_HEADER_BYTES {
            return Err(RequestError::TooLarge);
        }
        if let Some(value) = line
            .strip_prefix("Content-Length:")
            .or_else(|| line.strip_prefix("content-length:"))
        {
            content_length = value.trim().parse().map_err(|_| RequestError::Malformed)?;
        }
    }

    // Reject on the declared length first, so an oversized request is
    // refused without reading it at all.
    if content_length > MAX_BODY_BYTES {
        return Err(RequestError::TooLarge);
    }

    // Then cap the actual read. A header that understates the body cannot
    // trick us into buffering more than the limit.
    let mut body = vec![0u8; content_length];
    if content_length > 0 {
        reader
            .by_ref()
            .take(content_length as u64)
            .read_exact(&mut body)?;
    }

    Ok(Request { method, path, body })
}

fn write_response(stream: &mut TcpStream, res: &Response) -> std::io::Result<()> {
    let reason = match res.status {
        200 => "OK",
        400 => "Bad Request",
        404 => "Not Found",
        405 => "Method Not Allowed",
        409 => "Conflict",
        413 => "Payload Too Large",
        _ => "Internal Server Error",
    };
    write!(
        stream,
        "HTTP/1.1 {} {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        res.status,
        reason,
        res.body.len(),
        res.body
    )?;
    stream.flush()
}
