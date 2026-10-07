# Progress

## 8. Fix proxy streaming mati total — selesai
- Root cause: kode tak bisa di-build (`to_err_map` tak ada) → binary jadwal lama yang dipakai → video 0 menit.
- Fix mime: CDN menyamarkan segmen (`.jpg`/`.js`/`.css`/`.html` tapi isi MPEG-TS) → `mime_for` by-extension salah → sekarang selalu sniff chunk pertama (magic bytes), fallback ekstensi → label upstream → octet-stream. Satu jalur, tanpa 2-layer if/else.
- Hapus `Fetched.crange` (tak pernah dipakai).
- Verifikasi curl (provider yuki, sub eng): master 200 (3 varian), media playlist 200 (333 segmen + ENDLIST), semua 333 segmen 200 & TS valid, total durasi 23.7 menit, sub VTT `text/vtt`, segmen `video/mp2t`. clippy -D warnings 0.

## 9. Player `Invalid base URL` — selesai
- Penyebab: `abs()` mengembalikan path relatif (`/api/hls?u=…`); parser player resolve URI via `new URL(uri, base).href` dengan `base` kosong → `TypeError: Failed to construct 'URL': Invalid base URL`, manifest gagal → durasi 0:00.
- Fix: `abs()` di `web/src/lib/api.ts` → `new URL(u, location.origin).href` (relatif jadi absolut, http(s) apa adanya).
- Verifikasi simulasi alur player (node, fungsi resolve sama): src absolut → varian → segmen → HTTP 200, TS sync, durasi 25 menit, sub `text/vtt`. `pnpm check` 0 error/warning, `web/dist` di-rebuild + disajikan ulang oleh Rust.

## 10. Spinner selamanya — tipe media tak terbaca player
- Penyebab: player (`@videojs/html` bundle) menentukan tipe media dari **ekstensi URL**: `{".ts":"video/mp2t",".aac":"audio/aac"}` → `VE(pathname)`. URL proxy kita `/api/fetch?u=<b64>` tak punya ekstensi → `mimeType` undefined → player salah jalur (pikir bisa feed langsung ke MSE) → append gagal, video tak pernah mulai.
- Fix: `rewrite_playlist` menulis segmen sebagai `/api/fetch/<nama>.ts?u=<b64>` (`.mp4` bila playlist punya `#EXT-X-MAP`); atribut `URI="…"` (key/map/media) tetap `/api/fetch?u=`. Route ditambah: `/api/fetch/{name}` (selain `/api/fetch`) via `web::resource`.
- Verifikasi: segmen[0] = `/api/fetch/seg-1-f1-v1-a1.ts?u=…` → deteksi ekstensi = `video/mp2t` (sebelumnya `undefined`), HTTP 200, 809528 B, TS sync, ffprobe h264+aac, durasi 25 menit, sub `text/vtt`, clippy 0.
- Catatan: bundle player tidak punya demuxer TS sendiri (`0x47`/`PAT`/`PMT`/`transmux` = 0 hit) → jalur TS mengandalkan MSE `video/mp2t` bawaan browser.

## 11b. Player video.js 10 + adapter hlsjs — selesai (statis)
- `@videojs/html` (`<hls-video>`, engine SPF) tak bisa MPEG-TS — dokumentasinya sendiri menyebut padanan berbasis hls.js yang "plays MPEG-TS". Solusi resmi: adapter `@videojs/hlsjs-video` → `<hlsjs-video>` (UI/skin tetap video.js 10).
- `Player.svelte`: `@videojs/html/video/player` + `@videojs/html/video/skin` + `@videojs/html/media/hlsjs-video`, isi `<hlsjs-video>` dengan `<track>` subtitle (pola resmi dari guides/captions.md). `hls.js` langsung dihapus (dibawa adapter, v1.6.7).
- Verifikasi: `pnpm check` 0/0; aset `index-CvNDyMSA.js` (961 KB) disajikan; bundel berisi `hlsjs-video`, `video-player`, `video-skin`, `hlsManifestParsed`.

## 11. Player sempat diganti ke hls.js langsung — digantikan 11b

- `@videojs/html` (video.js 10) tak bisa memutar MPEG-TS: bundlenya tanpa transmuxer TS, sedangkan MSE hanya menerima input fMP4 (alasan hls.js memakai mux.js). Sumber CDN semua TS → video mustahil mulai.
- `Player.svelte` ditulis ulang: `hls.js` + `<video controls>`; Safari pakai HLS native (`canPlayType('application/vnd.apple.mpegurl')`). `@videojs/html` dihapus dari `web/package.json`; ditambah `hls.js@1.7.3`.
- Verifikasi: `pnpm check` 0 error/warning; aset `index-CgkQ5s7r.js` (650 KB, berisi `hlsManifestParsed`/`maxBufferLength` = hls.js benar ter-bundle) disajikan Rust; negosiasi gzip benar (`content-encoding: gzip`, 204 KB → 650 KB). `Player` props (`src`, `sub`) tak berubah → `Watch.svelte` aman.
- Batas: playback nyata belum bisa diuji agen (tanpa browser di lingkungan ini).

## 1. CLI anime (dari `y.txt`) — selesai
- Alur: input judul → pilih anime → pilih episode → URL stream + sub English (provider `yuki`).
- API dari HAR: `POST graphql.animex.one/graphql` (`CatalogAnime`), `GET api.anistream.one/rest/api/episodes`, `GET .../sources?...&providerId=yuki`.
- Fix bug pilih episode: browser UA + `Origin`/`Referer`, fallback nomor episode saat API `[]`, judul fallback `en → x-jat → ja`, guard EOF anti-loop.
- Verifikasi stream 1 MB: master m3u8 cuma playlist (~257 B) → resolve varian → segmen → `Range: bytes=0-1048575`, ukur byte aktual.

## 2. API Actix — selesai
- CLI dihapus total. Rust tetap di root; `src/` difolderkan per modul: `config/`, `error/`, `handlers/`, `models/`, `routes/`, `upstream/` + `main.rs`/`lib.rs`.
- `main.rs` bootstrap → `lib.rs:create_app` → `routes.rs` → `handlers.rs` → `upstream.rs`; `ResponseError` → JSON; `Cors::permissive` (untuk player web); port **3000**.
- Log resource tiap 2 detik via `sysinfo` (refresh ditarget ke pid sendiri): `proc_mem`, `proc_cpu`, `sys_mem`.
- Log level default `info` (request Logger: peer localhost + method/path/status terlihat), startup log URL penuh `http://127.0.0.1:3000`.
- `cargo check` + `clippy -D warnings` lolos. Testing runtime milik user, agen hanya statis.

## 3. Semua API `y.txt` jadi proxy `/api/*`, URL base64 — selesai
- Route: `/api/search`, `/api/anime?anilistId=&section=base|seasons|relations|recommendations|characters`, `/api/episodes`, `/api/servers`, `/api/sources`, `/api/stream`, `/api/skiptimes`, `/api/comments`, `/api/episode-meta`, `POST /api/anilist`, `/api/fetch?u=<base64url>`, `/api/hls?u=<base64url>`.
- Nol direct: gambar/cover/sub/segmen/playlist ditulis ulang ke `/api/*`; anti-SSRF via blocklist host privat (CDN berotasi tetap lolos).
- `/api/stream` kembalikan URL ter-proxy (`/api/hls`, `/api/fetch`). `/api/hls` tolak konten non-playlist (blokir Cloudflare → 502 jelas); header default megaplay.
- Multi-resolusi: semua varian dipertahankan + rekursi `/api/hls`; deteksi `.m3u8` abaikan query (`?token=`). Subtitle: `<track>` + `crossorigin`, VTT via `/api/fetch` (CORS + mime diteruskan).
- Gambar dinormalisasi dua sisi (`img_url` backend, `imgOf` web) untuk object `{extraLarge,large,medium}`.
- `test-upstream.sh`: 8 cek upstream langsung (catalog, episodes, servers, sources, master, segmen 1 MB, VTT, aniskip) — 8/8 lolos.
- Tes localhost lolos: search (cover terisi), episodes, detail, relations, stream (probe 1 MB), `/api/hls` (varian 1080p + nested + I-FRAME), sub `WEBVTT`, index 200, gzip/identitas, gambar `image/jpeg`.
- Fix label MIME: CDN salah label (`.ts`→jpeg, `.vtt`→octet-stream) → `mime_for` by-extension di `/api/fetch`.

## 4. Proxy streaming hemat resource — selesai
- `/api/fetch` streaming per-chunk (`bytes_stream`, tanpa buffer penuh), teruskan `content-type`/`content-range`/`content-length` + `Range`. Tidak wajib zero-memory, prioritas CPU/memory.

## 5. Web nonton anime (`web/`, pnpm) — selesai (statis)
- Router `svelte-spa-router` v5, Tailwind v4 (Vite plugin, `@custom-variant dark`), `@videojs/html@10.0.1` (`hls-video` + skin), icon `@lucide/svelte`.
- Struktur `features/<nama>/{api,hooks,stores,types,components,index}.ts`: `search`, `episodes`, `watch`; `pages/`: Home, Anime, Watch, NotFound.
- Tanpa env: same-origin + Vite proxy `/api → 127.0.0.1:3000`, web port **5173**. Default dark, tanpa branding, hanya class `rounded` (4 px).
- Halaman Anime: banner + deskripsi + episode + **Terkait** (prequel/sequel via `section=relations`).
- `pnpm check` 0 error/warning; `pnpm build` ok (warning chunk >500 kB dari player — wajar).

## 6. Serve web dari Rust + Makefile — selesai (statis)
- `web/dist` di-embed via `rust-embed` (`src/spa/`); non-`/api` → `index.html` + aset (mime via `mime_guess`).
- Zero-copy: `Bytes::from_static` + map startup (`OnceLock`), ETag + `304`, `Cache-Control: immutable` untuk `/assets/*`, `no-cache` untuk lainnya, `HEAD` + `GET`.
- Precompress: `vite-plugin-compression` gzip level 9 (53 `.gz`); spa negosiasi `Accept-Encoding` → `.gz` + `Content-Encoding: gzip` + `Vary`, else identitas. `/api/fetch` teruskan `Accept-Encoding`/`Content-Encoding` upstream (tanpa decode → nol CPU).
- `Makefile`: `make server` (cargo run --release, API+web :3000), `make client` (pnpm dev :5173 + proxy /api), `make build` (pnpm build → cargo build --release → `./zanime`).
- `.gitignore`: `/target`, `/zanime`, `web/dist`, `web/node_modules`. `make -n` valid; build/run milik user.

## 7. Biner release kecil — selesai (statis)
- `[profile.release]`: `strip`, `opt-level="z"`, `lto`, `codegen-units=1`. Tanpa `panic=abort` (server jangan mati saat handler panic).
- `web/dist` cuma 1,2 MB (gz 200 KB) → embed tetap murah. Ukur hasil via `make build` + `ls -lh zanime`.

## 12. Bisu log request + tanpa `touch` — selesai
- `Logger` actix dihapus dari `main.rs` (log `res proc_mem=…` tetap). Verifikasi: satu GET nyata → 0 baris request di log.
- `Makefile`: `touch src/spa/mod.rs` dihapus — terbukti cargo sudah mendeteksi perubahan `web/dist` sendiri (`include_bytes!` rust-embed), diuji: ubah `dist/index.html` → `cargo build --release` recompile 37s & konten baru ter-embed.

## 13. Detail anime di halaman Watch — selesai (statis)
- `Watch.svelte`: fetch `fetchDetail(id)` lokal (`$state`), render cover + judul (EN/romaji) + chip (format, season+year, status, durasi, episode, ★score, studio, genre) + deskripsi 3 baris; `Episode {ep}` tetap jadi fallback saat detail belum ada.
- `types.ts`: `AnimeDetail` ditambah `format/status/season/seasonYear/duration/averageScore/source/genres/studios`.
- Verifikasi: `pnpm check` 0/0; aset `index-BoJENobh.js` disajikan (berisi kode chip); data nyata Frieren: `TV FALL 2023 | FINISHED | 24 mnt | 28 eps | ★ 9.1 | Adventure, Drama, Fantasy | MADHOUSE`.

## 14. Halaman Anime tahan episode banyak — selesai (statis)
- `EpisodeList.svelte`: input cari + pagination 50/halaman (prev/next, nomor halaman dengan ellipsis, `x–y dari N episode`, auto-scroll ke daftar, reset ke halaman 1 saat query berubah).
- Pencarian: `term = q.trim().toLowerCase().replace(/^ep(?:isode)?s?\s*/, '')` → nomor episode `startsWith(term)` atau judul `includes(term)`; kosong = semua. Judul kolom jadi `Episode (n dari m)` saat memfilter.
- Verifikasi (`pnpm check` 0/0, aset `index-BwWOXEt5.js` disajikan, matcher baru ada di bundel): Naruto Shippuden 500 ep → 10 halaman (hal.2 = ep 51–100, terakhir = 451–500); cari `episode 5`/`ep5` → 16 cocok mulai ep 5; `1000` → 0 cocok (memang tak ada). One Piece 1180 ep → 24 halaman (terakhir 1151–1180).

## 15. Katalog dari skema animex + home berisi section — selesai
- Introspeksi `graphql.animex.one/graphql` (docs-first): `queryType` = `anime/searchAnime/catalogAnime`; `AnimeSortField` = POPULARITY, TRENDING, AVERAGE_SCORE, MEAN_SCORE, FAVOURITES, SEASON_YEAR, UPDATED_AT, CREATED_AT, EPISODE_COUNT, DURATION, TITLE_ENGLISH, TITLE_ROMAJI, NEXT_AIRING_AT, SUB_COUNT, DUB_COUNT; `AnimeCatalogFilterInput` punya `query/statusIn/formatIn/seasonIn/seasonYearMin|Max/genres/…`.
- Backend: `GET /api/catalog?sort=&direction=&status=&format=&season=&year=&genre=&query=&limit=&offset=` (whitelist enum → 400 jelas), upstream `catalog()` generik; `search()` kini memanggil `catalog()` (dedup). `CATALOG_QUERY` + item: `format seasonYear averageScore`; `AnimeItem`/`AnimeOut` ditambah field itu. Handler `catalog` + route.
- Web: fitur `home/` (`api/hooks/stores/types/index` + `components/HomeSection.svelte`); `Home.svelte` menampilkan 6 section (Sedang Tren, Sedang Tayang, Rating Tertinggi, Musim `<season year>` dinamis, Akan Datang, Paling Difavoritkan) sebagai baris scroll horizontal; pencarian tetap menang bila ada hasil. `AnimeCard` dapat `block` + baris format/tahun/eps + skor.
- Verifikasi: clippy `-D warnings` 0; `pnpm check` 0/0; 9 varian curl lolos (termasuk `sort=BOGUS`→400, `status=BOGUS`→400) + regresi `/api/search` 200; simulasi loader home: 6/6 section 200 dengan 12 item & cover lewat proxy.

## 16. Label segmen TS stabil `video/mp2t` — selesai
- Root cause: `fetch_stream` teruskan `Accept-Encoding` klien → upstream balas gzip → `bytes_stream` tanpa decode → sniff baca byte gzip (`1f8b`), kalah → fallback ekstensi `.jpg` → `image/jpeg`.
- Fix: selalu minta `identity` ke upstream (video/gambar sudah terkompresi → hemat CPU), hapus plumbing `cenc`/`accept_encoding` (`Streamed.cenc`, param, header). Sniff prefix ≤2KB lintas-chunk + single-sync `0x47` (chunk pertama bisa kecil).
- Verifikasi: Frieren ep1 340 segmen, 26.0 menit, ENDLIST, 3/3 `video/mp2t` TS-valid; clippy `-D warnings` 0.

## 17. Subtitle Indonesia progresif (Groq Qwen) — selesai (statis)
- Model `qwen/qwen3.8-27b` via Groq (1.7 dtk/6 cue; GLM flash ditinggal — reasoning ~40 dtk/call). Key di `.env` (`GROQ_API_KEY`, via `dotenvy`), `.env` di-gitignore.
- Job FIFO 1 permit (tak tabrak limit 30 req/mnt, 8K token/mnt, 1K/hari): `POST /api/subid` → `GET /status` (`done/total/queue`) → `GET /result` (VTT parsial, sisa fallback EN). Batch 6 cue/call, jeda 2.5 dtk, 429 retry 3x.
- Prompt profesional (FAR Pedersen: 20 CPS, 42 char/baris, ≤2 baris, honorifik, anti-literal). Per cue (bukan per baris): line-break cue dipertahankan via baris lanjutan parser.
- Cache `.cache/subtitles/<hash-en>.vtt` (gitignore) → tonton ulang instan; dedup job per URL.
- Web: track `Indonesia` muncul progresif (`{#key subVer}`, video tak restart → aman saat seek), indikator kanan-bawah `Subtitle Indonesia… N/total`, hilang saat done.
- Verifikasi: 9 cue → 1.4 dtk, honorifik (`-sama/-kun/Sensei`) + jeda baris utuh; `clippy -D warnings` 0, `pnpm check` 0/0.

## 18. Subtitle on-demand posisi (3 sisa → 6 berikut) — selesai (statis)
- Worker cuma fetch+parse EN (tanpa LLM); `POST /api/subid/batch` terjemahkan 6 berikut, 1 permit semaphore → serial antar user.
- Backend kirim `next_at` (akhir cue done-3 minus 2 dtk); Player `timeupdate` → lewat ambang = 1 batch. Tanpa runaway; seek ikut kejar berurutan.
- Track Indonesia + English di player; indikator kanan-bawah `N/total`, hilang saat done. Key Groq di `.env`, cache `.cache/subtitles/`.
- Verifikasi: `clippy -D warnings` 0, `pnpm check` 0/0. Tanpa test live (hemat limit).

## 19. Indonesia di menu sejak awal — selesai (statis)
- Sebab: track blob dipasang telat → menu player snapshot saat init. Fix: `sub_id` dikembalikan `/api/stream` langsung; track `Indonesia` src stabil `/api/subid/result?id=` sejak paint pertama, reload per batch via key.
- `result` kosong → `WEBVTT` 200 (entri menu aman, bukan 502). Blob/URL.revoke dihapus. Error sub tampil di indikator (tak lagi silent).
- Verifikasi: `clippy -D warnings` 0, `pnpm check` 0/0, `dist` rebuild + embed ulang. Butuh restart backend.

## 20. Cooldown batch anti-loop — selesai (statis)
- Sebab lag/loop: gagal/tanpa progres → `timeupdate` 4Hz memanggil batch terus (track reload tiap kali). Fix: cooldown 15 dtk + skip bila `done` tak maju.
- Verifikasi: `pnpm check` 0/0, `dist` rebuild + embed. Tanpa test live.

## 21. Cache Redis + README — selesai
- `redis` 1.7.1 (tokio-comp + tokio-rustls-comp, untuk `rediss://`); `.env` `REDIS_URL` (gitignored) → `Config.redis_url`.
- `src/cache/mod.rs`: satu fungsi `cached(key, ttl, future)` — hit → JSON dari Redis, miss/error → jalankan upstream lalu `SET … EX ttl`. Best-effort total: redis mati/URL salah/isi rusak tidak pernah jadi 5xx. Dipanggil sekali di `main` sebelum `HttpServer`; pesan `cache: redis siap` / `cache: redis gagal (…) → tanpa cache`.
- Dipasang di `upstream`: `catalog` (+`search` ikut lewat catalog), `anime`, `episodes`, `servers`, `skiptimes`, `comments`, `episode_meta`. TTL: 600 daftar, 3600 metadata, 86400 skiptime.
- **Sengaja tidak**: `sources`/`stream` (URL CDN bertanda tangan + berkedaluwarsa `?token=<exp>.<hmac>` → entri basi = playback mati), `hls`/`fetch` (streaming, per-`Range`), `anilist` (query bebas klien → key tak terbatas), `subid/*` (stateful).
- Bersih-bersih lint dari clippy 1.98 yang bikin `-D warnings` gagal sejak sebelum perubahan: `manual_map` (mistral content) + `useless_vec` (`build_vtt_id`) — semantik sama.
- Verifikasi (redis Upstash nyata + TLS): startup `cache: redis siap`; key `catalog:POPULARITY:DESC:3:0:{}` ada dengan `ttl=594` dan isi JSON katalog asli; **read path** diuji dengan menyuntik sentinel → respons API = sentinel (bukan data upstream); `episodes`/`anime`/`skiptimes` ter-cache (ttl 3589/3589/86389), `sources` + `stream` 200 tanpa key sama sekali; `REDIS_URL` diarahkan ke port mati → warn + API tetap 200. `cargo clippy --all-targets -- -D warnings` 0.
- `README.md` baru: ringkasan, struktur, cara jalan (Makefile), env, tabel endpoint + TTL, aturan cache, pipeline subtitle, web, batasan.

## 22. Lihat semua per section + batas limit upstream — selesai
- Web: route `/browse/:key`, halaman `pages/Browse.svelte` — satu section penuh (katalog 30/halaman via `offset`, auto-load saat sentinel bawah masuk viewport, `$effect` + `untrack` biar tak loop). `SECTIONS`/`sectionByKey` di `features/home/hooks.ts` dipakai bareng baris home; `HomeSection.svelte` dapat link "lihat semua".
- API tak perlu endpoint baru: `/api/catalog` yang ada sudah menerima `sort`/`status`/`season`/`year`/`offset`. y.txt dicek: cuma detail/subtitle/sources/servers, tak ada endpoint list.
- Bug ketemu saat uji: `catalogAnime` membalas `items: null` bila `limit > 30` → 502 "invalid type: null, expected a sequence"; clamp handler salah (1–50). Fix root: `UPSTREAM_MAX_PAGE = 30` di `handlers`, `UPSTREAM_MAX_PAGE` dipakai handler + `PER_PAGE` halaman browse.
- Verifikasi: `svelte-check` 0/0; 6 section × 2 halaman (limit=30) → 30+30 item, tumpang-tindih 0; `limit=50/999` → 30 item (tak lagi 502), `limit=0` → 1; aset tersaji `index-D65XK6sv.js` memuat `browse/:key` + "lihat semua"; `/browse/trending` → 200 text/html; gzip 274 KB. `clippy -D warnings` 0. Batas: klik/render di browser tak bisa diuji di lingkungan ini.

## 23. Pagination halaman lihat-semua, cache sehari, kartu +status — selesai
- `Browse.svelte`: pagination bernomor (prev/next + elipsis) dengan nomor halaman di URL (`/browse/:key?page=N` via `router.querystring` + `push`), scroll ke atas tiap ganti halaman, tombol "coba lagi" saat gagal, 5 kolom di layar lebar. IntersectionObserver (auto-load) dihapus — diganti halaman eksplisit.
- Upstream tak punya `total`/`pageInfo`/`startDate` (dicek langsung ke graphql.animex.one: `Unknown field … on type AnimeConnection`), jadi jumlah halaman heuristik: halaman penuh (30) → ada halaman berikutnya; tombol nomor tumbuh seiring halaman terbukti ada.
- Cache Redis: satu TTL `86400` (sehari) untuk semua entri — sebelumnya 600/3600/86400. Konten tak basi karena kedaluwarsa sendiri lalu diisi ulang.
- `CATALOG_QUERY` + `AnimeItem`/`AnimeOut` + tipe web `Anime` ditambah `status` (diverifikasi ada di AnimeNode: `{"status":"FINISHED"}`); `AnimeCard` kini menampilkan tahun rilis tebal + format + eps + skor + chip status (`Tayang`/`Tamat`/`Akan datang`/`Hiatus`/`Dibatalkan`).
- Verifikasi: `clippy -D warnings` 0, `pnpm check` 0/0; API membalas `"status":"RELEASING"`/`"NOT_YET_RELEASED"`; 3 halaman trending (offset 0/30/60) 30 item berbeda-beda; TTL entri baru `86377`; aset `index-DFHznzHq.js` tersaji memuat "coba lagi"/chip status; `/browse/trending?page=2` 200 html. Batas: klik/render di browser tak bisa diuji di sini.

## 24. Tahun + status di kartu Terkait — selesai
- `ANIME_RELATIONS` menambah `seasonYear status` (dicek dulu ke upstream: node relasi memang punya keduanya — `{"seasonYear":2021,"status":"FINISHED"}`), jadi tak ada asumsi skema.
- `Relation` (web) dapat `seasonYear`/`status`; `RelatedList.svelte` kini seragam dengan `AnimeCard`: tahun tebal + tipe + eps + chip status (`statusLabel` dipakai ulang dari `search/types`), supaya urutan seri (prequel/sequel) kelihatan. Entri tanpa `animeId` dirender sebagai kartu tanpa link (dulu selalu `/anime/` kosong). Urutan tetap dari upstream — tak diurutkan ulang.
- Karena bentuk respons `section=relations` berubah, 7 key `anime:*` lama di Redis dibuang manual (TTL sehari → kalau tidak, halaman akan tampil tanpa tahun sampai 24 jam). Aturan ini ditulis di README.
- Verifikasi: `clippy -D warnings` 0, `pnpm check` 0/0; `/api/anime?id=how-not-to-summon-a-demon-lord-j1kqq&section=relations` kini `2021 | TV | 10 eps | FINISHED`; AoT 10/10 dan Gintama 6/6 relasi punya tahun; aset `index-Din-spn8.js` tersaji. Batas: render di browser tak bisa diuji di sini.

## 25. Judul English+jepang & monitor resource di navbar — selesai
- Judul: `titles` (JSON scalar di AnimeNode) ditambah ke `ANIME_BASE`; episode dapat `titleJp` (kanji, mis. `魔王演技`) dari `titles.ja` upstream (fallback `x-jat`). Web: `japaneseTitle()` di `features/episodes/types.ts` (kanji bila ada, else romaji, kosong bila sama dengan judul utama) dipakai halaman detail + halaman putar; `EpisodeList` menampilkan `titleJp` di bawah judul English.
- Navbar: modul `src/stats` (atomik MEM/PEAK_MEM/CPU/PEAK_CPU; CPU disimpan persen×10) diisi loop sysinfo yang sudah ada tiap 2 dtk, dibaca `GET /api/stats` → `{memMb, peakMemMb, cpu, peakCpu}`. `App.svelte` poll tiap 3 dtk, tampil `md:` ke atas; log `res …` kini ikut mencetak `peak_cpu`.
- Dua kesalahan nyata ketemu & ditutup saat verifikasi: (1) `titles{en ja}` salah — `titles` scalar JSON, seluruh query detail gagal → 404; (2) query yang gagal mengembalikan `Ok(null)` sehingga **null ikut ter-cache** sehari. Fix: `cached` tidak menyimpan `null` + `log::warn!` saat GraphQL balas error tanpa data, jadi kegagalan tak lagi menyamar jadi "anime tidak ketemu" yang lengket.
- Verifikasi: `clippy -D warnings` 0, `pnpm check` 0/0; base → `titleEnglish=How NOT to Summon…`, `titles.ja=異世界魔王と召喚少女の奴隷魔術`; episode 1–3 punya `titleJp` kanji; `/api/stats` → `{"cpu":1.5,"memMb":36,"peakCpu":1.5,"peakMemMb":36}`; id tak ada → 404 **tanpa** key baru di Redis (null tidak di-cache); aset `index-DejpH3qT.js` tersaji memuat `/api/stats` + `titleJp`. Batas: render di browser tak bisa diuji di sini.
- Susulan: awalnya cuma EN + kanji (romaji kelewat). Kini ketiganya tampil — `altTitles()` mengembalikan [romaji, kanji] (dedup, buang yang sama dengan judul utama); `EpisodeItem` dapat `titleRomaji` dari upstream `x-jat` sehingga baris episode menampilkan EN → romaji → kanji. Contoh nyata ep 1: `The Demon Lord Act | Maou Engi | 魔王演技`; anime: `How NOT to Summon a Demon Lord` / `Isekai Maou to Shoukan Shoujo no Dorei Majutsu` / `異世界魔王と召喚少女の奴隷魔術`. Cache `episodes:*` dibuang (bentuk berubah); aset `index-CPKUDLg2.js`; `pnpm check` 0/0, `clippy -D warnings` 0.
