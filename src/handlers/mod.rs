use actix_web::{get, post, web, HttpRequest, HttpResponse};

use crate::{
    error::AppError,
    models::{AnimeItem, AnimeOut, AnilistBody, CatalogQuery, CommentsQuery, EpisodeItem, EpisodeMetaQuery, EpisodesQuery, ProxyQuery, SearchQuery, ServersQuery, SkiptimesQuery, SourcesQuery, StreamQuery, StreamResponse, SubBatchReq, SubIdQuery, SubStatus, AnimeQuery},
    upstream::{self, px},
};

/// Masukkan filter enum (status/format/season) sebagai `key: [VALUE]` bila diisi.
fn pick(v: Option<&str>, allowed: &[&str], key: &str, f: &mut serde_json::Map<String, serde_json::Value>) -> Result<(), AppError> {
    let Some(s) = v else { return Ok(()) };
    let u = s.to_uppercase();
    if !allowed.contains(&u.as_str()) {
        return Err(AppError::BadRequest(format!("{key} tidak dikenal")));
    }
    f.insert(key.to_string(), serde_json::json!([u]));
    Ok(())
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
        q.limit.unwrap_or(18).clamp(1, 50),
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
        .map(|e| EpisodeItem {
            number: e["number"].as_i64().unwrap_or(0),
            title: e["titles"]["en"]
                .as_str()
                .or(e["titles"]["x-jat"].as_str())
                .or(e["titles"]["ja"].as_str())
                .unwrap_or("?")
                .to_string(),
            img: px(e["img"].as_str().unwrap_or("")),
        })
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
    Ok(HttpResponse::Ok().json(upstream::sources(&http, &cfg, q.id.trim(), q.ep, &q.r#type, &q.provider).await?))
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
          let s = upstream::sources(&http, &cfg, q.id.trim(), q.ep, "sub", cfg.provider).await?;
          let (url, _) = upstream::probe_stream(&http, &s).await?;
          let sub = s["tracks"]
              .as_array()
              .and_then(|t| t.iter().find(|x| x["lang"].as_str().is_some_and(|l| l.eq_ignore_ascii_case("english"))))
              .and_then(|x| x["url"].as_str())
              .unwrap_or("");
          let sub_id = if sub.is_empty() { String::new() } else { enqueue_subid(&jobs, http.as_ref(), sub.to_string()) };
    Ok(HttpResponse::Ok().json(StreamResponse {
        stream: format!("/api/hls?u={}", upstream::b64(&url)),
        sub_en: px(sub),
        sub_id,
    }))
}

#[get("/skiptimes")]
pub async fn skiptimes(http: web::Data<reqwest::Client>, q: web::Query<SkiptimesQuery>) -> Result<HttpResponse, AppError> {
    Ok(HttpResponse::Ok().json(upstream::skiptimes(&http, q.mal, q.ep, q.len).await?))
}

#[get("/comments")]
pub async fn comments(http: web::Data<reqwest::Client>, q: web::Query<CommentsQuery>) -> Result<HttpResponse, AppError> {
    if q.media.trim().is_empty() {
        return Err(AppError::BadRequest("media kosong".to_string()));
    }
    Ok(HttpResponse::Ok().json(upstream::comments(&http, q.media.trim(), q.offset, &q.sort, q.limit).await?))
}

#[get("/episode-meta")]
pub async fn episode_meta(http: web::Data<reqwest::Client>, q: web::Query<EpisodeMetaQuery>) -> Result<HttpResponse, AppError> {
    Ok(HttpResponse::Ok().json(upstream::episode_meta(&http, q.anilist_id, q.ep).await?))
}

#[post("/anilist")]
pub async fn anilist(http: web::Data<reqwest::Client>, body: web::Json<AnilistBody>) -> Result<HttpResponse, AppError> {
    if body.query.trim().is_empty() {
        return Err(AppError::BadRequest("query kosong".to_string()));
    }
    Ok(HttpResponse::Ok().json(upstream::anilist(&http, body.query.trim(), body.variables.clone()).await?))
}

/// Proxy generik: /api/fetch?u=<base64url> (gambar, sub) dan
/// /api/fetch/<nama>.<ext>?u=<base64url> (segmen; ekstensi dipakai player untuk deteksi tipe).
pub async fn fetch(http: web::Data<reqwest::Client>, req: HttpRequest, q: web::Query<ProxyQuery>) -> Result<HttpResponse, AppError> {
    let url = upstream::decode_target(&q.u)?;
    let range = req.headers().get("Range").and_then(|v| v.to_str().ok()).map(str::to_string);
    let f = upstream::fetch_stream(&http, &url, q.origin.as_deref(), q.referer.as_deref(), range).await?;
    let mut b = HttpResponse::build(actix_web::http::StatusCode::from_u16(f.status).unwrap_or(actix_web::http::StatusCode::OK));
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
        && let Some(j) = mu.get_mut(id) {
            j.state = state.to_string();
        }
}

/// Worker: fetch VTT EN + parse + simpan (tanpa LLM). Batch diterjemahkan
/// on-demand via `/subid/batch` tiap 3 cue tampil → hemat limit.
async fn sub_worker(jobs: web::Data<upstream::SubJobs>, http: reqwest::Client, id: String, url: String) {
    sub_set(&jobs, &id, "working");
    let out = async {
        let f = upstream::fetch_upstream(&http, &url, None, None, None).await?;
        let text = String::from_utf8(f.body).map_err(|_| AppError::Upstream("sub bukan utf8".to_string()))?;
        if !text.trim_start().starts_with("WEBVTT") {
            return Err(AppError::Upstream("bukan VTT".to_string()));
        }
        let path = upstream::sub_cache_path(&text).to_string_lossy().to_string();
        if let Ok(hit) = std::fs::read_to_string(&path)
            && !hit.is_empty() {
                let (head, cues) = upstream::parse_vtt(&hit);
                let ids: std::collections::HashMap<usize, String> = cues
                    .iter()
                    .enumerate()
                    .filter_map(|(i, c)| upstream::cue_lines(c).map(|ls| (i, ls.join("\n"))))
                    .collect();
                let mut mu = jobs.mu.lock().map_err(|_| AppError::Upstream("lock".to_string()))?;
                if let Some(j) = mu.get_mut(&id) {
                    let n = cues.iter().filter(|c| c.iter().any(|l| l.contains("-->"))).count();
                    j.head = head;
                    j.cues = cues;
                    j.dlg = (0..n).collect();
                    j.id = ids;
                    j.cache = path;
                }
                return Ok(());
            }
        let (head, cues) = upstream::parse_vtt(&text);
        let dlg: Vec<usize> = cues.iter().enumerate().filter(|(_, c)| upstream::cue_lines(c).is_some()).map(|(i, _)| i).collect();
        {
            let mut mu = jobs.mu.lock().map_err(|_| AppError::Upstream("lock".to_string()))?;
            if let Some(j) = mu.get_mut(&id) {
                j.head = head;
                j.cues = cues;
                j.dlg = dlg;
                j.cache = path;
            }
        }
        Ok(())
    }
    .await;
    if let Ok(mut mu) = jobs.mu.lock()
        && let Some(j) = mu.get_mut(&id) {
            match out {
                Ok(()) => {
                    j.state = if j.id.len() >= j.dlg.len() && !j.dlg.is_empty() { "done".to_string() } else { "ready".to_string() };
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
    if cfg.mistral_key.is_empty() {
        return Err(AppError::BadRequest("MISTRAL_API_KEY kosong (isi .env)".to_string()));
    }
    let _permit = jobs.sem.acquire().await.map_err(|_| AppError::Upstream("antre penuh".to_string()))?;
    let batch: Vec<(usize, Vec<String>)> = {
        let mu = jobs.mu.lock().map_err(|_| AppError::Upstream("lock".to_string()))?;
        let j = mu.get(&body.id).ok_or_else(|| AppError::NotFound("job tak ada".to_string()))?;
        if j.state == "failed" {
            return Err(AppError::Upstream(j.err.clone().unwrap_or_else(|| "gagal".to_string())));
        }
        if j.cues.is_empty() {
            return Err(AppError::Upstream("belum siap".to_string()));
        }
        let at = body.at.unwrap_or(0.0);
        let start_at = |i: &usize| j.cues.get(*i)?.iter().find(|l| l.contains("-->")).map(|l| upstream::ts_start(l));
        // Sertakan 1 cue sebelum posisi seek biar sub sebelumnya sudah tampil.
        let pos = j.dlg.iter().position(|i| start_at(i).is_some_and(|t| t >= at)).unwrap_or(j.dlg.len());
        let begin = pos.saturating_sub(1);
        let mut todo: Vec<usize> = j.dlg[begin..].iter().filter(|i| !j.id.contains_key(i)).take(upstream::SUB_BATCH).copied().collect();
        if todo.len() < upstream::SUB_BATCH {
            let extra: Vec<usize> = j.dlg.iter().filter(|i| !j.id.contains_key(i) && !todo.contains(i)).take(upstream::SUB_BATCH - todo.len()).copied().collect();
            todo.extend(extra);
        }
        todo.iter().filter_map(|i| upstream::cue_lines(&j.cues[*i]).map(|ls| (*i, ls))).collect()
    };
    let mut delta = String::new();
    if !batch.is_empty() {
        let m = upstream::translate_batch(http.as_ref(), &cfg.mistral_key, &batch).await?;
        {
            let mu = jobs.mu.lock().map_err(|_| AppError::Upstream("lock".to_string()))?;
            if let Some(j) = mu.get(&body.id) {
                let mut parts: Vec<String> = Vec::new();
                for (i, txt) in &m {
                    if let Some(c) = j.cues.get(*i)
                        && let Some(line) = c.iter().find(|l| l.contains("-->")) {
                            parts.push(format!("{line}\n{txt}"));
                        }
                }
                delta = parts.join("\n\n");
            }
        }
        let mut mu = jobs.mu.lock().map_err(|_| AppError::Upstream("lock".to_string()))?;
        if let Some(j) = mu.get_mut(&body.id) {
            j.id.extend(m);
            if j.id.len() >= j.dlg.len() {
                j.state = "done".to_string();
                let done = upstream::build_vtt(&j.head, &j.cues, &j.id);
                if !j.cache.is_empty() {
                    if let Some(dir) = std::path::Path::new(&j.cache).parent() {
                        let _ = std::fs::create_dir_all(dir);
                    }
                    let _ = std::fs::write(&j.cache, &done);
                }
            }
        }
    }
    let mut st = sub_status_of(&jobs, &body.id, body.at.unwrap_or(0.0))?;
    st.vtt = delta;
    Ok(HttpResponse::Ok().json(st))
}

fn sub_status_of(jobs: &upstream::SubJobs, id: &str, at: f64) -> Result<SubStatus, AppError> {
    let mu = jobs.mu.lock().map_err(|_| AppError::Upstream("lock".to_string()))?;
    let j = mu.get(id).ok_or_else(|| AppError::NotFound("job tak ada".to_string()))?;
    let queue_total = jobs
        .order
        .lock()
        .map_err(|_| AppError::Upstream("lock".to_string()))?
        .iter()
        .filter(|x| mu.get(*x).is_some_and(|v| v.state == "queued"))
        .count();
    let queue = jobs
        .order
        .lock()
        .map_err(|_| AppError::Upstream("lock".to_string()))?
        .iter()
        .filter(|x| mu.get(*x).is_some_and(|v| v.state == "queued"))
        .position(|x| x == id)
        .map(|p| p + 1)
        .unwrap_or(0);
    Ok(SubStatus {
        state: j.state.clone(),
        queue,
        queue_total,
        // Estimasi: tiap job ±2 mnt penuh; job berjalan dianggap ±1 sisa.
        eta_sec: if queue > 0 { (queue.saturating_sub(1)) as u64 * 120 } else { 0 },
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
        .filter_map(|i| j.cues.get(*i)?.iter().find(|l| l.contains("-->")).map(|l| upstream::ts_start(l)))
        .filter(|t| *t >= at)
        .collect();
    ahead.sort_by(|a, b| a.partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal));
    if ahead.len() < 5 {
        return at;
    }
    (ahead[ahead.len() - 5] - 2.0).max(0.0)
}

/// Daftarkan job subtitle (dedup per URL). Dipakai /stream + /subid.
fn enqueue_subid(jobs: &web::Data<upstream::SubJobs>, http: &reqwest::Client, url: String) -> String {
    let id = upstream::b64(&url);
    let fresh = {
        let mut mu = match jobs.mu.lock() {
            Ok(m) => m,
            Err(_) => return id,
        };
        if mu.contains_key(&id) {
            false
        } else {
            mu.insert(id.clone(), upstream::SubJob { state: "queued".to_string(), err: None, head: String::new(), cues: Vec::new(), dlg: Vec::new(), id: std::collections::HashMap::new(), cache: String::new() });
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
pub async fn subid_status(jobs: web::Data<upstream::SubJobs>, q: web::Query<SubIdQuery>) -> Result<HttpResponse, AppError> {
    Ok(HttpResponse::Ok().json(sub_status_of(&jobs, &q.id, q.at.unwrap_or(0.0))?))
}

/// VTT Indonesia, parsial selama jalan (cue sisa fallback EN), penuh saat done.
#[get("/subid/result")]
pub async fn subid_result(jobs: web::Data<upstream::SubJobs>, q: web::Query<SubIdQuery>) -> Result<HttpResponse, AppError> {
    let mu = jobs.mu.lock().map_err(|_| AppError::Upstream("lock".to_string()))?;
    let j = mu.get(&q.id).ok_or_else(|| AppError::NotFound("job tak ada".to_string()))?;
    if j.state == "failed" {
        return Err(AppError::Upstream(j.err.clone().unwrap_or_else(|| "gagal".to_string())));
    }
    if j.cues.is_empty() {
        return Ok(HttpResponse::Ok().content_type("text/vtt").body("WEBVTT\n\n"));
    }
    if q.only.as_deref() == Some("id") {
        return Ok(HttpResponse::Ok().content_type("text/vtt").body(upstream::build_vtt_id(&j.head, &j.cues, &j.id)));
    }
    Ok(HttpResponse::Ok().content_type("text/vtt").body(upstream::build_vtt(&j.head, &j.cues, &j.id)))
}

/// Proxy playlist: /api/hls?u=<base64url master m3u8> — isi ditulis ulang ke /api/* (base64).
#[get("/hls")]
pub async fn hls(http: web::Data<reqwest::Client>, q: web::Query<ProxyQuery>) -> Result<HttpResponse, AppError> {
    let url = upstream::decode_target(&q.u)?;
    let f = upstream::fetch_upstream(&http, &url, q.origin.as_deref(), q.referer.as_deref(), None).await?;
    let text = String::from_utf8(f.body.to_vec()).map_err(|_| AppError::Upstream("playlist bukan utf8".to_string()))?;
    if !text.contains("#EXTM3U") {
        return Err(AppError::Upstream("upstream bukan playlist (kemungkinan diblokir)".to_string()));
    }
    let base = url.rsplit_once('/').map(|(b, _)| b).unwrap_or(&url);
    let out = upstream::rewrite_playlist(base, &text, q.origin.as_deref(), q.referer.as_deref());
    Ok(HttpResponse::Ok().content_type("application/vnd.apple.mpegurl").body(out))
}
