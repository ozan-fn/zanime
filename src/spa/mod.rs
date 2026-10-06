use std::collections::HashMap;
use std::sync::OnceLock;

use actix_web::{http::header, web, HttpRequest, HttpResponse};
use bytes::Bytes;
use rust_embed::RustEmbed;

/// web/dist di-embed ke biner (debug: baca dari fs). Non-/api → index.html + aset.
#[derive(RustEmbed)]
#[folder = "web/dist/"]
struct Dist;

struct Variant {
    bytes: Bytes,
    etag: String,
}

struct Entry {
    bytes: Bytes, // release: from_static (zero-copy); clone = refcount saja
    gz: Option<Variant>, // precompress gzip level-9 dari vite build
    mime: mime_guess::Mime,
    etag: String,
    immutable: bool, // true untuk /assets/* (nama file ber-hash Vite)
}

fn assets() -> &'static HashMap<&'static str, Entry> {
    static MAP: OnceLock<HashMap<&'static str, Entry>> = OnceLock::new();
    MAP.get_or_init(|| {
        // chisle: key leak sekali di startup → per request tanpa alokasi
        let mut m = HashMap::new();
        for name in Dist::iter().filter(|n| !n.ends_with(".gz")) {
            if let Some(f) = Dist::get(&name) {
                let bytes = match f.data {
                    std::borrow::Cow::Borrowed(b) => Bytes::from_static(b),
                    std::borrow::Cow::Owned(v) => Bytes::from(v), // debug (fs): 1 alokasi
                };
                let gz = Dist::get(format!("{name}.gz").as_str()).map(|g| {
                    let bytes = match g.data {
                        std::borrow::Cow::Borrowed(b) => Bytes::from_static(b),
                        std::borrow::Cow::Owned(v) => Bytes::from(v),
                    };
                    Variant { etag: format!("{:x}-{}-gz", hash(&bytes), bytes.len()), bytes }
                });
                let key: &'static str = Box::leak(name.into_owned().into_boxed_str());
                m.insert(key, Entry {
                    mime: mime_guess::from_path(key).first_or_octet_stream(),
                    immutable: key.starts_with("assets/"),
                    etag: format!("{:x}-{}", hash(&bytes), bytes.len()),
                    bytes,
                    gz,
                });
            }
        }
        m
    })
}

fn hash(b: &[u8]) -> u64 {
    use std::hash::{DefaultHasher, Hash, Hasher};
    let mut h = DefaultHasher::new();
    b.hash(&mut h);
    h.finish()
}

pub async fn index(req: HttpRequest, name: web::Path<String>) -> HttpResponse {
    let map = assets();
    let path = name.as_str().trim_start_matches('/');
    let key = if path.is_empty() { "index.html" } else { path };
    let e = map.get(key).or_else(|| map.get("index.html"));
    let Some(e) = e else { return HttpResponse::NotFound().finish() };
    // Negosiasi gzip: .gz precompress (level-9) bila klien Accept-Encoding: gzip, else identitas.
    let ae = req.headers().get(header::ACCEPT_ENCODING).and_then(|v| v.to_str().ok()).unwrap_or("");
    let want_gzip = ae.split(',').any(|t| t.trim().split(';').next() == Some("gzip"));
    let (bytes, etag, enc) = match (&e.gz, want_gzip) {
        (Some(g), true) => (&g.bytes, g.etag.as_str(), Some("gzip")),
        _ => (&e.bytes, e.etag.as_str(), None),
    };
    if req.headers().get(header::IF_NONE_MATCH).and_then(|v| v.to_str().ok()) == Some(etag) {
        let mut n = HttpResponse::NotModified();
        n.insert_header((header::ETAG, etag));
        if e.gz.is_some() {
            n.insert_header((header::VARY, "Accept-Encoding"));
        }
        return n.finish(); // 304: tanpa body
    }
    let mut b = HttpResponse::Ok();
    b.insert_header((header::CONTENT_TYPE, e.mime.as_ref()));
    b.insert_header((header::ETAG, etag));
    b.insert_header((header::CACHE_CONTROL, if e.immutable { "public, max-age=31536000, immutable" } else { "no-cache" }));
    if let Some(enc) = enc {
        b.insert_header((header::CONTENT_ENCODING, enc));
    }
    if e.gz.is_some() {
        b.insert_header((header::VARY, "Accept-Encoding"));
    }
    if req.method() == actix_web::http::Method::HEAD {
        b.finish() // header saja, tanpa body
    } else {
        b.body(bytes.clone()) // Bytes::clone = refcount, tanpa copy
    }
}
