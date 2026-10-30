# zanime

Torrent video streaming server: Gin HTTP, virtual streaming + seek, cache piece di disk (`./tmp/cache`), plus a web UI untuk tempel magnet dan memilih video.

Alurnya: **torrent → [mkvgo](https://github.com/gravity-zero/mkvgo) on demand → client**. Playback lewat mkvgo (pure Go, tanpa ffmpeg/CGO): server membaca header + Cues file sekali lewat HTTP Range, membuat plan HLS, lalu setiap segmen CMAF/fMP4 di-remux saat player memintanya — tanpa encode ulang, tanpa menulis apa pun ke disk. [MoviPlayer](https://moviplayer.com) memutarnya (HLS diurus Shaka, fallback hls.js). Browser harus mendukung codec sumber (H.264/HEVC/AV1 + AAC/Opus/AC-3/E-AC-3). Tidak ada transcode: server murni remux/muxer.

## Kenapa `anacrolix/torrent`

- `File.NewReader()` mengembalikan `io.ReadSeekCloser`, langsung cocok untuk `http.ServeContent` (HEAD, Range/206, multi-range, 416) dan sebagai sumber HTTP Range mkvgo.
- Storage backend bisa diganti, jadi piece bisa disimpan di disk dengan budget LRU sendiri (`internal/diskcache`).
- `cenkalti/rain` v2.4.4 tidak mengekspos API baca piece (`File` hanya `Length`/`Path`), jadi tidak bisa virtual streaming/seek.

## Struktur

```
main.go                        gin server + API magnet/select, embed web/dist
web/                           frontend React+TS (Vite): features/ + pages/, dist di-embed go:embed
internal/config/               loader .env (PORT, CACHE_DIR, MAX_CACHE_MB)
internal/stream/               handler HTTP Range/seek (dipakai main + test)
internal/stream/*_test.go      unit test HEAD/Range/seek/416
internal/diskcache/            storage torrent di disk, budget LRU per torrent
internal/diskcache/*_test.go   unit test pemetaan piece→file, prune, registry
internal/hls/                  sesi HLS on demand (mp4.HLSPlan mkvgo, segmen CMAF saat diminta)
internal/hls/*_test.go         unit test sesi: playlist, segmen, subtitle, nama di luar sesi
internal/media/                metadata Matroska: judul/tag/chapter/track/cover
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
| 6 | Playlist | server | playlist **VOD lengkap** disusun dari durasi: slot 4 dtk sampai akhir + `#EXT-X-ENDLIST`. `master.m3u8` berisi ladder 1080/720 (tidak pernah di atas resolusi sumber) + satu rendition audio per track | `state.hls {index,ready,live,playlist,subs,error}` |
| 7 | Putar/seek | browser → server | player minta satu segmen; mkvgo me-remux satu jendela 4 dtk jadi fMP4 (`segNNNNN.m4s`) dari sampel sumber apa adanya. Posisi mana pun sudah ada di playlist, jadi seek tidak perlu menunggu apa pun diproses lebih dulu | `GET /hls/:index/segNNNNN.m4s` |
| 8 | Subtitle | browser → server | track teks (SRT/VTT/ASS/SSA) diekspos sebagai rendition HLS `subN.m3u8`; hls.js mengambil segmen VTT 4 dtk sesuai playhead, bukan satu file utuh | `GET /hls/:index/subN.m3u8`, `subN_NNNNN.vtt` |
| 9 | Audio/subtitle | browser → player | track audio & subtitle dipilih di player (MoviPlayer); semua track sumber disalin apa adanya, tidak ada pilihan bitrate/encode | menu player |

### Mode streaming

| Hal | Perilaku | Kapan dipakai | Konsekuensi |
|-----|----------|---------------|-------------|
| **copy** (satu-satunya mode) | sampel video & audio disalin verbatim ke segmen CMAF, satu rendition audio per track | browser mendukung codec sumber (H.264 + AAC paling aman) | tanpa CPU encode, segmen nyaris instan; HEVC/AV1/AAC bergantung browser |

Playlist dan segmen disusun oleh mkvgo, bukan tool eksternal: plan sudah lengkap (durasi penuh, `TYPE:VOD` + `EXT-X-ENDLIST`, `master.m3u8` + `playlist.m3u8` + `audioN.m3u8` + `subN.m3u8`) sebelum satu segmen pun diminta. Tiap track audio jadi rendition sendiri dengan `LANGUAGE` dari metadata; `manifest.mpd` (DASH) ikut tersedia. Segmen yang diservis server adalah `.m4s` (fMP4), bukan `.ts`.

### Siklus sesi

| Kejadian | Sesi lama | Sesi baru | Cache segmen |
|----------|-----------|-----------|--------------|
| Pilih file lain (`/api/select`) | dibatalkan | index baru, mode sama | lama dibuang |
| Ganti magnet (`/api/magnet`) | dibatalkan | index 0 torrent baru | lama dibuang |
| Index sama diminta lagi | dipakai ulang | tidak dibuat | tetap |
| Durasi gagal dibaca | — | — | tidak ada; `state.hls.error` dikirim ke UI |
| Durasi gagal dibaca | — | — | tidak ada; `state.hls.error` dikirim ke UI |

Catatan yang berlaku di semua baris tabel: tidak ada proses eksternal sama sekali — segmen dibuat di dalam proses Go; playlist `TYPE:VOD` + `EXT-X-ENDLIST` terbit begitu plan selesai, jadi player langsung mendapat durasi penuh dan bisa seek ke posisi mana pun; request segmen yang sama dibagikan oleh mkvgo (window cache) dan satu sesi aktif pada satu waktu.

Subtitle **teks** di dalam file dipetakan mkvgo jadi rendition HLS: SRT, WebVTT, ASS/SSA (gaya diratakan jadi teks) tersedia sebagai `subN.m3u8` + segmen `subN_NNNNN.vtt`. hls.js memuatnya per segmen saat playhead mendekat, jadi file 3 GiB tidak memicu satu permintaan ekstraksi raksasa yang bisa diputus Cloudflare. `subsN.vtt` utuh tetap tersedia untuk klien lain. Subtitle gambar (PGS/DVD) dibuang oleh mkvgo dan picker subtitle di player tetap bisa dipakai untuk file subtitle dari luar.

### Metadata, chapter, dan cover

Saat file dipilih, server membaca head Matroska sekali lewat `matroska.ReadMeta` (mkvgo) di samping perencanaan HLS: judul, tag (`DESCRIPTION`/`SYNOPSIS`, `RATING`, `GENRE`, tanggal), daftar chapter, bahasa/nama/codec/resolusi/sample-rate tiap track, dan attachment gambar. Hasilnya masuk `state.media`; UI menampilkan kartu info serta mendaftarkan chapter yang bisa diklik. Judul, chapter, dan cover juga dipasang sebagai atribut `title`, `chapters`, dan `poster` MoviPlayer, jadi overlay judul dan marker chapter di progress bar memakai fitur bawaan player.

Cover diambil dari attachment `cover.*`/`poster.*` (maksimal 8 MiB) dan diserve lewat `GET /cover/:index`; font attachment tidak dimuat karena subtitle sudah menjadi WebVTT tanpa styling ASS. Cues sudah dipakai mkvgo untuk seek. Track video ganda/angle, styling ASS + font, dan menu Matroska tidak didukung HLS/MoviPlayer, jadi tidak dipaksakan.

### Sumber mkvgo

Tidak ada binary yang diunduh atau dijalankan. Library `github.com/gravity-zero/mkvgo` di-import langsung (zero dependency, tanpa CGO), jadi satu binary statis sudah cukup dan tidak ada `./tmp/ffmpeg`, `PATH` lookup, maupun unduhan saat start.

## UI

Buka `http://127.0.0.1:8080`:

| Aksi di UI | Request | Efek |
|------------|---------|------|
| Tempel magnet → **Muat torrent** | `POST /api/magnet` | menunggu metadata, daftar file video tampil urut nama; magnet + episode terakhir disimpan di `localStorage` (`zanime.last`) |
| Klik satu file | `POST /api/select` | sesi HLS untuk file itu dimulai; begitu durasi terbaca (1–3 dtk) playlist VOD dipasang dan playback jalan. Metadata/chapter/cover dibaca paralel dan muncul di kartu info |
| Playback / seek | `GET /hls/:index/…` | segmen yang diminta di-remux saat itu juga (beberapa ms–ratusan ms); subtitle teks dalam file dimuat otomatis |
| Pilih audio / subtitle di player | — | menu audio/subtitle berisi rendition dari `master.m3u8`; subtitle dimuat hls.js per segmen (termasuk ASS/SSA yang diratakan), plus file subtitle dari picker player |
| Klik **Batalkan** saat memuat | `POST /api/cancel` | job magnet dihentikan; torrent yang sedang aktif tidak diusik |
| Buka **Cari** | `GET /api/addon?url=...` | Cinemeta (judul/seri/episode) + Torrentio (stream); katalog Muvibox/Absolute Cinema untuk browsing saja |
| Klik stream non-torrent | `GET /fetch?url=...` | media direct diputar lewat proxy RAM (Range diteruskan, tanpa disk) |
| Diamkan panel status | `GET /api/ws` | server mendorong status tiap 700 ms (angka tidak dipolling); fallback `/api/state` tiap 2 dtk kalau WS diblokir proxy |

Panel status menampilkan: piece lengkap/parsial/hash, peer aktif·total·seeder·negosiasi, chunk useful/wasted, byte wire dan hash, isi cache disk, laju eviction/menit, kecepatan peer→server dan server→client, user/tab, uptime, heap, goroutine, dan status sesi HLS. **Stream hanya diambil dari Torrentio** (magnet + `fileIndex` untuk season pack).

MoviPlayer (`movi-player@0.4.1`), Tailwind, dan lucide dibundel lewat Vite dari `web/`; `make build` membangun frontend ke `web/dist` sebelum binary Go. Status `loading` ikut terkirim lewat WS, jadi tab lain / halaman yang di-reload ikut benar.

## Endpoint

| Path | Fungsi |
|------|--------|
| `GET /` | UI SPA (embedded `web/dist`) |
| `GET /api/ws` | WebSocket: dorongan status JSON tiap 700 ms (satu-satunya sumber angka di UI) |
| `GET /api/state` | status JSON sekali ambil; dipakai test/curl, bukan UI |
| `POST /api/magnet` | `{"magnet":"...","fileIndex":N}` — ganti torrent aktif, tunggu metadata; `fileIndex` opsional (urutan file di torrent) |
| `POST /api/cancel` | batalkan pemuatan magnet yang sedang jalan |
| `POST /api/select` | `{"index":N}` — pilih file video, mulai sesi HLS-nya |
| `GET /api/addon?url=...` | proxy katalog/meta/streams (Cinemeta, Torrentio, Absolute Cinema, Muvibox) — hanya host yang di-whitelist; browser tidak pernah memanggil addon langsung |
| `GET /stream` | file terpilih apa adanya; dukung `HEAD` dan `Range` (sumber mkvgo + fallback) |
| `GET /stream/:index/:name` | file tertentu; `name` dipakai untuk MIME/ekstensi |
| `GET/HEAD /hls/:index/:file` | sesi HLS: `master.m3u8`, `playlist.m3u8`, `audioN.m3u8`, `subN.m3u8`, `manifest.mpd`, `init.mp4`, `segNNNNN.m4s`, `subN_NNNNN.vtt`, `subsN.vtt`; semua dibangun saat diminta; 404 bila sesi bukan untuk file tersebut atau nama di luar sesi |
| `GET/HEAD /cover/:index` | cover art yang tertanam di file terpilih (attachment gambar Matroska); 404 bila tidak ada |
| `GET/HEAD /fetch?url=...` | proxy streaming media direct (Range diteruskan; RAM, tanpa disk) |
| `GET /info` | ringkasan siap/nama/ukuran/cache (dipakai e2e) |
| `GET /healthz` | `ok` |

Contoh:

```sh
curl http://127.0.0.1:8080/api/state
curl -X POST http://127.0.0.1:8080/api/magnet \
  -H 'Content-Type: application/json' \
  -d '{"magnet":"magnet:?xt=urn:btih:..."}'
curl http://127.0.0.1:8080/hls/0/master.m3u8
curl -I http://127.0.0.1:8080/stream
curl -H 'Range: bytes=0-1048575' -o chunk.bin http://127.0.0.1:8080/stream
```

## Cache & disk

| Aspek | Detail | Lokasi |
|-------|--------|--------|
| Penyimpanan piece | satu file sparse per file torrent, di bawah `CACHE_DIR/<infohash>/`, dibatasi `MAX_CACHE_MB` / `-cache-mb` (default 4 GiB) | `internal/diskcache` |
| Saat budget penuh | folder torrent paling lama (LRU) dihapus utuh; kalau torrent aktif masih kebesaran, piece lama di belakang playhead di-`punch hole` per chunk (bukan menunggu satu episode selesai) | `diskcache.Prune`, `diskcache.evictPieceLocked` |
| Ukuran cache | blok disk yang benar-benar terpakai (`stat.Blocks`), bukan panjang logis file sparse; tail/Cues 3 GiB tidak membuat cache terlihat 3 GiB | `diskcache.dirSize` |
| Pindah episode | file episode lama di torrent aktif dihapus; bit `.complete` piece yang menyentuhnya dibersihkan dan completion klien di-refresh, jadi episode itu diunduh lagi kalau dipilih | `server.dropOtherFiles` |
| Urutan LRU | disimpan di `CACHE_DIR/lru.json` (tulis lewat tmp + rename), jadi urutan bertahan setelah restart | `internal/diskcache` |
| Status piece | bitmap `.complete` per torrent (satu bit per piece) ditulis saat piece terverifikasi dan dimuat lagi di `OpenTorrent`; cache tetap dianggap lengkap setelah restart, tanpa rehash/unduh ulang | `internal/diskcache` |
| Pengecekan budget | tiap 2 detik; piece terbaru (playhead + readahead) dipertahankan; torrent aktif dibatasi maksimal 512 MiB walau `MAX_CACHE_MB` lebih besar | `server.pruneCache`, `diskcache.activeCacheLimit` |
| Ganti magnet | folder cache torrent lama dihapus (`DropAll`) | `main.go` |
| Banyak tab | semua tab WS berbagi satu snapshot status 500 ms | `server.cachedState` |
| Disk segmen | tidak ada: playlist disusun di memori, segmen di-remux mkvgo ke RAM | `internal/hls` |

Isi `./tmp/cache` boleh dihapus kapan saja: setiap torrent diambil ulang dari magnet saat dibutuhkan. Cachenya bukan library, hanya mempercepat seek dan menonton ulang. Selama folder torrent masih ada, bit `.complete` membuat byte yang sudah terverifikasi dipakai lagi setelah restart.

## Test

Unit test (hermetic, termasuk eviction virtual):

```sh
make vet
make test
```

`tests/diskcache_test.go` menjalankan seeder + leecher di proses yang sama lewat localhost, torrent 8 MiB / 8 piece, dengan `diskcache` sebagai backend leecher. Byte diambil lewat HTTP Range (maju dan mundur) tanpa internet. `internal/diskcache/diskcache_test.go` menguji pemetaan piece yang menembus dua file, prune LRU yang melewati torrent aktif, registry yang bertahan setelah restart, dan bitmap `.complete` yang membuat piece terverifikasi tidak diunduh ulang setelah reopen.

`internal/hls/hls_test.go` menulis fixture Matroska kecil (H.264 + audio) dengan writer mkvgo, lalu memeriksa sesi: `master.m3u8` memuat satu rendition audio per track (termasuk playlist/init/segmen `audioN`/`seg_aN_*`), segmen `segNNNNN.m4s` terbit saat diminta, nama di luar sesi/lewat akhir file ditolak, dan subtitle yang tidak ada tidak dijual. Fixture subtitle memeriksa `LANGUAGE`/`NAME` dari master serta resource `subs0.vtt`, `sub1.vtt`, dan `sub1_00001.vtt`. `internal/media/media_test.go` menulis MKV dengan judul, tag, dua chapter tak berurutan, track video/audio/subtitle, font, dan cover; hasil baca harus mengurutkan chapter, membawa bahasa/teknis track, dan hanya memuat gambar cover. `main_test.go` menguji route `/hls` end-to-end dengan file yang sama diserve lewat HTTP Range, plus `state.media` dan `/cover/:index`.

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
- UI: `/`, `/files`, `/status` menyaji SPA (root React + bundle `/assets/…`), `/api/magnet` tersedia
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
- Subtitle hanya untuk codec teks, diekspos sebagai rendition HLS per segmen (`subN.m3u8`/`subN_NNNNN.vtt`); subtitle gambar (PGS/DVD) tidak didukung. Frontend tidak lagi meminta `subsN.vtt` utuh saat mulai memutar, karena ekstraksi seluruh track bisa lama dan memicu Cloudflare 521.
- Nama rendition audio mengikuti mkvgo (`audio1`, `audio2`, …) dengan tag `LANGUAGE` dari metadata file.
- Kalau MoviPlayer melaporkan `Timeout` (swarm berhenti mengirim byte), UI memuat ulang sumber yang sama dengan atribut `startat` di posisi terakhir, jadi playback lanjut dari situ — bukan mengulang dari detik 0. Maksimal 3 kali berturut-turut; hitungan direset setelah 60 dtk lancar.
