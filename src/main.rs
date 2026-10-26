use anyhow::{Context, Result, bail};
use bytes::Bytes;
use futures_util::TryStreamExt;
use http_body_util::{BodyExt, Full, StreamBody, combinators::BoxBody};
use hyper::body::Frame;
use hyper::header::{ACCEPT_RANGES, CONTENT_LENGTH, CONTENT_RANGE, CONTENT_TYPE, RANGE};
use hyper::service::service_fn;
use hyper::{Method, Request, Response, StatusCode};
use hyper_util::rt::TokioIo;
mod memstore;

use librqbit::api::TorrentIdOrHash;
use librqbit::storage::StorageFactoryExt;
use librqbit::{
    AddTorrent, AddTorrentOptions, AddTorrentResponse, Magnet, ManagedTorrent, Session,
    SessionOptions,
};
use std::convert::Infallible;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::io::{AsyncReadExt, AsyncSeekExt};
use tokio::net::TcpListener;
use tokio::sync::{Mutex, RwLock, mpsc};
use tokio::task::JoinHandle;
use tokio::time::sleep;
use tokio_util::io::ReaderStream;

const MAGNET: &str = "magnet:?xt=urn:btih:27a16c1792b0f279e608218d2644f4b9405289ed\
&dn=%5BErai-raws%5D%20Sousou%20no%20Frieren%202nd%20Season%20-%2009%20%5B1080p%20CR%20WEBRip%20HEVC%20AAC%5D%5BMultiSub%5D%5BD9A6C94A%5D\
&tr=http%3A%2F%2Fnyaa.tracker.wf%3A7777%2Fannounce\
&tr=udp%3A%2F%2Fopen.stealth.si%3A80%2Fannounce\
&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce\
&tr=udp%3A%2F%2Fexodus.desync.com%3A6969%2Fannounce\
&tr=udp%3A%2F%2Ftracker.torrent.eu.org%3A451%2Fannounce";

const INDEX_HTML: &str = include_str!("../index.html");
const MIB: u64 = 1024 * 1024;

struct Config {
    port: u16,
    mem_limit_mb: u64,
}

/// .env dibaca tanpa dependency; env process menang atas isi file.
fn load_config() -> Config {
    let file = std::fs::read_to_string(".env").unwrap_or_default();
    let get = |key: &str, default: u64| {
        std::env::var(key)
            .ok()
            .or_else(|| {
                file.lines().find_map(|line| {
                    let (k, v) = line.trim().split_once('=')?;
                    (k.trim() == key).then(|| v.trim().trim_matches(['"', '\'']).to_string())
                })
            })
            .and_then(|v| v.parse().ok())
            .unwrap_or(default)
    };
    Config {
        port: get("PORT", 8080) as u16,
        mem_limit_mb: get("MAX_MEMORY_MB", 150),
    }
}

/// Satu torrent aktif. Pegangan keepalive + evictor dibatalkan saat torrent diganti.
struct Current {
    handle: Arc<ManagedTorrent>,
    video_id: usize,
    len: u64,
    name: String,
    keepalive: JoinHandle<()>,
    evictor: JoinHandle<()>,
}

struct App {
    session: Arc<Session>,
    mem_limit_mb: u64,
    disk_only: bool,
    // None selama belum ada torrent yang siap
    current: RwLock<Option<Arc<Current>>>,
    // serialisasi load magnet (satu per satu)
    switching: Mutex<()>,
}

type Body = BoxBody<Bytes, std::io::Error>;

fn index_html() -> String {
    INDEX_HTML.replace("{{MAGNET_HTML}}", &MAGNET.replace('&', "&amp;"))
}

#[tokio::main]
async fn main() -> Result<()> {
    let cfg = load_config();
    let download_dir = PathBuf::from("./downloads");

    println!("[INFO] session torrent...");
    let session = Session::new_with_opts(
        download_dir.clone(),
        SessionOptions {
            // jangan tulis cache/config DHT ke disk: mode RAM harus zero disk
            disable_dht_persistence: true,
            ..Default::default()
        },
    )
    .await
    .context("gagal membuat session torrent")?;

    // default: cache piece di RAM ≤ MAX_MEMORY_MB (tanpa disk). --disk: ./downloads.
    let disk_only = std::env::args().any(|a| a == "--disk");
    if disk_only {
        std::fs::create_dir_all(&download_dir)?;
    }
    println!(
        "[INFO] storage: {}",
        if disk_only {
            "disk sparse (./downloads)".to_string()
        } else {
            format!("RAM ≤{}MB cache (evict + refetch)", cfg.mem_limit_mb)
        }
    );

    let app = Arc::new(App {
        session,
        mem_limit_mb: cfg.mem_limit_mb,
        disk_only,
        current: RwLock::new(None),
        switching: Mutex::new(()),
    });

    println!("[INFO] menambahkan magnet awal...");
    let first = start_torrent(&app, MAGNET).await?;
    println!(
        "[ OK ] {} ({:.2} MB)",
        first.name,
        first.len as f64 / 1024.0 / 1024.0
    );
    *app.current.write().await = Some(first);

    println!("[ OK ] http://localhost:{}", cfg.port);

    let addr = format!("0.0.0.0:{}", cfg.port);
    let listener = TcpListener::bind(&addr)
        .await
        .with_context(|| format!("bind {addr} gagal"))?;
    loop {
        let (stream, _) = listener.accept().await?;
        let app = app.clone();
        tokio::spawn(async move {
            let io = TokioIo::new(stream);
            let _ = hyper::server::conn::http1::Builder::new()
                .serve_connection(io, service_fn(move |req| route(app.clone(), req)))
                .await;
        });
    }
}

/// Tambah torrent baru, tunggu metadata, siapkan keepalive + evictor. Belum mengganti torrent aktif.
async fn start_torrent(app: &App, magnet: &str) -> Result<Arc<Current>> {
    let (evict_tx, mut evict_rx) = mpsc::unbounded_channel::<u32>();
    let factory = memstore::MemStorageFactory {
        limit: app.mem_limit_mb * MIB,
        evicted: evict_tx,
    };
    let opts = || AddTorrentOptions {
        paused: false,
        overwrite: true,
        // mode RAM: jangan pilih file apa pun, jadi librqbit tidak mengunduh
        // seluruh file di latar. Piece hanya diambil saat stream memintanya.
        only_files: (!app.disk_only).then(Vec::new),
        storage_factory: (!app.disk_only).then(|| factory.clone().boxed()),
        ..Default::default()
    };

    let mut resp = app
        .session
        .add_torrent(AddTorrent::from_url(magnet), Some(opts()))
        .await
        .context("gagal menambahkan magnet")?;
    if let AddTorrentResponse::AlreadyManaged(_, h) = &resp {
        // torrent sisa (mis. magnet yang sama sudah pernah dimuat): buang dulu
        // supaya storage RAM beserta kanal eviction-nya dibuat ulang.
        let _ = app.session.delete(TorrentIdOrHash::Id(h.id()), false).await;
        resp = app
            .session
            .add_torrent(AddTorrent::from_url(magnet), Some(opts()))
            .await
            .context("gagal menambahkan magnet")?;
    }
    let handle = match resp {
        AddTorrentResponse::Added(_, h) | AddTorrentResponse::AlreadyManaged(_, h) => h,
        AddTorrentResponse::ListOnly(_) => bail!("mode list-only tidak didukung"),
    };

    let cleanup = |e: anyhow::Error| {
        let session = app.session.clone();
        let id = handle.id();
        async move {
            let _ = session.delete(TorrentIdOrHash::Id(id), false).await;
            e
        }
    };

    let start = Instant::now();
    while handle.metadata.load().is_none() {
        if start.elapsed() > Duration::from_secs(120) {
            return Err(cleanup(anyhow::anyhow!(
                "timeout menunggu metadata — tidak ada peer?"
            ))
            .await);
        }
        sleep(Duration::from_millis(500)).await;
    }

    let (video_id, len, name) = match pick_video(&handle) {
        Ok(v) => v,
        Err(e) => return Err(cleanup(e).await),
    };

    // Stream "penjaga" di EOF. Tanpa ini librqbit menganggap torrent sudah selesai
    // (tidak ada file terpilih) lalu memutus peer; queue di posisi EOF kosong,
    // jadi tidak ada byte yang diunduh, tapi koneksi peer tetap hidup.
    let mut keepalive_fs = loop {
        match handle.clone().stream(video_id) {
            Ok(s) => break s,
            Err(e) => {
                if start.elapsed() > Duration::from_secs(120) {
                    return Err(cleanup(e.context("torrent tidak kunjung siap")).await);
                }
                sleep(Duration::from_millis(200)).await;
            }
        }
    };
    if let Err(e) = keepalive_fs.seek(std::io::SeekFrom::Start(len)).await {
        return Err(cleanup(anyhow::Error::new(e).context("gagal seek keepalive")).await);
    }
    let keepalive = tokio::spawn(async move {
        let _fs = keepalive_fs;
        std::future::pending::<()>().await
    });

    // piece yang dibuang dari RAM ditandai "belum punya" supaya bisa diunduh ulang
    let evict_handle = handle.clone();
    let evictor = tokio::spawn(async move {
        while let Some(pid) = evict_rx.recv().await {
            let mut batch = vec![pid];
            while let Ok(p) = evict_rx.try_recv() {
                batch.push(p);
            }
            for pid in batch {
                while evict_handle.forget_piece(pid).is_err() {
                    sleep(Duration::from_millis(200)).await;
                }
            }
            // glibc menahan blok yang sudah di-free; kembalikan ke OS supaya
            // RSS tetap dekat dengan MAX_MEMORY_MB.
            trim_heap();
        }
    });

    Ok(Arc::new(Current {
        handle,
        video_id,
        len,
        name,
        keepalive,
        evictor,
    }))
}

fn pick_video(handle: &ManagedTorrent) -> Result<(usize, u64, String)> {
    let guard = handle.metadata.load();
    let meta = guard.as_ref().context("metadata kosong")?;
    let mut best: Option<(usize, u64, String)> = None;
    for (i, f) in meta.file_infos.iter().enumerate() {
        let name = f.relative_filename.to_string_lossy().to_string();
        let ext = name.rsplit('.').next().unwrap_or("").to_lowercase();
        if !matches!(ext.as_str(), "mkv" | "mp4" | "webm" | "avi" | "mov" | "ts") {
            continue;
        }
        if best.as_ref().is_none_or(|(_, l, _)| f.len > *l) {
            best = Some((i, f.len, name));
        }
    }
    best.context("tidak ada file video di torrent")
}

/// Matikan torrent lama setelah torrent baru siap (beri waktu stream lama selesai).
fn retire(app: Arc<App>, old: Arc<Current>) {
    tokio::spawn(async move {
        sleep(Duration::from_secs(2)).await;
        old.keepalive.abort();
        old.evictor.abort();
        let _ = app
            .session
            .delete(TorrentIdOrHash::Id(old.handle.id()), false)
            .await;
    });
}

/// Kembalikan memori bebas ke OS (glibc menahannya, bikin RSS > batas).
#[cfg(target_os = "linux")]
fn trim_heap() {
    unsafe extern "C" {
        fn malloc_trim(pad: usize) -> i32;
    }
    unsafe {
        malloc_trim(0);
    }
}

#[cfg(not(target_os = "linux"))]
fn trim_heap() {}

async fn route(
    app: Arc<App>,
    req: Request<hyper::body::Incoming>,
) -> Result<Response<Body>, Infallible> {
    let head = req.method() == Method::HEAD;
    let resp = match (req.method(), req.uri().path()) {
        (&Method::GET | &Method::HEAD, "/") | (&Method::GET | &Method::HEAD, "/index.html") => {
            Response::builder()
                .status(StatusCode::OK)
                .header(CONTENT_TYPE, "text/html; charset=utf-8")
                .body(full(index_html()))
                .unwrap()
        }
        (&Method::POST, "/load") => load_magnet(&app, req).await,
        (&Method::GET | &Method::HEAD, "/stream") => {
            let current = app.current.read().await.clone();
            match current {
                Some(current) => stream(&current, req.headers(), head).await,
                None => Response::builder()
                    .status(StatusCode::SERVICE_UNAVAILABLE)
                    .body(full("belum ada torrent"))
                    .unwrap(),
            }
        }
        _ => Response::builder()
            .status(StatusCode::NOT_FOUND)
            .body(full("not found"))
            .unwrap(),
    };
    Ok(resp)
}

/// POST /load — body teks berisi magnet. Mengganti torrent aktif kalau berhasil.
async fn load_magnet(app: &Arc<App>, req: Request<hyper::body::Incoming>) -> Response<Body> {
    let result: Result<serde_json::Value> = async {
        let body = req
            .into_body()
            .collect()
            .await
            .context("gagal baca body")?
            .to_bytes();
        if body.len() > 16 * 1024 {
            bail!("magnet terlalu panjang");
        }
        let magnet = std::str::from_utf8(&body)
            .context("magnet bukan UTF-8")?
            .trim()
            .to_string();
        let parsed = Magnet::parse(&magnet).context("magnet tidak valid")?;
        if parsed.as_id20().is_none() && parsed.as_id32().is_none() {
            bail!("magnet tanpa info-hash");
        }

        let _guard = app.switching.lock().await;
        let current = app.current.read().await.clone();
        let current_matches = match (&current, parsed.as_id20()) {
            (Some(cur), Some(id)) => cur.handle.info_hash() == id,
            _ => false,
        };
        let active = if current_matches {
            current.unwrap()
        } else {
            let new = start_torrent(app, &magnet).await?;
            let old = app.current.write().await.replace(new.clone());
            if let Some(old) = old {
                retire(app.clone(), old);
            }
            new
        };

        Ok(serde_json::json!({ "name": active.name, "len": active.len }))
    }
    .await;

    match result {
        Ok(json) => Response::builder()
            .status(StatusCode::OK)
            .header(CONTENT_TYPE, "application/json")
            .body(full(json.to_string()))
            .unwrap(),
        Err(e) => Response::builder()
            .status(StatusCode::BAD_REQUEST)
            .header(CONTENT_TYPE, "application/json")
            .body(full(
                serde_json::json!({ "error": format!("{e:#}") }).to_string(),
            ))
            .unwrap(),
    }
}

async fn stream(current: &Arc<Current>, headers: &hyper::HeaderMap, head: bool) -> Response<Body> {
    let len = current.len;

    let range = headers
        .get(RANGE)
        .and_then(|v| v.to_str().ok())
        .and_then(|v| parse_range(v, len));

    let (start, end) = match range {
        Some(Ok(r)) => r,
        Some(Err(())) => {
            return Response::builder()
                .status(StatusCode::RANGE_NOT_SATISFIABLE)
                .header(CONTENT_RANGE, format!("bytes */{len}"))
                .body(full(""))
                .unwrap();
        }
        None => (0, len.saturating_sub(1)),
    };
    let body_len = end.saturating_sub(start) + 1;

    let mut builder = Response::builder()
        .status(if range.is_some() {
            StatusCode::PARTIAL_CONTENT
        } else {
            StatusCode::OK
        })
        .header(CONTENT_TYPE, "video/x-matroska")
        .header(ACCEPT_RANGES, "bytes")
        .header(CONTENT_LENGTH, body_len);

    if let Some(Ok(_)) = range {
        builder = builder.header(CONTENT_RANGE, format!("bytes {start}-{end}/{len}"));
    }

    if head {
        return builder.body(full("")).unwrap();
    }

    let mut fs = match current.handle.clone().stream(current.video_id) {
        Ok(s) => s,
        Err(e) => {
            eprintln!("[ERR ] stream: {e:#}");
            return Response::builder()
                .status(StatusCode::INTERNAL_SERVER_ERROR)
                .body(full("stream error"))
                .unwrap();
        }
    };

    // stream per-range langsung dari piece torrent — tanpa buffer/cache penuh
    if let Err(e) = fs.seek(std::io::SeekFrom::Start(start)).await {
        eprintln!("[ERR ] seek: {e:#}");
        return Response::builder()
            .status(StatusCode::INTERNAL_SERVER_ERROR)
            .body(full("seek error"))
            .unwrap();
    }

    let limited = fs.take(body_len);
    let body: Body = BodyExt::boxed(StreamBody::new(
        ReaderStream::with_capacity(limited, 128 * 1024).map_ok(Frame::data),
    ));

    builder.body(body).unwrap()
}

fn parse_range(header: &str, len: u64) -> Option<Result<(u64, u64), ()>> {
    let spec = header.strip_prefix("bytes=")?;
    let (a, b) = spec.split_once('-')?;
    if len == 0 {
        return Some(Err(()));
    }
    if a.is_empty() {
        let n: u64 = match b.trim().parse() {
            Ok(n) => n,
            Err(_) => return Some(Err(())),
        };
        if n == 0 {
            return Some(Err(()));
        }
        return Some(Ok((len.saturating_sub(n), len - 1)));
    }
    let start: u64 = match a.trim().parse() {
        Ok(n) => n,
        Err(_) => return Some(Err(())),
    };
    let end = if b.trim().is_empty() {
        len - 1
    } else {
        match b.trim().parse::<u64>() {
            Ok(n) => n.min(len - 1),
            Err(_) => return Some(Err(())),
        }
    };
    if start >= len || end < start {
        return Some(Err(()));
    }
    Some(Ok((start, end)))
}

fn full(s: impl Into<Bytes>) -> Body {
    Full::new(s.into()).map_err(std::io::Error::other).boxed()
}
