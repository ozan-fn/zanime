//! Tes **live** (butuh jaringan) untuk alur tonton dari `y.txt`:
//! provider `yuki` (`type=sub`) dan host player `minky.anistream.one`.
//!
//! Asal data di `y.txt` (HAR `anistream.one/watch/…-101371-episode-1`): id anime,
//! episode 1, provider `yuki`, dan bentuk URL host minky
//! `minky.anistream.one/fetch?url=<url di-encode>&origin=https%3A%2F%2Fmegaplay.buzz`
//! (varian & segmennya lewat `minky.anistream.one/?u=<blob>`).
//!
//! Semua tes di sini `#[ignore]`: kena upstream nyata + token m3u8 berumur pendek,
//! jadi `cargo test` biasa tetap offline & hijau. Jalankan:
//!   cargo test --test stream_live -- --ignored --nocapture

use actix_web::{
    body::MessageBody,
    dev::{Service, ServiceResponse},
    http::{StatusCode, header::HeaderMap},
    test, web,
};
use zanime::{config::Config, create_app, upstream};

/// y.txt: `watch/ms-vampire-who-lives-in-my-neighborhood-101371-episode-1`.
const ID: &str = "ms-vampire-who-lives-in-my-neighborhood-3l281";
const EP: i64 = 1;
/// Provider & tipe di y.txt (`providerId=yuki`, `type=sub`).
const PROVIDER: &str = "yuki";
/// Panjang episode (dtk) untuk aniskip (aniskip menolak permintaan tanpa ini).
const LEN: f64 = 1440.0;
/// Anime di y.txt tak punya data di aniskip (dicek: MAL 37579 semua ep → 404);
/// MAL 21 (One Piece) ep 1 punya → dipakai membuktikan pipenya jalan.
const MAL: i64 = 37579;
const MAL_ADA: i64 = 21;
const MEGA: &str = "https://megaplay.buzz";

// --- helper

/// GET lewat service test (tanpa bind port), balikin status + header + body.
async fn get<S, R, B>(app: &S, req: R) -> (StatusCode, HeaderMap, web::Bytes)
where
    S: Service<R, Response = ServiceResponse<B>, Error = actix_web::Error>,
    B: MessageBody,
{
    let res = test::call_service(app, req).await;
    let status = res.status();
    let head = res.headers().clone();
    (status, head, test::read_body(res).await)
}

/// `get!(app, "/api/x")` — bentuk ringkas `TestRequest` + `get`.
macro_rules! get {
    ($app:expr, $uri:expr) => {
        get($app, test::TestRequest::get().uri($uri).to_request()).await
    };
}

/// `first_segment!(app, playlist)` — ikuti playlist dari endpoint aplikasi sampai
/// segmen pertama: master → varian → segmen (playlist media gampang dikenali dari
/// `#EXT-X-TARGETDURATION`). Makro, bukan fungsi: tipe service dari `init_service`
/// opaque, jadi bound generiknya harus diselesaikan di tempat pemakaian.
macro_rules! first_segment {
    ($app:expr, $playlist:expr) => {{
        let mut uri = $playlist.to_string();
        let mut seg = None;
        for _ in 0..3 {
            let (st, _, b) = get!($app, &uri);
            let text = String::from_utf8_lossy(&b).to_string();
            assert_eq!(st, StatusCode::OK, "{uri} → {st}: {text}");
            let next = first_uri(&text);
            assert!(!next.is_empty(), "playlist tanpa URI: {uri}");
            if text.contains("#EXT-X-TARGETDURATION") {
                seg = Some(get!($app, &next));
                break;
            }
            uri = next;
        }
        seg.expect("playlist terlalu dalam (lebih dari 3 lompatan)")
    }};
}

/// URI pertama non-komentar (varian saat master, segmen saat playlist media).
fn first_uri(t: &str) -> String {
    t.lines()
        .map(str::trim)
        .find(|l| !l.is_empty() && !l.starts_with('#'))
        .unwrap_or("")
        .to_string()
}

/// URL di dalam playlist minky ditulis relatif (`/?u=…`) → jadikan absolut.
fn abs_minky(u: &str) -> String {
    if u.starts_with("http") {
        u.to_string()
    } else {
        format!("https://minky.anistream.one{u}")
    }
}

/// Persis bentuk di y.txt: `minky.anistream.one/fetch?url=<target>&origin=megaplay`.
fn minky_fetch(target: &str) -> String {
    format!(
        "https://minky.anistream.one/fetch?url={}&origin={}",
        enc(target),
        enc(MEGA)
    )
}

/// Percent-encode ketat untuk menitipkan URL di query (tanpa dependensi baru).
fn enc(s: &str) -> String {
    s.bytes()
        .map(|b| match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                (b as char).to_string()
            }
            _ => format!("%{b:02X}"),
        })
        .collect()
}

/// Sumber + trek Inggris fresh dari provider `yuki` (token di y.txt sudah kedaluwarsa).
async fn yuki_source(cfg: &Config, http: &reqwest::Client) -> (String, String) {
    let s = upstream::sources(http, cfg, ID, EP, "sub", PROVIDER)
        .await
        .unwrap_or_else(|e| panic!("sources {PROVIDER}: {e}"));
    let src = s["sources"][0]["url"]
        .as_str()
        .unwrap_or_else(|| panic!("tanpa sumber: {s}"))
        .to_string();
    let en = s["tracks"]
        .as_array()
        .and_then(|t| {
            t.iter()
                .find(|x| {
                    x["lang"]
                        .as_str()
                        .unwrap_or("")
                        .to_ascii_lowercase()
                        .contains("english")
                })
                .and_then(|x| x["url"].as_str())
        })
        .unwrap_or_else(|| panic!("tanpa trek english: {s}"))
        .to_string();
    (src, en)
}

/// GET mentah ke host (header anistream.one, sama seperti y.txt).
async fn raw(cfg: &Config, http: &reqwest::Client, url: &str) -> upstream::Fetched {
    upstream::fetch_upstream(http, url, Some(cfg.origin), Some(cfg.referer), None)
        .await
        .unwrap_or_else(|e| panic!("{url}: {e}"))
}

fn head(t: &str) -> String {
    t.chars().take(160).collect()
}

/// Auto skip dari aniskip lewat endpoint kita. `ada=false` untuk anime yang memang
/// belum terdaftar di aniskip: respons harus tetap sehat (bukan error).
fn assert_skip(v: &serde_json::Value, ada: bool) {
    let r = v["results"].as_array().unwrap_or_else(|| panic!("{v}"));
    assert_eq!(r.is_empty(), !ada, "hasil skip tak sesuai harapan: {v}");
    let Some(i) = r.first().map(|x| &x["interval"]) else {
        println!("auto skip ok: kosong (aniskip belum punya data) — {v}");
        return;
    };
    let (a, z) = (
        i["startTime"].as_f64().unwrap(),
        i["endTime"].as_f64().unwrap(),
    );
    assert!(a >= 0.0 && z > a, "interval aneh: {v}");
    println!("auto skip ok: {} {a}..{z}", r[0]["skipType"]);
}

/// Skip data bawaan provider (`chapters` Intro/Outro) — inilah auto skip yang ada
/// untuk anime di y.txt, karena aniskip tidak punya datanya.
fn assert_chapters(s: &serde_json::Value) {
    let c = s["chapters"]
        .as_array()
        .unwrap_or_else(|| panic!("tanpa chapters: {s}"));
    let has = |nama: &str| {
        c.iter().any(|x| {
            x["title"].as_str().unwrap_or("").eq_ignore_ascii_case(nama)
                && x["end"].as_f64().unwrap_or(0.0) > x["start"].as_f64().unwrap_or(0.0)
        })
    };
    assert!(has("intro") && has("outro"), "chapters tak lengkap: {s}");
    println!("chapters ok: {c:?}");
}

// --- tes

/// yuki: subtitle Inggris, stream, dan auto skip — tiga bahan halaman putar,
/// lewat endpoint backend apa adanya.
#[actix_web::test]
#[ignore = "live upstream (yuki + aniskip)"]
async fn yuki_sub_subtitle_en_stream_auto_skip() {
    let cfg = Config::default();
    let http = upstream::client(&cfg).unwrap();
    let app = test::init_service(create_app(
        cfg.clone(),
        http.clone(),
        web::Data::new(upstream::SubJobs::new()),
    ))
    .await;

    // /api/stream: provider yuki + type sub (dua-duanya default).
    let (st, _, b) = get!(&app, &format!("/api/stream?id={ID}&ep={EP}"));
    assert_eq!(st, StatusCode::OK, "{}", String::from_utf8_lossy(&b));
    let v: serde_json::Value = serde_json::from_slice(&b).unwrap();
    let stream = v["stream"].as_str().unwrap().to_string();
    let sub_en = v["sub_en"].as_str().unwrap().to_string();
    assert!(stream.starts_with("/api/hls?u="), "{stream}");
    assert!(sub_en.starts_with("/api/fetch?u="), "{sub_en}");
    assert!(
        !v["sub_id"].as_str().unwrap().is_empty(),
        "trek Indonesia kosong: {v}"
    );

    // Subtitle Inggris: VTT nyata, bukan sekadar URL.
    let (st, h, b) = get!(&app, &sub_en);
    assert_eq!(st, StatusCode::OK, "sub_en {st}");
    assert_eq!(
        h.get("content-type").and_then(|v| v.to_str().ok()),
        Some("text/vtt"),
        "label sub salah"
    );
    let vtt = String::from_utf8_lossy(&b);
    assert!(
        vtt.starts_with("WEBVTT") && vtt.contains("-->"),
        "bukan VTT: {}",
        head(&vtt)
    );

    // Stream: master → varian → segmen pertama harus MPEG-TS.
    let (st, _, seg) = first_segment!(&app, &stream);
    assert_eq!(st, StatusCode::OK, "segmen {st}");
    assert_eq!(
        seg.first(),
        Some(&0x47),
        "segmen bukan TS ({} B)",
        seg.len()
    );

    // Auto skip: MAL id dari /api/anime → /api/skiptimes (aniskip), plus `chapters`
    // bawaan provider untuk anime ini.
    let (st, _, b) = get!(&app, &format!("/api/anime?id={ID}"));
    assert_eq!(st, StatusCode::OK, "{}", String::from_utf8_lossy(&b));
    let mal = serde_json::from_slice::<serde_json::Value>(&b).unwrap()["malId"]
        .as_i64()
        .expect("malId");
    assert_eq!(mal, MAL, "malId dari y.txt berubah");
    for (m, ada) in [(MAL, false), (MAL_ADA, true)] {
        let uri = format!("/api/skiptimes?mal={m}&ep={EP}&len={LEN}");
        let (st, _, b) = get!(&app, &uri);
        assert_eq!(st, StatusCode::OK, "{uri}: {}", String::from_utf8_lossy(&b));
        assert_skip(&serde_json::from_slice(&b).unwrap(), ada);
    }
    let (st, _, b) = get!(
        &app,
        &format!("/api/sources?id={ID}&ep={EP}&type=sub&provider={PROVIDER}")
    );
    assert_eq!(st, StatusCode::OK, "{}", String::from_utf8_lossy(&b));
    assert_chapters(&serde_json::from_slice(&b).unwrap());

    println!("yuki ok: mal={mal} sub_en={sub_en}");
}

/// **Penetesan**: bisa tidak stream lewat host minky? Langsung ke host-nya
/// (tanpa backend kita) — master → varian → segmen + subtitle Inggris + auto skip.
#[actix_web::test]
#[ignore = "live upstream (host minky)"]
async fn minky_bisa_stream_langsung() {
    let cfg = Config::default();
    let http = upstream::client(&cfg).unwrap();
    let (src, en) = yuki_source(&cfg, &http).await;

    // Master lewat minky.
    let m = raw(&cfg, &http, &minky_fetch(&src)).await;
    assert_eq!(m.status, 200, "master minky: {}", m.status);
    let master = String::from_utf8(m.body).unwrap();
    assert!(master.contains("#EXTM3U"), "{}", head(&master));

    // Varian → playlist media (URI di dalamnya juga `?u=` minky).
    let var = abs_minky(&first_uri(&master));
    let v = raw(&cfg, &http, &var).await;
    assert_eq!(v.status, 200, "varian minky: {}", v.status);
    let media = String::from_utf8(v.body).unwrap();
    assert!(media.contains("#EXT-X-TARGETDURATION"), "{}", head(&media));

    // Segmen: label CDN bohong (`image/jpeg`) walau isinya TS.
    let seg = abs_minky(&first_uri(&media));
    let s = raw(&cfg, &http, &seg).await;
    assert_eq!(s.status, 200, "segmen minky: {}", s.status);
    assert_eq!(
        s.body.first(),
        Some(&0x47),
        "bukan TS ({} B, label {})",
        s.body.len(),
        s.ctype
    );

    // Subtitle Inggris lewat minky.
    let vtt = raw(&cfg, &http, &minky_fetch(&en)).await;
    assert_eq!(vtt.status, 200, "sub minky: {}", vtt.status);
    assert!(vtt.body.starts_with(b"WEBVTT"), "sub minky bukan VTT");

    // Auto skip lewat minky juga: MAL id dari detail anime → aniskip. Anime ini
    // memang belum terdaftar di aniskip; MAL_ADA memastikan pipenya benar-benar hidup.
    let a = upstream::anime(&http, &cfg, Some(ID), None, "base")
        .await
        .unwrap();
    let mal = a["malId"].as_i64().unwrap_or_else(|| panic!("malId: {a}"));
    assert_eq!(mal, MAL, "malId dari y.txt berubah");
    for (m, ada) in [(MAL, false), (MAL_ADA, true)] {
        assert_skip(
            &upstream::skiptimes(&http, m, EP, Some(LEN)).await.unwrap(),
            ada,
        );
    }

    println!(
        "minky ok: master {} B, varian {} B, segmen {} B (label {}), sub {} B, mal {mal}",
        master.len(),
        media.len(),
        s.body.len(),
        s.ctype,
        vtt.body.len()
    );
}

/// Penetesan kedua: jalur player kita — `/api/hls` + `/api/fetch` di atas URL
/// minky (varian & segmen minky tak berekstensi, jadi ini yang perlu dibuktikan).
#[actix_web::test]
#[ignore = "live upstream (host minky lewat /api/*)"]
async fn minky_lewat_api_kita() {
    let cfg = Config::default();
    let http = upstream::client(&cfg).unwrap();
    let app = test::init_service(create_app(
        cfg.clone(),
        http.clone(),
        web::Data::new(upstream::SubJobs::new()),
    ))
    .await;
    let (src, _) = yuki_source(&cfg, &http).await;

    let our = format!("/api/hls?u={}", upstream::b64(&minky_fetch(&src)));
    let (st, _, seg) = first_segment!(&app, &our);
    assert_eq!(st, StatusCode::OK, "segmen lewat API {st}");
    assert_eq!(
        seg.first(),
        Some(&0x47),
        "segmen minky lewat /api/fetch bukan TS ({} B)",
        seg.len()
    );
    // Auto skip di jalur ini dari aniskip yang sama (lihat tes yuki), jadi tak
    // diulang: yang diuji di sini khusus jalur playback-nya.
    println!("minky lewat /api/* ok: segmen {} B", seg.len());
}

/// Dua sisi fallback production (minky dulu → CDN provider/yuki): kandidat yang
/// mati **ditolak** probe (kalau diloloskan, fallback tak akan pernah jalan), dan
/// kandidat fallback (respons `sources` apa adanya) memang hidup & bisa diputar.
#[actix_web::test]
#[ignore = "live upstream (fallback minky → yuki)"]
async fn kandidat_mati_ditolak_dan_fallback_yuki_hidup() {
    let cfg = Config::default();
    let http = upstream::client(&cfg).unwrap();
    let src = upstream::sources(&http, &cfg, ID, EP, "sub", PROVIDER)
        .await
        .unwrap();

    // Kandidat 1 dibuat mati: bungkus minky diarahkan ke CDN yang tidak ada.
    let mut mati = src.clone();
    mati["sources"][0]["url"] =
        serde_json::json!(minky_fetch("https://nexabloom.invalid/x/master.m3u8"));
    assert!(
        upstream::probe_stream(&http, &mati).await.is_err(),
        "kandidat mati malah dianggap hidup → fallback tak akan jalan"
    );

    // Kandidat 2 = `sources` apa adanya (CDN provider). Inilah URL yang dipakai
    // `/api/stream` saat minky gagal — harus hidup dan bukan minky lagi.
    let (url, n) = upstream::probe_stream(&http, &src)
        .await
        .expect("CDN provider harus hidup");
    assert!(n > 0, "probe tanpa byte: {n}");
    assert_eq!(url, src["sources"][0]["url"].as_str().unwrap());
    assert!(
        !url.contains("minky.anistream.one"),
        "fallback malah balik ke minky: {url}"
    );
    println!(
        "fallback ok: kandidat mati ditolak, CDN provider hidup ({n} B) — {}",
        head(&url)
    );
}

/// Kode production: `/api/stream` memakai host minky dulu (fallback: CDN provider),
/// dan hasilnya di-cache sampai token hampir mati — aturan penyegarannya diuji
/// offline (`upstream::tests::play_cache_basi_sintetis`).
#[actix_web::test]
#[ignore = "live upstream (minky-first lewat /api/stream)"]
async fn stream_minky_dulu_lalu_cache() {
    let cfg = Config::default();
    let http = upstream::client(&cfg).unwrap();
    let app = test::init_service(create_app(
        cfg.clone(),
        http.clone(),
        web::Data::new(upstream::SubJobs::new()),
    ))
    .await;

    let uri = format!("/api/stream?id={ID}&ep={EP}");
    let (st, _, b) = get!(&app, &uri);
    assert_eq!(st, StatusCode::OK, "{}", String::from_utf8_lossy(&b));
    let v: serde_json::Value = serde_json::from_slice(&b).unwrap();
    let stream = v["stream"].as_str().unwrap().to_string();

    // Isi bungkus base64-nya: harus URL minky, bukan CDN provider apa adanya.
    let inner = upstream::decode_target(stream.strip_prefix("/api/hls?u=").unwrap()).unwrap();
    assert!(
        inner.starts_with("https://minky.anistream.one/fetch?url="),
        "bukan minky: {}",
        head(&inner)
    );

    // Panggilan kedua dilayani cache: URL sama (probe segmen ~800 KB tak diulang).
    let (st, _, b) = get!(&app, &uri);
    assert_eq!(st, StatusCode::OK);
    assert_eq!(
        serde_json::from_slice::<serde_json::Value>(&b).unwrap()["stream"],
        v["stream"],
        "cache tidak jalan (URL berubah di panggilan kedua)"
    );

    // Dan URL production itu benar-benar bisa diputar sampai segmen.
    let (st, _, seg) = first_segment!(&app, &stream);
    assert_eq!(st, StatusCode::OK, "segmen {st}");
    assert_eq!(seg.first(), Some(&0x47), "bukan TS ({} B)", seg.len());

    println!(
        "minky-first + cache ok: {} (segmen {} B)",
        head(&inner),
        seg.len()
    );
}
