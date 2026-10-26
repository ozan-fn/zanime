// Uji stream virtual (tanpa browser): HEAD + Range GET, seek, 416, memori/disk server.
// Butuh server jalan: ./zanime  →  cargo test --release -- --ignored
use std::fs;
use std::io::{Read, Write};
use std::net::TcpStream;
use std::os::unix::fs::MetadataExt;
use std::time::{Duration, Instant};

const HOST: &str = "127.0.0.1:8080";
const CHUNK: usize = 4 * 1024 * 1024;

struct Response {
    status: String,
    headers: Vec<(String, String)>,
    first_byte: Duration,
}

impl Response {
    fn header(&self, name: &str) -> &str {
        self.headers
            .iter()
            .find(|(k, _)| k.eq_ignore_ascii_case(name))
            .map(|(_, v)| v.as_str())
            .unwrap_or("-")
    }
}

fn http_request(method: &str, path: &str, range: Option<&str>, read_bytes: usize) -> Response {
    let mut stream =
        TcpStream::connect(HOST).expect("server zanime tidak menerima koneksi di 8080");
    stream
        .set_read_timeout(Some(Duration::from_secs(120)))
        .unwrap();

    let mut req = format!("{method} {path} HTTP/1.1\r\nHost: {HOST}\r\nConnection: close\r\n");
    if let Some(r) = range {
        req.push_str(&format!("Range: {r}\r\n"));
    }
    req.push_str("\r\n");
    stream.write_all(req.as_bytes()).unwrap();

    let start = Instant::now();
    let mut raw = Vec::new();
    let mut buf = [0u8; 64 * 1024];
    let head_len = loop {
        let n = stream.read(&mut buf).expect("gagal baca header");
        assert!(n > 0, "koneksi ditutup sebelum header lengkap");
        raw.extend_from_slice(&buf[..n]);
        if let Some(p) = raw.windows(4).position(|w| w == b"\r\n\r\n") {
            break p + 4;
        }
    };

    let head = String::from_utf8_lossy(&raw[..head_len]);
    let mut lines = head.lines();
    let status = lines.next().unwrap_or_default().to_string();
    let headers = lines
        .filter_map(|l| l.split_once(':'))
        .map(|(k, v)| (k.trim().to_string(), v.trim().to_string()))
        .collect();

    let mut body = raw.len() - head_len;
    let first_byte = if body > 0 {
        start.elapsed()
    } else {
        Duration::ZERO
    };
    while body < read_bytes {
        match stream.read(&mut buf) {
            Ok(0) => break,
            Ok(n) => body += n,
            Err(e) => panic!("gagal baca body ({body}/{read_bytes} byte): {e}"),
        }
    }

    Response {
        status,
        headers,
        first_byte,
    }
}

fn dir_blocks(path: &str) -> u64 {
    let mut total = 0;
    let mut stack = vec![std::path::PathBuf::from(path)];
    while let Some(dir) = stack.pop() {
        let Ok(entries) = fs::read_dir(&dir) else {
            continue;
        };
        for entry in entries.flatten() {
            let Ok(meta) = entry.metadata() else { continue };
            if meta.is_dir() {
                stack.push(entry.path());
            } else {
                total += meta.blocks() * 512;
            }
        }
    }
    total
}

fn server_pid() -> Option<u32> {
    for entry in fs::read_dir("/proc").ok()?.flatten() {
        let Ok(pid) = entry.file_name().to_string_lossy().parse::<u32>() else {
            continue;
        };
        // comm = nama binary "zanime" (bukan proses lain yang cuma menyebut path).
        let Ok(comm) = fs::read_to_string(format!("/proc/{pid}/comm")) else {
            continue;
        };
        if comm.trim().trim_start_matches('.') != "zanime" {
            continue;
        }
        return Some(pid);
    }
    None
}

fn server_rss() -> Option<u64> {
    let status = fs::read_to_string(format!("/proc/{}/status", server_pid()?)).ok()?;
    let kb: u64 = status
        .lines()
        .find(|l| l.starts_with("VmRSS:"))?
        .split_whitespace()
        .nth(1)?
        .parse()
        .ok()?;
    Some(kb * 1024)
}

fn mib(bytes: u64) -> f64 {
    bytes as f64 / 1_048_576.0
}

#[test]
#[ignore = "butuh server zanime jalan di 127.0.0.1:8080"]
fn serves_mkv_ranges_without_waiting_for_full_download() {
    let head = http_request("HEAD", "/stream", None, 0);
    assert!(head.status.contains("200"), "HEAD /stream: {}", head.status);
    assert_eq!(head.header("accept-ranges"), "bytes");
    let file_len: u64 = head
        .header("content-length")
        .parse()
        .expect("content-length tidak valid");
    assert!(file_len > 0);

    let rss_start = server_rss().unwrap_or(0);
    let disk_start = dir_blocks("downloads");
    eprintln!(
        "file={:.1}MiB rss={:.1}MiB disk={:.1}MiB",
        mib(file_len),
        mib(rss_start),
        mib(disk_start)
    );

    let html = http_request("GET", "/", None, 512);
    assert!(html.status.contains("200"), "GET /: {}", html.status);

    let positions = [
        ("awal", 0u64),
        ("tengah", file_len / 2),
        ("akhir", file_len - CHUNK as u64),
        ("seek-mundur", 1_048_576),
    ];
    for (name, start) in positions {
        let end = (start + CHUNK as u64 - 1).min(file_len - 1);
        let r = http_request(
            "GET",
            "/stream",
            Some(&format!("bytes={start}-{end}")),
            CHUNK,
        );
        assert_eq!(r.status, "HTTP/1.1 206 Partial Content", "range {name}");
        assert!(
            r.header("content-range")
                .starts_with(&format!("bytes {start}-{end}/{file_len}")),
            "content-range {name}: {}",
            r.header("content-range")
        );
        assert!(
            r.first_byte < Duration::from_secs(5),
            "byte pertama {name} lambat: {:?}",
            r.first_byte
        );
        eprintln!(
            "{name:12} byte1={:>5}ms range={}",
            r.first_byte.as_millis(),
            r.header("content-range")
        );
    }

    // baca 200MB berurutan: melewati batas RAM, jadi eviction harus jalan
    // tanpa menyentuh disk dan tanpa menyeret seluruh file ke memori.
    let panjang = 200 * 1024 * 1024;
    let r = http_request(
        "GET",
        "/stream",
        Some(&format!("bytes=0-{}", panjang - 1)),
        panjang,
    );
    assert_eq!(r.status, "HTTP/1.1 206 Partial Content", "range panjang");
    assert!(
        r.first_byte < Duration::from_secs(5),
        "byte pertama range panjang lambat: {:?}",
        r.first_byte
    );
    let rss_panjang = server_rss().unwrap_or(0);
    eprintln!("panjang      rss={:.1}MiB", mib(rss_panjang));
    // 150MiB cache + ~22MiB binary/peer/allocator = ~172MiB; kalau eviction
    // tidak jalan, RSS akan menembus 700MB.
    assert!(
        rss_panjang < 190 * 1024 * 1024,
        "RSS setelah baca 200MB = {:.1}MiB, eviction tidak jalan",
        mib(rss_panjang)
    );
    assert_eq!(
        dir_blocks("downloads"),
        0,
        "mode RAM tidak boleh pakai disk"
    );
    assert!(
        !std::path::Path::new("downloads").exists(),
        "folder downloads tidak boleh dibuat di mode RAM"
    );

    let unsatisfiable = http_request("GET", "/stream", Some(&format!("bytes={file_len}-")), 0);
    assert!(
        unsatisfiable.status.contains("416"),
        "range di luar file harus 416, dapat: {}",
        unsatisfiable.status
    );

    eprintln!(
        "selesai: rss={:.1}MiB disk={:.1}MiB ({:.1}% dari file)",
        mib(server_rss().unwrap_or(0)),
        mib(dir_blocks("downloads")),
        dir_blocks("downloads") as f64 / file_len as f64 * 100.0
    );
}
