//! Minimal HTTP/1.1 server.
//!
//! Requests are bounded before they are buffered: `Content-Length` is
//! checked against the limit, and the body read is capped regardless of
//! what the header claimed. A header that lies must not be able to allocate
//! more than the limit.

use std::io::{BufRead, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};

use crate::handler::{handle, Request, Response, MAX_BODY_BYTES};

/// Cap on the request line and headers, so a client cannot stream headers
/// forever and exhaust memory before the body limit applies.
const MAX_HEADER_BYTES: usize = 16 * 1024;

pub fn serve(addr: &str) -> std::io::Result<()> {
    let listener = TcpListener::bind(addr)?;
    eprintln!("risk-service listening on {addr}");
    for stream in listener.incoming() {
        match stream {
            // One connection at a time is sufficient: the engine is a pure
            // function with no shared state, and the demo is single-client.
            // Concurrency would add failure modes without adding capability.
            Ok(s) => {
                if let Err(e) = handle_connection(s) {
                    eprintln!("connection error: {e}");
                }
            }
            Err(e) => eprintln!("accept error: {e}"),
        }
    }
    Ok(())
}

fn handle_connection(mut stream: TcpStream) -> std::io::Result<()> {
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
