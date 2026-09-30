# zanime

Streaming anime dari hianime.at dengan subtitle terjemahan Indonesia — HTTP
API + SPA di atas satu binary Go, di-port dari
[ani-cli](https://github.com/pystardust/ani-cli) (`ref/ani-cli`, bash).

- **Backend** Go 1.25 + chi: scraping, proxy HLS, konversi subtitle, SPA embed.
- **Frontend** React 19 + React Router 8 (React Compiler aktif), dibundel
  rsbuild + Tailwind 4, player
  shaka.ui.Overlay (menu kualitas & subtitle on/off).
- **Subtitle** diterjemahkan ke Indonesia via kenari.id `deepseek-v4-1-flash`
  (job latar + progress, hasil di-cache ke disk).
- **Tanpa ffmpeg sama sekali** — video diputar lewat proxy HLS; remux dibuang.

## Menjalankan

```bash
cd web && npm install && npm run build   # hasil ke web/dist (di-embed go:embed)
go run . -addr :8080                     # API + SPA di :8080
```

Dev frontend (hot reload, proxy `/api` → :8080):

```bash
cd web && npm run dev
```

Verifikasi kode:

```bash
./check.sh        # go mod tidy + vet + test (swap module sementara)
cd web && npx tsc --noEmit && npx rslint
```

> Go 1.25.1 di `go.mod`; toolchain di mesin ini lewat mise (1.27). `check.sh`
> men-`unset GOROOT` karena export GOROOT dari mise membuat compiler komplain.

## Endpoint — semua API di bawah `/api/`

| Method | Path | Fungsi |
|---|---|---|
| GET | `/` | SPA (index.html + aset hashed, embed) |
| GET | `/api/healthz` | status + sisa kuota |
| GET | `/api/limits` | pemakaian kuota kenari |
| GET | `/api/search?q=` | cari judul (poster + sinopsis per kartu) |
| GET | `/api/img?u=` | proxy gambar CDN (base64url, allow-list host) |
| GET | `/api/anime/{id}` | detail anime: meta, genre, studio, related |
| GET | `/api/anime/{id}/episodes` | daftar episode |
| GET | `/api/episode/{id}?mode=` | master URL, referer, kualitas, subtitle |
| GET | `/api/subtitle/{id}?lang=id&mode=` | `.vtt` kalau siap, `202` + progress selama konversi |
| GET | `/api/subtitle/{id}/status` | `{state,done,total,eta_seconds}` |
| GET | `/api/hls/{id}/master.m3u8?mode=` | master upstream, varian ditulis ulang ke path proxy |
| GET | `/api/hls/{id}/{mode}/{quality}/index.m3u8` | playlist media |
| GET | `/api/hls/{id}/{mode}/{quality}/{file}` | segmen, di-stream apa adanya |

SPA dan API dipisah tegas: chi hanya melayani `/api/*`, aset build `web/dist`
di-embed dan disajikan di root.

## Halaman web

| Path | Isi |
|---|---|
| `/` | form cari |
| `/s/{q}` | hasil pencarian |
| `/a/{animeId}` | detail + daftar episode |
| `/w/{animeId}/{epId}?mode=dub` | player + detail anime + daftar episode |

URL adalah sumber kebenaran: back/refresh/share link mendarat di halaman yang
sama. `mode=dub` di query; sisanya di path.

## Player

`shaka.ui.Overlay` (setup programatik per docs shaka) di dalam
`web/src/components/Player.tsx`:

- `quality` — pilih resolusi dari varian HLS (360p/720p/1080p).
- `captions` — on/off + pilih subtitle Indonesia.
- `fadeDelay: 3` — kontrol hilang 3 detik setelah interaksi; tombol fullscreen
  mobile ikut lewat event `showingui`/`hidingui`.
- Subtitle dipasang `addTextTrackAsync` hanya saat status `ready` — file yang
  belum selesai ditolak player (CONTENT_NOT_LOADED), jadi `Watch` polling
  `/api/subtitle/{id}/status` dan remount lewat prop.

### Jarak & latar subtitle

`TextDisplayerConfiguration` tidak punya opsi posisi/latar, jadi diatur CSS
(`web/src/global.css`):

- `.shaka-text-container { transform: translateY(-2rem) }` — gap dari dasar;
  `transform` tidak bentrok dengan inline `bottom` yang ditulis shaka.
- Latar cue `rgba(0, 0, 0, 0.8)` yang ditulis inline UITextDisplayer
  diturunkan ke `0.5` lewat selector string + `!important`.

### Fullscreen iOS

iPhone Safari `document.fullscreenEnabled` false → shaka jatuh ke
`webkitEnterFullscreen` (player native iOS; subtitle digambar sistem, kebal
CSS). Jalur itu dicegah: tombol/double-click/rotasi fullscreen dimatikan,
container dibuat fullscreen lewat CSS (**pseudo-fullscreen**) supaya
UITextDisplayer + gap tetap berlaku. Desktop/iPad tetap fullscreen asli.

## Subtitle Indonesia

1. Ambil `.vtt` asli (hampir selalu Inggris) pakai referer yang sama.
2. Parse blok cue WebVTT; deduplikasi cue berteks identik.
3. Pack cue ke satu panggilan (`<index>|<teks>` → `<index>|<terjemahan>`)
   ke `deepseek-v4-1-flash` — satu episode nyata = satu panggilan.
4. Pasang balik ke blok aslinya; timing dan header tidak disentuh.
5. Konversi jalan sebagai **job latar** (`maxConcurrentJobs = 4`), progress
   via `/api/subtitle/{id}/status`, hasil di-cache `.cache/subtitles`.

Kuota & laju: pace 30 panggilan/menit, 429 dibedakan (rate limit dipecah +
`Retry-After`, penolakan upstream gagal segera), `eta_seconds` dari laju
nyata. Key kenari di-hardcode di `main.go` — kalau dipakai serius, pindah ke
env var.

## Proxy HLS

Browser tidak bisa mengirim `Referer` yang diminta host stream, jadi master,
playlist, dan segmen di-proxy:

- Kualitas di **path** (`/api/hls/{id}/{mode}/{quality}/…`) — browser
  me-resolve URI segmen relatif terhadap URL playlist dan membuang query.
- Segmen di-stream `io.Copy`, `max-age=3600`; playlist `no-cache`.
- `MaxIdleConnsPerHost: 16` — default Go (2) mengantrekan unduhan paralel.
- `reSegment` + allow-list host gambar menjaga proxy tetap tertutup.

## Struktur

```
main.go               API + scraping + proxy HLS + subtitle job
main_test.go          unit test (parse master, proxy, routing)
check.sh              go mod tidy + vet + test
progress.md           catatan keputusan desain & status
web/src/
  index.tsx           entry render
  App.tsx             Route: /, /s/:q?, /a/:animeId, /w/:animeId/:epId, 404
  components/         Header (pencarian), Player (shaka Overlay), NotFound, ui/Icon
  features/search/    Search.tsx + api.ts
  features/watch/     Watch.tsx, Anime.tsx, player.ts, catalog.ts, api.ts
  lib/                api.ts (fetch + /api), urls.ts, types.ts
ref/ani-cli           sumber porting (bash, tidak di-build)
```

## Dilewati

- **Range request** — segmen di-proxy apa adanya.
- **Pemilihan track audio (sub/dub)** — di level playlist (varian sub vs dub).
- **Cloudflare bypass** — `net/http` bisa kena blok; `errCloudflare` → 502
  dengan pesan jelas. Solusinya `curl-impersonate`, bukan regex.
- **Picture-in-picture** — tidak ada di layout kontrol.
