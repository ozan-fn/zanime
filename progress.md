# zanime — progress

HTTP API + SPA web player di atas hianime.at, di-port dari `ani-cli`
(`ref/ani-cli`, 671 baris bash). Backend Go 1.25 + chi; frontend Preact +
preact-router, dibundel rsbuild dengan Tailwind 4 (plugin resmi, bukan CDN
browser). Subtitle diterjemahkan ke Indonesia lewat kenari.id
`deepseek-v4-1-flash` (job latar + progress), video diputar lewat proxy HLS
dengan shaka-player (paket npm, dibundel — bukan skrip CDN). Tanpa ffmpeg sama
sekali: remux `-c copy` sudah dibuang.

## Status

| Bagian | Status |
|---|---|
| `go mod init zanime` + `go get chi/v5` | done |
| Port logika scraping (search / episodes / servers / resolve megaplay AES) | done |
| Parse master playlist + pilih kualitas | done |
| Terjemahan subtitle ID via kenari.id | done |
| Model `deepseek-v4-1-flash` (bukan route gratis) | done |
| Pace 30 panggilan/menit (buatan sendiri) | done |
| Batch beranggaran token, 1 panggilan/episode | done |
| Batas 4 konversi bersamaan | done |
| Cache subtitle ke disk | done |
| SPA Preact + preact-router (rsbuild) | done |
| Detail anime (poster, meta, genre, related) | done |
| Routing per episode + navigasi prev/next | done |
| Status konversi subtitle (job latar + progress) | done |
| Proxy HLS (kualitas di path) | done |
| shaka.ui.Overlay: kontrol + menu kualitas & subtitle | done |
| `go vet` + `go test` | pass (`go test ./...`) |
| `tsc --noEmit` (web) | pass |

## Endpoint — semua API di bawah `/api/`

API dan aset SPA dipisah tegas: chi hanya melayani `/api/*`, aset build
`web/dist` di-embed (`go:embed`) dan disajikan di root. Dev berjalan lewat
proxy rsbuild (`server.proxy['/api'] → :8080`), jadi satu entri proxy untuk
semua endpoint.

| Method | Path | Fungsi |
|---|---|---|
| GET | `/` | SPA (index.html + aset hashed, embed) |
| GET | `/api/healthz` | status + sisa kuota |
| GET | `/api/limits` | pemakaian kuota kenari |
| GET | `/api/search?q=` | cari judul (poster + sinopsis per kartu, poster di-proxy) |
| GET | `/api/img?u=` | proxy gambar CDN (base64url, allow-list host) |
| GET | `/api/anime/{id}` | detail anime: meta, genre, studio, related, recommended |
| GET | `/api/anime/{id}/episodes` | daftar episode |
| GET | `/api/episode/{id}?mode=` | master URL, referer, kualitas, subtitle |
| GET | `/api/subtitle/{id}?lang=id&mode=` | `.vtt` kalau siap, `202` + progress kalau masih dikonversi |
| GET | `/api/subtitle/{id}/status` | `{state,done,total,eta_seconds}` |
| GET | `/api/hls/{id}/master.m3u8?mode=` | master upstream, varian ditulis ulang ke `/api/hls/{id}/{mode}/{label}/index.m3u8` |
| GET | `/api/hls/{id}/{mode}/{quality}/index.m3u8` | playlist media, semua URI segmen ditulis ulang ke `/s/{base64url}` |
| GET | `/api/hls/{id}/{mode}/{quality}/s/{seg}` | segmen absolut (base64url), content-type di-sniff dari byte |

## Frontend

Preact 10 + preact-router, dibundel rsbuild (`@rsbuild/plugin-preact` +
`@rsbuild/plugin-tailwindcss`). Tidak ada skrip CDN apa pun — shaka-player
datang dari npm dan di-import langsung. Struktur:

```
web/src/
  index.tsx          entry render
  App.tsx            Route: /, /s/:q?, /a/:animeId, /w/:animeId/:epId, 404
  components/        Header (pencarian), Player (shaka), NotFound, ui/Icon
  features/search/   Search.tsx + api.ts
  features/watch/    Watch.tsx, Anime.tsx, player.ts, catalog.ts, api.ts
  lib/               api.ts (fetch + /api), urls.ts, types.ts
```

### Routing dan URL

preact-router, path nyata (bukan hash): back, forward, refresh, dan share link
mendarat di halaman yang sama. `mode=dub` ada di query watch; sisanya di path.

| Path | Isi |
|---|---|
| `/` | form cari |
| `/s/{q}` | hasil pencarian |
| `/a/{animeId}` | detail + daftar episode |
| `/w/{animeId}/{epId}?mode=dub` | player |

`animeId` ikut di URL watch supaya halaman itu memuat daftar episodenya sendiri
— tombol Sebelumnya/Berikutnya jalan tanpa mampir ke halaman lain. Pindah
episode wajib bongkar total: preact-router tidak me-remount komponen ketika
hanya param route berubah, dan `useEffect [src]` di Player tidak cukup — video
element lama tetap memegang stream episode sebelumnya. Dua lapis:
`KeyedPage` (`App.tsx`) membungkus halaman dengan `<div key={useUrl()}>` dari
`useRouter()` supaya subtree remount tiap URL berubah, dan link episode
(prev/next/daftar) di `Watch.tsx` pakai **`data-native`** — atribut bawaan
preact-router yang melepas link ke browser, jadi klik = navigasi penuh ke URL
episode baru (MSE/shaka lama ikut lenyap; pindah episode memang harus muat
ulang). Judul diturunkan dulu dari slug (`one-piece-100` → `One Piece`)
lalu diganti judul asli begitu katalog/hasil pencarian memberinya (`catalog.ts`,
Map in-memory yang diisi `rememberTitles`). Halaman `/w/` juga memuat detail
anime (`/api/anime/{id}`): poster, meta, genre, dan sinopsis tampil di bawah
player.

### Player

`components/Player.tsx` memakai `shaka.ui.Overlay` (cara *Programmatic UI
setup* di docs shaka): `new shaka.Player()` + `new shaka.ui.Overlay(player,
container, video)` → `attach(video)` → `load(master.m3u8, 0,
'application/vnd.apple.mpegurl')`. `shaka.Player` polos tidak merender kontrol
apa pun — itu sebabnya dulu video tidak bisa diputar. `controlPanelElements`
memuat `quality` (pilih resolusi dari varian HLS) dan `captions` (on/off +
pilih subtitle Indonesia), `overflowMenuButtons` mengulang keduanya +
`playback_rate`. `fadeDelay: 3` (docs: "delay sebelum kontrol fade out",
default 0) membuat bar hilang 3 detik setelah interaksi; tombol fullscreen
mobile ikut hilang lewat event `showingui`/`hidingui` dari Controls.

Subtitle dipasang lewat `addTextTrackAsync` hanya ketika status `ready` —
file yang belum selesai akan ditolak player (docs: CONTENT_NOT_LOADED), jadi
polling di `Watch` yang mengulang mount lewat prop; `selectTextTrack`
menyalakannya dengan lencana CC singkat. Antarmuka shaka dideklarasikan lokal
(tipe paket tidak diekspor rapi untuk bundler). Galat stream ditangkap di
`catch` load sebagai pesan "Stream gagal dimuat".

Jarak & latar subtitle (`global.css`): `TextDisplayerConfiguration` hanya punya
`fontScaleFactor`, `positionArea`, `subtitleDelay`, `suspendRenderingWhenHidden`
— tanpa opsi posisi, jadi gap dibuat dengan
`.shaka-text-container { transform: translateY(-2rem) }` (transform tidak
bentrok dengan inline `bottom` yang ditulis shaka: tinggi bar saat tampil,
0px saat hilang). Latar cue `rgba(0, 0, 0, 0.8)` yang ditulis inline
UITextDisplayer diturunkan ke `0.5` lewat selector string + `!important`.

Fullscreen iOS: iPhone Safari `document.fullscreenEnabled` false → shaka
terpaksa memanggil `webkitEnterFullscreen` (player native iOS; subtitle
digambar sistem, posisinya hanya bisa diatur di Settings → Subtitles &
Captions, kebal CSS). Docs tak punya opsi mematikan jalur itu, jadi tombol,
double-click, dan rotasi fullscreen dimatikan, lalu container dibuat
fullscreen lewat CSS (**pseudo-fullscreen**, tombol maximize/minimize
sendiri) supaya UITextDisplayer + gap tetap berlaku. Desktop/iPad tetap
fullscreen asli.

### Status subtitle dipolling di `Watch`

`Watch.tsx` mem-poll `/api/subtitle/{id}/status` tiap 2 s selama `converting`
(4 s kalau satu jawaban gagal — job tetap jalan di server, jadi polling sengaja
lengket). Player hanya menerima status sebagai prop; ia tidak tahu ada
penerjemahan yang berjalan. Progress ditampilkan sebagai teks
"Menerjemahkan subtitle… done/total" di bawah player.

## Pemetaan dari ani-cli

| ani-cli (bash) | Go |
|---|---|
| `hianime_search` (:199) | `search` — potong `id="main-sidebar"`, split `film-detail` |
| `hianime_episodes` (:213) | `episodes` — route pakai id angka di ujung slug |
| `hianime_m3u8` (:222) | `resolve` — pilih server pertama per mode, lalu resolve embed megaplay |
| parse `#EXT-X-STREAM-INF` (:240) | `parseMaster` — skip `I-FRAME`, sort tinggi |

### Resolve upstream (2026-09): ZokoAnime → megaplay

Upstream tidak lagi menyajikan server `ZokoAnime` (yang dulu pakai blob
`window.__P` XOR `otaku-embed-v1`). Sekarang hanya `HD-2`/`Vidstream-2`,
keduanya menunjuk embed **megaplay.buzz**. Alur `resolve` yang baru:
1. `serverHash` — ambil `data-hash` server pertama dengan `data-type`
   sub/dub yang diminta (nama server tidak difilter; mode pemisahnya).
2. Decode base64 → URL embed `megaplay.buzz/stream/s-2/{realid}/{sub|dub}?s=...`.
3. Scrap halaman embed → `data-id`, `data-realid`, `data-mediaid` dari
   `#megaplay-player`.
4. GET `megaplay.buzz/stream/getSources?id=&cid=&cidu=` dengan header
   `X-Requested-With: XMLHttpRequest` + referer embed (endpoint menolak
   non-AJAX: 403 "accepts only AJAX requests").
5. Respons JSON: `enc` (token) + `tracks[]` (subtitle .vtt, `label` = bahasa).
6. `megaDecrypt` — AES-256-CBC (`cipher.NewCBCDecrypter`, bukan ECB per-blok);
   key `i?LMTAx0Q6,:}50U` zero-padded ke 32 byte, IV `W0;27ToaUpl_P%'c`
   (keduanya dari `newclient.min.js` embed), base64url, PKCS#7 dibuang manual
   → plaintext `{"file":".../master.m3u8"}`.
7. Master diparse `parseMaster` seperti sebelumnya; subtitle dari `tracks[]`
   `getSources` (label = bahasa, semuanya `default`).

Catatan `go.mod`: modul diganti `module zanime` (dulu `module main`) — `go test`
tidak bisa meng-import package bernama `main` di toolchain baru, jadi trik
swap-modul `check.sh` tidak lagi perlu.

Yang **tidak** dipindah: history/logview, jadwal animeschedule, mpv, syncplay,
IINA, android_mpv, yt-dlp, curl-impersonate failover, dan `download` (:346)
yang butuh remux ffmpeg. Tidak ada yang butuh web player.

## Video

Jalur yang dipakai web player adalah **proxy HLS**: browser tidak bisa mengirim
`Referer` yang diminta host stream, jadi tiga rute mem-proxy playlist + segmen
(lihat tabel endpoint). Kualitas ada di **path**, bukan query: browser
me-resolve URI segmen relatif terhadap URL playlist dan membuang query
(RFC 3986), jadi segmen selalu jatuh ke varian yang benar tanpa kunci tambahan.

- Segmen **di-stream** (`io.Copy` dari body upstream), bukan `io.ReadAll` —
  membuffer satu segmen penuh sebelum byte pertama terkirim menggandakan
  latensi di jalur dua-hop.
- Transport sendiri: `MaxIdleConnsPerHost: 16`. Default Go (2) membuat unduhan
  segmen paralel saling mengantre.
- Segmen `max-age=3600` (immutable), playlist `no-cache` (token upstream
  berumur pendek).
- `reSegment` + allow-list host gambar menjaga proxy tetap tertutup (bukan open
  proxy/relay).

Aplikasi **tanpa ffmpeg sama sekali**: remux `-c copy` dan endpoint stream
dibuang, jadi tidak ada `os/exec`.

Segmen upstream berkamuflase: MPEG-TS dipakaikan nama `.jpg`/`.html` dan
 disimpan di host CDN lain dari playlist-nya (`79qle.hiddenvertex.top` dkk).
 Karena itu playlist varian ditulis ulang penuh — setiap URI segmen (absolut
 maupun relatif) menjadi `/api/hls/.../s/{base64url URL absolut}`, dan
 content-type segmen di-sniff dari magic byte (`0x47` → TS, `ftyp` → MP4),
 bukan dari ekstensi nama file.

## Subtitle Indonesia

1. Ambil `.vtt` asli (hampir selalu Inggris) pakai referer yang sama.
2. Kalau sumbernya belum `id`, parse blok cue WebVTT.
3. Pack cue ke dalam satu panggilan sampai ~`batchTokens`, kirim sebagai
   `<index>|<teks>`, minta balik `<index>|<terjemahan>`, stream SSE. Satu
   episode nyata (285–394 cue) jadi **satu panggilan** — penting bukan cuma
   soal kecepatan: model yang menjawab dipilih acak per panggilan (dulu), jadi
   dua panggilan berarti dua kosakata.
4. Pasang balik ke blok aslinya. Timing, cue id, dan header `WEBVTT` tidak
   pernah disentuh.
4b. **Deduplikasi**: cue dengan teks identik dikirim sekali, hasilnya disalin
   ke semua kemunculannya. Prompt mengecil dan konsistensi jadi gratis.
5. Cue yang tidak dijawab model diulang sekali (pass kedua).

### Job latar + progress

Konversi **tidak** jalan di dalam request (satu episode ~150 s, jauh melewati
timeout request):

- `subtitleStatus` membuat job (`sync.Map` per `episode|mode|lang`) dan
  menjalankannya di goroutine dengan `context.Background()`.
- Job yang sudah ada dijawab dari memori, sebelum upstream disentuh.
- `done`/`total` ditulis bersama oleh `setProgress`, selalu seukuran jumlah
  baris unik yang diminta ke model.
- `/api/subtitle/{id}` → `202` + `{state,done,total,eta_seconds}` selama
  `converting`, lalu `.vtt` setelah `ready` (dengan `no-store` agar 202 tidak
  masuk cache browser).
- `maxConcurrentJobs = 4` (semaphore `jobSlots`) membatasi konversi bersamaan;
  kelebihannya menunggu, bukan gagal.
- `eta_seconds` dihitung dari laju nyata (`done` per waktu berjalan).
- Hasil akhir masuk cache disk `.cache/subtitles` (default), jadi pemutaran
  kedua instan dan tidak memakai kuota dua kali.

### Prompt dan konsistensi

`translateSystem` menjaga hasilnya nyambung: natural spoken Indonesian ala
subtitle anime, nama/jurus tetap romaji, honorifik dipertahankan, dua dasbor
untuk baris dua penutur, tanda baca sumber dipertahankan, istilah berulang
wajib kata yang sama, konsep sehari-hari memakai kata Indonesia baku.
`temperature` 0.2. Deduplikasi menghilangkan sumber ketidakkonsistenan untuk
baris berulang.

### Klien, timeout, dan 429

- `translateBatch` memakai `streamClient` (tanpa `Timeout`) — `httpClient` yang
  ber-timeout 20 s mencakup pembacaan body stream dan itulah akar
  "Client.Timeout while reading body". Batasnya `translateCallTimeout` (5 menit)
  yang dimulai **setelah** `waitForBudget`.
- `limiter.take()` per panggilan model di dalam `translateBatch`, bukan per
  request HTTP — dulu satu request web dicatat 1 padahal memicu ~24 panggilan,
  `/limits` bohong dan burst disambut 429.
- 429 dibedakan: batas laju (`rate_limit_exceeded`, `free_quota_*`,
  `plan_limit_reached`, `upstream_error`, `all_providers_failed`) → batch
  dipecah + `Retry-After` dipakai; penolakan upstream (`upstream_rejected`,
  `invalid_request_error`) → gagal segera, memecahnya cuma membuang kuota.
  `Retry-After` > 1 menit (jatah harian/plan) → job berhenti dan dilaporkan.
- `finish_reason=length` dengan baris belum terjawab → `errTruncated`: batch
  dipecah tanpa jeda.
- 503 `all_providers_failed` diperlakukan bisa dicoba ulang.

## Kuota

Key kenari di-hardcode di `main.go` (`llmKey`) sesuai instruksi.
`https://kenari.id/v1/chat/completions`, model `deepseek-v4-1-flash` — berbayar
(bukan `:free`), konteks 1M, balasan `content` saja untuk prompt ini. Harga
20 IDR/1M token masuk dan 50 IDR/1M keluar, jadi satu episode ≈ Rp 0,2.
`limitRequestsPerMinute = 30` angka sendiri (header `x-ratelimit-limit` tidak
dikirim untuk model berbayar).

> Siapa pun yang punya repo ini punya key tersebut. Kalau mulai dipakai serius,
> pindah ke env var.

## Toolchain

- Go: `go.mod` menyebut 1.25.1; toolchain di mesin ini via mise (1.27).
  `check.sh` men-`unset GOROOT` karena export GOROOT dari mise membuat
  compiler komplain versi tidak cocok.
- Web: node + rsbuild; typecheck `npx tsc --noEmit` dari `web/`.

## Menjalankan

```bash
go run . -addr :8080        # API + SPA di :8080 (web/dist hasil build, di-embed)
cd web && npm run dev       # dev server rsbuild + proxy /api → :8080
cd web && npm run build     # hasil ke web/dist (di-embed oleh go:embed)
go test ./...               # vet + test
```

`go:embed` mengunci `web/dist` saat kompilasi: ubah frontend → `npm run build`
dulu, lalu **compile ulang + restart** binary — mengganti dist saja tidak
mengubah apa yang disajikan `:8080`.

## Dilewati

- **Range request** — segmen di-proxy apa adanya, tanpa dukungan `Range`.
- **Pemilihan track audio (sub/dub)** — ditentukan di level playlist (varian
  sub vs dub), bukan track di dalam satu file.
- **Cloudflare bypass** — `net/http` bisa kena blok. `errCloudflare` → 502
  dengan pesan jelas. Solusinya `curl-impersonate`, bukan regex.
- **Picture-in-picture** — tidak ada di layout kontrol.
