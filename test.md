# Test

Terakhir: 2026-10-21 — `cargo test` → 19 passed, 0 failed (offline sintetis, tanpa network) + 5 ignored (live). `cargo clippy --all-targets` → 0 warning. `pnpm --dir web check` → 0 errors, 0 warnings. `pnpm build` → ok.

## Ronde 2026-10-21

LLM: Gemini → endpoint kompatibel OpenAI (`OPENAI_API_KEY`, `OPENAI_BASE_URL=https://kenari.id/v1`, `OPENAI_MODEL=gemma-4-31b-it`), `llm_chat` → `<base>/chat/completions` + Bearer, respons `choices[0].message.content`, rate limit 429 (cooldown 20 dtk/key). Live: 1 batch dengan system prompt asli → `[3] …` / `[7] …`, 10/10 format `[N]` valid. Test bentuk respons: `upstream::llm_shapes_sintetis`.

Player: kualitas terakhir disimpan di localStorage `zanime-quality` (tinggi rendisi, bukan id — id hls.js cuma indeks level) dan dipulihkan tiap sumber baru; `{#key url}` dipindah ke dalam `<video-skin>` supaya `media-container` (= elemen fullscreen `VIDEO-SKIN`) tidak dibongkar saat pindah episode → fullscreen bertahan di auto next; `play()` eksplisit setelah ganti media (`autoplay` tidak berlaku untuk elemen pengganti); jeda selama fetch subtitle (`busy`) lalu lanjut otomatis; `lastDur` jadi `$state` agar tanda OP/ED muncul saat durasi diketahui. Konfigurasi pembatas preload (`preload="metadata"`, `HLS_LIMITS`) sengaja tidak diubah.

Jeda subtitle: video hanya ditahan saat cue terjemahan **benar-benar habis** — Player menerima `subUntil` (akhir cue terjauh yang sudah diterima, dari `lastCueEnd` di `web/src/lib/vtt.ts`) + `subDone` + `idSub`; prefetch yang masih punya cadangan cue tidak lagi mempause. Saat batch tiba `subUntil` maju → video lanjut sendiri. Kalau retry habis (`subProg` = "Subtitle gagal…") `idSub` dimatikan agar video tidak tertahan selamanya. `lastCueEnd` diuji lewat node (5/5: cue setting `align:`/`position:`, lintas jam, tanpa cue, string kosong). `pnpm check` 0/0, `pnpm build` ok.

Halaman anime: chip genre (`$detail.genres`).

Cache subtitle: **redis saja**, tanpa disk. Kunci `subid:<hash VTT EN>` (`upstream::sub_cache_id`), TTL 30 hari (`SUB_CACHE_TTL`); worker membaca redis saat job dibuat, VTT penuh ditulis saat job selesai (guard mutex tidak melintasi `await`). Field `SubJob.cache` (path disk) diganti `SubJob.key` (kunci redis); `.cache/subtitles/` tidak dipakai lagi. Belum diuji dengan redis hidup di environment ini.

Verifikasi perilaku player saat itu memakai Chromium (harness sementara + app asli, sudah dihapus): pilih 480p → tersimpan → reload → terpulihkan; ganti sumber saat fullscreen → fullscreen + 480p tetap, video lanjut; `busy` true → pause, false → lanjut; ep 1 → ep 2 lewat `location.hash` → player sama, fullscreen tetap, ep 2 main otomatis. Sesuai instruksi terakhir, pengetesan browser tidak dipakai lagi.

Live (yuki + minky, dari `y.txt`): `cargo test --test stream_live -- --ignored --nocapture` → 4 passed.
- `yuki_sub_subtitle_en_stream_auto_skip` — `/api/stream` (provider yuki, type sub) → sub EN VTT `text/vtt`, master → varian → segmen `0x47` (TS), `chapters` Intro 159–249 / Outro 1265–1332, `/api/skiptimes` MAL 37579 kosong (`found:false`, memang belum terdaftar di aniskip) dan MAL 21 → op 310.571–400.571 (pipenya hidup).
- `minky_bisa_stream_langsung` — langsung ke host minky (tanpa backend kita): master 615 B, varian 62.369 B, segmen 827.576 B label `image/jpeg` tapi isi TS, sub EN 25.441 B VTT.
- `minky_lewat_api_kita` — jalur player kita di atas URL minky: `/api/hls` → varian → `/api/fetch/seg.ts` 200 `video/mp2t` 827.576 B.
- `stream_minky_dulu_lalu_cache` — kode production: `/api/stream` mengembalikan URL minky, panggilan kedua URL sama (cache), dan URL itu benar-benar bisa diputar sampai segmen.
- `kandidat_mati_ditolak_dan_fallback_yuki_hidup` — kandidat minky yang diarahkan ke CDN tidak ada → `probe_stream` **Err** (jadi fallback akan jalan, tidak "Ok palsu"); kandidat fallback (`sources` apa adanya) → Ok 65.536 B dengan URL CDN provider (bukan minky). Urutan kandidat (minky → CDN) diuji offline: `upstream::minky_dan_token_sintetis`.

Sinkronisasi subtitle (dicek langsung, bukan asumsi): VTT EN lewat `/api/fetch` byte-identik dengan upstream; jadwal `-->` VTT Indonesia sama persis dengan EN (396 cue, naik monoton); playlist minky = playlist CDN (279 segmen / 1420.047 dtk / seq 1); PTS segmen pertama +0.18 dtk dan konstan di segmen 0/139/278 (nol drift). The Garden of Words: 390 cue, pertama `00:00:34.540`, terakhir berakhir 2729.29 dtk dari film ~2749 dtk. Kesimpulan: pipeline tidak menggeser/memotong timestamp — kalau ada yang meleset, itu dari file sumbernya, dan ditangani kontrol delay subtitle.

Fitur baru (web): delay subtitle `−/0.0s/+` di sebelah Auto next (0.5 dtk, ±10 dtk, localStorage `zanime-subdelay`, VTT digeser di klien; delay 0 = URL asli) dan lanjut-dari-posisi-terakhir (localStorage `zanime-pos:<id>:<ep>`, tiap 5 dtk, dihapus saat tamat/10 dtk terakhir). `pnpm check` 0/0; `pnpm build` ok; `shiftVtt` diuji dari file sumber lewat node — 6/6 ok (±dtk, clamp 0, cue setting, lintas jam, delay 0); bundel tersaji memuat `zanime-subdelay`, `zanime-pos:`, `padStart(6`.

Semantik auto refresh (server nyata, token berlaku 91 dtk): panggilan ke-2 saat token masih berlaku → URL **sama** (tidak refresh berkala); tunggu sampai token lewat `exp` (+5 dtk) → panggilan ke-3 URL **baru** (refresh setelah expired). Token CDN di dalam URL minky dibaca lewat `pct_decode` (`upstream::token_expiry`), jadi cachenya ikut `exp` token, bukan TTL buta.

Server nyata (redis siap, `PORT=3000`): `/api/stream` → URL minky; panggilan berulang URL identik; **restart proses → URL masih identik** (cache Redis terbaca). `/api/hls` 841 B → varian 97.296 B → `/api/fetch/seg.ts` 200 `video/mp2t` 827.576 B (`47 40 00 10`).

Temuan: token m3u8 (`?token=<b64(exp|path)>.<sig>`) nominal hanya ~90 dtk, tapi **CDN tidak menegakkannya** — URL dari `y.txt` (16 menit lewat) tetap 200. Auto-refresh karena itu jaring aman + penghemat probe (~800 KB/probe lewat minky), bukan perbaikan playback mati. Provider yang benar-benar ada: `nero`, `yuki`, `sora`, `loli` — `minky` **bukan** provider (`404 provider not found`), melainkan host player.

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
- `upstream::rewrite_playlist_sintetis` — streaming rewrite → /api/hls + /api/fetch (+ varian tanpa ekstensi minky tetap playlist, segmen tanpa nama → `seg.ts`) — ok
- `upstream::minky_dan_token_sintetis` — bungkus URL minky, urutan kandidat (minky → CDN), parse `?token=` — ok
- `upstream::play_cache_basi_sintetis` — aturan basi/penyegaran entri `play` — ok
- `cache::tanpa_redis_get_set_aman` — `get`/`set` tanpa redis aman — ok
- `tests/stream_live.rs` — 4 tes live `#[ignore]` (yuki, minky langsung, minky lewat `/api/*`, minky-first + cache) — ok
- `upstream::sniff_mime_sintetis` — streaming sniff/mime/is_m3u8 — ok
- `upstream::vtt_sintetis_roundtrip` — streaming parse/build VTT — ok
- `upstream::ts_parse_sintetis` — streaming ts_start/ts_end — ok
- `upstream::throttle_cooldown_fail_fast` — 429 → gagal-cepat tanpa network + status 429 — ok
- `upstream::llm_shapes_sintetis` — parse `choices[0].message.content` + deteksi rate limit 429 — ok
- `upstream::sub_cache_id_sintetis` — hash VTT EN stabil, isi beda → kunci beda — ok
- `upstream::pick_key_round_robin` — rotasi key, lompati yang cooldown, gagal-cepat 429 — ok
