# zanime

Streaming server Go + SPA React: cari judul, pilih server, tonton. Browser tidak
pernah menghubungi host video maupun sumber katalog — semua lewat server.
Tanpa proses eksternal (`bun`, `node`, `ffmpeg`) saat runtime.

Alur: **cari (Cinemeta) → pilih server (embedmaster) → putar lewat `/s/<slug>`**.
Scraping, kredensial, dan dekripsi embedmaster di Go (`internal/em`); URL media
panjang disembunyikan di balik slug pendek.

Stack: [chi v5](https://github.com/go-chi/chi) (HTTP server),
[resty v2](https://github.com/go-resty/resty) (HTTP client), React 19 + Vite +
Vidstack (UI), hls.js (HLS).

## Struktur

```
main.go            chi server: API, proxy /s/<slug>, embed web/dist
internal/tmdb      Cinemeta (cari + meta), cache tmp/tmdb
internal/em        scrape embedmaster: gerbang → kredensial → feed → resolve
internal/subdl     subtitle subdl, cache tmp/subdl
internal/resolve   slug /s/<slug> → URL media, peta tmp/resolve
internal/logbus    hub log realtime (websocket)
internal/config    loader .env
web/               React + TS (Vite), features/ + pages/
```

## Jalankan

```sh
cp .env.example .env
make build    # frontend → web/dist, binary ./zanime
make run      # go run .
```

Dev frontend: `pnpm --dir web dev` (proxy ke `localhost:8080`).
Frontend tanpa `make`: `pnpm --dir web build` lalu `go build -o zanime .`.
`web/dist` di-embed (`go:embed`), jadi satu binary cukup.

## Konfigurasi

Backend, dibaca dari `.env` (contoh: `.env.example`); env proses menang:

| Variabel | Default | Arti |
|----------|---------|------|
| `PORT` | `8080` | port HTTP |
| `GOMAXPROCS` | `30` | batas paralelisme runtime, dipakai sebagai `min(NumCPU, 30)` |

Nilai bawaan yang diubah dari default library/Go, semuanya di kode:

| Bagian | Nilai | Lokasi |
|--------|-------|--------|
| Paralelisme runtime | `GOMAXPROCS=30`, bukan semua core (batas nproc cPanel/LVE, mis. 100) | `main.go` |
| Resolve server | 6 worker serentak per scrape | `internal/em/em.go` |
| Batas waktu scrape | 120 dtk; HTTP keluar 20 dtk; subtitle 20 dtk; zip subtitle 30 dtk | `internal/em`, `internal/tmdb`, `internal/subdl`, `main.go` |
| TTL cache | tmdb 24 jam, em 15 mnt, subdl 10 mnt, resolve 6 jam | `internal/*` |
| Riwayat log | server 200 baris, klien 400 baris | `internal/logbus`, `web/src/lib/logs.ts` |
| Transport proxy media | HTTP/2 dipaksa, 256 idle conn / 64 per host, kompresi mati | `main.go` |

Frontend (`web/`):

| Bagian | Nilai | Lokasi |
|--------|-------|--------|
| Dev proxy | `/api`, `/hls`, `/fetch` → `http://localhost:8080` | `vite.config.ts` |
| hls.js | dibundel lokal, bukan default Vidstack (jsDelivr) | `package.json`, `Player.tsx` |
| Subtitle otomatis | Indonesia dulu, lalu Inggris | `web/src/features/player/tracks.ts` |
| Alias import | `@` → `src`; vitest: jsdom, `src/**/*.test.ts(x)` | `vite.config.ts`, `vitest.config.ts` |

## Endpoint

| Path | Fungsi |
|------|--------|
| `GET /api/search?q=&tab=movie\|tv` | cari judul (Cinemeta) |
| `GET /api/meta/{tab}/{id}` | detail judul; tv menyertakan episode |
| `GET /api/streams?tab=&id=&s=&e=` | daftar server (`url` sudah berupa `/s/<slug>`) |
| `GET/HEAD /s/{slug}` | proxy media; playlist m3u8 ditulis ulang ke slug |
| `GET /api/subtitles?tab=&id=&s=&e=&refresh=` | subtitle subdl (Indonesian + English) |
| `GET /api/subtitles/file?u=<slug>` | zip → VTT siap dipasang |
| `WS /api/logs` | log realtime `{id,at,level,text}` |
| `GET /healthz` | `ok` |

## Catatan desain

- **Kenapa lewat server**: host video menolak request tanpa `Referer:
  https://embdmstrplayer.com/` dan CORS-nya dipatok; slug menyembunyikan URL
  panjang bertoken, dan semua URI playlist/segmen ikut lewat proxy yang sama.
- **Jenis stream** ditentukan dari `resolve.php` + ekstensi URL; slug membawa
  ekstensi media supaya player memilih jalur HLS yang benar.
- **Kripto** embedmaster diporting ke Go (obfuscation kredensial, dekripsi
  SHA256-XOR). Golden test `TestDecryptKnownVector` memakai vektor asli.
- **Log** dikirim per tahap (scrape, resolve, playlist, pemutaran) lewat
  websocket; teks tidak memuat URL/host.

## Test

```sh
golangci-lint run ./...   # harus 0 issue
make test                 # go test -short -race ./... -timeout 15s
make test-live            # scrape embedmaster sungguhan (~10 dtk, butuh jaringan)
cd web && pnpm lint && pnpm test
```

Cakupan: pipeline em di atas `httptest` (gerbang → feed → resolve, cache,
retry, ctx batal), kripto (vektor asli + roundtrip), tmdb/subdl (parsing, cache,
error), resolve (slug, TTL, prune), logbus, kontrak API + proxy + websocket di
`main`, dan komponen/store frontend.

## Batasan

- Sumber stream hanya embedmaster; kalau skrip kredensial/kripto di sana
  berubah, `internal/em` perlu disesuaikan.
- Embedmaster men-throttle scrape beruntun dari IP yang sama; cache 15 menit
  mengurangi frekuensinya.
- Slug segmen hanya di memori, jadi stream yang sedang jalan putus setelah
  server restart (browser membaca ulang playlist induk).
- `GOMAXPROCS` membatasi paralelisme, bukan jumlah thread OS secara keras;
  untuk batas nproc cPanel, nilai 30 dari 100 memberi jarak aman.
