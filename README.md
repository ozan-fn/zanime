# zanime

Torrent video streaming server: Gin HTTP, virtual streaming + seek, cache piece di disk (`./tmp/cache`), plus a web UI untuk tempel magnet dan memilih video.

Alurnya: **torrent → [mkvgo](https://github.com/gravity-zero/mkvgo) on demand → client**. Playback lewat mkvgo (pure Go, tanpa ffmpeg/CGO): server membaca header + Cues file sekali lewat HTTP Range, membuat plan HLS, lalu setiap segmen CMAF/fMP4 di-remux saat player memintanya — tanpa encode ulang, tanpa menulis apa pun ke disk. [MoviPlayer](https://moviplayer.com) memutarnya (HLS diurus Shaka, fallback hls.js). Browser harus mendukung codec sumber (H.264/HEVC/AV1 + AAC/Opus/AC-3/E-AC-3); toggle **transcode** tidak berpengaruh lagi.

## Kenapa `anacrolix/torrent`

- `File.NewReader()` mengembalikan `io.ReadSeekCloser`, langsung cocok untuk `http.ServeContent` (HEAD, Range/206, multi-range, 416) dan sebagai sumber HTTP Range mkvgo.
- Storage backend bisa diganti, jadi piece bisa disimpan di disk dengan budget LRU sendiri (`internal/diskcache`).
- `cenkalti/rain` v2.4.4 tidak mengekspos API baca piece (`File` hanya `Length`/`Path`), jadi tidak bisa virtual streaming/seek.

## Struktur

```
main.go                        gin server, API magnet/file/transcode, embed web/
web/index.html                 UI Tailwind + <movi-player>, di-embed lewat go:embed
internal/config/               loader .env (PORT, CACHE_DIR, MAX_CACHE_MB)
internal/stream/               handler HTTP Range/seek (dipakai main + test)
internal/stream/*_test.go      unit test HEAD/Range/seek/416
internal/diskcache/            storage torrent di disk, budget LRU per torrent
internal/diskcache/*_test.go   unit test pemetaan piece→file, prune, registry
internal/hls/                  sesi HLS on demand (mp4.HLSPlan mkvgo, segmen CMAF saat diminta)
internal/hls/*_test.go         unit test sesi: playlist, segmen, subtitle, nama di luar sesi
tests/eviction_test.go         eviction + refetch, seeder/leecher lokal (tanpa internet)
tests/e2e_test.go              e2e streaming + seek + API + cek disk ke server hidup
```

## Build

Butuh Go 1.27+.

```sh
cp .env.example .env   # lalu sesuaikan PORT / CACHE_DIR / MAX_CACHE_MB
make build             # ./zanime: ldflags "-s -w", lalu upx --best --lzma
make run               # go run .
make vet               # go vet ./...
make test              # unit + eviction virtual, -race, timeout 15s
make clean             # hapus ./zanime dan zanime.exe
```

Tanpa `make`: `go build -ldflags "-s -w" -o zanime .`

`make build` membuang tabel simbol dan info debug (`-s -w`), lalu langsung
mengompres binary dengan
[upx](https://upx.github.io/) (`--best --lzma`) bila upx ada di `PATH`. Tanpa
upx, build tetap selesai dan hanya memberi pesan bahwa kompresi dilewati.

## Konfigurasi

`.env` di root (contoh lengkap di `.env.example`):

```dotenv
PORT=8080
CACHE_DIR=./tmp/cache
MAX_CACHE_MB=4096
```

| Variabel | Arti |
|----------|------|
| `PORT` | port HTTP web (satu-satunya port yang perlu dibuka di produksi) |
| `CACHE_DIR` | folder cache piece torrent (default `./tmp/cache`) |
| `MAX_CACHE_MB` | batas ukuran cache di disk (MiB, default 4096) |

Flag CLI menimpa nilai `.env`:

```sh
go run .                       # magnet contoh, 0.0.0.0:8080, cache dari .env
go run . -magnet ''            # mulai tanpa torrent, isi magnet dari UI
go run . -addr 0.0.0.0:8080 -cache-dir ./tmp/cache -cache-mb 8192
```

## Alur

Urutan tetap setiap kali sebuah video dipilih:

| # | Tahap | Pelaku | Yang terjadi | Artefak / endpoint |
|---|-------|--------|--------------|--------------------|
| 1 | Magnet | browser → server | `POST /api/magnet` menunggu metadata (maks 45 dtk), lalu daftar file video diurutkan nama | `state.files[]` |
| 2 | Pilih file | browser → server | `POST /api/select` menyetel `selected` dan memulai sesi HLS untuk index itu | `state.selected` |
| 3 | Ambil piece | torrent client | piece ditulis ke file sparse di `./tmp/cache/<infohash>/<nama torrent>/`; kalau cache lewat batas, folder torrent paling lama (LRU) dihapus utuh | `state.cache`, `state.torrent` |
| 4 | Baca file | mkvgo → `/stream/:index/:name` | mkvgo (`httpfs` + `mp4.PlanHLS`) membaca file lewat HTTP Range dari server sendiri, jadi bisa seek tanpa mengunduh seluruh file | `GET /stream...` |
| 5 | Plan | mkvgo → `/stream` | satu kali pembacaan head: durasi, track video/audio (+bahasa), subtitle teks, index Cues. File tidak diunduh | plan `mp4.HLSPlan` |
| 6 | Playlist | server | playlist **VOD lengkap** disusun dari durasi: slot 4 dtk sampai akhir + `#EXT-X-ENDLIST`. `master.m3u8` berisi ladder 1080/720 (tidak pernah di atas resolusi sumber) + satu rendition audio per track | `state.hls {index,ready,live,transcode,audio_kbps,playlist,subs,error}` |
| 7 | Putar/seek | browser → server | player minta satu segmen; mkvgo me-remux satu jendela 4 dtk jadi fMP4 (`segNNNNN.m4s`) dari sampel sumber apa adanya. Posisi mana pun sudah ada di playlist, jadi seek tidak perlu menunggu apa pun diproses lebih dulu | `GET /hls/:index/segNNNNN.m4s` |
| 8 | Subtitle | browser → server | track teks (SRT/VTT/ASS/SSA) dijadikan satu WebVTT utuh saat diminta; subtitle gambar (PGS) dibuang | `GET /hls/:index/subsN.vtt` |
| 9 | Toggle | browser → server | `POST /api/transcode` hanya menyimpan flag; stream tidak berubah (mkvgo tidak pernah meng-encode) | `state.transcode` |
| 10 | Bitrate audio | browser → server | `POST /api/audio` hanya menyimpan angka; audio sumber disalin apa adanya | `state.audio_kbps`, `state.hls.audio_kbps` |

### Mode streaming

| Hal | Perilaku | Kapan dipakai | Konsekuensi |
|-----|----------|---------------|-------------|
| **copy** (satu-satunya mode) | sampel video & audio disalin verbatim ke segmen CMAF, satu rendition audio per track | browser mendukung codec sumber (H.264 + AAC paling aman) | tanpa CPU encode, segmen nyaris instan; HEVC/AV1/AAC bergantung browser |
| **transcode** | tidak ada | — | mkvgo tidak punya transcoder: file HEVC 10-bit ke browser tanpa dukungan HEVC tidak akan bisa diputar |

Playlist dan segmen disusun oleh mkvgo, bukan tool eksternal: plan sudah lengkap (durasi penuh, `TYPE:VOD` + `EXT-X-ENDLIST`, `master.m3u8` + `playlist.m3u8` + `audioN.m3u8` + `subN.m3u8`) sebelum satu segmen pun diminta. Tiap track audio jadi rendition sendiri dengan `LANGUAGE` dari metadata; `manifest.mpd` (DASH) ikut tersedia. Segmen yang diservis server adalah `.m4s` (fMP4), bukan `.ts`.

### Siklus sesi

| Kejadian | Sesi lama | Sesi baru | Cache segmen |
|----------|-----------|-----------|--------------|
| Pilih file lain (`/api/select`) | dibatalkan | index baru, mode sama | lama dibuang |
| Ganti magnet (`/api/magnet`) | dibatalkan | index 0 torrent baru | lama dibuang |
| Toggle transcode / bitrate audio | tetap | tidak dibuat ulang | tetap (flag hanya untuk UI) |
| Index sama diminta lagi | dipakai ulang | tidak dibuat | tetap |
| Durasi gagal dibaca | — | — | tidak ada; `state.hls.error` dikirim ke UI |
| Durasi gagal dibaca | — | — | tidak ada; `state.hls.error` dikirim ke UI |

Catatan yang berlaku di semua baris tabel: tidak ada proses eksternal sama sekali — segmen dibuat di dalam proses Go; playlist `TYPE:VOD` + `EXT-X-ENDLIST` terbit begitu plan selesai, jadi player langsung mendapat durasi penuh dan bisa seek ke posisi mana pun; request segmen yang sama dibagikan oleh mkvgo (window cache) dan satu sesi aktif pada satu waktu.

Subtitle **teks** di dalam file dipetakan mkvgo jadi rendition WebVTT: SRT, WebVTT, ASS/SSA (gaya diratakan jadi teks) semuanya tersedia sebagai `subsN.vtt` utuh, karena loader subtitle eksternal movi-player 0.4.1 hanya mem-parsing srt/ttml/vtt. Subtitle gambar (PGS/DVD) dibuang oleh mkvgo dan picker subtitle di player tetap bisa dipakai untuk file subtitle dari luar.

### Sumber mkvgo

Tidak ada binary yang diunduh atau dijalankan. Library `github.com/gravity-zero/mkvgo` di-import langsung (zero dependency, tanpa CGO), jadi satu binary statis sudah cukup dan tidak ada `./tmp/ffmpeg`, `PATH` lookup, maupun unduhan saat start.

## UI

Buka `http://127.0.0.1:8080`:

| Aksi di UI | Request | Efek |
|------------|---------|------|
| Tempel magnet → **Muat torrent** | `POST /api/magnet` | menunggu metadata, daftar file video tampil urut nama; magnet + episode terakhir disimpan di `localStorage` (`zanime.last`) |
| Klik satu file | `POST /api/select` | sesi HLS untuk file itu dimulai; begitu durasi terbaca (1–3 dtk) playlist VOD dipasang dan playback jalan |
| Playback / seek | `GET /hls/:index/…` | segmen yang diminta di-remux saat itu juga (beberapa ms–ratusan ms); subtitle teks dalam file dimuat otomatis |
| Pilih audio / subtitle di player | — | menu audio berisi tiap rendition dari `master.m3u8`; menu subtitle berisi track dari file (`subsN.srt` apa adanya, `subsN.vtt` untuk ASS/SSA), plus file subtitle yang diunggah lewat picker player. Track didaftarkan sekali per sesi dan tidak diulang tiap update status, jadi pilihan tidak berkedip |
| Klik toggle **transcode** di header | `POST /api/transcode` | tidak ada efek (legacy; mkvgo tidak meng-encode) |
| Pilih **audio 128k/192k/256k** di header | `POST /api/audio` | tidak ada efek (legacy; audio sumber disalin apa adanya) |
| Klik **Batalkan** saat memuat | `POST /api/cancel` | job magnet dihentikan; torrent yang sedang aktif tidak diusik |
| Buka **Cari** | `GET /api/addon?url=...` | Cinemeta (judul/seri/episode) + Torrentio (stream); katalog Muvibox/Absolute Cinema untuk browsing saja |
| Klik stream non-torrent | `GET /fetch?url=...` | media direct diputar lewat proxy RAM (Range diteruskan, tanpa disk) |
| Diamkan panel status | `GET /api/ws` | server mendorong status tiap 700 ms (angka tidak dipolling); fallback `/api/state` tiap 2 dtk kalau WS diblokir proxy |

Panel status menampilkan: piece lengkap/parsial/hash, peer aktif·total·seeder·negosiasi, chunk useful/wasted, byte wire dan hash, isi cache disk, laju eviction/menit, kecepatan peer→server dan server→client, user/tab, uptime, heap, goroutine, dan status sesi HLS. **Stream hanya diambil dari Torrentio** (magnet + `fileIndex` untuk season pack).

MoviPlayer (`movi-player@0.4.1`), Tailwind, dan lucide di-load dari CDN. Tidak ada build frontend. Status `loading` ikut terkirim lewat WS, jadi tab lain / halaman yang di-reload ikut benar.

## Endpoint

| Path | Fungsi |
|------|--------|
| `GET /` | UI (embedded `web/index.html`) |
| `GET /api/ws` | WebSocket: dorongan status JSON tiap 700 ms (satu-satunya sumber angka di UI) |
| `GET /api/state` | status JSON sekali ambil; dipakai test/curl, bukan UI |
| `POST /api/magnet` | `{"magnet":"...","fileIndex":N}` — ganti torrent aktif, tunggu metadata; `fileIndex` opsional (urutan file di torrent) |
| `POST /api/cancel` | batalkan pemuatan magnet yang sedang jalan |
| `POST /api/select` | `{"index":N}` — pilih file video, mulai sesi HLS-nya |
| `POST /api/transcode` | `{"enabled":true\|false}` — legacy; nilai disimpan dan dikirim di state, tanpa efek pada stream |
| `POST /api/audio` | `{"kbps":128\|192\|256}` (64–320) — legacy; audio sumber disalin apa adanya |
| `GET /api/addon?url=...` | proxy katalog/meta/streams (Cinemeta, Torrentio, Absolute Cinema, Muvibox) — hanya host yang di-whitelist; browser tidak pernah memanggil addon langsung |
| `GET /stream` | file terpilih apa adanya; dukung `HEAD` dan `Range` (sumber mkvgo + fallback) |
| `GET /stream/:index/:name` | file tertentu; `name` dipakai untuk MIME/ekstensi |
| `GET/HEAD /hls/:index/:file` | sesi HLS: `master.m3u8`, `playlist.m3u8`, `audioN.m3u8`, `manifest.mpd`, `init.mp4`, `segNNNNN.m4s`, `subsN.vtt`; semua dibangun saat diminta; 404 bila sesi bukan untuk file tersebut atau nama di luar sesi |
| `GET/HEAD /fetch?url=...` | proxy streaming media direct (Range diteruskan; RAM, tanpa disk) |
| `GET /info` | ringkasan siap/nama/ukuran/cache (dipakai e2e) |
| `GET /healthz` | `ok` |

Contoh:

```sh
curl http://127.0.0.1:8080/api/state
curl -X POST http://127.0.0.1:8080/api/magnet \
  -H 'Content-Type: application/json' \
  -d '{"magnet":"magnet:?xt=urn:btih:..."}'
curl -X POST http://127.0.0.1:8080/api/transcode \
  -H 'Content-Type: application/json' -d '{"enabled":true}'
curl -X POST http://127.0.0.1:8080/api/audio \
  -H 'Content-Type: application/json' -d '{"kbps":192}'
curl http://127.0.0.1:8080/hls/0/master.m3u8
curl -I http://127.0.0.1:8080/stream
curl -H 'Range: bytes=0-1048575' -o chunk.bin http://127.0.0.1:8080/stream
```

## Cache & disk

| Aspek | Detail | Lokasi |
|-------|--------|--------|
| Penyimpanan piece | satu file sparse per file torrent, di bawah `CACHE_DIR/<infohash>/`, dibatasi `MAX_CACHE_MB` / `-cache-mb` (default 4 GiB) | `internal/diskcache` |
| Saat budget penuh | folder torrent paling lama (LRU) dihapus utuh; torrent yang sedang diputar tidak pernah dihapus | `diskcache.Prune` |
| Urutan LRU | disimpan di `CACHE_DIR/lru.json` (tulis lewat tmp + rename), jadi urutan bertahan setelah restart | `internal/diskcache` |
| Pengecekan budget | tiap 10 detik, bukan per piece, supaya jalan hanya saat cache memang membesar | `server.pruneCache` |
| Ganti magnet | folder cache torrent lama dihapus (`DropAll`) | `main.go` |
| Banyak tab | semua tab WS berbagi satu snapshot status 500 ms | `server.cachedState` |
| Disk segmen | tidak ada: playlist disusun di memori, segmen di-remux mkvgo ke RAM | `internal/hls` |

Isi `./tmp/cache` boleh dihapus kapan saja: setiap torrent diambil ulang dari magnet saat dibutuhkan. Cachenya bukan library, hanya mempercepat seek dan menonton ulang.

## Test

Unit test (hermetic, termasuk eviction virtual):

```sh
make vet
make test
```

`tests/diskcache_test.go` menjalankan seeder + leecher di proses yang sama lewat localhost, torrent 8 MiB / 8 piece, dengan `diskcache` sebagai backend leecher. Byte diambil lewat HTTP Range (maju dan mundur) tanpa internet. `internal/diskcache/diskcache_test.go` menguji pemetaan piece yang menembus dua file, prune LRU yang melewati torrent aktif, dan registry yang bertahan setelah restart.

`internal/hls/hls_test.go` menulis fixture Matroska kecil (H.264 + audio) dengan writer mkvgo, lalu memeriksa sesi: `master.m3u8` memuat satu rendition audio per track, segmen `segNNNNN.m4s` terbit saat diminta, nama di luar sesi/lewat akhir file ditolak, dan subtitle yang tidak ada tidak dijual. `main_test.go` menguji route `/hls` end-to-end dengan file yang sama diserve lewat HTTP Range.

E2E butuh server hidup dan network torrent:

```sh
go run .                                                  # terminal 1
ZANIME_E2E=1 go test -run TestE2E -timeout 90s ./tests    # terminal 2
```

Yang dicek e2e:

- `/info`: `storage=disk`, file video, `cache_used <= cache_limit`
- `HEAD /stream`: 200, `Content-Length`, `Accept-Ranges: bytes`
- Range 0–128 KiB: 206 + `Content-Range`
- seek 1 MiB dan 50% file: 206, offset benar
- range sama dua kali: byte identik
- `bytes=<size>-`: 416 + `Content-Range: bytes */<size>`
- disk: `/proc/<pid>/fd` server tidak membuka file data (fd selain stdin/stdout/stderr)
- UI: `/` memuat `<movi-player id="player">`, `subtitlepicker`, Tailwind, `/api/magnet`, `/api/transcode`
- API: magnet invalid ditolak 400, `/api/state` berisi file + URL stream yang HEAD-nya 200, `/api/select` valid 200 dan index invalid 400

Env test:

- `ZANIME_URL` — default `http://127.0.0.1:8080`
- `ZANIME_TIMEOUT` — timeout per request, default 15 detik
- `ZANIME_E2E=1` — wajib untuk menjalankan e2e

Jika satu piece torrent (4 MiB di magnet contoh) tidak selesai dalam timeout, test streaming di-skip, bukan gagal — swarm publik bisa lambat. Naikkan `ZANIME_TIMEOUT` bila perlu.

## Batasan

- Satu torrent aktif; menambah magnet baru akan mengganti torrent lama.
- Hanya file dengan ekstensi video yang ditampilkan (`.mkv`, `.mp4`, `.webm`, `.avi`, `.mov`, `.ts`, `.m2ts`, …).
- Satu sesi HLS aktif pada satu waktu: hanya file terpilih yang dilayani.
- Playback mulai setelah plan selesai (head read, 1–3 dtk), bukan setelah file diunduh. Seek ke bagian yang belum pernah ditonton menunggu satu window segmen dibaca dari torrent lalu di-remux.
- Segmen mkvgo dipotong di keyframe dengan durasi target 4 dtk (sampling terdekat), jadi batasnya bisa bergeser sedikit dari kelipatan 4 dtk.
- Subtitle hanya untuk codec teks. Semua dijadikan WebVTT (`subsN.vtt`) karena movi-player 0.4.1 tidak mem-parsing ASS/SSA sebagai track eksternal. Subtitle gambar (PGS/DVD) tidak didukung. Mengekstrak subtitle membaca seluruh file sekali, lalu hasilnya di-cache.
- Nama rendition audio mengikuti mkvgo (`audio1`, `audio2`, …) dengan tag `LANGUAGE` dari metadata file.
- Kalau MoviPlayer melaporkan `Timeout` (swarm berhenti mengirim byte), UI memuat ulang sumber yang sama dengan atribut `startat` di posisi terakhir, jadi playback lanjut dari situ — bukan mengulang dari detik 0. Maksimal 3 kali berturut-turut; hitungan direset setelah 60 dtk lancar.
