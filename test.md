# Test

Terakhir: 2026-10-13 — `cargo test` → 15 passed, 0 failed (0.00s). Offline sintetis, tanpa network. `cargo clippy` → 0 warning. `cargo fmt --check` → bersih. `pnpm --dir web check` → 0 errors, 0 warnings.

Live: `gemini-3.1-flash-lite` batch 10 cue + system prompt utuh → 7.45 dtk, 10/10 cue format `[N]` valid, honorifik utuh. (`gemini-flash-lite-latest` 503 high-demand, ganti ke stable ID.)

Migrasi Mistral → Gemini (`GEMINI_API_KEY`, `generateContent` + `x-goog-api-key`, system prompt lama utuh, 429/RESOURCE_EXHAUSTED → 429 + cooldown ikut `retryDelay`):

Fix: rate-limit Mistral → HTTP 429 beneran (`AppError::RateLimited`, dulu 502) — frontend teruskan pesan apa adanya, retry 10 dtk tetap jalan.

Fix: `mistral_chat` di `src/upstream/mod.rs` — endpoint `/v1/conversations` (Le Chat stateful, ~25 dtk/batch) → `/v1/chat/completions` + system prompt 1 kalimat + timeout 120→30 dtk.
Fix: `mistral_chat` hapus loop backoff 6x/95 dtk yang menahan permit semaphore → gagal-cepat saat 429 + cooldown global 20 dtk (`MISTRAL_COOLDOWN_UNTIL`, clamp 5–60 dtk, hormati `Retry-After` bila ada).
Fix: retry `needMore` di `web/src/pages/Watch.svelte` — tanpa jeda → 10 dtk, hormati cooldown backend (tadinya thundering herd saat 429).

Fix: `sub_status_of` di `src/handlers/mod.rs` — lock `order` 2x + iterasi 2x → 1 lock, 1 pass (posisi + total antrean sekaligus).
Fix: `probe_stream` di `src/upstream/mod.rs` — skip unduh body saat status non-2xx (error-page Cloudflare) + probe segmen 1MB → 64KB (byte count dibuang caller; sekuensial + early-exit dipertahankan: paralel = request CDN lebih banyak, bukan lebih sedikit).
Fix: `Player.svelte` — 2 `setInterval` (1s durasi + 500ms skip) → 1 timer 500ms.
Fix: `sniff` di `src/upstream/mod.rs` — magic image/mp4 dicek dulu sebelum sync-byte TS (GIF `0x47...` sempat salah label `video/mp2t`).
Fix: `sniff` cek brand `avif`/`avis` sebelum `ftyp` generik + `mime_for` tambah `.avif`/`.aac`/`.mp3` (TDD: red `video/mp4` → green).

- `cache::tanpa_redis_lewat_langsung` — ok
- `handlers::pick_en_sub_handles_real_upstream_shapes` — ok
- `handlers::catalog_pick_sintetis` — scraping filter sort/status — ok
- `handlers::to_out_proxy_sintetis` — scraping cover/banner → /api/fetch — ok
- `handlers::episodes_map_sort_sintetis` — helper `episode_item` prod dipakai bareng handler + test (dulu logika mapping diduplikat di test) — ok
- `handlers::next_at_sintetis` — streaming next_at done/kosong → -1 — ok
- `upstream::img_url_shapes` — scraping cover object/string — ok
- `upstream::b64_target_ssrf_sintetis` — streaming SSRF block — ok
- `upstream::playlist_urls_master_vs_media` — streaming master vs media — ok
- `upstream::rewrite_playlist_sintetis` — streaming rewrite → /api/hls + /api/fetch — ok
- `upstream::sniff_mime_sintetis` — streaming sniff/mime/is_m3u8 — ok
- `upstream::vtt_sintetis_roundtrip` — streaming parse/build VTT — ok
- `upstream::ts_parse_sintetis` — streaming ts_start/ts_end — ok
- `upstream::throttle_cooldown_fail_fast` — 429 → gagal-cepat tanpa network + status 429 — ok
- `upstream::gemini_shapes_sintetis` — parse parts + deteksi RESOURCE_EXHAUSTED/retryDelay — ok
