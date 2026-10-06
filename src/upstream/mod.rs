use std::collections::{HashMap, VecDeque};
use std::pin::Pin;

use base64::{engine::general_purpose::URL_SAFE_NO_PAD as B64, Engine as _};
use futures_util::StreamExt as _;
use serde_json::Value;

use crate::{config::Config, error::AppError, models::AnimeItem};

const CATALOG_QUERY: &str = "query CatalogAnime($filter: AnimeCatalogFilterInput $sort: [AnimeSortInput!] $limit: Int $offset: Int){catalogAnime(filter:$filter sort:$sort limit:$limit offset:$offset){items{id anilistId titleRomaji titleEnglish episodeCount coverImage bannerImage format seasonYear averageScore}}}";

/// Nilai sah `AnimeSortField` (dari introspeksi skema upstream).
pub const SORT_FIELDS: [&str; 15] = [
    "POPULARITY", "TRENDING", "AVERAGE_SCORE", "MEAN_SCORE", "FAVOURITES", "SEASON_YEAR", "UPDATED_AT", "CREATED_AT",
    "EPISODE_COUNT", "DURATION", "TITLE_ENGLISH", "TITLE_ROMAJI", "NEXT_AIRING_AT", "SUB_COUNT", "DUB_COUNT",
];
pub const STATUSES: [&str; 5] = ["FINISHED", "RELEASING", "NOT_YET_RELEASED", "CANCELLED", "HIATUS"];
pub const FORMATS: [&str; 7] = ["TV", "TV_SHORT", "MOVIE", "SPECIAL", "OVA", "ONA", "MUSIC"];
pub const SEASONS: [&str; 4] = ["WINTER", "SPRING", "SUMMER", "FALL"];

const ANIME_BASE: &str = "query AnimeDetailBase($id: String, $anilistId: Int){anime(id:$id anilistId:$anilistId){id anilistId malId titleRomaji titleEnglish coverImage bannerImage backdropUrl description episodeCount status duration genres source format seasonYear season averageScore popularity studios}}";
const ANIME_SEASONS: &str = "query AnimeDetailSeasons($id: String, $anilistId: Int){anime(id:$id anilistId:$anilistId){id seasons{animeId anilistId title image coverImage episodeCount type rating}}}";
const ANIME_RELATIONS: &str = "query AnimeDetailRelations($id: String, $anilistId: Int){anime(id:$id anilistId:$anilistId){id relations{animeId anilistId title image coverImage episodeCount type rating}}}";
const ANIME_RECS: &str = "query AnimeDetailRecommendations($id: String, $anilistId: Int){anime(id:$id anilistId:$anilistId){id recommendations{animeId anilistId title image coverImage episodeCount type rating}}}";
const ANIME_CHARS: &str = "query AnimeDetailCharacters($id: String, $anilistId: Int){anime(id:$id anilistId:$anilistId){id characters}}";

pub fn b64(u: &str) -> String {
    B64.encode(u)
}

/// Ambil URL gambar: string langsung, atau object {extraLarge,large,medium,...}.
pub fn img_url(v: &Value) -> &str {
    if let Some(s) = v.as_str() {
        return s;
    }
    if let Some(o) = v.as_object() {
        for k in ["extraLarge", "large", "medium", "image", "url", "src", "cover", "banner"] {
            if let Some(s) = o.get(k).and_then(|x| x.as_str()).filter(|s| !s.is_empty()) {
                return s;
            }
        }
    }
    ""
}
pub fn px(u: &str) -> String {
    if u.is_empty() {
        return String::new();
    }
    format!("/api/fetch?u={}", b64(u))
}

/// Host privat/loopback/metadata dilarang (anti-SSRF). Sisanya bebas
/// karena host CDN video/gambar berotasi (dari y.txt: silentvoyage, moonridge, lunar, ...).
fn blocked_host(h: &str) -> bool {
    let h = h.to_lowercase();
    if h == "localhost" || h.ends_with(".localhost") || h.ends_with(".local") || h.ends_with(".internal")
        || h == "0.0.0.0" || h == "::1" || h == "[::1]" || h.contains("metadata.google") { return true; }
    let p: Vec<&str> = h.split('.').collect();
    match p.as_slice() {
        ["127", _, _, _] | ["10", _, _, _] | ["192", "168", _, _] | ["169", "254", _, _] => true,
        ["172", b, _, _] => b.parse::<u8>().is_ok_and(|n| (16..=31).contains(&n)),
        _ => false,
    }
}

pub fn decode_target(u: &str) -> Result<String, AppError> {
    let raw = B64.decode(u).map_err(|_| AppError::BadRequest("u bukan base64".to_string()))?;
    let s = String::from_utf8(raw).map_err(|_| AppError::BadRequest("u bukan utf8".to_string()))?;
    let host = s.split("://").nth(1).unwrap_or("").split('/').next().unwrap_or("").rsplit('@').next().unwrap_or("");
    if !(s.starts_with("https://") || s.starts_with("http://")) || host.is_empty() || blocked_host(host) {
        return Err(AppError::BadRequest("host tidak diizinkan".to_string()));
    }
    Ok(s)
}

pub fn client(cfg: &Config) -> Result<reqwest::Client, AppError> {
    reqwest::Client::builder()
        .user_agent(cfg.user_agent)
        .default_headers({
            let mut h = reqwest::header::HeaderMap::new();
            h.insert("Referer", cfg.referer.parse().map_err(|e| AppError::Upstream(format!("{e:?}")))?);
            h.insert("Origin", cfg.origin.parse().map_err(|e| AppError::Upstream(format!("{e:?}")))?);
            h
        })
        .build()
        .map_err(|e| AppError::Upstream(e.to_string()))
}

/// `catalogAnime` generik: filter/sort upstream apa adanya.
pub async fn catalog(client: &reqwest::Client, cfg: &Config, filter: &Value, sort: &str, direction: &str, limit: i64, offset: i64) -> Result<Vec<AnimeItem>, AppError> {
    let r: Value = client
        .post(cfg.graphql_url)
        .json(&serde_json::json!({"query": CATALOG_QUERY, "variables": {"filter": filter, "sort": [{"field": sort, "direction": direction}], "limit": limit, "offset": offset}}))
        .send()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))?
        .json()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))?;
    serde_json::from_value(r["data"]["catalogAnime"]["items"].clone()).map_err(|e| AppError::Upstream(e.to_string()))
}

pub async fn search(client: &reqwest::Client, cfg: &Config, title: &str) -> Result<Vec<AnimeItem>, AppError> {
    catalog(client, cfg, &serde_json::json!({"query": title}), "POPULARITY", "DESC", 24, 0).await
}

pub async fn episodes(client: &reqwest::Client, cfg: &Config, id: &str) -> Result<Value, AppError> {
    client
        .get(format!("{}/episodes?id={id}", cfg.api_base))
        .send()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))?
        .json()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))
}

pub async fn sources(client: &reqwest::Client, cfg: &Config, id: &str, ep: i64, r#type: &str, provider: &str) -> Result<Value, AppError> {
    client
        .get(format!("{}/sources?id={id}&epNum={ep}&type={type}&providerId={provider}", cfg.api_base, type = r#type, provider = provider))
        .send()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))?
        .json()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))
}

pub async fn servers(client: &reqwest::Client, cfg: &Config, id: &str, ep: i64) -> Result<Value, AppError> {
    client
        .get(format!("{}/servers?id={id}&epNum={ep}", cfg.api_base))
        .send()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))?
        .json()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))
}

pub async fn anime(client: &reqwest::Client, cfg: &Config, id: Option<&str>, anilist_id: Option<i64>, section: &str) -> Result<Value, AppError> {
    let q = match section {
        "seasons" => ANIME_SEASONS,
        "relations" => ANIME_RELATIONS,
        "recommendations" => ANIME_RECS,
        "characters" => ANIME_CHARS,
        _ => ANIME_BASE,
    };
    let r: Value = client
        .post(cfg.graphql_url)
        .json(&serde_json::json!({"query": q, "variables": {"id": id, "anilistId": anilist_id}}))
        .send()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))?
        .json()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))?;
    Ok(r["data"]["anime"].clone())
}

pub async fn skiptimes(client: &reqwest::Client, mal: i64, ep: i64, len: Option<f64>) -> Result<Value, AppError> {
    let mut url = format!("https://api.aniskip.com/v2/skip-times/{mal}/{ep}?types[]=op&types[]=ed&types[]=mixed-op&types[]=mixed-ed&types[]=recap");
    if let Some(l) = len {
        url += &format!("&episodeLength={l}");
    }
    client.get(url).send().await.map_err(|e| AppError::Upstream(e.to_string()))?.json().await.map_err(|e| AppError::Upstream(e.to_string()))
}

pub async fn comments(client: &reqwest::Client, media: &str, offset: u32, sort: &str, limit: u32) -> Result<Value, AppError> {
    client
        .get(format!("https://theanimecommunity.com/api/v1/comments/{media}?offset={offset}&sortBy={sort}&limit={limit}"))
        .send()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))?
        .json()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))
}

pub async fn episode_meta(client: &reqwest::Client, anilist_id: i64, ep: i64) -> Result<Value, AppError> {
    client
        .get(format!("https://theanimecommunity.com/api/v1/episodes/mediaItemID?AniList_ID={anilist_id}&mediaType=anime&episodeChapterNumber={ep}"))
        .send()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))?
        .json()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))
}

pub async fn anilist(client: &reqwest::Client, query: &str, variables: Option<Value>) -> Result<Value, AppError> {
    client
        .post("https://graphql.anilist.co/")
        .json(&serde_json::json!({"query": query, "variables": variables}))
        .send()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))?
        .json()
        .await
        .map_err(|e| AppError::Upstream(e.to_string()))
}

/// Fallback ekstensi bila sniff tak kenal isi. Label CDN sering bohong
/// (segmen TS dinamai .jpg) → sniff selalu menang di fetch_stream.
fn mime_for(url: &str) -> Option<String> {
    let path = url.split(['?', '#']).next().unwrap_or("");
    let ext = path.rsplit('.').next().unwrap_or("").to_lowercase();
    match ext.as_str() {
        "ts" => Some("video/mp2t"),
        "m4s" => Some("video/iso.segment"),
        "mp4" => Some("video/mp4"),
        "webm" => Some("video/webm"),
        "vtt" => Some("text/vtt"),
        "m3u8" => Some("application/vnd.apple.mpegurl"),
        "mpd" => Some("application/dash+xml"),
        "jpg" | "jpeg" => Some("image/jpeg"),
        "png" => Some("image/png"),
        "webp" => Some("image/webp"),
        "gif" => Some("image/gif"),
        _ => None,
    }
    .map(str::to_string)
}

/// Tebak tipe dari isi (magic bytes). Dipakai dulu — ekstensi/label CDN sering bohong
/// (segmen TS dinamai .jpg, .vtt → octet-stream).
fn sniff(b: &[u8]) -> Option<&'static str> {
    if b.starts_with(b"WEBVTT") {
        Some("text/vtt")
    } else if b.starts_with(b"#EXTM3U") {
        Some("application/vnd.apple.mpegurl")
    } else if b.first() == Some(&0x47) && (b.len() < 189 || b[188] == 0x47) {
        Some("video/mp2t") // sync-byte TS; chunk pertama bisa <188B → single-sync cukup
    } else if b.len() > 8 && &b[4..8] == b"ftyp" {
        Some("video/mp4")
    } else if b.starts_with(&[0xFF, 0xD8, 0xFF]) {
        Some("image/jpeg")
    } else if b.starts_with(&[0x89, 0x50, 0x4E, 0x47]) {
        Some("image/png")
    } else if b.starts_with(b"RIFF") && b.len() > 12 && &b[8..12] == b"WEBP" {
        Some("image/webp")
    } else if b.starts_with(b"GIF8") {
        Some("image/gif")
    } else {
        None
    }
}
fn def_headers(origin: Option<&str>, referer: Option<&str>) -> (String, String) {
    (
        origin.unwrap_or("https://megaplay.buzz").to_string(),
        referer.unwrap_or("https://megaplay.buzz/").to_string(),
    )
}

pub struct Fetched {
    pub status: u16,
    pub ctype: String,
    pub body: Vec<u8>,
}

pub struct Streamed {
    pub status: u16,
    pub ctype: String,
    pub crange: Option<String>,
    pub clen: Option<String>,
    pub stream: Chunks,
}

type Chunk = Result<bytes::Bytes, AppError>;
type Chunks = Pin<Box<dyn futures_util::Stream<Item = Chunk> + Send>>;

fn to_err(e: reqwest::Error) -> AppError {
    AppError::Upstream(e.to_string())
}

/// Streaming tanpa buffer penuh → hemat memory/CPU (body mengalir per chunk).
/// Selalu minta `identity` ke upstream: `bytes_stream` tanpa decode, jadi sniff
/// melihat byte asli (gzip merusak sniff → label salah). Video/gambar sudah
/// terkompresi → tanpa gzip justru hemat CPU dua sisi.
pub async fn fetch_stream(client: &reqwest::Client, url: &str, origin: Option<&str>, referer: Option<&str>, range: Option<String>) -> Result<Streamed, AppError> {
    let (o, f) = def_headers(origin, referer);
    let mut r = client.get(url).header("Origin", o).header("Referer", f).header("Accept-Encoding", "identity");
    if let Some(g) = range {
        r = r.header("Range", g);
    }
    let res = r.send().await.map_err(|e| AppError::Upstream(e.to_string()))?;
    let status = res.status().as_u16();
    let h = res.headers().clone();
    let upstream_ct = h.get("content-type").and_then(|v| v.to_str().ok());
    let crange = h.get("content-range").and_then(|v| v.to_str().ok()).map(str::to_string);
    let clen = h.get("content-length").and_then(|v| v.to_str().ok()).map(str::to_string);
    // Intip prefix ≤2KB (bisa lintas chunk) → sniff magic bytes (CDN salah label:
    // segmen TS dinamai .jpg). Sniff kalah → ekstensi → label upstream → octet-stream.
    // Body utuh, tak berubah.
    let mut s = res.bytes_stream();
    let mut prefix: Vec<bytes::Bytes> = Vec::new();
    let mut plen = 0usize;
    while plen < 2048 {
        match s.next().await {
            Some(Ok(c)) => {
                plen += c.len();
                prefix.push(c);
            }
            Some(Err(e)) => return Err(to_err(e)),
            None => break,
        }
        if prefix.len() > 32 {
            break;
        }
    }
    let flat: Vec<u8> = prefix.iter().flat_map(|c| c.iter().copied()).take(2048).collect();
    let t = sniff(&flat)
        .map(str::to_string)
        .or_else(|| mime_for(url))
        .or_else(|| upstream_ct.map(str::to_string))
        .unwrap_or_else(|| "application/octet-stream".to_string());
    let head = futures_util::stream::iter(prefix.into_iter().map(Ok::<_, AppError>));
    let tail = s.map(|r| r.map_err(to_err));
    let stream = Box::pin(head.chain(tail));
    Ok(Streamed { status, ctype: t, crange, clen, stream })
}

pub async fn fetch_upstream(client: &reqwest::Client, url: &str, origin: Option<&str>, referer: Option<&str>, range: Option<String>) -> Result<Fetched, AppError> {
    let (o, f) = def_headers(origin, referer);
    let mut r = client.get(url).header("Origin", o).header("Referer", f);
    if let Some(g) = range {
        r = r.header("Range", g);
    }
    let res = r.send().await.map_err(|e| AppError::Upstream(e.to_string()))?;
    let status = res.status().as_u16();
    let ctype = res.headers().get("content-type").and_then(|v| v.to_str().ok()).unwrap_or("application/octet-stream").to_string();
    let body = res.bytes().await.map_err(|e| AppError::Upstream(e.to_string()))?.to_vec();
    Ok(Fetched { status, ctype, body })
}

/// Path proxy segmen. Wajib berakhiran ekstensi: player web menentukan tipe media
/// dari ekstensi URL (`{.ts → video/mp2t, .aac → audio/aac}`); tanpa itu ia salah pilih
/// jalur MSE dan append gagal (video tak pernah mulai).
fn seg_path(abs: &str, ext: &str) -> String {
    let name = abs.split(['?', '#']).next().unwrap_or("").rsplit('/').next().unwrap_or("seg");
    let stem = name.split('.').next().unwrap_or("seg");
    format!("/api/fetch/{stem}{ext}?u={}", b64(abs))
}

/// Tulis ulang playlist m3u8 → semua URI menunjuk /api/hls atau /api/fetch (base64).
pub fn rewrite_playlist(base: &str, t: &str, origin: Option<&str>, referer: Option<&str>) -> String {
    let suf = match (origin, referer) {
        (Some(o), Some(r)) if !o.contains(['&', '?']) && !r.contains(['&', '?']) => format!("&origin={o}&referer={r}"),
        _ => String::new(),
    };
    let ext = if t.contains("#EXT-X-MAP") { ".mp4" } else { ".ts" };
    let wrap = |abs: &str| {
        if is_m3u8(abs) { format!("/api/hls?u={}{suf}", b64(abs)) } else { format!("{}{suf}", seg_path(abs, ext)) }
    };
    let wrap_attr = |abs: &str| {
        if is_m3u8(abs) { format!("/api/hls?u={}{suf}", b64(abs)) } else { format!("/api/fetch?u={}{suf}", b64(abs)) }
    };
    t.lines()
        .map(|line| {
            let l = line.trim();
            if l.is_empty() {
                return String::new();
            }
            if l.starts_with('#') {
                let mut out = String::with_capacity(l.len() + 64);
                let mut rest = l;
                while let Some(i) = rest.find("URI=\"") {
                    let start = i + 5;
                    match rest[start..].find('"') {
                        Some(end) => {
                            out.push_str(&rest[..start]);
                            out.push_str(&wrap_attr(&resolve(base, &rest[start..start + end])));
                            out.push('"');
                            rest = &rest[start + end + 1..];
                        }
                        None => break,
                    }
                }
                out.push_str(rest);
                out
            } else {
                wrap(&resolve(base, l))
            }
        })
        .collect::<Vec<_>>()
        .join("\n")
}

fn base_of(u: &str) -> &str {
    u.rsplit_once('/').map(|(b, _)| b).unwrap_or(u)
}

fn resolve(base: &str, r: &str) -> String {
    if r.starts_with("http") { r.to_string() } else { format!("{base}/{}", r.trim_start_matches('/')) }
}

/// Cek ekstensi abaikan query/fragment (?token=…), biar varian tak dikira segmen.
fn is_m3u8(u: &str) -> bool {
    u.split(['?', '#']).next().is_some_and(|p| p.ends_with(".m3u8"))
}

fn playlist_urls(t: &str) -> Vec<String> {
    let l: Vec<&str> = t.lines().map(str::trim).filter(|x| !x.is_empty()).collect();
    let mut o: Vec<String> = l
        .windows(2)
        .filter(|w| w[0].starts_with("#EXT-X-STREAM-INF") && !w[1].starts_with('#'))
        .map(|w| w[1].to_string())
        .collect();
    if o.is_empty() {
        o = l.iter().filter(|x| !x.starts_with('#')).map(|x| x.to_string()).collect();
    }
    o
}

/// Master m3u8 cuma playlist (~ratusan byte) → resolve ke segmen video, ukur 1MB.
/// Return (source_url, probe_bytes). Gagal semua → NotFound.
pub async fn probe_stream(client: &reqwest::Client, s: &Value) -> Result<(String, usize), AppError> {
    let origin = s["headers"]["Origin"].as_str().unwrap_or("https://megaplay.buzz");
    let referer = s["headers"]["Referer"].as_str().unwrap_or("https://megaplay.buzz/");
    let srcs = s["sources"].as_array().cloned().unwrap_or_default();
    for x in &srcs {
        let u = x["url"].as_str().unwrap_or_default();
        if u.is_empty() {
            continue;
        }
        let mt = match client.get(u).header("Origin", origin).header("Referer", referer).send().await {
            Ok(r) => r.text().await.unwrap_or_default(),
            Err(_) => continue,
        };
        if !mt.contains("#EXTM3U") {
            continue; // blokir Cloudflare / bukan playlist
        }
        let media = playlist_urls(&mt).last().map(|v| resolve(base_of(u), v)).unwrap_or_else(|| u.to_string());
        let seg = if is_m3u8(&media) {
            match client.get(&media).header("Origin", origin).header("Referer", referer).send().await {
                Ok(r) => {
                    let t = r.text().await.unwrap_or_default();
                    let b = base_of(&media).to_string();
                    playlist_urls(&t).into_iter().next().map(|v| resolve(&b, &v)).unwrap_or(media)
                }
                Err(_) => continue,
            }
        } else {
            media
        };
        match client.get(&seg).header("Origin", origin).header("Referer", referer).header("Range", "bytes=0-1048575").send().await {
            Ok(r) if r.status().is_success() || r.status().as_u16() == 206 => {
                let n = r.bytes().await.map(|b| b.len()).unwrap_or(0);
                if n > 0 {
                    return Ok((u.to_string(), n));
                }
            }
            _ => continue,
        }
    }
    Err(AppError::NotFound("no playable source".to_string()))
}

// --- Subtitle ID via Groq (Qwen). Batch 6 cue/call, jeda 2.5 dtk
// (limit 30 req/mnt, 8K token/mnt, 1K req/hari) → progresif per batch.
const GROQ_URL: &str = "https://api.groq.com/openai/v1/chat/completions";
const GROQ_MODEL: &str = "qwen/qwen3.8-27b";
pub const SUB_BATCH: usize = 10;

/// Antrean subtitle ID: 1 permit → 1 video diterjemahkan dalam satu waktu,
/// serial FIFO antar user, tak menabrak limit Groq.
pub struct SubJob {
    pub state: String, // queued|ready|working|done|failed
    pub err: Option<String>,
    pub head: String,
    pub cues: Vec<Vec<String>>,
    pub dlg: Vec<usize>, // indeks cue dialog (ada "-->")
    pub id: HashMap<usize, String>, // cue idx → teks ID
    pub cache: String, // path cache disk (.cache/subtitles)
}

pub struct SubJobs {
    pub sem: tokio::sync::Semaphore,
    pub mu: std::sync::Mutex<HashMap<String, SubJob>>,
    pub order: std::sync::Mutex<VecDeque<String>>,
}

impl SubJobs {
    pub fn new() -> Self {
        Self { sem: tokio::sync::Semaphore::new(1), mu: std::sync::Mutex::new(HashMap::new()), order: std::sync::Mutex::new(VecDeque::new()) }
    }
}

impl Default for SubJobs {
    fn default() -> Self {
        Self::new()
    }
}

async fn groq_chat(client: &reqwest::Client, key: &str, user: &str) -> Result<String, AppError> {
    let mut wait = 5;
    for _ in 0..4 {
        let r = client
            .post(GROQ_URL)
            .bearer_auth(key)
            .timeout(std::time::Duration::from_secs(120))
            .json(&serde_json::json!({"model": GROQ_MODEL, "temperature": 0, "max_completion_tokens": 1024, "messages": [{"role": "system", "content": "Kamu adalah penerjemah subtitle profesional Inggris → Indonesia untuk anime. Standar: akurat, natural, mudah dibaca (FAR Pedersen 2017). Maksimal 20 CPS, 42 karakter per baris, maksimal 2 baris per subtitle. Pertahankan honorifik Jepang (san, chan, kun, sama, senpai, sensei) dan nama asli. Jaga register bicara tiap karakter. Adaptasi wordplay, jangan literal. Italic untuk lagu, telepon, dan bisikan. Jangan menambah atau mengurangi makna, jangan spoiler, hindari bahasa gaul usang dan kalimat kaku."}, {"role": "user", "content": user}]}))
            .send().await.map_err(|e| AppError::Upstream(e.to_string()))?;
        if r.status().as_u16() == 429 {
            tokio::time::sleep(std::time::Duration::from_secs(wait)).await;
            wait *= 2;
            continue;
        }
        let v: Value = r.json().await.map_err(|e| AppError::Upstream(e.to_string()))?;
        if let Some(e) = v["error"]["message"].as_str() {
            return Err(AppError::Upstream(format!("groq: {e}")));
        }
        let finish = v["choices"][0]["finish_reason"].as_str().unwrap_or("?").to_string();
        return v["choices"][0]["message"]["content"].as_str().map(str::to_string)
            .filter(|s| !s.is_empty())
            .ok_or_else(|| AppError::Upstream(format!("groq tanpa content ({finish})")));
    }
    Err(AppError::Upstream("groq rate limit, coba lagi".to_string()))
}

/// Cache disk `.cache/subid/<hash-en>.vtt` → tonton ulang instan, hemat limit.
pub fn sub_cache_path(t: &str) -> std::path::PathBuf {
    use std::collections::hash_map::DefaultHasher;
    use std::hash::{Hash, Hasher};
    let mut h = DefaultHasher::new();
    t.hash(&mut h);
    std::path::PathBuf::from(format!(".cache/subtitles/{:016x}.vtt", h.finish()))
}

/// VTT → (head, cues). Cue tanpa "-->" (NOTE/STYLE) ikut lolos utuh saat build.
pub fn parse_vtt(t: &str) -> (String, Vec<Vec<String>>) {
    let t = t.replace("\r\n", "\n");
    let mut blocks = t.split("\n\n");
    let head = blocks.next().unwrap_or("WEBVTT").to_string();
    let cues: Vec<Vec<String>> = blocks.map(|b| b.lines().map(str::to_string).collect()).filter(|v: &Vec<String>| !v.is_empty()).collect();
    (head, cues)
}

/// Baris teks cue dialog (per cue, struktur baris dipertahankan).
pub fn cue_lines(c: &[String]) -> Option<Vec<String>> {
    c.iter().position(|l| l.contains("-->")).map(|p| c[p + 1..].iter().filter(|l| !l.trim().is_empty()).cloned().collect()).filter(|v: &Vec<String>| !v.is_empty())
}

/// Rakit VTT dari map terjemahan; cue belum diterjemahkan fallback EN.
pub fn build_vtt(head: &str, cues: &[Vec<String>], id: &HashMap<usize, String>) -> String {
    let mut out = vec![head.to_string()];
    for (i, c) in cues.iter().enumerate() {
        match c.iter().position(|l| l.contains("-->")) {
            Some(p) => {
                let mut v: Vec<String> = c[..=p].to_vec();
                v.push(id.get(&i).cloned().unwrap_or_else(|| c[p + 1..].join(" ")));
                out.push(v.join("\n"));
            }
            None => out.push(c.join("\n")),
        }
    }
    out.join("\n\n") + "\n"
}

/// VTT hanya cue terjemahan (cue belum terjemah dilewati, tanpa fallback EN).
pub fn build_vtt_id(head: &str, cues: &[Vec<String>], id: &HashMap<usize, String>) -> String {
    let mut out = vec![head.to_string()];
    for (i, c) in cues.iter().enumerate() {
        let Some(p) = c.iter().position(|l| l.contains("-->")) else { continue };
        if let Some(txt) = id.get(&i) {
            out.push(vec![c[p].clone(), txt.clone()].join("\n"));
        }
    }
    out.join("\n\n") + "\n"
}

/// Detik awal timestamp cue ("00:01:02.500 --> ..."). Gagal parse → 0.
pub fn ts_start(line: &str) -> f64 {
    let start = line.split("-->").next().unwrap_or("").trim();
    let mut p = start.split(':').rev();
    let s: f64 = p.next().unwrap_or("0").parse().unwrap_or(0.0);
    let m: f64 = p.next().unwrap_or("0").parse().unwrap_or(0.0);
    let h: f64 = p.next().unwrap_or("0").parse().unwrap_or(0.0);
    h * 3600.0 + m * 60.0 + s
}

/// Detik akhir timestamp cue ("00:01:02.500 --> ..."). Gagal parse → 0.
pub fn ts_end(line: &str) -> f64 {
    let end = line.split("-->").nth(1).unwrap_or("").trim();
    let mut p = end.split(':').rev();
    let s: f64 = p.next().unwrap_or("0").parse().unwrap_or(0.0);
    let m: f64 = p.next().unwrap_or("0").parse().unwrap_or(0.0);
    let h: f64 = p.next().unwrap_or("0").parse().unwrap_or(0.0);
    h * 3600.0 + m * 60.0 + s
}
/// Baris lanjutan (tanpa `[N]`) ditempel ke cue aktif. Cue gagal dilewati (EN).
pub async fn translate_batch(client: &reqwest::Client, key: &str, jobs: &[(usize, Vec<String>)]) -> Result<HashMap<usize, String>, AppError> {
    let q = jobs.iter().map(|(i, ls)| format!("[{i}] {}", ls.join("\n"))).collect::<Vec<_>>().join("\n");
    let a = groq_chat(client, key, &format!("Translate each cue to Indonesian, keeping its line breaks. Reply ONLY as [N] <indonesian>, same order:\n{q}")).await?;
    let mut out = HashMap::new();
    let mut cur: Option<usize> = None;
    for line in a.lines().map(str::trim).filter(|l| !l.is_empty()) {
        if let Some((k, txt)) = line.strip_prefix('[').and_then(|r| r.split_once(']')).and_then(|(k, t)| k.parse::<usize>().ok().map(|k| (k, t.trim()))) {
            cur = Some(k);
            if !txt.is_empty() {
                out.insert(k, txt.to_string());
            }
        } else if let Some(k) = cur {
            out.entry(k).and_modify(|s| {
                s.push('\n');
                s.push_str(line);
            });
        }
    }
    Ok(out)
}
