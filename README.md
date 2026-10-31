# zanime

Streaming server Gin + SPA React: cari judul, pilih server, tonton. Semua alur
streaming ditangani backend — **browser tidak pernah menghubungi host video
maupun sumber katalog secara langsung** — dan tanpa proses eksternal sama
sekali: tidak ada `bun`, `node`, `ffmpeg`, maupun torrent.

Alurnya: **cari (Cinemeta) → pilih server (embedmaster) → putar lewat slug
`/s/<slug>`**. Scraping dan dekripsi kredensial embedmaster diimplementasikan
di Go (`internal/em`), hasil resolve disimpan sebagai slug pendek supaya URL
media yang panjang dan ter-encode tidak muncul di jaringan browser.

## Struktur

```
main.go                        gin server: API + proxy /s/<slug> + embed web/dist
web/                           frontend React+TS (Vite): features/ + pages/
internal/tmdb/                 klien Cinemeta (cari + meta/episode), cache ./tmp/tmdb
internal/em/                   scraping embedmaster: gerbang → kredensial → feed → resolve
internal/em/crypto.go          kripto: obfuscation kredensial + dekripsi resolve.php
internal/em/scrape.go          HTTP: cookie jar, gerbang welcome, feed NDJSON, resolve
internal/em/verify.go          verifikasi URL stream: probe 128 KiB, sniff HLS/MP4 vs halaman embed
internal/em/log.go             Logger pipeline (baris log per tahap scrape)
internal/logbus/               hub log realtime: fan-out websocket + riwayat
internal/resolve/              pool slug: /s/<slug> → URL media, peta di ./tmp/resolve
internal/config/               loader .env (PORT)
```

## Build

Butuh Go 1.27+ dan Node/pnpm untuk frontend.

```sh
cp .env.example .env   # sesuaikan PORT bila perlu
make build             # frontend → web/dist, binary ./zanime (strip + upx bila ada)
make run               # go run .
make vet               # go vet ./...
make test              # unit test hermetic (-short -race, timeout 15s)
make test-live         # live scrape embedmaster (~10 dtk, butuh jaringan)
make clean             # hapus ./zanime dan zanime.exe
```

Tanpa `make`:

```sh
pnpm --dir web build
go build -ldflags "-s -w" -o zanime .
```

`web/dist` di-embed ke binary (`go:embed`), jadi satu file binary sudah cukup
untuk dijalankan. `make build` membuang tabel simbol (`-s -w`) lalu
mengompres dengan [upx](https://upx.github.io/) (`--best --lzma`) bila upx ada
di `PATH`; tanpa upx, build tetap selesai.

## Konfigurasi

`.env` di root (contoh di `.env.example`):

| Variabel | Arti |
|----------|------|
| `PORT` | port HTTP web, default `8080` |

Flag CLI menimpa `.env`: `go run . -addr 0.0.0.0:8080`.

## Alur

Urutan tetap setiap kali sebuah judul dipilih:

| # | Tahap | Pelaku | Yang terjadi | Endpoint / artefak |
|---|-------|--------|--------------|--------------------|
| 1 | Cari judul | browser → server | tab Movie/TV menentukan tipe katalog; hasil hanya id `tt…` yang dipakai | `GET /api/search?q=&tab=` |
| 2 | Pilih judul | browser → server | metadata + daftar episode (tv) dari Cinemeta; `moviedb_id` diambil dari sini | `GET /api/meta/:tab/:id` |
| 3 | Scrape server | server → embedmaster | halaman embed → gerbang welcome (`POST tr=`) → skrip kredensial obfuscated → feed NDJSON `/api/sources` | `internal/em` |
| 4 | Resolve + verifikasi | server → embedmaster, lalu host video | tiap server ditanyakan ke `resolve.php` membawa `X-EM-Ticket`; blob base64url didekripsi (nonce16 + SHA256-XOR), lalu URL-nya diuji (128 KiB) untuk membuang halaman embed | `internal/em/crypto.go`, `internal/em/verify.go` |
| 5 | Slug | server | setiap URL (server, tiap kualitas, tiap subtitle) didaftarkan ke `/s/<slug>` (+ ekstensi media); peta JSON di `./tmp/resolve` | `internal/resolve` |
| 6 | Putar | browser → server | player meminta `/s/<slug>`; server meneruskan `Range`/`HEAD` dan memasang `Referer` host video | `GET/HEAD /s/:slug` |
| 7 | HLS | browser → server | playlist `.m3u8` ditulis ulang di server: setiap URI (playlist turun, segmen, `URI="…"` di tag) jadi `/s/<slug>` juga | `internal/em` + `main.go` |
| 8 | Subtitle | browser → server | track VTT lewat slug; proxy menormalkan timestamp ke `HH:MM:SS.mmm`, lalu didaftarkan ke MoviPlayer lewat `source({subtitles})` | `main.go` + `SearchDialog` → `Player` |
| 9 | Log | server → browser | tiap tahap (scrape, uji 128 KiB, playlist/subtitle/segmen, event player) dikirim sebagai baris `{id,at,level,text}` | `ws /api/logs` (`internal/logbus` + `web/src/lib/logs.ts`) |

### Kenapa lewat server

- Host video embedmaster menolak permintaan tanpa `Referer:
  https://embdmstrplayer.com/` (tanpa itu: 404/502) dan CORS-nya dipatok ke
  origin mereka, jadi media tidak bisa dibaca langsung oleh browser.
- URL media sangat panjang dan berisi token; browser cukup melihat
  `/s/<slug>` pendek. Slug stream/kualitas/subtitle disimpan di
  `./tmp/resolve/<slug>.json`; slug segmen ribuan cukup di memori.
- Playlist harus ditulis ulang di server supaya playlist turunan dan segmen
  ikut lewat proxy yang sama (kalau tidak, hls.js menembak host video langsung
  dan ditolak).
- Slug menyertakan ekstensi media (`.m3u8`, `.mp4`, `.vtt`): movi-player
  memilih jalur adaptif (Shaka/MSE) — menu kualitas + track DASH/HLS — hanya
  bila `src`-nya mengandung `.m3u8`/`.mpd`. Tanpa itu HLS diperlakukan sebagai
  file tunggal, jadi menu resolusi hilang. Kalau URL-nya tanpa ekstensi,
  ekstensi diambil dari hasil verifikasi (lihat "Verifikasi stream").

### Performa proxy

HLS satu judul bisa berisi ribuan segmen, jadi jalur panasnya dijaga:

- **Segmen di memori.** Playlist turunan + segmen didaftarkan lewat
  `RegisterTemp` (tanpa tulis file); hanya stream/kualitas/subtitle yang
  persisten. Rewrite playlist 2000+ segmen turun dari ~0,5 dtk jadi ~10 ms.
- **Upstream HTTP/2.** `DialContext` kustom mematikan negosiasi HTTP/2, jadi
  `ForceAttemptHTTP2` dinyalakan dan batas koneksi idle per host dinaikkan —
  banyak segmen paralel di-multiplex, tanpa TLS handshake baru tiap segmen.
- **Cache playlist VOD.** Playlist dengan `#EXT-X-ENDLIST` dibalas
  `Cache-Control: public, max-age=600` (bisa 1+ MB, tidak perlu di-fetch ulang);
  playlist live tetap `no-store`.

### Scraping embedmaster di Go

Tidak ada `bun -e`/`node -e`; dua bagian yang tadinya di-`eval` di JS sudah
diporting (lihat komentar di `internal/em/crypto.go`):

| Bagian | Bentuk |
|--------|--------|
| Skrip kredensial | `var A="<base64>",B=[…16 int…],C=<n>` → `byte[i] = b64[i] ^ ((C + B[i%16] + i) & 0xFF)` → UTF-8 → JSON `window.__EM_PLAYER__` (ticket, pk, scrapeToken, feedUrl, resolveUrl) |
| Daftar episode (tv) | `window.__EM_EPISODES__ = {JSON}` apa adanya |
| `resolve.php` | `data = base64url(blob)`; `nonce = data[:16]`; `ct = data[16:]`; blok ke-c = `SHA256(key ‖ nonce ‖ be32(c))`; `plaintext = ct XOR blok` (key = `hex(pk)`) |

Golden test `TestDecryptKnownVector` (di `internal/em`) memakai vektor asli
(blob + pk + plaintext dari situs) sebagai jaring pengaman kalau kripto di sana
berubah.

### Performa scrape

Satu `/api/streams` ≈ 3 dtk ambil halaman player + 1,5 dtk feed; sisanya
resolve server. Resolve dijalankan lewat **worker pool** (`parallel`, 6 worker):
15 server berurutan ~6 dtk, paralel ~1–2 dtk, dan urutan hasil tetap sama
seperti feed (indeks dijaga). Hasilnya di-cache 15 menit, jadi pencarian judul
yang sama berikutnya instan.

### Verifikasi stream

Tidak semua server yang muncul di feed bisa diputar: sebagian resolve-nya cuma
halaman embed HTML (mis. `/e/<id>`, `/embed/<id>`) yang tidak pernah mengalirkan
media. Setiap URL yang berhasil di-resolve diuji di `internal/em/verify.go`:

- URL berpola halaman embed (`embedURLRe`) ditolak tanpa HTTP.
- Selain itu, ambil **128 KiB** pertama dengan `Referer` halaman player (sama
  seperti proxy), lalu sniff isinya: `#EXTM3U` → HLS, byte MP4 (`ftyp`) →
  mp4, `text/html`/JSON → ditolak. Body diperiksa lebih dulu karena sebagian
  host salah mengirim `Content-Type` untuk playlist m3u8 yang sah.
- Jenis hasil verifikasi menimpa tebakan resolve (mis. server "mp4" yang
  ternyata playlist → jadi `hls`, slug-nya pun dapat `.m3u8`).
- Server yang gagal diuji ditandai `err` dan tidak ditampilkan UI.

## UI

Buka `http://127.0.0.1:8080`:

| Aksi di UI | Request | Efek |
|------------|---------|------|
| Buka **Cari** → ketik judul | `GET /api/search?q=&tab=movie\|tv` | hasil Cinemeta tampil (tab Movie/TV di backend) |
| Klik hasil | `GET /api/meta/:tab/:id` | movie langsung ke daftar server; tv menampilkan chip season + episode |
| Pilih season/episode | `GET /api/meta/tv/:id` | daftar episode dari metadata |
| Pilih episode | `GET /api/streams?tab=tv&id=&s=&e=` | daftar `(server, kualitas)` dari embedmaster |
| Klik satu baris stream | `play(src)` | player memuat `/s/<slug>`; subtitle stream itu didaftarkan otomatis |
| Subtitle | — | track didaftarkan lewat API MoviPlayer (`source({video, subtitles})`), track pertama langsung aktif |
| Panel **Log** | `ws /api/logs` | log realtime (scrape, uji 128 KiB, pemuatan video) tampil sebagai overlay di depan player |

Player: [MoviPlayer](https://moviplayer.com) (`movi-player`) merender lewat
canvas + WASM demuxer-nya sendiri. Karena itu subtitle **tidak** dipasang
sebagai `<track>` anak (diabaikan saat `src` terpasang), melainkan didaftarkan
lewat `el.source({video:{src}, subtitles:[{src,lang,label,format:"vtt"}]})`
(daftar subtitle disimpan elemen sebelum `src` dipasang, jadi track langsung
ikut termuat tanpa balapan dengan `el.player`).

Parser VTT di MoviPlayer hanya mengenali timestamp `HH:MM:SS.mmm`, sedangkan
CDN subtitle (scrapemaster) mengirim `MM:SS.mmm` — track ter-fetch 200 OK tapi
0 cue, sehingga tidak ada subtitle tampil. Proxy karena itu menormalkan
timestamp tiap baris cue (`normalizeVTT`) sebelum body diteruskan.

## Log realtime

Panel log di depan player menampilkan jalannya scrape dan pemuatan video.
Semuanya satu arah (server → browser) lewat websocket:

- `internal/logbus` = `Hub` fan-out: tiap baris (`{id,at,level,text}`)
  disiarkan non-blocking ke semua klien; klien yang lambat dilewati, bukan
  diblok. Riwayat 200 baris terakhir ikut dikirim ke klien baru (reload tetap
  punya konteks).
- `internal/em` menulis baris per tahap (halaman → gerbang → kredensial → feed
  → resolve → uji) lewat `Logger`; `streamProxy` menulis tiap permintaan
  playlist/subtitle/segmen. `id` sama = baris yang sama, jadi UI menampilkan
  `[sedang…]` lalu baris itu berubah jadi `[OK]`/`[GAGAL]`.
- Sisi browser (`web/src/lib/logs.ts`) menerima lewat `ws /api/logs` dan
  menambah baris lokal dari player (event media: `loadstart`, `canplay`,
  `qualitychange`, `waiting`, `error`, …). Ring buffer 400 baris.

## Endpoint

| Path | Fungsi |
|------|--------|
| `GET /` | UI SPA (embedded `web/dist`); path lain yang bukan API/`/s/` juga dapat SPA |
| `GET /assets/*filepath`, `/favicon.ico` | aset hasil build Vite |
| `GET /api/search?q=&tab=movie\|tv` | cari judul; `{results:[{id,tmdb,name,year,type,poster}]}` |
| `GET /api/meta/:tab/:id` | detail judul (`id` harus `tt…`); tv menyertakan `episodes[]` |
| `GET /api/streams?tab=&id=&s=&e=` | pecahkan stream: `{id,tab,servers:[{name,type,quality,qualities,subs,url,err}]}`; `url`/`qualities[].url`/`subs[].url` sudah `/s/<slug>`; tv wajib `s`/`e` |
| `GET/HEAD /s/:slug` | proxy media: teruskan `Range`/`Content-Range`/`Accept-Ranges`, `Referer` host video, dan tulis ulang playlist m3u8; 404 bila slug tidak ada, 502 bila upstream gagal |
| `GET /api/logs` (websocket) | siarkan log realtime `{id,at,level,text}`; riwayat terakhir dikirim saat menyambung |
| `GET /healthz` | `ok` |

Contoh:

```sh
curl 'http://127.0.0.1:8080/api/search?q=frieren&tab=tv'
curl 'http://127.0.0.1:8080/api/meta/movie/tt0145487'
curl 'http://127.0.0.1:8080/api/streams?tab=movie&id=557'
curl -I 'http://127.0.0.1:8080/s/seg-1a2b3c4d5e6f'
curl -H 'Range: bytes=0-1048575' -o chunk.bin 'http://127.0.0.1:8080/s/<slug>'
```

Error dari upstream dikirim sebagai JSON `{"error":"…"}`: 502 (gagal
menghubungi upstream) atau 504 (scrape melewati batas waktu).

## Cache & tmp

Semua cache ada di `./tmp` dan boleh dihapus kapan saja:

| Direktori | Isi | TTL | Lokasi |
|-----------|-----|-----|--------|
| `tmp/tmdb/` | hasil pencarian & metadata Cinemeta | 24 jam | `internal/tmdb` |
| `tmp/em/` | daftar server hasil scrape (URL-nya berumur pendek) | 15 menit | `internal/em` |
| `tmp/resolve/` | peta `slug → URL` untuk stream/kualitas/subtitle (satu file `<slug>.json`) | 6 jam, dipangkas berkala | `internal/resolve` |

- Slug dibentuk dari nama server/kualitas/lang yang mudah dibaca
  (`premium-wmovies.m3u8`); ekstensi media disertakan (dari URL atau hasil
  verifikasi), bila bentrok ditambah hash pendek. Slug yang tidak ada → 404.
- Segmen dan playlist turunan (`seg-<hash>`) hanya di memori — jumlahnya ribuan
  per judul dan tidak perlu lintas restart; entri memori dipangkas setelah TTL
  yang sama.
- `tmp/resolve` dipangkas saat start dan berkala (tiap jam).
- Cache stream 15 menit karena URL resolve embedmaster kedaluwarsa; scrape
  ulang terjadi otomatis setelah itu.

## Test

```sh
make vet                # go vet ./...
make test               # unit test hermetic: go test -short -race ./... -timeout 15s
make test-live          # live scrape ke embedmaster.link (~10 dtk, butuh jaringan)
cd web && pnpm lint && pnpm test
```

Cakupan sekarang:

| Paket | Yang diuji |
|-------|-----------|
| `internal/tmdb` | pencarian (filter `tt…`, `releaseInfo`→year), meta movie/tv (`moviedb_id` angka/string via `FlexString`), TTL cache hit/expired, upstream 5xx |
| `internal/em` | pipeline penuh di atas `httptest` (gerbang cookie → POST `tr=` → kredensial → feed → resolve), pemilihan episode tv, episode hilang, semua server gagal, cache hit/expired, ctx batal, HTTP 5xx, pola kredensial rusak, dekripsi (vektor asli + roundtrip + error), verifikasi stream (sniff jenis, tolak halaman embed), baris log per tahap |
| `internal/logbus` | fan-out ke banyak pelanggan, replay riwayat, cancel idempoten, pelanggan lambat tidak memblok, batas riwayat |
| `internal/resolve` | register/load/idempoten, kolisi, hint kotor, guard `Load`, persist antar-pool, file rusak, prune + jadwal tidy, ekstensi media pada slug |
| `main` | kontrak `/api/search` & `/api/meta` (ok/400), `/api/streams` → slug, `streamProxy` (rewrite playlist master+child, `URI=` tag, `Referer`, HEAD tanpa body, 404, 502, baris log), websocket `/api/logs` (riwayat + live), SPA fallback |
| `web` (vitest) | dialog pencarian (hasil, movie→stream per kualitas, tv→episode→streams, error), provider play/stop/status, `setPlayerSource`/`selectFirstSub`, store log (`push`/id sama/ring buffer), panel `LogOverlay` |

Live test (jaringan sungguhan, ~10 s):

```sh
make test-live           # go test -race ./internal/em -run TestLiveEmbedmaster -timeout 120s
```

`TestLiveEmbedmaster` menembak embedmaster.link dari pipeline Go memakai
"Spider-Man: Brand New Day" (tmdb `969681`), lalu memastikan setiap server yang
punya URL lolos verifikasi (server embed sudah disaring).

## Batasan

- Sumber stream hanya embedmaster; kalau skrip kredensial atau kripto di sana
  berubah, `internal/em` perlu disesuaikan (golden test `TestDecryptKnownVector`
  akan gagal lebih dulu).
- Satu judul per permintaan: tiap `/api/streams` membuat sesi sendiri (cookie
  gerbang tidak dipakai bersama antar judul) dan scrape dibatasi 120 detik.
  Resolve server jalan paralel (6 worker), tapi `playerPage` + `feed` tetap
  satu permintaan berurutan (~4–5 dtk), jadi daftar server muncul ~5 dtk.
- Embedmaster men-throttle bila beberapa scrape berturut-turut dari IP yang
  sama; saat itu semua server gagal resolve dan API membalas error. Cache 15
  menit mengurangi frekuensinya.
- Slug stream/kualitas/subtitle dipetakan per proses + file di `./tmp/resolve`;
  entri lama yang belum dipangkas tetap bisa diputar sampai TTL-nya lewat.
  Slug segmen hanya in-memory, jadi browser harus membaca ulang playlist induk
  setelah server restart (stream yang sedang jalan akan putus).
- Subtitle yang tersedia adalah apa yang dikirim server (biasanya VTT), bukan
  hasil ekstraksi dari file video. Master playlist embedmaster tidak memuat
  rendition `TYPE=SUBTITLES`, jadi track subtitle didaftarkan terpisah lewat
  API `source({subtitles})` (bukan dari manifest), dan waktunya dinormalkan
  server-side ke `HH:MM:SS.mmm` (`normalizeVTT`) karena parser movi-player
  mensyaratkan itu.
- Token pada URL media tetap bisa dilihat server video lain; yang dihilangkan
  dari browser hanya URL panjang dan ter-encode.
