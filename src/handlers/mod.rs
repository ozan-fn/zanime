use actix_web::{HttpRequest, HttpResponse, get, post, web};

use crate::{
    cache,
    error::AppError,
    models::{
        AnilistBody, AnimeItem, AnimeOut, AnimeQuery, CatalogQuery, CommentsQuery, EpisodeItem,
        EpisodeMetaQuery, EpisodesQuery, ProxyQuery, SearchQuery, ServersQuery, SkiptimesQuery,
        SourcesQuery, StreamQuery, StreamResponse, SubBatchReq, SubIdQuery, SubStatus,
    },
    upstream::{self, px},
};

/// TTL cache terjemahan subtitle di redis (detik): 30 hari.
const SUB_CACHE_TTL: u64 = 30 * 86400;

/// Masukkan filter enum (status/format/season) sebagai `key: [VALUE]` bila diisi.
fn pick(
    v: Option<&str>,
    allowed: &[&str],
    key: &str,
    f: &mut serde_json::Map<String, serde_json::Value>,
) -> Result<(), AppError> {
    let Some(s) = v else { return Ok(()) };
    let u = s.to_uppercase();
    if !allowed.contains(&u.as_str()) {
        return Err(AppError::BadRequest(format!("{key} tidak dikenal")));
    }
    f.insert(key.to_string(), serde_json::json!([u]));
    Ok(())
}

/// Halaman maksimum yang dilayani upstream `catalogAnime` (di atas ini items = null).
pub const UPSTREAM_MAX_PAGE: i64 = 30;

/// Entri upstream /episodes → baris web (judul en > x-jat > ja, gambar via proxy).
fn episode_item(e: &serde_json::Value) -> EpisodeItem {
    EpisodeItem {
        number: e["number"].as_i64().unwrap_or(0),
        title: e["titles"]["en"]
            .as_str()
            .or(e["titles"]["x-jat"].as_str())
            .or(e["titles"]["ja"].as_str())
            .unwrap_or("?")
            .to_string(),
        title_romaji: e["titles"]["x-jat"].as_str().unwrap_or("").to_string(),
        // Judul Jepang asli (kanji) — terpisah dari romaji biar keduanya bisa tampil.
        title_jp: e["titles"]["ja"].as_str().unwrap_or("").to_string(),
        img: px(e["img"].as_str().unwrap_or("")),
    }
}
/// Item katalog → output web (gambar lewat proxy).
fn to_out(a: AnimeItem) -> AnimeOut {
    AnimeOut {
        id: a.id,
        anilist_id: a.anilist_id,
        title_romaji: a.title_romaji,
        title_english: a.title_english,
        episode_count: a.episode_count,
        cover: px(upstream::img_url(&a.cover_image)),
        banner: px(upstream::img_url(&a.banner_image)),
        format: a.format,
        season_year: a.season_year,
        average_score: a.average_score,
        status: a.status,
    }
}

#[get("/search")]
pub async fn search(
    cfg: web::Data<crate::config::Config>,
    http: web::Data<reqwest::Client>,
    q: web::Query<SearchQuery>,
) -> Result<HttpResponse, AppError> {
    if q.query.trim().is_empty() {
        return Err(AppError::BadRequest("query kosong".to_string()));
    }
    let items = upstream::search(&http, &cfg, q.query.trim()).await?;
    if items.is_empty() {
        return Err(AppError::NotFound("tidak ketemu".to_string()));
    }
    Ok(HttpResponse::Ok().json(items.into_iter().map(to_out).collect::<Vec<_>>()))
}

/// Katalog home: /api/catalog?sort=TRENDING&status=RELEASING&season=FALL&year=2026&format=TV&genre=Action&limit=18
#[get("/catalog")]
pub async fn catalog(
    cfg: web::Data<crate::config::Config>,
    http: web::Data<reqwest::Client>,
    q: web::Query<CatalogQuery>,
) -> Result<HttpResponse, AppError> {
    let bad = |m: &str| AppError::BadRequest(m.to_string());
    let sort = q.sort.as_deref().unwrap_or("POPULARITY").to_uppercase();
    if !upstream::SORT_FIELDS.contains(&sort.as_str()) {
        return Err(bad("sort tidak dikenal"));
    }
    let dir = q.direction.as_deref().unwrap_or("DESC").to_uppercase();
    if dir != "ASC" && dir != "DESC" {
        return Err(bad("direction harus ASC/DESC"));
    }
    let mut f = serde_json::Map::new();
    pick(q.status.as_deref(), &upstream::STATUSES, "statusIn", &mut f)?;
    pick(q.format.as_deref(), &upstream::FORMATS, "formatIn", &mut f)?;
    pick(q.season.as_deref(), &upstream::SEASONS, "seasonIn", &mut f)?;
    if let Some(y) = q.year {
        f.insert("seasonYearMin".into(), serde_json::json!(y));
        f.insert("seasonYearMax".into(), serde_json::json!(y));
    }
    if let Some(g) = q.genre.as_deref().map(str::trim).filter(|g| !g.is_empty()) {
        f.insert("genres".into(), serde_json::json!([g]));
    }
    if let Some(s) = q.query.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        f.insert("query".into(), serde_json::json!(s));
    }
    let items = upstream::catalog(
        &http,
        &cfg,
        &serde_json::Value::Object(f),
        &sort,
        &dir,
        // 30 = batas keras upstream: limit > 30 → `items: null` (502 "invalid type: null").
        q.limit.unwrap_or(18).clamp(1, UPSTREAM_MAX_PAGE),
        q.offset.unwrap_or(0).max(0),
    )
    .await?;
    Ok(HttpResponse::Ok().json(items.into_iter().map(to_out).collect::<Vec<_>>()))
}

#[get("/anime")]
pub async fn anime(
    cfg: web::Data<crate::config::Config>,
    http: web::Data<reqwest::Client>,
    q: web::Query<AnimeQuery>,
) -> Result<HttpResponse, AppError> {
    if q.id().is_none() && q.anilist_id().is_none() {
        return Err(AppError::BadRequest("id atau anilistId wajib".to_string()));
    }
    let v = upstream::anime(&http, &cfg, q.id(), q.anilist_id(), &q.section).await?;
    if v.is_null() {
        return Err(AppError::NotFound("anime tidak ketemu".to_string()));
    }
    Ok(HttpResponse::Ok().json(v))
}

#[get("/episodes")]
pub async fn episodes(
    cfg: web::Data<crate::config::Config>,
    http: web::Data<reqwest::Client>,
    q: web::Query<EpisodesQuery>,
) -> Result<HttpResponse, AppError> {
    if q.id.trim().is_empty() {
        return Err(AppError::BadRequest("id kosong".to_string()));
    }
    let v = upstream::episodes(&http, &cfg, q.id.trim()).await?;
    let mut out: Vec<EpisodeItem> = v
        .as_array()
        .cloned()
        .unwrap_or_default()
        .iter()
        .map(episode_item)
        .collect();
    if out.is_empty() {
        return Err(AppError::NotFound("episode kosong".to_string()));
    }
    out.sort_by_key(|e| e.number);
    Ok(HttpResponse::Ok().json(out))
}

#[get("/servers")]
pub async fn servers(
    cfg: web::Data<crate::config::Config>,
    http: web::Data<reqwest::Client>,
    q: web::Query<ServersQuery>,
) -> Result<HttpResponse, AppError> {
    if q.id.trim().is_empty() {
        return Err(AppError::BadRequest("id kosong".to_string()));
    }
    Ok(HttpResponse::Ok().json(upstream::servers(&http, &cfg, q.id.trim(), q.ep).await?))
}

#[get("/sources")]
pub async fn sources(
    cfg: web::Data<crate::config::Config>,
    http: web::Data<reqwest::Client>,
    q: web::Query<SourcesQuery>,
) -> Result<HttpResponse, AppError> {
    if q.id.trim().is_empty() {
        return Err(AppError::BadRequest("id kosong".to_string()));
    }
    Ok(HttpResponse::Ok()
        .json(upstream::sources(&http, &cfg, q.id.trim(), q.ep, &q.r#type, &q.provider).await?))
}

#[get("/stream")]
pub async fn stream(
    cfg: web::Data<crate::config::Config>,
    http: web::Data<reqwest::Client>,
    jobs: web::Data<upstream::SubJobs>,
    q: web::Query<StreamQuery>,
) -> Result<HttpResponse, AppError> {
    if q.id.trim().is_empty() {
        return Err(AppError::BadRequest("id kosong".to_string()));
    }
    let p = upstream::play(&http, &cfg, q.id.trim(), q.ep, "sub", cfg.provider).await?;
    let sub = pick_en_sub(&p.sources);
    let sub_id = if sub.is_empty() {
        String::new()
    } else {
        enqueue_subid(&jobs, http.as_ref(), sub.to_string())
    };
    Ok(HttpResponse::Ok().json(StreamResponse {
        stream: format!("/api/hls?u={}", upstream::b64(&p.stream)),
        sub_en: px(sub),
        sub_id,
    }))
}

/// URL VTT trek Inggris dari `tracks` upstream. `lang` tak pernah bersih:
/// `"english (cr english)"`, `"English"`, `"en"`. Cukup cari yang memuat "english".
fn pick_en_sub(s: &serde_json::Value) -> &str {
    s["tracks"]
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
        .unwrap_or("")
}

#[cfg(test)]
mod tests {
    use super::{next_at_of, pick, pick_en_sub, to_out};

    #[test]
    fn catalog_pick_sintetis() {
        let mut f = serde_json::Map::new();
        pick(
            Some("releasing"),
            &crate::upstream::STATUSES,
            "statusIn",
            &mut f,
        )
        .unwrap();
        assert_eq!(f["statusIn"], serde_json::json!(["RELEASING"]));
        assert!(
            pick(
                Some("ngawur"),
                &crate::upstream::STATUSES,
                "statusIn",
                &mut f
            )
            .is_err()
        );
        assert!(pick(None, &crate::upstream::STATUSES, "statusIn", &mut f).is_ok());
    }

    #[test]
    fn to_out_proxy_sintetis() {
        let a: crate::models::AnimeItem = serde_json::from_value(serde_json::json!({
            "id": "x", "anilistId": 1, "titleRomaji": "T",
            "coverImage": {"large": "https://cdn/c.jpg"}, "bannerImage": "https://cdn/b.jpg"
        }))
        .unwrap();
        let o = to_out(a);
        assert!(o.cover.starts_with("/api/fetch?u="));
        assert!(o.banner.starts_with("/api/fetch?u="));
    }

    #[test]
    fn episodes_map_sort_sintetis() {
        // Bentuk upstream /episodes → EpisodeItem (judul en > x-jat > ja, sort number).
        let v = serde_json::json!([
            {"number": 2, "titles": {"en": "Two", "x-jat": "Ni", "ja": "二"}, "img": "https://cdn/2.jpg"},
            {"number": 1, "titles": {"en": null, "x-jat": "Ichi", "ja": "一"}, "img": ""},
            {"number": 3, "titles": {}, "img": ""}
        ]);
        let mut out: Vec<crate::models::EpisodeItem> = v
            .as_array()
            .cloned()
            .unwrap_or_default()
            .iter()
            .map(super::episode_item)
            .collect();
        out.sort_by_key(|e| e.number);
        assert_eq!(out[0].title, "Ichi");
        assert_eq!(out[1].title, "Two");
        assert_eq!(out[2].title, "?");
        assert!(out[0].img.is_empty()); // img kosong → tanpa proxy
    }

    #[test]
    fn next_at_sintetis() {
        use std::collections::HashMap;
        let mk = |id: Vec<usize>, n: usize| crate::upstream::SubJob {
            state: "ready".to_string(),
            err: None,
            head: "WEBVTT".to_string(),
            cues: (0..n)
                .map(|i| {
                    vec![
                        format!("00:00:{i:02}.000 --> 00:00:{:02}.000", i + 1),
                        format!("L{i}"),
                    ]
                })
                .collect(),
            dlg: (0..n).collect(),
            id: id
                .into_iter()
                .map(|i| (i, format!("ID{i}")))
                .collect::<HashMap<_, _>>(),
            key: String::new(),
        };
        assert_eq!(next_at_of(&mk(vec![0, 1], 2), 0.0), -1.0); // done penuh
        assert_eq!(next_at_of(&mk(vec![], 0), 0.0), -1.0); // kosong
    }

    #[test]
    fn pick_en_sub_handles_real_upstream_shapes() {
        // Bentuk asli api.anistream.one: lang "english (cr english)", bukan "english".
        let v = serde_json::json!({"tracks": [
            {"lang": "portuguese (cr portuguese(brazil))", "url": "pt.vtt"},
            {"lang": "english (cr english)", "url": "en.vtt"},
            {"lang": "spanish (cr spanish)", "url": "es.vtt"},
        ]});
        assert_eq!(pick_en_sub(&v), "en.vtt");

        // Huruf besar/kecil & label bersih juga kena.
        let v = serde_json::json!({"tracks": [{"lang": "English", "url": "en.vtt"}]});
        assert_eq!(pick_en_sub(&v), "en.vtt");

        // `en` polos tak memuat "english" → kosong (bukan salah pilih).
        let v = serde_json::json!({"tracks": [{"lang": "en", "url": "en.vtt"}]});
        assert_eq!(pick_en_sub(&v), "");

        // Tanpa Inggris → kosong (bukan salah pilih trek).
        let v = serde_json::json!({"tracks": [{"lang": "spanish (cr spanish)", "url": "es.vtt"}]});
        assert_eq!(pick_en_sub(&v), "");
        assert_eq!(pick_en_sub(&serde_json::json!({})), "");
    }
}

#[get("/skiptimes")]
pub async fn skiptimes(
    http: web::Data<reqwest::Client>,
    q: web::Query<SkiptimesQuery>,
) -> Result<HttpResponse, AppError> {
    Ok(HttpResponse::Ok().json(upstream::skiptimes(&http, q.mal, q.ep, q.len).await?))
}

#[get("/comments")]
pub async fn comments(
    http: web::Data<reqwest::Client>,
    q: web::Query<CommentsQuery>,
) -> Result<HttpResponse, AppError> {
    if q.media.trim().is_empty() {
        return Err(AppError::BadRequest("media kosong".to_string()));
    }
    Ok(HttpResponse::Ok()
        .json(upstream::comments(&http, q.media.trim(), q.offset, &q.sort, q.limit).await?))
}

#[get("/episode-meta")]
pub async fn episode_meta(
    http: web::Data<reqwest::Client>,
    q: web::Query<EpisodeMetaQuery>,
) -> Result<HttpResponse, AppError> {
    Ok(HttpResponse::Ok().json(upstream::episode_meta(&http, q.anilist_id, q.ep).await?))
}

#[post("/anilist")]
pub async fn anilist(
    http: web::Data<reqwest::Client>,
    body: web::Json<AnilistBody>,
) -> Result<HttpResponse, AppError> {
    if body.query.trim().is_empty() {
        return Err(AppError::BadRequest("query kosong".to_string()));
    }
    Ok(HttpResponse::Ok()
        .json(upstream::anilist(&http, body.query.trim(), body.variables.clone()).await?))
}

/// Resource proses ini (mem + cpu, sekarang & puncaknya) untuk navbar web.
#[get("/stats")]
pub async fn stats() -> HttpResponse {
    let (mem_mb, peak_mem_mb, cpu, peak_cpu) = crate::stats::snapshot();
    HttpResponse::Ok().json(serde_json::json!({
        "memMb": mem_mb,
        "peakMemMb": peak_mem_mb,
        "cpu": cpu,
        "peakCpu": peak_cpu,
    }))
}

/// Proxy generik: /api/fetch?u=<base64url> (gambar, sub) dan
/// /api/fetch/<nama>.<ext>?u=<base64url> (segmen; ekstensi dipakai player untuk deteksi tipe).
pub async fn fetch(
    http: web::Data<reqwest::Client>,
    req: HttpRequest,
    q: web::Query<ProxyQuery>,
) -> Result<HttpResponse, AppError> {
    let url = upstream::decode_target(&q.u)?;
    let range = req
        .headers()
        .get("Range")
        .and_then(|v| v.to_str().ok())
        .map(str::to_string);
    let f = upstream::fetch_stream(
        &http,
        &url,
        q.origin.as_deref(),
        q.referer.as_deref(),
        range,
    )
    .await?;
    let mut b = HttpResponse::build(
        actix_web::http::StatusCode::from_u16(f.status).unwrap_or(actix_web::http::StatusCode::OK),
    );
    // Gambar (dari `px`) tak berubah di URL sama → biar browser cache, tak fetch tiap render.
    if f.ctype.starts_with("image/") {
        b.insert_header(("cache-control", "public, max-age=31536000, immutable"));
    }
    b.content_type(f.ctype);
    if let Some(cr) = f.crange {
        b.insert_header(("content-range", cr));
    }
    if let Some(cl) = f.clen {
        b.insert_header(("content-length", cl));
    }
    Ok(b.streaming(f.stream))
}

fn sub_set(jobs: &upstream::SubJobs, id: &str, state: &str) {
    if let Ok(mut mu) = jobs.mu.lock()
        && let Some(j) = mu.get_mut(id)
    {
        j.state = state.to_string();
    }
}

/// Worker: fetch VTT EN + parse + simpan (tanpa LLM). Batch diterjemahkan
/// on-demand via `/subid/batch` tiap 3 cue tampil → hemat limit.
async fn sub_worker(
    jobs: web::Data<upstream::SubJobs>,
    http: reqwest::Client,
    id: String,
    url: String,
) {
    sub_set(&jobs, &id, "working");
    let out = async {
        let f = upstream::fetch_upstream(&http, &url, None, None, None).await?;
        let text = String::from_utf8(f.body)
            .map_err(|_| AppError::Upstream("sub bukan utf8".to_string()))?;
        if !text.trim_start().starts_with("WEBVTT") {
            return Err(AppError::Upstream("bukan VTT".to_string()));
        }
        // Hasil terjemahan hanya di redis (tak ada salinan disk): kunci = hash VTT EN.
        let key = format!("subid:{}", upstream::sub_cache_id(&text));
        if let Some(hit) = cache::get::<String>(&key).await.filter(|v| !v.trim().is_empty()) {
            let (head, cues) = upstream::parse_vtt(&hit);
            let ids: std::collections::HashMap<usize, String> = cues
                .iter()
                .enumerate()
                .filter_map(|(i, c)| upstream::cue_lines(c).map(|ls| (i, ls.join("\n"))))
                .collect();
            let mut mu = jobs
                .mu
                .lock()
                .map_err(|_| AppError::Upstream("lock".to_string()))?;
            if let Some(j) = mu.get_mut(&id) {
                let n = cues
                    .iter()
                    .filter(|c| c.iter().any(|l| l.contains("-->")))
                    .count();
                j.head = head;
                j.cues = cues;
                j.dlg = (0..n).collect();
                j.id = ids;
                j.key = key;
            }
            return Ok(());
        }
        let (head, cues) = upstream::parse_vtt(&text);
        let dlg: Vec<usize> = cues
            .iter()
            .enumerate()
            .filter(|(_, c)| upstream::cue_lines(c).is_some())
            .map(|(i, _)| i)
            .collect();
        {
            let mut mu = jobs
                .mu
                .lock()
                .map_err(|_| AppError::Upstream("lock".to_string()))?;
            if let Some(j) = mu.get_mut(&id) {
                j.head = head;
                j.cues = cues;
                j.dlg = dlg;
                j.key = key;
            }
        }
        Ok(())
    }
    .await;
    if let Ok(mut mu) = jobs.mu.lock()
        && let Some(j) = mu.get_mut(&id)
    {
        match out {
            Ok(()) => {
                j.state = if j.id.len() >= j.dlg.len() && !j.dlg.is_empty() {
                    "done".to_string()
                } else {
                    "ready".to_string()
                };
            }
            Err(e) => {
                j.state = "failed".to_string();
                j.err = Some(e.to_string());
            }
        }
    }
}

/// 1 batch (6 cue berikut yang belum) — dipicu tiap 3 cue tampil di player.
/// 1 permit semaphore → serial FIFO antar user.
#[post("/subid/batch")]
pub async fn subid_batch(
    cfg: web::Data<crate::config::Config>,
    jobs: web::Data<upstream::SubJobs>,
    http: web::Data<reqwest::Client>,
    body: web::Json<SubBatchReq>,
) -> Result<HttpResponse, AppError> {
    if cfg.llm_keys.is_empty() {
        return Err(AppError::BadRequest(
            "OPENAI_API_KEY kosong (isi .env)".to_string(),
        ));
    }
    let _permit = jobs
        .sem
        .acquire()
        .await
        .map_err(|_| AppError::Upstream("antre penuh".to_string()))?;
    let batch: Vec<(usize, Vec<String>)> = {
        let mu = jobs
            .mu
            .lock()
            .map_err(|_| AppError::Upstream("lock".to_string()))?;
        let j = mu
            .get(&body.id)
            .ok_or_else(|| AppError::NotFound("job tak ada".to_string()))?;
        if j.state == "failed" {
            return Err(AppError::Upstream(
                j.err.clone().unwrap_or_else(|| "gagal".to_string()),
            ));
        }
        if j.cues.is_empty() {
            return Err(AppError::Upstream("belum siap".to_string()));
        }
        let at = body.at.unwrap_or(0.0);
        let start_at = |i: &usize| {
            j.cues
                .get(*i)?
                .iter()
                .find(|l| l.contains("-->"))
                .map(|l| upstream::ts_start(l))
        };
        // Sertakan 1 cue sebelum posisi seek biar sub sebelumnya sudah tampil.
        let pos = j
            .dlg
            .iter()
            .position(|i| start_at(i).is_some_and(|t| t >= at))
            .unwrap_or(j.dlg.len());
        let begin = pos.saturating_sub(1);
        let mut todo: Vec<usize> = j.dlg[begin..]
            .iter()
            .filter(|i| !j.id.contains_key(i))
            .take(upstream::SUB_BATCH)
            .copied()
            .collect();
        if todo.len() < upstream::SUB_BATCH {
            let extra: Vec<usize> = j
                .dlg
                .iter()
                .filter(|i| !j.id.contains_key(i) && !todo.contains(i))
                .take(upstream::SUB_BATCH - todo.len())
                .copied()
                .collect();
            todo.extend(extra);
        }
        todo.iter()
            .filter_map(|i| upstream::cue_lines(&j.cues[*i]).map(|ls| (*i, ls)))
            .collect()
    };
    let mut delta = String::new();
    if !batch.is_empty() {
        let m = upstream::translate_batch(
            http.as_ref(),
            &cfg.llm_keys,
            &cfg.llm_base,
            &cfg.llm_model,
            &batch,
        )
        .await?;
        {
            let mu = jobs
                .mu
                .lock()
                .map_err(|_| AppError::Upstream("lock".to_string()))?;
            if let Some(j) = mu.get(&body.id) {
                let mut parts: Vec<String> = Vec::new();
                for (i, txt) in &m {
                    if let Some(c) = j.cues.get(*i)
                        && let Some(line) = c.iter().find(|l| l.contains("-->"))
                    {
                        parts.push(format!("{line}\n{txt}"));
                    }
                }
                delta = parts.join("\n\n");
            }
        }
        // Guard mu tidak boleh hidup saat await → hasil cache dikeluarkan dari blok.
        let cached: Option<(String, String)> = {
            let mut mu = jobs
                .mu
                .lock()
                .map_err(|_| AppError::Upstream("lock".to_string()))?;
            let mut out = None;
            if let Some(j) = mu.get_mut(&body.id) {
                j.id.extend(m);
                if j.id.len() >= j.dlg.len() {
                    j.state = "done".to_string();
                    let done = upstream::build_vtt(&j.head, &j.cues, &j.id);
                    if !j.key.is_empty() {
                        out = Some((j.key.clone(), done));
                    }
                }
            }
            out
        };
        if let Some((key, vtt)) = cached {
            cache::set(&key, SUB_CACHE_TTL, &vtt).await;
        }
    }
    let mut st = sub_status_of(&jobs, &body.id, body.at.unwrap_or(0.0))?;
    st.vtt = delta;
    Ok(HttpResponse::Ok().json(st))
}

fn sub_status_of(jobs: &upstream::SubJobs, id: &str, at: f64) -> Result<SubStatus, AppError> {
    let mu = jobs
        .mu
        .lock()
        .map_err(|_| AppError::Upstream("lock".to_string()))?;
    let j = mu
        .get(id)
        .ok_or_else(|| AppError::NotFound("job tak ada".to_string()))?;
    // Satu lock, satu pass: posisi + total antrean dihitung sekaligus.
    let (queue, queue_total) = {
        let order = jobs
            .order
            .lock()
            .map_err(|_| AppError::Upstream("lock".to_string()))?;
        let mut pos = 0;
        let mut total = 0;
        for x in order.iter() {
            if mu.get(x.as_str()).is_some_and(|v| v.state == "queued") {
                total += 1;
                if x.as_str() == id {
                    pos = total;
                }
            }
        }
        (pos, total)
    };
    Ok(SubStatus {
        state: j.state.clone(),
        queue,
        queue_total,
        // Estimasi: tiap job ±2 mnt penuh; job berjalan dianggap ±1 sisa.
        eta_sec: if queue > 0 {
            (queue.saturating_sub(1)) as u64 * 120
        } else {
            0
        },
        done: j.id.len(),
        total: j.dlg.len(),
        next_at: next_at_of(j, at),
        error: j.err.clone(),
        vtt: String::new(),
    })
}

/// Detik pemicu batch berikut: akhir cue ke-(done-3), minus 2 dtk ancang.
/// done penuh → -1 (nonaktif).
/// Pemicu batch berikut: saat cue ID tersisa di depan posisi `at` menyentuh 8.
/// Cue dipemetakan lewat ts_start; 0 → langsung. Semua sudah → -1.
fn next_at_of(j: &upstream::SubJob, at: f64) -> f64 {
    if j.id.len() >= j.dlg.len() || j.dlg.is_empty() {
        return -1.0;
    }
    let mut ahead: Vec<f64> = j
        .dlg
        .iter()
        .filter(|i| j.id.contains_key(i))
        .filter_map(|i| {
            j.cues
                .get(*i)?
                .iter()
                .find(|l| l.contains("-->"))
                .map(|l| upstream::ts_start(l))
        })
        .filter(|t| *t >= at)
        .collect();
    ahead.sort_by(|a, b| a.partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal));
    if ahead.len() < 5 {
        return at;
    }
    (ahead[ahead.len() - 5] - 2.0).max(0.0)
}

/// Daftarkan job subtitle (dedup per URL). Dipakai /stream + /subid.
fn enqueue_subid(
    jobs: &web::Data<upstream::SubJobs>,
    http: &reqwest::Client,
    url: String,
) -> String {
    let id = upstream::b64(&url);
    let fresh = {
        let mut mu = match jobs.mu.lock() {
            Ok(m) => m,
            Err(_) => return id,
        };
        if mu.contains_key(&id) {
            false
        } else {
            mu.insert(
                id.clone(),
                upstream::SubJob {
                    state: "queued".to_string(),
                    err: None,
                    head: String::new(),
                    cues: Vec::new(),
                    dlg: Vec::new(),
                    id: std::collections::HashMap::new(),
                    key: String::new(),
                },
            );
            if let Ok(mut q) = jobs.order.lock() {
                q.push_back(id.clone());
                while q.len() > 100 {
                    if let Some(old) = q.pop_front() {
                        mu.remove(&old);
                    }
                }
            }
            true
        }
    };
    if fresh {
        actix_web::rt::spawn(sub_worker(jobs.clone(), http.clone(), id.clone(), url));
    }
    id
}
/// Status job: `{state: queued|ready|working|done|failed, queue, done, total, error?}`.
#[get("/subid/status")]
pub async fn subid_status(
    jobs: web::Data<upstream::SubJobs>,
    q: web::Query<SubIdQuery>,
) -> Result<HttpResponse, AppError> {
    Ok(HttpResponse::Ok().json(sub_status_of(&jobs, &q.id, q.at.unwrap_or(0.0))?))
}

/// VTT Indonesia, parsial selama jalan (cue sisa fallback EN), penuh saat done.
#[get("/subid/result")]
pub async fn subid_result(
    jobs: web::Data<upstream::SubJobs>,
    q: web::Query<SubIdQuery>,
) -> Result<HttpResponse, AppError> {
    let mu = jobs
        .mu
        .lock()
        .map_err(|_| AppError::Upstream("lock".to_string()))?;
    let j = mu
        .get(&q.id)
        .ok_or_else(|| AppError::NotFound("job tak ada".to_string()))?;
    if j.state == "failed" {
        return Err(AppError::Upstream(
            j.err.clone().unwrap_or_else(|| "gagal".to_string()),
        ));
    }
    if j.cues.is_empty() {
        return Ok(HttpResponse::Ok()
            .content_type("text/vtt")
            .body("WEBVTT\n\n"));
    }
    if q.only.as_deref() == Some("id") {
        return Ok(HttpResponse::Ok()
            .content_type("text/vtt")
            .body(upstream::build_vtt_id(&j.head, &j.cues, &j.id)));
    }
    Ok(HttpResponse::Ok()
        .content_type("text/vtt")
        .body(upstream::build_vtt(&j.head, &j.cues, &j.id)))
}

/// Proxy playlist: /api/hls?u=<base64url master m3u8> — isi ditulis ulang ke /api/* (base64).
#[get("/hls")]
pub async fn hls(
    http: web::Data<reqwest::Client>,
    q: web::Query<ProxyQuery>,
) -> Result<HttpResponse, AppError> {
    let url = upstream::decode_target(&q.u)?;
    let f = upstream::fetch_upstream(&http, &url, q.origin.as_deref(), q.referer.as_deref(), None)
        .await?;
    let text = String::from_utf8(f.body.to_vec())
        .map_err(|_| AppError::Upstream("playlist bukan utf8".to_string()))?;
    if !text.contains("#EXTM3U") {
        return Err(AppError::Upstream(
            "upstream bukan playlist (kemungkinan diblokir)".to_string(),
        ));
    }
    let base = url.rsplit_once('/').map(|(b, _)| b).unwrap_or(&url);
    let out = upstream::rewrite_playlist(base, &text, q.origin.as_deref(), q.referer.as_deref());
    Ok(HttpResponse::Ok()
        .content_type("application/vnd.apple.mpegurl")
        .body(out))
}
