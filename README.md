# zanime

Streaming server Go + SPA React: cari judul, pilih server, tonton. Browser tidak
pernah menghubungi host video maupun sumber katalog — semua lewat server.
Tanpa proses eksternal (`bun`, `node`, `ffmpeg`) saat runtime.

Alur: **cari (Cinemeta) → pilih server (embedmaster + miruro) → putar lewat
`/s/<slug>`**. Scraping, kredensial, dan dekripsi embedmaster di Go
(`internal/em`); anime juga diambil dari miruro (`internal/miruro`, dicocokkan
lewat imdb/TMDB). URL media panjang disembunyikan di balik slug pendek.

Stack: [fiber v2](https://github.com/gofiber/fiber) (HTTP engine + router),
[resty v2](https://github.com/go-resty/resty) (HTTP client),
[godotenv](https://github.com/joho/godotenv) (`.env`), React 19 + Vite +
Vidstack (UI), hls.js (HLS).

## Struktur

```
main.go            embed web/dist + panggil internal/app
internal/app       server fiber: route, handler web/api/proxy, rate limit
internal/tmdb      Cinemeta (cari + meta), cache tmp/tmdb
internal/em        scrape embedmaster: gerbang → kredensial → feed → resolve
internal/miruro    stream anime miruro: cari judul → cocokkan id → sources
internal/subdl     subtitle subdl, cache tmp/subdl
internal/translate terjemahan subtitle (Indonesia) lewat Google batchexecute, cache tmp/translate
internal/resolve   slug /s/<slug> → URL media, peta tmp/resolve
internal/logbus    hub log realtime (SSE)
internal/config    loader .env (godotenv)
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
| `WORKERS` | `20` | batas pekerja paralel (resolve server, terjemahan cue) |
| `PROXY_URL` | – | proxy HTTP/SOCKS5 untuk subdl + embedmaster; kosong = langsung. Proxy webshare bersufiks `-n` otomatis diputar `-1`…`-10` (paralel) saat diblokir, dan yang lolos ditulis balik ke `.env` |

Nilai bawaan yang diubah dari default library/Go, semuanya di kode:

| Bagian | Nilai | Lokasi |
|--------|-------|--------|
| Resolve server & terjemahan | worker pool global, batas `WORKERS` | `internal/pool` |
| Batas waktu scrape | total 30 dtk; HTTP keluar 20 dtk; subtitle 20 dtk; zip subtitle 30 dtk | `internal/em`, `internal/tmdb`, `internal/subdl`, `main.go` |
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
| `GET /api/streams?tab=&id=&s=&e=&imdb=` | daftar server (`url` sudah berupa `/s/<slug>`; `imdb` opsional untuk sumber miruro) |
| `GET/HEAD /s/{slug}` | proxy media; playlist m3u8 ditulis ulang ke slug |
| `GET /api/subtitles?tab=&id=&s=&e=&refresh=` | subtitle subdl (Indonesian + English) |
| `GET /api/subtitles/file?u=<slug>` | zip → VTT siap dipasang |
| `GET /api/logs` | stream SSE: log `{id,at,level,text}` + `{workers:n}` tiap 0,7 dtk |
| `GET /healthz` | `ok` |

## Catatan desain

- **Kenapa lewat server**: host video menolak request tanpa `Referer:
  https://embdmstrplayer.com/` dan CORS-nya dipatok; slug menyembunyikan URL
  panjang bertoken, dan semua URI playlist/segmen ikut lewat proxy yang sama.
- **Jenis stream** ditentukan dari `resolve.php` + ekstensi URL; slug membawa
  ekstensi media supaya player memilih jalur HLS yang benar.
- **Kripto** embedmaster diporting ke Go (obfuscation kredensial, dekripsi
  SHA256-XOR). Golden test `TestDecryptKnownVector` memakai vektor asli.
- **Log** dikirim per tahap (scrape, resolve, playlist, pemutaran) lewat SSE;
  teks tidak memuat URL/host.

## Test

```sh
golangci-lint run ./...   # harus 0 issue
make test                 # go test -short -race ./... -timeout 15s
make test-live            # scrape embedmaster sungguhan (~10 dtk, butuh jaringan)
cd web && pnpm lint && pnpm test
```

Cakupan: pipeline em di atas `httptest` (gerbang → feed → resolve, cache,
retry, ctx batal), kripto (vektor asli + roundtrip), tmdb/subdl (parsing, cache,
error), resolve (slug, TTL, prune), logbus, kontrak API + proxy + SSE di
`main`, dan komponen/store frontend.

## Batasan

- Sumber stream hanya embedmaster; kalau skrip kredensial/kripto di sana
  berubah, `internal/em` perlu disesuaikan.
- Embedmaster men-throttle scrape beruntun dari IP yang sama; cache 15 menit
  mengurangi frekuensinya.
- Slug segmen hanya di memori, jadi stream yang sedang jalan putus setelah
  server restart (browser membaca ulang playlist induk).
