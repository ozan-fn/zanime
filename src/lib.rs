//! zanime: streaming torrent, cache RAM-only, zero disk.
//!
//! Padanan Rust dari server Go: satu torrent aktif, satu job pemuatan magnet
//! yang bisa dibatalkan dan dipakai bersama, cache piece di RAM dengan
//! eviction + refetch, dan `/stream` yang mendukung HEAD + Range.

pub mod memstore;
pub mod remux;

use anyhow::{Context, Result, bail};
use axum::Json;
use axum::body::Body;
use axum::extract::ws::{Message, WebSocket, WebSocketUpgrade};
use axum::extract::{Path as AxumPath, State};
use axum::http::{HeaderMap, Method, StatusCode, header};
use axum::response::{Html, IntoResponse, Response};
use axum::routing::{get, post};
use axum::{Router, serve};
use bytes::{Bytes, BytesMut};
use librqbit::api::TorrentIdOrHash;
use librqbit::storage::StorageFactoryExt;
use librqbit::{
    AddTorrent, AddTorrentOptions, AddTorrentResponse, ManagedTorrent, Session, SessionOptions,
};
use librqbit_core::magnet::Magnet;
use memstore::{MemStorageFactory, Stats};
use serde::Deserialize;
use serde_json::{Value, json};
use std::io::SeekFrom;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, AtomicU16, AtomicU64, AtomicUsize, Ordering};
use std::time::{Duration, Instant};
use tokio::io::{AsyncReadExt, AsyncSeekExt};
use tokio::net::TcpListener;
use tokio::sync::{Mutex, RwLock, mpsc, watch};
use tokio::task::JoinHandle;
use tokio::time::sleep;
use tokio_util::io::ReaderStream;

use tokio::time::timeout;

const INDEX_HTML: &str = include_str!("../web/index.html");
const MIB: u64 = 1024 * 1024;
const METADATA_TIMEOUT: Duration = Duration::from_secs(45);
const WS_DELAY: Duration = Duration::from_millis(700);
/// Berapa banyak dari ekor file yang dibaca lebih dulu: index container (MKV
/// Cues, MP4 moov non-faststart) ada di sana, dan tanpa itu player hanya bisa
/// menampilkan durasi 00:00 sampai file terunduh hampir penuh.
const TAIL_WARM: u64 = 1 << 20;
/// Kepala file dipanaskan sebelum player membuka media; kalau byte 0 belum
/// ada, request pertama player menunggu unduhan dan MoviPlayer bisa
/// "Failed to open media: Timeout at 0".
const HEAD_WARM: u64 = 1 << 20;
/// Batas waktu task pemanas; tanpa ini task yang menggantung menahan
/// torrent lama beserta storage RAM-nya.
const WARM_TIMEOUT: Duration = Duration::from_secs(30);

const ERR_CANCELLED: &str = "pemuatan magnet dibatalkan";
const ERR_TIMEOUT: &str = "metadata timeout";

const DEFAULT_MAGNET: &str = "magnet:?xt=urn:btih:3c5356a2834c80305e18479ad39d16a4f8b8682b&dn=%5BErai-raws%5D%20Gabriel%20DropOut%20-%2001%20~%2012%20%5B1080p%5D%5BMultiple%20Subtitle%5D&tr=http%3A%2F%2Fnyaa.tracker.wf%3A7777%2Fannounce&tr=udp%3A%2F%2Fopen.stealth.si%3A80%2Fannounce&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce&tr=udp%3A%2F%2Fexodus.desync.com%3A6969%2Fannounce&tr=udp%3A%2F%2Ftracker.torrent.eu.org%3A451%2Fannounce";

// ---------------------------------------------------------------- config ---

pub struct Config {
    pub addr: String,
    pub cache_mb: u64,
    pub magnet: String,
}

impl Config {
    /// `.env` dibaca tanpa dependency; env proses dan argumen CLI menang.
    pub fn from_env_and_args() -> Result<Self> {
        let file = std::fs::read_to_string(".env").unwrap_or_default();
        let var = |key: &str| {
            std::env::var(key).ok().or_else(|| {
                file.lines().find_map(|line| {
                    let (k, v) = line.trim().split_once('=')?;
                    (k.trim() == key).then(|| v.trim().trim_matches(['"', '\'']).to_string())
                })
            })
        };

        let mut addr = format!("0.0.0.0:{}", var("PORT").unwrap_or_else(|| "8080".into()));
        let mut cache_mb = var("MAX_MEMORY_MB")
            .and_then(|v| v.parse().ok())
            .unwrap_or(200);
        let mut magnet = DEFAULT_MAGNET.to_string();

        let mut args = std::env::args().skip(1);
        while let Some(arg) = args.next() {
            match arg.as_str() {
                "--addr" => addr = args.next().context("--addr butuh nilai")?,
                "--cache-mb" => {
                    cache_mb = args
                        .next()
                        .context("--cache-mb butuh nilai")?
                        .parse()
                        .context("--cache-mb harus angka")?
                }
                "--magnet" => magnet = args.next().context("--magnet butuh nilai")?,
                other => bail!("argumen tidak dikenal: {other}"),
            }
        }
        if cache_mb == 0 {
            bail!("cache-mb harus positif");
        }
        Ok(Self {
            addr,
            cache_mb,
            magnet,
        })
    }
}

/// Alamat dengar diubah jadi sesuatu yang bisa diklik di terminal: wildcard
/// host menjadi localhost, karena 0.0.0.0 bukan tujuan yang bisa dibuka.
pub fn browser_url(addr: &str) -> String {
    let Some((host, port)) = addr.rsplit_once(':') else {
        return addr.to_string();
    };
    let host = host.trim_matches(['[', ']']);
    let host = if host.is_empty() || host == "0.0.0.0" || host == "::" {
        "localhost"
    } else {
        host
    };
    if host.contains(':') {
        format!("http://[{host}]:{port}")
    } else {
        format!("http://{host}:{port}")
    }
}

// ------------------------------------------------------------------ app ---

pub struct App {
    session: Arc<Session>,
    cache_mb: u64,
    stats: Arc<Stats>,
    /// Budget piece yang tersisa (mulai dari `cache_mb`), dibagi dengan
    /// buffer remux supaya total RAM tetap dalam batas env.
    mem_limit: Arc<AtomicU64>,
    /// Port HTTP yang sudah di-bind; 0 = belum (remux butuh URL sendiri).
    http_port: AtomicU16,
    current: RwLock<Option<Arc<Current>>>,
    job: Mutex<Option<Arc<LoadJob>>>,
    switching: Mutex<()>,
}

/// Job remux ffmpeg untuk satu file: hasil disimpan di RAM, terbatas budget.
/// `stop` ditandai saat ganti episode; task pembacanya yang memulihkan budget.
struct RemuxJob {
    index: usize,
    stop: Arc<AtomicBool>,
    buf: Arc<RwLock<Option<Bytes>>>,
}

/// Torrent aktif. Pegangan keepalive + evictor dibatalkan saat torrent diganti.
struct Current {
    handle: Arc<ManagedTorrent>,
    name: String,
    hash: String,
    /// File video saja, urut nama (huruf kecil), sesuai indeks yang dipakai UI.
    videos: Vec<FileEntry>,
    selected: AtomicUsize,
    keepalive: JoinHandle<()>,
    evictor: JoinHandle<()>,
    remux: Mutex<Option<RemuxJob>>,
}

#[derive(Clone)]
pub struct FileEntry {
    /// Indeks file di torrent (urutan `file_infos`), dipakai untuk `fileIndex`.
    pub id: usize,
    pub path: String,
    pub len: u64,
}

impl FileEntry {
    pub fn name(&self) -> &str {
        self.path.rsplit(['/', '\\']).next().unwrap_or(&self.path)
    }
}

impl Current {
    /// Baca kepala file sampai `HEAD_WARM` byte terunduh penuh supaya open
    /// player tidak menunggu. Berjalan di latar dengan batas waktu: task yang
    /// menggantung selamanya menahan `ManagedTorrent` lama → storage RAM-nya
    /// tidak pernah lepas setelah ganti torrent.
    fn warm_head(self: &Arc<Self>, index: usize) {
        let Some(file) = self.videos.get(index) else {
            return;
        };
        let handle = self.handle.clone();
        let (id, want) = (file.id, file.len.min(HEAD_WARM));
        tokio::spawn(async move {
            let Ok(mut stream) = handle.stream(id) else {
                return;
            };
            let deadline = Instant::now() + WARM_TIMEOUT;
            let mut remaining = want;
            let mut buf = vec![0u8; 64 * 1024];
            while remaining > 0 && Instant::now() < deadline {
                match timeout(Duration::from_secs(10), stream.read(&mut buf)).await {
                    Ok(Ok(0)) | Ok(Err(_)) | Err(_) => break,
                    Ok(Ok(n)) => remaining -= n as u64,
                }
            }
        });
    }

    /// Baca sedikit dari ekor file supaya piece yang memuat index container
    /// cepat datang. Tanpa ini librqbit hanya mengunduh apa yang dibaca player
    /// dari awal file.
    fn warm_tail(self: &Arc<Self>, index: usize) {
        let Some(file) = self.videos.get(index) else {
            return;
        };
        if file.len <= TAIL_WARM {
            return;
        }
        let handle = self.handle.clone();
        let (id, len) = (file.id, file.len);
        tokio::spawn(async move {
            let Ok(mut stream) = handle.stream(id) else {
                return;
            };
            if stream.seek(SeekFrom::Start(len - TAIL_WARM)).await.is_err() {
                return;
            }
            let mut buf = [0u8; 16 * 1024];
            let _ = timeout(WARM_TIMEOUT, stream.read(&mut buf)).await;
        });
    }
}

/// Satu job pemuatan magnet. Dimiliki server, bukan request, supaya bisa
/// dibatalkan dari mana saja dan dipakai bersama oleh request yang sama.
struct LoadJob {
    hash: String,
    cancel: AtomicBool,
    tx: watch::Sender<Option<Result<(), String>>>,
}

impl LoadJob {
    async fn wait(&self) -> Result<(), String> {
        let mut rx = self.tx.subscribe();
        loop {
            let done = rx.borrow().clone();
            if let Some(result) = done {
                return result;
            }
            if rx.changed().await.is_err() {
                return Err(ERR_CANCELLED.to_string());
            }
        }
    }
}

pub fn magnet_hash(magnet: &str) -> Result<String> {
    let parsed = Magnet::parse(magnet)?;
    if let Some(id) = parsed.as_id20() {
        return Ok(id.as_string());
    }
    if let Some(id) = parsed.as_id32() {
        return Ok(id.as_string());
    }
    bail!("magnet tanpa info hash")
}

/// Daftarkan job untuk magnet ini. Job yang sudah berjalan untuk magnet yang
/// sama dipakai ulang; magnet lain membatalkan job sebelumnya (yang terbaru
/// menang). Pemuatan tetap jalan walau request pemanggilnya mati.
async fn begin_load(app: &Arc<App>, magnet: &str, hash: &str) -> Arc<LoadJob> {
    let mut guard = app.job.lock().await;
    if let Some(job) = guard.as_ref() {
        if job.hash == hash {
            return job.clone();
        }
        job.cancel.store(true, Ordering::SeqCst);
    }
    let (tx, _rx) = watch::channel(None);
    let job = Arc::new(LoadJob {
        hash: hash.to_string(),
        cancel: AtomicBool::new(false),
        tx,
    });
    *guard = Some(job.clone());
    drop(guard);

    let app = app.clone();
    let running = job.clone();
    let magnet = magnet.to_string();
    tokio::spawn(async move {
        let result = load_torrent(app.clone(), running.clone(), magnet).await;
        let _ = running.tx.send(Some(result));
        let mut guard = app.job.lock().await;
        if guard.as_ref().is_some_and(|j| Arc::ptr_eq(j, &running)) {
            *guard = None;
        }
    });
    job
}

/// Bisa dibatalkan lewat `/api/cancel`, dibatasi `METADATA_TIMEOUT`.
fn cancelled(job: &LoadJob) -> bool {
    job.cancel.load(Ordering::SeqCst)
}

async fn drop_torrent(app: &App, handle: &Arc<ManagedTorrent>) {
    let _ = app
        .session
        .delete(TorrentIdOrHash::Id(handle.id()), false)
        .await;
}

async fn load_torrent(app: Arc<App>, job: Arc<LoadJob>, magnet: String) -> Result<(), String> {
    if app
        .current
        .read()
        .await
        .as_ref()
        .is_some_and(|c| c.hash == job.hash)
    {
        return Ok(()); // sudah aktif: menekan Muat lagi tidak me-restart
    }

    let _guard = app.switching.lock().await;
    if cancelled(&job) {
        return Err(ERR_CANCELLED.into());
    }
    if app
        .current
        .read()
        .await
        .as_ref()
        .is_some_and(|c| c.hash == job.hash)
    {
        return Ok(());
    }

    let (evict_tx, evict_rx) = mpsc::unbounded_channel::<u32>();
    let factory = MemStorageFactory {
        limit: app.mem_limit.clone(),
        evicted: evict_tx,
        stats: app.stats.clone(),
    };
    let opts = || AddTorrentOptions {
        paused: false,
        overwrite: true,
        // mode RAM: jangan pilih file apa pun, jadi librqbit tidak mengunduh
        // seluruh file di latar. Piece hanya diambil saat stream memintanya.
        only_files: Some(Vec::new()),
        storage_factory: Some(factory.clone().boxed()),
        ..Default::default()
    };

    let mut resp = app
        .session
        .add_torrent(AddTorrent::from_url(&magnet), Some(opts()))
        .await
        .map_err(|e| format!("tambah torrent: {e}"))?;
    if let AddTorrentResponse::AlreadyManaged(_, handle) = &resp {
        // Sisa magnet yang sama: buang dulu supaya storage RAM beserta kanal
        // eviction-nya dibuat ulang.
        let _ = app
            .session
            .delete(TorrentIdOrHash::Id(handle.id()), false)
            .await;
        resp = app
            .session
            .add_torrent(AddTorrent::from_url(&magnet), Some(opts()))
            .await
            .map_err(|e| format!("tambah torrent: {e}"))?;
    }
    let handle = match resp {
        AddTorrentResponse::Added(_, handle) | AddTorrentResponse::AlreadyManaged(_, handle) => {
            handle
        }
        AddTorrentResponse::ListOnly(_) => return Err("mode list-only tidak didukung".into()),
    };

    let deadline = Instant::now() + METADATA_TIMEOUT;
    while handle.metadata.load().is_none() {
        if cancelled(&job) {
            drop_torrent(&app, &handle).await;
            return Err(ERR_CANCELLED.into());
        }
        if Instant::now() >= deadline {
            drop_torrent(&app, &handle).await;
            return Err(ERR_TIMEOUT.into());
        }
        sleep(Duration::from_millis(200)).await;
    }

    let videos = video_files(&handle);
    if videos.is_empty() {
        drop_torrent(&app, &handle).await;
        return Err("torrent tidak berisi file video".into());
    }

    let current = attach(handle, videos, evict_rx);
    log::ready(&current.name, current.videos.len());
    current.warm_head(0);
    current.warm_tail(0);
    spawn_remux(&app, &current, 0);
    let old = app.current.write().await.replace(current);
    if let Some(old) = old {
        retire(app.clone(), old);
    }
    Ok(())
}

/// Bungkus torrent yang siap dipakai `/stream`: penjaga supaya librqbit tidak
/// melepas peer, dan task yang menandai piece yang dibuang dari RAM supaya bisa
/// diunduh ulang saat player seek kembali.
fn attach(
    handle: Arc<ManagedTorrent>,
    videos: Vec<FileEntry>,
    mut evict_rx: mpsc::UnboundedReceiver<u32>,
) -> Arc<Current> {
    // Stream "penjaga" yang duduk di EOF: tanpa ini librqbit menganggap tidak
    // ada file yang dipilih lalu melepas peer, sehingga tidak ada byte yang
    // diunduh saat player mulai membaca.
    let keep_handle = handle.clone();
    let first = videos[0].clone();
    let keepalive = tokio::spawn(async move {
        let start = Instant::now();
        loop {
            match keep_handle.clone().stream(first.id) {
                Ok(mut stream) => {
                    let _ = stream.seek(SeekFrom::Start(first.len)).await;
                    std::future::pending::<()>().await;
                }
                Err(_) if start.elapsed() < Duration::from_secs(120) => {
                    sleep(Duration::from_millis(200)).await
                }
                Err(_) => return,
            }
        }
    });

    let evict_handle = handle.clone();
    let evictor = tokio::spawn(async move {
        while let Some(piece) = evict_rx.recv().await {
            for _ in 0..600 {
                if evict_handle.forget_piece(piece).is_ok() {
                    break;
                }
                sleep(Duration::from_millis(200)).await;
            }
        }
    });

    Arc::new(Current {
        name: handle.name().unwrap_or_default(),
        hash: handle.info_hash().as_string(),
        handle,
        videos,
        selected: AtomicUsize::new(0),
        keepalive,
        evictor,
        remux: Mutex::new(None),
    })
}

/// Matikan torrent lama setelah torrent baru siap.
fn retire(app: Arc<App>, old: Arc<Current>) {
    tokio::spawn(async move {
        sleep(Duration::from_secs(2)).await;
        old.keepalive.abort();
        old.evictor.abort();
        stop_remux(&old).await;
        let _ = app
            .session
            .delete(TorrentIdOrHash::Id(old.handle.id()), false)
            .await;
    });
}

// --------------------------------------------------------------- remux ----

/// MKV punya index `Cues` di ekor? Baca maksimal 8 MiB terakhir dan cari ID
/// elemennya. Tanpa Cues, player harus memindai seluruh file sebelum mutar —
/// file seperti ini yang perlu diremux.
async fn mkv_has_cues(handle: &Arc<ManagedTorrent>, file: &FileEntry) -> bool {
    let tail = file.len.min(8 << 20);
    let Ok(mut stream) = handle.clone().stream(file.id) else {
        return false;
    };
    if stream.seek(SeekFrom::Start(file.len - tail)).await.is_err() {
        return false;
    }
    let mut buf = vec![0u8; tail as usize];
    if stream.read_exact(&mut buf).await.is_err() {
        return false;
    }
    buf.windows(4).any(|w| w == remux::CUES_ID)
}

async fn stop_remux(current: &Current) {
    if let Some(job) = current.remux.lock().await.take() {
        job.stop.store(true, Ordering::SeqCst);
    }
}

/// Mulai remux episode terpilih bila perlu: ffmpeg ada, file Matroska, dan
/// tidak punya Cues. Hasil dipipe ke RAM; setiap byte memotong `mem_limit`
/// sehingga cache piece ikut menyusut — total RAM tetap dalam batas env.
fn spawn_remux(app: &Arc<App>, current: &Arc<Current>, index: usize) {
    let Some(file) = current.videos.get(index).cloned() else {
        return;
    };
    if !remux::is_matroska(&file.path) {
        return;
    }
    let app = app.clone();
    let current = current.clone();
    tokio::spawn(async move {
        // Job untuk file yang sama sedang berjalan / sudah siap: jangan diulang.
        {
            let slot = current.remux.lock().await;
            if slot.as_ref().is_some_and(|j| j.index == index) {
                return;
            }
        }
        // Hentikan job file lain supaya budget bebas untuk deteksi + remux baru.
        stop_remux(&current).await;
        if mkv_has_cues(&current.handle, &file).await {
            return; // sudah punya index: player langsung bisa memutar
        }
        let Some(ffmpeg) = remux::ensure().await else {
            return;
        };
        start_remux(&app, &current, index, &file, ffmpeg).await;
    });
}

async fn start_remux(
    app: &Arc<App>,
    current: &Arc<Current>,
    index: usize,
    file: &FileEntry,
    ffmpeg: PathBuf,
) {
    let port = app.http_port.load(Ordering::SeqCst);
    if port == 0 {
        return; // server belum listen (hanya di test)
    }
    let url = format!("http://127.0.0.1:{port}{}", stream_url(index, file.name()));
    let mut child = match tokio::process::Command::new(&ffmpeg)
        .args([
            "-hide_banner",
            "-loglevel",
            "error",
            "-nostdin",
            "-i",
            &url,
            "-map",
            "0",
            "-c",
            "copy",
            "-f",
            "matroska",
            "pipe:1",
        ])
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::null())
        .spawn()
    {
        Ok(child) => child,
        Err(error) => {
            eprintln!("remux: ffmpeg gagal jalan: {error}");
            return;
        }
    };
    let mut out = child.stdout.take().expect("stdout ffmpeg dipipe");

    let stop = Arc::new(AtomicBool::new(false));
    let taken = Arc::new(AtomicU64::new(0));
    let buf: Arc<RwLock<Option<Bytes>>> = Arc::new(RwLock::new(None));
    *current.remux.lock().await = Some(RemuxJob {
        index,
        stop: stop.clone(),
        buf: buf.clone(),
    });

    let limit = app.mem_limit.clone();
    let name = file.name().to_string();
    tokio::spawn(async move {
        let mut acc = BytesMut::new();
        let mut chunk = vec![0u8; 128 * 1024];
        let mut failed = false;
        loop {
            if stop.load(Ordering::SeqCst) {
                failed = true;
                break;
            }
            match out.read(&mut chunk).await {
                Ok(0) => break,
                Ok(n) => {
                    // Potong budget piece; kalau limit tidak cukup untuk satu
                    // chunk, remux digagalkan alih-alih melewati batas memori.
                    let n64 = n as u64;
                    let mut cut = false;
                    loop {
                        let left = limit.load(Ordering::SeqCst);
                        if left < n64 {
                            break;
                        }
                        if limit
                            .compare_exchange(left, left - n64, Ordering::SeqCst, Ordering::SeqCst)
                            .is_ok()
                        {
                            cut = true;
                            break;
                        }
                    }
                    if !cut {
                        eprintln!("remux: melewati batas memori, dibatalkan");
                        failed = true;
                        break;
                    }
                    taken.fetch_add(n64, Ordering::SeqCst);
                    acc.extend_from_slice(&chunk[..n]);
                }
                Err(_) => {
                    failed = true;
                    break;
                }
            }
        }
        if failed {
            let _ = child.start_kill();
            limit.fetch_add(taken.load(Ordering::SeqCst), Ordering::SeqCst);
            return;
        }
        if matches!(child.wait().await, Ok(status) if status.success()) {
            println!("remux siap: {name} ({} byte)", acc.len());
            *buf.write().await = Some(acc.freeze());
        } else {
            limit.fetch_add(taken.load(Ordering::SeqCst), Ordering::SeqCst);
        }
    });
}

// --------------------------------------------------------------- state ----

async fn state_json(app: &App) -> Value {
    let loading = app.job.lock().await.is_some();
    let current = app.current.read().await.clone();
    let cache = json!({
        "used": app.stats.used(),
        "limit": app.cache_mb * MIB,
        "evictions": app.stats.evictions(),
    });

    let Some(current) = current else {
        return json!({
            "ready": false,
            "loading": loading,
            "files": [],
            "selected": -1,
            "cache": cache,
        });
    };

    let selected = current.selected.load(Ordering::SeqCst);
    let file = current.videos.get(selected);
    let stats = current.handle.stats();
    // Byte file terpilih yang masih ada di cache RAM — nol saat ganti episode
    // ke file yang belum diunduh, dan kembali naik saat balik ke episode yang
    // piece-nya masih tersimpan.
    let downloaded = file
        .map(|f| {
            let guard = current.handle.metadata.load();
            let Some(meta) = guard.as_ref() else {
                return 0;
            };
            let piece_len = meta.lengths.default_piece_length().max(1) as u64;
            let info = &meta.file_infos[f.id];
            let (fstart, fend) = (info.offset_in_torrent, info.offset_in_torrent + info.len);
            let mut bytes = 0u64;
            for (&pid, &have) in &app.stats.cached_pieces() {
                let pstart = pid as u64 * piece_len;
                let overlap = (pstart + piece_len)
                    .min(fend)
                    .saturating_sub(pstart.max(fstart));
                bytes += overlap.min(have);
            }
            bytes.min(f.len)
        })
        .unwrap_or(0);
    let peers = stats
        .live
        .as_ref()
        .map(|live| live.snapshot.peer_stats.live)
        .unwrap_or(0);
    // diagnostik sementara: fetched_bytes naik = byte datang; pieces naik =
    // piece lolos verifikasi.
    let fetched = stats
        .live
        .as_ref()
        .map(|live| live.snapshot.fetched_bytes)
        .unwrap_or(0);
    let pieces = stats
        .live
        .as_ref()
        .map(|live| live.snapshot.downloaded_and_checked_pieces)
        .unwrap_or(0);
    let files: Vec<Value> = current
        .videos
        .iter()
        .enumerate()
        .map(|(index, file)| {
            json!({
                "index": index,
                "name": file.name(),
                "path": file.path,
                "size": file.len,
                "url": stream_url(index, file.name()),
            })
        })
        .collect();

    let remux_state = {
        let slot = current.remux.lock().await;
        match slot.as_ref() {
            Some(job) => {
                let buf = job.buf.read().await;
                Some(json!({
                    "index": job.index,
                    "ready": buf.is_some(),
                    "size": buf.as_ref().map(|b| b.len()).unwrap_or(0),
                }))
            }
            None => None,
        }
    };

    json!({
        "ready": true,
        "loading": loading,
        "files": files,
        "selected": selected,
        "cache": cache,
        "name": current.name,
        "info_hash": current.hash,
        "length": file.map(|f| f.len).unwrap_or(0),
        "downloaded": downloaded,
        "fetched": fetched,
        "pieces": pieces,
        "peers": peers,
        "size": file.map(|f| f.len).unwrap_or(0),
        // byte kumulatif; kecepatan (Mbps) dihitung di klien dari selisihnya
        "net": {
            "from_peers": fetched,
            "to_client": app.stats.served(),
        },
        "remux": remux_state,
    })
}

/// RSS proses dari /proc; bukan bagian cache, dipakai untuk membandingkan
/// pemakaian memori total vs batas cache.
fn rss_bytes() -> u64 {
    std::fs::read_to_string("/proc/self/statm")
        .ok()
        .and_then(|s| {
            s.split_whitespace()
                .nth(1)
                .and_then(|pages| pages.parse::<u64>().ok())
        })
        .map(|pages| pages * 4096)
        .unwrap_or(0)
}

async fn info_json(app: &App) -> Value {
    let current = app.current.read().await.clone();
    let mut response = json!({
        "ready": false,
        "storage": "memory",
        "cache_limit": app.cache_mb * MIB,
        "cache_used": app.stats.used(),
        "cache_evictions": app.stats.evictions(),
        "rss_bytes": rss_bytes(),
    });
    if let Some(current) = current {
        let selected = current.selected.load(Ordering::SeqCst);
        if let Some(file) = current.videos.get(selected) {
            response["ready"] = json!(true);
            response["name"] = json!(file.path);
            response["size"] = json!(file.len);
        }
    }
    response
}

// ------------------------------------------------------------- helpers ----

pub fn is_video(path: &str) -> bool {
    let ext = path
        .rsplit_once('.')
        .map(|(_, ext)| ext.to_lowercase())
        .unwrap_or_default();
    matches!(
        ext.as_str(),
        "mkv" | "mp4" | "m4v" | "webm" | "avi" | "mov" | "ts" | "m2ts"
    )
}

/// Saring file video lalu urutkan menurut nama (huruf kecil), sama seperti Go.
pub fn pick_videos(all: impl Iterator<Item = (usize, String, u64)>) -> Vec<FileEntry> {
    let mut videos: Vec<FileEntry> = all
        .filter(|(_, path, _)| is_video(path))
        .map(|(id, path, len)| FileEntry { id, path, len })
        .collect();
    videos.sort_by_key(|file| file.path.to_lowercase());
    videos
}

fn video_files(handle: &Arc<ManagedTorrent>) -> Vec<FileEntry> {
    let guard = handle.metadata.load();
    let Some(metadata) = guard.as_ref() else {
        return Vec::new();
    };
    pick_videos(metadata.file_infos.iter().enumerate().map(|(i, file)| {
        (
            i,
            file.relative_filename.to_string_lossy().to_string(),
            file.len,
        )
    }))
}

pub fn video_mime(path: &str) -> &'static str {
    match path
        .rsplit_once('.')
        .map(|(_, ext)| ext.to_lowercase())
        .unwrap_or_default()
        .as_str()
    {
        "mkv" => "video/x-matroska",
        "mp4" | "m4v" => "video/mp4",
        "webm" => "video/webm",
        "avi" => "video/x-msvideo",
        "mov" => "video/quicktime",
        "ts" | "m2ts" => "video/mp2t",
        _ => "application/octet-stream",
    }
}

pub fn encode_segment(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    for byte in text.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(byte as char)
            }
            _ => out.push_str(&format!("%{byte:02X}")),
        }
    }
    out
}

pub fn stream_url(index: usize, name: &str) -> String {
    format!("/stream/{index}/{}", encode_segment(name))
}

#[derive(Debug, PartialEq)]
pub enum RangeSpec {
    Full,
    Partial { start: u64, end: u64 },
    Unsatisfiable,
}

/// `shortcut: multi-range diabaikan, ambil rentang pertama; cukup untuk <video>`
pub fn parse_range(header_value: Option<&str>, len: u64) -> RangeSpec {
    let Some(value) = header_value else {
        return RangeSpec::Full;
    };
    let Some(spec) = value.trim().strip_prefix("bytes=") else {
        return RangeSpec::Full;
    };
    let first = spec.split(',').next().unwrap_or_default().trim();
    let Some((from, to)) = first.split_once('-') else {
        return RangeSpec::Full;
    };
    if len == 0 {
        return RangeSpec::Unsatisfiable;
    }
    let (start, end) = if from.is_empty() {
        let Ok(suffix) = to.parse::<u64>() else {
            return RangeSpec::Full;
        };
        if suffix == 0 {
            return RangeSpec::Unsatisfiable;
        }
        (len.saturating_sub(suffix), len - 1)
    } else {
        let Ok(start) = from.parse::<u64>() else {
            return RangeSpec::Full;
        };
        let end = if to.is_empty() {
            len - 1
        } else {
            match to.parse::<u64>() {
                Ok(end) => end.min(len - 1),
                Err(_) => return RangeSpec::Full,
            }
        };
        (start, end)
    };
    if start >= len || start > end {
        return RangeSpec::Unsatisfiable;
    }
    RangeSpec::Partial { start, end }
}

fn json_error(status: StatusCode, message: &str) -> Response {
    (status, Json(json!({ "error": message }))).into_response()
}

// ------------------------------------------------------------ handlers ----

async fn index() -> Html<&'static str> {
    Html(INDEX_HTML)
}

async fn healthz() -> &'static str {
    "ok"
}

async fn info(State(app): State<Arc<App>>) -> Json<Value> {
    Json(info_json(&app).await)
}

async fn api_ws(ws: WebSocketUpgrade, State(app): State<Arc<App>>) -> Response {
    ws.on_upgrade(move |socket| ws_loop(socket, app))
}

/// Server mendorong status tiap `WS_DELAY`; pesan masuk hanya dibaca untuk
/// tahu koneksinya ditutup.
async fn ws_loop(mut socket: WebSocket, app: Arc<App>) {
    loop {
        let state = state_json(&app).await;
        if socket
            .send(Message::Text(state.to_string().into()))
            .await
            .is_err()
        {
            return;
        }
        tokio::select! {
            _ = sleep(WS_DELAY) => {}
            message = socket.recv() => {
                if !matches!(message, Some(Ok(_))) {
                    return;
                }
            }
        }
    }
}

#[derive(Deserialize)]
struct MagnetRequest {
    magnet: String,
    #[serde(rename = "fileIndex")]
    file_index: Option<usize>,
}

async fn api_magnet(State(app): State<Arc<App>>, Json(req): Json<MagnetRequest>) -> Response {
    let magnet = req.magnet.trim().to_string();
    let Ok(hash) = magnet_hash(&magnet) else {
        return json_error(StatusCode::BAD_REQUEST, "magnet tidak valid");
    };

    let job = begin_load(&app, &magnet, &hash).await;
    if let Err(error) = job.wait().await {
        let status = match error.as_str() {
            ERR_TIMEOUT => StatusCode::GATEWAY_TIMEOUT,
            ERR_CANCELLED => StatusCode::CONFLICT,
            _ => StatusCode::BAD_GATEWAY,
        };
        return json_error(status, &error);
    }

    // fileIndex = urutan file di torrent (dari index Torrentio), bukan urutan
    // daftar video yang disortir; dipetakan lewat identitas indeks file.
    if let Some(file_index) = req.file_index {
        let current = app.current.read().await.clone();
        let Some(current) = current else {
            return json_error(StatusCode::BAD_REQUEST, "torrent belum siap");
        };
        match current.videos.iter().position(|f| f.id == file_index) {
            Some(position) => current.selected.store(position, Ordering::SeqCst),
            None => {
                return json_error(
                    StatusCode::BAD_REQUEST,
                    &format!("fileIndex {file_index} bukan video"),
                );
            }
        }
    }
    Json(state_json(&app).await).into_response()
}

async fn api_cancel(State(app): State<Arc<App>>) -> Response {
    let job = app.job.lock().await.clone();
    let Some(job) = job else {
        return json_error(StatusCode::CONFLICT, "tidak ada magnet yang sedang dimuat");
    };
    job.cancel.store(true, Ordering::SeqCst);
    Json(state_json(&app).await).into_response()
}

#[derive(Deserialize)]
struct SelectRequest {
    index: usize,
}

async fn api_select(State(app): State<Arc<App>>, Json(req): Json<SelectRequest>) -> Response {
    let current = app.current.read().await.clone();
    let Some(current) = current else {
        return json_error(
            StatusCode::SERVICE_UNAVAILABLE,
            "torrent metadata belum siap",
        );
    };
    if req.index >= current.videos.len() {
        return json_error(StatusCode::BAD_REQUEST, "index file di luar jangkauan");
    }
    current.selected.store(req.index, Ordering::SeqCst);
    current.warm_head(req.index);
    current.warm_tail(req.index);
    spawn_remux(&app, &current, req.index);
    Json(state_json(&app).await).into_response()
}

async fn stream_selected(
    State(app): State<Arc<App>>,
    method: Method,
    headers: HeaderMap,
) -> Response {
    serve_stream(app, None, method, headers).await
}

async fn stream_index(
    State(app): State<Arc<App>>,
    AxumPath((index, _name)): AxumPath<(usize, String)>,
    method: Method,
    headers: HeaderMap,
) -> Response {
    serve_stream(app, Some(index), method, headers).await
}

/// Keluaran remux dari RAM, dukungan Range sama seperti /stream supaya
/// seek player instan tanpa menyentuh swarm.
async fn remux_index(
    State(app): State<Arc<App>>,
    AxumPath((index, _name)): AxumPath<(usize, String)>,
    method: Method,
    headers: HeaderMap,
) -> Response {
    let current = app.current.read().await.clone();
    let Some(current) = current else {
        return json_error(
            StatusCode::SERVICE_UNAVAILABLE,
            "torrent metadata belum siap",
        );
    };
    let slot = current.remux.lock().await;
    let Some(job) = slot.as_ref().filter(|j| j.index == index) else {
        return json_error(StatusCode::NOT_FOUND, "remux tidak tersedia untuk file ini");
    };
    let Some(bytes) = job.buf.read().await.clone() else {
        return json_error(StatusCode::SERVICE_UNAVAILABLE, "remux masih berjalan");
    };
    drop(slot);
    let len = bytes.len() as u64;
    let spec = parse_range(
        headers
            .get(header::RANGE)
            .and_then(|value| value.to_str().ok()),
        len,
    );
    let (status, start, count, content_range) = match spec {
        RangeSpec::Unsatisfiable => {
            return Response::builder()
                .status(StatusCode::RANGE_NOT_SATISFIABLE)
                .header(header::ACCEPT_RANGES, "bytes")
                .header(header::CONTENT_RANGE, format!("bytes */{len}"))
                .body(Body::empty())
                .expect("response 416 selalu valid");
        }
        RangeSpec::Full => (StatusCode::OK, 0u64, len, None),
        RangeSpec::Partial { start, end } => (
            StatusCode::PARTIAL_CONTENT,
            start,
            end - start + 1,
            Some(format!("bytes {start}-{end}/{len}")),
        ),
    };
    let body = if method == Method::HEAD {
        Body::empty()
    } else {
        Body::from(bytes.slice(start as usize..(start + count) as usize))
    };
    let mut builder = Response::builder()
        .status(status)
        .header(header::CONTENT_TYPE, "video/x-matroska")
        .header(header::ACCEPT_RANGES, "bytes")
        .header(header::CONTENT_LENGTH, count.to_string());
    if let Some(range) = content_range {
        builder = builder.header(header::CONTENT_RANGE, range);
    }
    builder.body(body).expect("response remux selalu valid")
}

/// HEAD, Range (206), dan 416. Byte dikirim begitu librqbit punya datanya.
async fn serve_stream(
    app: Arc<App>,
    index: Option<usize>,
    method: Method,
    headers: HeaderMap,
) -> Response {
    let current = app.current.read().await.clone();
    let Some(current) = current else {
        return json_error(
            StatusCode::SERVICE_UNAVAILABLE,
            "torrent metadata belum siap",
        );
    };
    let selected = index.unwrap_or_else(|| current.selected.load(Ordering::SeqCst));
    let Some(file) = current.videos.get(selected).cloned() else {
        return json_error(StatusCode::BAD_REQUEST, "index file di luar jangkauan");
    };
    let len = file.len;
    let spec = parse_range(
        headers
            .get(header::RANGE)
            .and_then(|value| value.to_str().ok()),
        len,
    );
    let (status, start, count, content_range) = match spec {
        RangeSpec::Unsatisfiable => {
            return Response::builder()
                .status(StatusCode::RANGE_NOT_SATISFIABLE)
                .header(header::ACCEPT_RANGES, "bytes")
                .header(header::CONTENT_RANGE, format!("bytes */{len}"))
                .body(Body::empty())
                .expect("response 416 selalu valid");
        }
        RangeSpec::Full => (StatusCode::OK, 0, len, None),
        RangeSpec::Partial { start, end } => (
            StatusCode::PARTIAL_CONTENT,
            start,
            end - start + 1,
            Some(format!("bytes {start}-{end}/{len}")),
        ),
    };

    let body = if method == Method::HEAD {
        Body::empty()
    } else {
        match current.handle.clone().stream(file.id) {
            Ok(mut stream) => {
                if start > 0
                    && let Err(error) = stream.seek(SeekFrom::Start(start)).await
                {
                    return json_error(
                        StatusCode::INTERNAL_SERVER_ERROR,
                        &format!("seek stream: {error}"),
                    );
                }
                // Hitung byte yang benar-benar keluar ke player untuk statistik
                // "server → client".
                use futures_util::StreamExt;
                let stats = app.stats.clone();
                Body::from_stream(ReaderStream::new(stream.take(count)).map(move |chunk| {
                    if let Ok(bytes) = &chunk {
                        stats.add_served(bytes.len() as u64);
                    }
                    chunk
                }))
            }
            Err(error) => {
                return json_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    &format!("buka stream: {error}"),
                );
            }
        }
    };

    let mut builder = Response::builder()
        .status(status)
        .header(header::CONTENT_TYPE, video_mime(&file.path))
        .header(header::ACCEPT_RANGES, "bytes")
        .header(header::CONTENT_LENGTH, count.to_string());
    if let Some(range) = content_range {
        builder = builder.header(header::CONTENT_RANGE, range);
    }
    builder.body(body).expect("response stream selalu valid")
}

// ----------------------------------------------------------------- run ----

/// Route yang sama dipakai `run()` dan test router (tanpa membuka port).
fn router(app: Arc<App>) -> Router {
    Router::new()
        .route("/", get(index))
        .route("/healthz", get(healthz))
        .route("/info", get(info))
        .route("/api/ws", get(api_ws))
        .route("/api/magnet", post(api_magnet))
        .route("/api/cancel", post(api_cancel))
        .route("/api/select", post(api_select))
        .route("/stream", get(stream_selected))
        .route("/stream/{index}/{name}", get(stream_index))
        .route("/remux/{index}/{name}", get(remux_index))
        .with_state(app)
}

pub async fn run() -> Result<()> {
    let config = Config::from_env_and_args()?;
    // Lokasi binary ffmpeg opsional (diunduh sendiri bila tidak ada di sistem).
    let _ = std::fs::create_dir_all("./tmp");

    // Zero disk: DataDir hanya dipakai kalau storage bawaan yang aktif.
    let session = Session::new_with_opts(
        PathBuf::from("./downloads"),
        SessionOptions {
            disable_dht_persistence: true,
            ..Default::default()
        },
    )
    .await
    .context("gagal membuat session torrent")?;

    let app = Arc::new(App {
        session,
        cache_mb: config.cache_mb,
        stats: Arc::new(Stats::default()),
        mem_limit: Arc::new(AtomicU64::new(config.cache_mb * MIB)),
        http_port: AtomicU16::new(0),
        current: RwLock::new(None),
        job: Mutex::new(None),
        switching: Mutex::new(()),
    });

    // Bind dulu: remux butuh port sendiri sebagai input URL ffmpeg.
    let listener = TcpListener::bind(&config.addr)
        .await
        .with_context(|| format!("bind {} gagal", config.addr))?;
    app.http_port.store(
        listener.local_addr().context("alamat listener")?.port(),
        Ordering::SeqCst,
    );
    log::listening(&browser_url(&config.addr), config.cache_mb);

    if !config.magnet.is_empty() {
        match magnet_hash(&config.magnet) {
            Ok(hash) => {
                let job = begin_load(&app, &config.magnet, &hash).await;
                tokio::spawn(async move {
                    if let Err(error) = job.wait().await {
                        eprintln!("magnet awal: {error}");
                    }
                });
            }
            Err(error) => eprintln!("magnet awal: {error}"),
        }
    }

    let router = router(app);
    serve(listener, router).await.context("server berhenti")
}

/// Log sederhana supaya tidak menambah dependency.
mod log {
    use std::sync::OnceLock;

    fn started() -> &'static OnceLock<bool> {
        static STARTED: OnceLock<bool> = OnceLock::new();
        &STARTED
    }

    pub fn ready(name: &str, files: usize) {
        started().set(true).ok();
        println!("torrent siap: {name} ({files} file video)");
    }

    pub fn listening(url: &str, cache_mb: u64) {
        println!("listening on {url} (memory cache {cache_mb} MiB, zero disk)");
    }
}

/// Alamat dengar untuk log, dipisah supaya `SocketAddr` bisa dipakai di test.
pub fn parse_addr(addr: &str) -> Result<SocketAddr> {
    addr.parse().context("alamat tidak valid")
}

// ---------------------------------------------------------------- tests ---

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn browser_url_maps_wildcard_host() {
        let cases = [
            ("0.0.0.0:8080", "http://localhost:8080"),
            (":8080", "http://localhost:8080"),
            ("[::]:8080", "http://localhost:8080"),
            ("127.0.0.1:9000", "http://127.0.0.1:9000"),
            ("[::1]:9000", "http://[::1]:9000"),
            ("tanpa-port", "tanpa-port"),
        ];
        for (addr, want) in cases {
            assert_eq!(browser_url(addr), want, "browser_url({addr})");
        }
    }

    #[test]
    fn config_reads_cache_limit_and_magnet() {
        // env proses menang atas .env; nilai default 200 MiB.
        let config = Config {
            addr: "0.0.0.0:8080".into(),
            cache_mb: 200,
            magnet: DEFAULT_MAGNET.into(),
        };
        assert_eq!(browser_url(&config.addr), "http://localhost:8080");
        assert!(magnet_hash(&config.magnet).is_ok());
        assert!(magnet_hash("bukan magnet").is_err());
    }
}

/// Route diuji lewat router, tanpa membuka port dan tanpa torrent.
#[cfg(test)]
mod router_tests {
    use super::*;
    use axum::body::to_bytes;
    use axum::http::Request;
    use tower::ServiceExt;

    async fn test_app() -> Arc<App> {
        let session = Session::new_with_opts(
            std::env::temp_dir().join("zanime-router-test"),
            SessionOptions {
                disable_dht: true,
                disable_dht_persistence: true,
                ..Default::default()
            },
        )
        .await
        .expect("session torrent untuk test");
        Arc::new(App {
            session,
            cache_mb: 200,
            stats: Arc::new(Stats::default()),
            mem_limit: Arc::new(AtomicU64::new(200 * MIB)),
            http_port: AtomicU16::new(0),
            current: RwLock::new(None),
            job: Mutex::new(None),
            switching: Mutex::new(()),
        })
    }

    async fn call(app: Arc<App>, method: Method, uri: &str, body: &str) -> (StatusCode, String) {
        let request = Request::builder()
            .method(method)
            .uri(uri)
            .header(header::CONTENT_TYPE, "application/json")
            .body(Body::from(body.to_string()))
            .expect("request test");
        let response = router(app).oneshot(request).await.expect("router test");
        let status = response.status();
        let bytes = to_bytes(response.into_body(), 1 << 20)
            .await
            .expect("body test");
        (status, String::from_utf8_lossy(&bytes).to_string())
    }

    #[tokio::test]
    async fn serves_index_health_and_info() {
        let app = test_app().await;
        let (status, html) = call(app.clone(), Method::GET, "/", "").await;
        assert_eq!(status, StatusCode::OK);
        assert!(html.contains("movi-player"), "index.html ter-embed");

        let (status, body) = call(app.clone(), Method::GET, "/healthz", "").await;
        assert_eq!((status, body.as_str()), (StatusCode::OK, "ok"));

        let (status, body) = call(app, Method::GET, "/info", "").await;
        assert_eq!(status, StatusCode::OK);
        let info: Value = serde_json::from_str(&body).unwrap();
        assert_eq!(info["storage"], "memory");
        assert_eq!(info["cache_limit"], 200 * MIB);
        assert_eq!(info["ready"], false);
    }

    #[tokio::test]
    async fn state_without_torrent_is_empty() {
        // Status hanya dikirim lewat WebSocket, jadi payload-nya diuji langsung.
        let app = test_app().await;
        let state = state_json(&app).await;
        assert_eq!(state["ready"], false);
        assert_eq!(state["files"], json!([]));
        assert_eq!(state["selected"], -1);
        assert_eq!(state["cache"]["limit"], 200 * MIB);
    }

    #[tokio::test]
    async fn stream_and_select_without_torrent_are_unavailable() {
        let app = test_app().await;
        let (status, body) = call(app.clone(), Method::GET, "/stream", "").await;
        assert_eq!(status, StatusCode::SERVICE_UNAVAILABLE);
        assert!(body.contains("belum siap"));

        let (status, _) = call(app, Method::POST, "/api/select", r#"{"index":0}"#).await;
        assert_eq!(status, StatusCode::SERVICE_UNAVAILABLE);
    }

    #[tokio::test]
    async fn cancel_without_job_conflicts() {
        let (status, body) = call(test_app().await, Method::POST, "/api/cancel", "").await;
        assert_eq!(status, StatusCode::CONFLICT);
        assert!(body.contains("tidak ada magnet"));
    }

    #[tokio::test]
    async fn magnet_with_invalid_link_is_bad_request() {
        let (status, body) = call(
            test_app().await,
            Method::POST,
            "/api/magnet",
            r#"{"magnet":"bukan magnet"}"#,
        )
        .await;
        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert!(body.contains("magnet tidak valid"));
    }
}

/// Streaming diuji dengan torrent sungguhan, tapi seluruh swarm-nya hidup di
/// localhost: satu session menyebar, satu session mengunduh lewat storage RAM.
/// Jadi tidak ada byte yang keluar ke internet dan test berjalan dalam detik.
#[cfg(test)]
mod stream_tests {
    use super::*;
    use axum::body::to_bytes;
    use axum::http::Request;
    use librqbit::{CreateTorrentOptions, create_torrent};
    use tokio::time::timeout;
    use tower::ServiceExt;

    const PIECE: usize = 4096;
    const PIECES: usize = 8;

    fn virtual_data() -> Vec<u8> {
        (0..PIECE * PIECES)
            .map(|i| (i * 31 + i / 7) as u8)
            .collect()
    }

    async fn get(
        app: &Arc<App>,
        method: Method,
        range: Option<&str>,
    ) -> (StatusCode, HeaderMap, Vec<u8>) {
        let mut builder = Request::builder().method(method).uri("/stream/0/ep01.mkv");
        if let Some(range) = range {
            builder = builder.header(header::RANGE, range);
        }
        let response = router(app.clone())
            .oneshot(builder.body(Body::empty()).expect("request test"))
            .await
            .expect("router test");
        let status = response.status();
        let headers = response.headers().clone();
        let body = to_bytes(response.into_body(), 1 << 20)
            .await
            .expect("body test")
            .to_vec();
        (status, headers, body)
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn streams_ranges_and_refetches_evicted_pieces() {
        timeout(Duration::from_secs(60), virtual_swarm())
            .await
            .expect("streaming selesai dalam batas waktu");
    }

    async fn virtual_swarm() {
        let root = std::env::temp_dir().join(format!("zanime-stream-test-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        let seed_dir = root.join("seed");
        let leech_dir = root.join("leech");
        std::fs::create_dir_all(&seed_dir).unwrap();
        std::fs::create_dir_all(&leech_dir).unwrap();

        let data = virtual_data();
        std::fs::write(seed_dir.join("ep01.mkv"), &data).unwrap();
        let torrent = create_torrent(
            &seed_dir,
            CreateTorrentOptions {
                name: None,
                piece_length: Some(PIECE as u32),
            },
        )
        .await
        .expect("bikin torrent");
        let bytes = torrent.as_bytes().expect("serialize torrent");

        let seed_session = Session::new_with_opts(
            root.join("seed-session"),
            SessionOptions {
                disable_dht: true,
                persistence: None,
                listen_port_range: Some(21000..21100),
                enable_upnp_port_forwarding: false,
                ..Default::default()
            },
        )
        .await
        .expect("session penyebar");
        seed_session
            .add_torrent(
                AddTorrent::from_bytes(bytes.clone()),
                Some(AddTorrentOptions {
                    paused: false,
                    overwrite: true,
                    output_folder: Some(seed_dir.to_string_lossy().into_owned()),
                    ..Default::default()
                }),
            )
            .await
            .expect("tambah torrent penyebar")
            .into_handle()
            .expect("handle penyebar")
            .wait_until_completed()
            .await
            .expect("penyebar selesai");
        let seed_addr = SocketAddr::new(
            "127.0.0.1".parse().unwrap(),
            seed_session.tcp_listen_port().expect("port penyebar"),
        );

        let (evict_tx, evict_rx) = mpsc::unbounded_channel();
        let stats = Arc::new(Stats::default());
        let leech_session = Session::new_with_opts(
            leech_dir.clone(),
            SessionOptions {
                disable_dht: true,
                persistence: None,
                enable_upnp_port_forwarding: false,
                ..Default::default()
            },
        )
        .await
        .expect("session pengunduh");
        let handle = leech_session
            .add_torrent(
                AddTorrent::from_bytes(bytes),
                Some(AddTorrentOptions {
                    paused: false,
                    overwrite: true,
                    initial_peers: Some(vec![seed_addr]),
                    // seperti server sungguhan: tidak ada file yang dipilih,
                    // piece hanya diambil saat stream memintanya
                    only_files: Some(Vec::new()),
                    storage_factory: Some(
                        MemStorageFactory {
                            limit: Arc::new(AtomicU64::new(2 * PIECE as u64)),
                            evicted: evict_tx,
                            stats: stats.clone(),
                        }
                        .boxed(),
                    ),
                    ..Default::default()
                }),
            )
            .await
            .expect("tambah torrent pengunduh")
            .into_handle()
            .expect("handle pengunduh");
        handle
            .wait_until_initialized()
            .await
            .expect("metadata pengunduh");

        let videos = video_files(&handle);
        assert_eq!(videos.len(), 1, "satu file video terdeteksi");
        let app = Arc::new(App {
            session: leech_session,
            cache_mb: 1,
            stats: stats.clone(),
            mem_limit: Arc::new(AtomicU64::new(MIB)),
            http_port: AtomicU16::new(0),
            current: RwLock::new(Some(attach(handle, videos, evict_rx))),
            job: Mutex::new(None),
            switching: Mutex::new(()),
        });

        // Rentang di tengah file: 206 dengan byte yang tepat (jalur seek).
        let (status, headers, body) = get(&app, Method::GET, Some("bytes=9000-12000")).await;
        assert_eq!(status, StatusCode::PARTIAL_CONTENT);
        assert_eq!(headers[header::CONTENT_RANGE], "bytes 9000-12000/32768");
        assert_eq!(body, data[9000..12001]);

        // Seluruh file lewat cache 2 piece untuk 8 piece: harus ada eviction.
        let (status, _, body) = get(&app, Method::GET, None).await;
        assert_eq!(status, StatusCode::OK);
        assert_eq!(body, data, "byte yang keluar sama dengan berkas aslinya");
        assert!(
            stats.evictions() > 0,
            "cache 2 piece tidak cukup untuk 8 piece"
        );
        assert!(stats.used() <= 2 * PIECE as u64, "cache melewati batas");

        // Piece 0 sudah dibuang dari RAM: membacanya lagi harus mengunduh ulang.
        let (status, _, body) = get(&app, Method::GET, Some("bytes=0-4095")).await;
        assert_eq!(status, StatusCode::PARTIAL_CONTENT);
        assert_eq!(body, data[..PIECE]);

        let (status, headers, body) = get(&app, Method::HEAD, None).await;
        assert_eq!(status, StatusCode::OK);
        assert_eq!(
            headers[header::CONTENT_LENGTH],
            (PIECE * PIECES).to_string()
        );
        assert!(body.is_empty(), "HEAD tanpa badan");

        let (status, _, _) = get(&app, Method::GET, Some("bytes=999999-")).await;
        assert_eq!(status, StatusCode::RANGE_NOT_SATISFIABLE);

        // Zero disk: pengunduh tidak menulis apa pun ke folder datanya.
        assert!(
            std::fs::read_dir(&leech_dir).unwrap().next().is_none(),
            "storage RAM tidak menulis ke disk"
        );
        let _ = std::fs::remove_dir_all(&root);
    }
}
