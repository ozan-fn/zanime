# Test

Terakhir: 2026-10-10 — `cargo test` → 13 passed, 0 failed. Offline sintetis, tanpa network.

Fix: `sniff` di `src/upstream/mod.rs` — magic image/mp4 dicek dulu sebelum sync-byte TS (GIF `0x47...` sempat salah label `video/mp2t`).
Fix: `sniff` cek brand `avif`/`avis` sebelum `ftyp` generik + `mime_for` tambah `.avif`/`.aac`/`.mp3` (TDD: red `video/mp4` → green).

- `cache::tanpa_redis_lewat_langsung` — ok
- `handlers::pick_en_sub_handles_real_upstream_shapes` — ok
- `handlers::catalog_pick_sintetis` — scraping filter sort/status — ok
- `handlers::to_out_proxy_sintetis` — scraping cover/banner → /api/fetch — ok
- `handlers::episodes_map_sort_sintetis` — scraping episodes map+sort — ok
- `handlers::next_at_sintetis` — streaming next_at done/kosong → -1 — ok
- `upstream::img_url_shapes` — scraping cover object/string — ok
- `upstream::b64_target_ssrf_sintetis` — streaming SSRF block — ok
- `upstream::playlist_urls_master_vs_media` — streaming master vs media — ok
- `upstream::rewrite_playlist_sintetis` — streaming rewrite → /api/hls + /api/fetch — ok
- `upstream::sniff_mime_sintetis` — streaming sniff/mime/is_m3u8 — ok
- `upstream::vtt_sintetis_roundtrip` — streaming parse/build VTT — ok
- `upstream::ts_parse_sintetis` — streaming ts_start/ts_end — ok
