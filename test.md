# Test

Terakhir: 2026-10-17 — implementasi V saja (Rust dihapus), alur API diuji sampai bisa
streaming, plus SPA ter-embed.

| Perintah | Hasil |
| --- | --- |
| `v test internal/` | **8 file, 22 test — 8 passed, 0 failed** (offline, deterministik) |
| `v fmt -verify .` | bersih (0 diff) |
| `v vet .` | exit 0 (hanya notice "fixed array" di `upstream`, tanpa error) |
| `v -o zanime .` | build dev sukses (≈5,1 MB, aset dari disk) |
| `v -d embed_dist -o zanime .` | build ter-embed sukses (≈6,8 MB, `web/dist` di dalam biner) |
| `ZANIME_LIVE=1 v test internal/app/live_test.v` | **passed** (±48 dtk, upstream nyata) |
| `ZANIME_LIVE=1 ZANIME_LIVE_GEMINI=1 …live_test.v` | **passed** (termasuk 1 batch terjemahan) |

## Daftar test (22)

`internal/app/app_test.v` (4) — mapping + validasi query:

- `test_catalog_pick` — filter enum → `key:[VALUE]`, nilai asing → error, kosong → skip
  *(Rust `handlers::catalog_pick_sintetis`)*
- `test_to_out_proxy` — cover/banner katalog → `/api/fetch?u=`
  *(Rust `handlers::to_out_proxy_sintetis`)*
- `test_episode_item` — judul `en > x-jat > ja`, img kosong → tanpa proxy
  *(Rust `handlers::episodes_map_sort_sintetis`)*
- `test_pick_en_sub` — `english (cr english)`, `English`, `en` polos, hanya spanish, `{}`
  *(Rust `handlers::pick_en_sub_handles_real_upstream_shapes`)*

`internal/app/server_test.v` (1) — server nyata di `127.0.0.1:39177`:

- `test_server_end_to_end` — `/api/stats` 200 JSON, `/api/search?query=` 400,
  `/api/fetch` SSRF 400, `/api/subid/status?id=nope` 404, `/api/subid/batch` 400
  (GEMINI_API_KEY kosong)

`internal/app/flow_test.v` (1) — alur API penuh lawan upstream + CDN tiruan:

- `test_api_flow_search_to_stream` — search (`id`, `cover`/`banner` → `/api/fetch?u=`,
  `status`) → catalog (400 sort/direction/status/format/season asing, **clamp limit
  999 → 200**, **offset 30 → halaman `p2`**, semua filter sekaligus) → anime
  base/seasons/relations/recommendations/characters (400 tanpa id, 404 id tak ada) →
  episodes (terurut + `title`/`titleRomaji`/`titleJp`) → servers → sources →
  **stream** (probe CDN tiruan → `/api/hls?u=`, `sub_en`, `sub_id`) → subid status
  (`ready`) + result (`text/vtt`, `only=id`) + 404 → guard `/api/fetch` &
  `/api/hls` host privat 400 → comments 400 → anilist 400 → stats

`internal/app/spa_test.v` (3) — penyajian SPA (fixture dir + peta embed):

- `test_spa_from_disk_fixture` — index 200 `text/html` `no-cache`, fallback rute
  (`/watch/abc/1`, `/browse/trending?page=2`), aset `immutable`, **`.gz` precompress**
  (byte `.gz` apa adanya + `content-encoding: gzip` + `vary`), ETag `-gz` → **304**,
  tanpa `.gz` → tanpa encoding/vary, `HEAD` tanpa body
- `test_spa_from_embedded_map` — jalur embed (tanpa berkas di disk): index + fallback
  rute + aset + negosiasi gzip
- `test_spa_without_assets_is_404` — tanpa aset → 404

`internal/app/live_test.v` (1) — **opt-in** (`ZANIME_LIVE=1`):

- `test_live_api_flow_streams` — upstream nyata: search → catalog → anime → episodes →
  servers → sources → **stream** → `/api/hls` master ditulis ulang → varian →
  **`/api/fetch` segmen** (`video/*`, byte awal `0x47`) → cover lewat proxy →
  `subid/status` (`ready`) → `subid/result` (`text/vtt`, `-->`) →
  skiptimes/episode-meta/comments/anilist/stats; dengan `ZANIME_LIVE_GEMINI=1`:
  `POST /api/subid/batch` → `done == 10`, delta VTT, penanda `[N]` habis dikonsumsi

`internal/cache/cache_test.v` (1):

- `test_tanpa_redis_lewat_langsung` — tanpa Redis hasil upstream lewat apa adanya
  *(Rust `cache::tanpa_redis_lewat_langsung`)*

`internal/upstream/stream_test.v` (1) — pipeline streaming lawan CDN tiruan:

- `test_cdn_pipeline_probe_rewrite_fetch` — `probe_stream` (master → varian → segmen),
  `rewrite_playlist` master → `/api/hls?u=` & media → `/api/fetch/seg-1.ts?u=` (tak ada
  URL CDN tersisa), `fetch_proxied` (MIME dari **sniff** `video/mp2t` walau label
  upstream `octet-stream`, byte awal `0x47`, `Range` → 206 + `content-range`),
  `fetch_upstream` VTT + `parse_vtt`/`cue_lines`

`internal/upstream/upstream_test.v` (10):

- `test_parse_build_vtt` *(Rust `vtt_sintetis_roundtrip`)*
- `test_ts` *(Rust `ts_parse_sintetis`)*
- `test_next_at_sintetis` *(Rust `handlers::next_at_sintetis`)*
- `test_decode_target_ssrf` *(Rust `b64_target_ssrf_sintetis`)*
- `test_playlist_urls` *(Rust `playlist_urls_master_vs_media`)*
- `test_img_url_shapes` *(Rust `img_url_shapes`)*
- `test_rewrite_playlist` *(Rust `rewrite_playlist_sintetis`)*
- `test_sniff_mime` *(Rust `sniff_mime_sintetis`)*
- `test_gemini_pick_key_round_robin` *(Rust `throttle_cooldown_fail_fast`)*
- `test_gemini_shapes_sintetis` *(Rust `gemini_shapes_sintetis`)*

## Bukti live

- Stream: `/api/hls` master 200 `mpegurl` → varian → segmen `/api/fetch/…ts` 200
  `video/mp2t`, 238.008 B, sync `0x47`; cover lewat `/api/fetch` 200 `image/*`.
- Subtitle: VTT EN 19.972 B → status `ready` (302 cue) → `POST /api/subid/batch` 6,5 dtk
  → `done=10`, delta 10 cue Indonesia, `<i>` & baris ganda utuh, `[N]` habis dikonsumsi.
- Embed: biner `-d embed_dist` dijalankan dari direktori **tanpa** `web/dist`:
  `/` 200 `text/html`, aset `assets/*.js` 200 dengan `content-encoding: gzip` + `vary` +
  `immutable`, `/watch/a/1` 200 (fallback SPA).

## Verifikasi manual di browser (2026-10-17)

Dilakukan sendiri oleh user di browser, bukan di lingkungan agen:

- **Playback lancar** — video diputar di halaman `/watch/:id/:ep` tanpa macet
  (HLS lewat `/api/hls` + segmen `/api/fetch`, `hlsjs-video`).
- **Subtitle keluar** — subtitle tampil di player; terjemahan Indonesia ikut muncul
  saat video berjalan (batch progresif `/api/subid/batch`).

Ini menutup batas "playback belum bisa diuji" — jalur HTTP-nya sudah diuji otomatis
(unit + live), dan hasil nyata di browser sudah dikonfirmasi.

## Bug yang ditemukan & diperbaiki saat uji

- **`internal/upstream/subs.v` (`sub_worker`)** memakai `http.fetch` tanpa UA/header →
  CDN subtitle balas **403 HTML** → job `failed` ("bukan VTT"), subtitle ID mati.
  Fix: `internal/config` dapat `const browser_ua`, `upstream/fetch_with` (proxy.v)
  mengirim UA itu untuk semua request keluar, dan `sub_worker` memakai
  `fetch_upstream` (UA + `Origin`/`Referer` megaplay) seperti versi Rust.

## Menjalankan

```bash
make deps                                          # v install gechandesu.embedfs (sekali)
v test internal/                                   # offline, deterministik
v fmt -verify . && v vet .
ZANIME_LIVE=1 v test internal/app/live_test.v      # jaringan (upstream nyata)
ZANIME_LIVE=1 ZANIME_LIVE_GEMINI=1 v test internal/app/live_test.v  # + kuota Gemini
make build && ./zanime                             # embed + :3000
```

## Batas

- Playback + subtitle di browser **sudah dikonfirmasi manual** (lihat bagian di atas);
  yang tersisa otomatis-nya tetap di level HTTP (status, MIME, byte awal segmen `0x47`,
  playlist valid) + `pnpm check` — agen tidak punya browser untuk mengulanginya sendiri.
- `test_live_api_flow_streams` butuh jaringan dan upstream pihak ketiga (karena itu
  default-nya dilewati).
- `/api/fetch` menahan body di memori (belum streaming per-chunk); `stats.sample()`
  membaca `/proc/self` (khusus Linux).
