# zanime

Torrent video streaming server: Gin HTTP, virtual streaming + seek, cache RAM-only, zero disk, plus a web UI untuk tempel magnet dan memilih video.

Player memakai [MoviPlayer](https://moviplayer.com/) dari CDN (`<movi-player>`), jadi MKV/HEVC/AV1 dan subtitle di dalam file diputar di browser lewat WebCodecs + FFmpeg WASM. Server hanya mengirim byte lewat HTTP Range; tidak ada transcoding di server.

## Kenapa `anacrolix/torrent`

- `File.NewReader()` mengembalikan `io.ReadSeekCloser`, langsung cocok untuk `http.ServeContent` (HEAD, Range/206, multi-range, 416).
- Storage backend bisa diganti, jadi piece bisa disimpan di RAM.
- `cenkalti/rain` v2.4.4 tidak mengekspos API baca piece (`File` hanya `Length`/`Path`), jadi tidak bisa virtual streaming/seek.

## Struktur

```
main.go                        gin server, API magnet/file, embed web/
web/index.html                 UI Tailwind + <movi-player>, di-embed lewat go:embed
internal/config/               loader .env (PORT, MAX_MEMORY_MB)
internal/stream/               handler HTTP Range/seek (dipakai main + test)
internal/stream/*_test.go      unit test HEAD/Range/seek/416
internal/memstorage/           storage torrent RAM-only, LRU
internal/memstorage/*_test.go  unit test cache + eviction
tests/eviction_test.go         eviction + refetch, seeder/leecher lokal (tanpa internet)
tests/e2e_test.go              e2e streaming + seek + API + cek disk ke server hidup
```

## Konfigurasi

`.env` di root:

```dotenv
PORT=8080
MAX_MEMORY_MB=200
```

- `PORT` — port HTTP di `127.0.0.1`.
- `MAX_MEMORY_MB` — batas cache piece di RAM (MiB).

Flag CLI menimpa nilai `.env`:

```sh
go run .                       # magnet contoh, 127.0.0.1:8080, cache dari .env
go run . -magnet ''            # mulai tanpa torrent, isi magnet dari UI
go run . -addr 0.0.0.0:8080 -cache-mb 128
```

## UI

Buka `http://127.0.0.1:8080`:

1. Tempel magnet, klik **Muat torrent**. Server menunggu metadata lalu menampilkan daftar file video.
2. Klik salah satu file. `src` player diarahkan ke `/stream/<index>/<nama>` dan playback langsung dicoba.
3. Panel status menampilkan cache RAM, eviction, peer, dan byte yang tersedia.

Player di-load dari `https://moviplayer.com/dist/element.js`; Tailwind dari `https://cdn.tailwindcss.com`. Keduanya CDN, tidak perlu build frontend.

## Endpoint

| Path | Fungsi |
|------|--------|
| `GET /` | UI (embedded `web/index.html`) |
| `GET /api/state` | status torrent, daftar file video, file terpilih, cache, peer |
| `POST /api/magnet` | `{"magnet":"..."}` — ganti torrent aktif, tunggu metadata |
| `POST /api/select` | `{"index":N}` — pilih file video |
| `GET /stream` | file terpilih; dukung `HEAD` dan `Range` |
| `GET /stream/:index/:name` | file tertentu; `name` dipakai untuk MIME/ekstensi |
| `GET /info` | ringkasan siap/nama/ukuran/cache (dipakai e2e) |
| `GET /healthz` | `ok` |

Contoh:

```sh
curl http://127.0.0.1:8080/api/state
curl -X POST http://127.0.0.1:8080/api/magnet \
  -H 'Content-Type: application/json' \
  -d '{"magnet":"magnet:?xt=urn:btih:..."}'
curl -I http://127.0.0.1:8080/stream
curl -H 'Range: bytes=0-1048575' -o chunk.bin http://127.0.0.1:8080/stream
```

## Cache & zero disk

- `internal/memstorage` menyimpan piece di `map` + LRU (`container/list`), total dibatasi `MAX_MEMORY_MB` / `-cache-mb` (default 200 MiB).
- Saat budget terlampaui, piece paling lama dibuang dan `Eviction{Hash, Index}` dikirim lewat channel `Evicted()`.
- `server.syncEvictions()` memanggil `t.Piece(index).UpdateCompletion()` untuk piece yang dibuang, supaya torrent client menandainya belum lengkap dan mengunduhnya lagi saat di-seek. Eviction dari torrent lama diabaikan lewat info hash.
- `cfg.DataDir = ""` dan `DefaultStorage` diganti `memstorage`; torrent client tidak pernah membuka file data.
- Satu piece yang lebih besar dari limit tetap disimpan agar progres tidak macet.

## Test

Unit test (hermetic, termasuk eviction virtual):

```sh
go vet ./...
go test -race ./... -count=1 -timeout 15s
```

`tests/eviction_test.go` menjalankan seeder + leecher di proses yang sama lewat localhost, torrent 8 MiB / 8 piece, cache 2 piece. Efeknya eviction, refetch setelah seek, dan isi byte diverifikasi tanpa internet (~0,2 detik). Kedua `DataDir` dicek tetap kosong.

E2E butuh server hidup dan network torrent:

```sh
go run .                                                  # terminal 1
ZANIME_E2E=1 go test -run TestE2E -timeout 90s ./tests    # terminal 2
```

Yang dicek e2e:

- `/info`: `storage=memory`, file video, `cache_used <= cache_limit`
- `HEAD /stream`: 200, `Content-Length`, `Accept-Ranges: bytes`
- Range 0–128 KiB: 206 + `Content-Range`
- seek 1 MiB dan 50% file: 206, offset benar
- range sama dua kali: byte identik
- `bytes=<size>-`: 416 + `Content-Range: bytes */<size>`
- disk: `/proc/<pid>/fd` server tidak membuka file data (fd selain stdin/stdout/stderr)
- UI: `/` memuat `movi-player` + Tailwind + endpoint `/api/magnet`
- API: magnet invalid ditolak 400, `/api/state` berisi file + URL stream yang HEAD-nya 200, `/api/select` valid 200 dan index invalid 400

Env test:

- `ZANIME_URL` — default `http://127.0.0.1:8080`
- `ZANIME_TIMEOUT` — timeout per request, default 15 detik
- `ZANIME_E2E=1` — wajib untuk menjalankan e2e

Jika satu piece torrent (4 MiB di magnet contoh) tidak selesai dalam timeout, test streaming di-skip, bukan gagal — swarm publik bisa lambat. Naikkan `ZANIME_TIMEOUT` bila perlu.

## Batasan

- Satu torrent aktif; menambah magnet baru akan mengganti torrent lama.
- Hanya file dengan ekstensi video yang ditampilkan (`.mkv`, `.mp4`, `.webm`, `.avi`, `.mov`, `.ts`, `.m2ts`, …).
- Tidak ada transcode/remux di server; server hanya menyajikan byte dari torrent.
- Format yang bisa diputar tergantung browser: MoviPlayer memakai WebCodecs (Chrome/Edge terbaru paling aman). Safari tanpa WebCodecs akan fallback ke native `<video>`.
