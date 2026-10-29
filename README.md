# zanime

Torrent video streaming server: Gin HTTP, virtual streaming + seek, cache RAM-only, zero disk, plus a web UI untuk tempel magnet dan memilih video.

Alurnya: **torrent → ffmpeg HLS → client**. Semua playback lewat ffmpeg: server membaca piece dari torrent (RAM), ffmpeg mengubah file terpilih menjadi playlist + segmen HLS, dan [MoviPlayer](https://moviplayer.com) memutarnya (HLS diurus Shaka, fallback hls.js). Toggle **transcode** menentukan ffmpeg menyalin track (`-c copy`) atau meng-encode H.264/AAC.

## Kenapa `anacrolix/torrent`

- `File.NewReader()` mengembalikan `io.ReadSeekCloser`, langsung cocok untuk `http.ServeContent` (HEAD, Range/206, multi-range, 416) dan sebagai input ffmpeg.
- Storage backend bisa diganti, jadi piece bisa disimpan di RAM.
- `cenkalti/rain` v2.4.4 tidak mengekspos API baca piece (`File` hanya `Length`/`Path`), jadi tidak bisa virtual streaming/seek.

## Struktur

```
main.go                        gin server, API magnet/file/transcode, embed web/
web/index.html                 UI Tailwind + <movi-player>, di-embed lewat go:embed
internal/config/               loader .env (PORT, MAX_MEMORY_MB)
internal/stream/               handler HTTP Range/seek (dipakai main + test)
internal/stream/*_test.go      unit test HEAD/Range/seek/416
internal/memstorage/           storage torrent RAM-only, LRU
internal/memstorage/*_test.go  unit test cache + eviction
internal/hls/                  sesi ffmpeg HLS (master + rendition audio) + auto-download binary
internal/hls/*_test.go         unit test sesi, probe audio, argumen ffmpeg, toggle mode
tests/eviction_test.go         eviction + refetch, seeder/leecher lokal (tanpa internet)
tests/e2e_test.go              e2e streaming + seek + API + cek disk ke server hidup
```

## Build

Butuh Go 1.27+.

```sh
cp .env.example .env   # lalu sesuaikan PORT / MAX_MEMORY_MB
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
MAX_MEMORY_MB=200
```

| Variabel | Arti |
|----------|------|
| `PORT` | port HTTP web (satu-satunya port yang perlu dibuka di produksi) |
| `MAX_MEMORY_MB` | batas RAM cache piece torrent (MiB) |

Flag CLI menimpa nilai `.env`:

```sh
go run .                       # magnet contoh, 0.0.0.0:8080, cache dari .env
go run . -magnet ''            # mulai tanpa torrent, isi magnet dari UI
go run . -addr 0.0.0.0:8080 -cache-mb 128
```

## Alur

Urutan tetap setiap kali sebuah video dipilih:

| # | Tahap | Pelaku | Yang terjadi | Artefak / endpoint |
|---|-------|--------|--------------|--------------------|
| 1 | Magnet | browser → server | `POST /api/magnet` menunggu metadata (maks 45 dtk), lalu daftar file video diurutkan nama | `state.files[]` |
| 2 | Pilih file | browser → server | `POST /api/select` menyetel `selected` dan memulai sesi HLS untuk index itu | `state.selected` |
| 3 | Ambil piece | torrent client | piece ditulis ke RAM (`memstorage`), LRU membuang yang paling lama; eviction ditandai belum lengkap agar diunduh ulang | `state.cache`, `state.torrent` |
| 4 | Baca file | ffmpeg → `/stream/:index/:name` | ffmpeg membaca file lewat HTTP Range dari server sendiri, jadi bisa seek | `GET /stream...` |
| 5 | Segmentasi | ffmpeg | menulis playlist + segmen ke folder temp OS `zanime-hls-*` (`-hls_time 4`, `-hls_playlist_type vod`). Satu track audio → `index.m3u8`; lebih dari satu → `master.m3u8` + `video/`, `audio0..N/`. `-progress` dilaporkan ke server | `master.m3u8`, `video/index.m3u8`, `audio0/index.m3u8`, `seg00000.ts` |
| 6 | Siap | server | ffmpeg selesai dan playlist berisi `#EXT-X-ENDLIST` → `hls.ready = true`. Selama menyiapkan, `state.hls.progress` (0–100%) tampil di UI | `state.hls {index,ready,transcode,playlist,progress,duration,error}` |
| 7 | Putar | browser | `<movi-player>` memuat playlist **VOD** (HLS lewat Shaka, fallback hls.js): durasi langsung terisi, seek ke mana saja, menu audio berisi satu item per rendition | `GET /hls/:index/:file` |
| 8 | Toggle | browser → server | `POST /api/transcode` mengganti mode, sesi lama dibatalkan, sesi baru dimulai, player dimuat ulang | `state.transcode` |

### Mode ffmpeg

| Toggle | Perintah | Kapan dipakai | Konsekuensi |
|--------|----------|---------------|-------------|
| **transcode: off** (default) | `-c copy -sn` + satu rendition per track audio | browser mendukung codec sumber (H.264/AAC paling aman) | tanpa CPU tambahan, mulai cepat, tapi HEVC/AV1 bergantung browser |
| **transcode: on** | `-c:v libx264 -preset veryfast -c:a aac -b:a 128k -sn` | file HEVC/AV1 atau browser tanpa dukungan codec | bisa diputar di browser apa pun, butuh CPU server dan lebih lambat siap |

Track audio dihitung lewat banner `ffmpeg -i` (ffprobe tidak ikut di build statis) lalu tiap track jadi rendition sendiri: `-map 0:a:0 ... -map 0:a:N`, `-var_stream_map "v:0,agroup:aud,name:video a:0,agroup:aud,name:audio0,default:yes a:1,..."`. Bahasa dari metadata dipakai sebagai `LANGUAGE` rendition.

### Siklus sesi

| Kejadian | Sesi lama | Sesi baru | Folder temp |
|----------|-----------|-----------|--------------|
| Pilih file lain (`/api/select`) | dibatalkan | index baru, mode sama | lama dihapus, baru dibuat |
| Ganti magnet (`/api/magnet`) | dibatalkan | index 0 torrent baru | lama dihapus |
| Toggle transcode (`/api/transcode`) | dibatalkan | index sama, mode baru | lama dihapus |
| Index + mode sama diminta lagi | dipakai ulang | tidak dibuat | tetap |
| ffmpeg selesai | — | — | **disimpan** sampai sesi diganti (playback tetap jalan) |
| ffmpeg gagal | — | — | dihapus; `state.hls.error` dikirim ke UI |

Catatan yang berlaku di semua baris tabel: subtitle di dalam file tidak ikut (`-sn`, karena PGS/ASS membuat muxer TS gagal) — penonton bisa menambahkan file subtitle sendiri lewat tombol subtitle di player (`subtitlepicker`); playlist `TYPE:VOD` + `EXT-X-ENDLIST` ditulis setelah segmentasi selesai, jadi player mendapat durasi penuh dan bisa seek ke posisi mana pun; satu sesi aktif pada satu waktu.

### Sumber ffmpeg

| Urutan | Yang dipakai | Kapan |
|--------|--------------|-------|
| 1 | `ffmpeg` dari `PATH` | ffmpeg terpasang di sistem |
| 2 | `./tmp/ffmpeg` (`./tmp/ffmpeg.exe` di Windows) | sistem tidak punya, salinan lama masih ada dan bisa dijalankan |
| 3 | unduh build statis ffmpeg-static ke `./tmp` | keduanya tidak ada |

Unduhan hanya terjadi sekali: hasilnya dicek dengan `ffmpeg -version` sebelum dipakai, ditulis ke `*.part` lalu di-rename (unduhan terputus tidak meninggalkan binary rusak), dan path yang sudah terselesaikan di-cache untuk proses itu.

## UI

Buka `http://127.0.0.1:8080`:

| Aksi di UI | Request | Efek |
|------------|---------|------|
| Tempel magnet → **Muat torrent** | `POST /api/magnet` | menunggu metadata, daftar file video tampil urut nama; magnet + episode terakhir disimpan di `localStorage` (`zanime.last`) |
| Klik satu file | `POST /api/select` | sesi HLS untuk file itu dimulai; status menampilkan `Menyiapkan … · N%`, player baru memasang playlist `state.hls.playlist` ketika `state.hls.ready` |
| Tunggu penyiapan | `GET /api/ws` | persen dari `-progress` ffmpeg terhadap durasi file; gagal → pesan error dari `state.hls.error` |
| Pilih audio / subtitle di player | — | menu audio berisi tiap rendition dari `master.m3u8`; subtitle dalam file belum dikirim, subtitle eksternal bisa diunggah lewat picker player |
| Klik toggle **transcode** di header | `POST /api/transcode` | mode ffmpeg berganti, sesi diulang, player dimuat ulang |
| Klik **Batalkan** saat memuat | `POST /api/cancel` | job magnet dihentikan; torrent yang sedang aktif tidak diusik |
| Buka **Cari** | `GET /api/addon?url=...` | Cinemeta (judul/seri/episode) + Torrentio (stream); katalog Muvibox/Absolute Cinema untuk browsing saja |
| Klik stream non-torrent | `GET /fetch?url=...` | media direct diputar lewat proxy RAM (Range diteruskan, tanpa disk) |
| Diamkan panel status | `GET /api/ws` | server mendorong status tiap 700 ms (angka tidak dipolling); fallback `/api/state` tiap 2 dtk kalau WS diblokir proxy |

Panel status menampilkan: piece lengkap/parsial/hash, peer aktif·total·seeder·negosiasi, chunk useful/wasted, byte wire dan hash, isi RAM per piece, laju eviction/menit, kecepatan peer→server dan server→client, user/tab, uptime, heap, goroutine, dan status sesi HLS. **Stream hanya diambil dari Torrentio** (magnet + `fileIndex` untuk season pack).

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
| `POST /api/transcode` | `{"enabled":true\|false}` — mode ffmpeg `copy` ↔ encode H.264/AAC; sesi terpilih diulang |
| `GET /api/addon?url=...` | proxy katalog/meta/streams (Cinemeta, Torrentio, Absolute Cinema, Muvibox) — hanya host yang di-whitelist; browser tidak pernah memanggil addon langsung |
| `GET /stream` | file terpilih apa adanya; dukung `HEAD` dan `Range` (input ffmpeg + fallback) |
| `GET /stream/:index/:name` | file tertentu; `name` dipakai untuk MIME/ekstensi |
| `GET/HEAD /hls/:index/:file` | file sesi HLS: `index.m3u8` (satu audio) atau `master.m3u8` + `video/index.m3u8`, `audio0/index.m3u8`, … dan segmen `.ts`; 404 bila sesi bukan untuk file tersebut |
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
curl http://127.0.0.1:8080/hls/0/index.m3u8
curl -I http://127.0.0.1:8080/stream
curl -H 'Range: bytes=0-1048575' -o chunk.bin http://127.0.0.1:8080/stream
```

## Cache & disk

| Aspek | Detail | Lokasi |
|-------|--------|--------|
| Penyimpanan piece | `map` + LRU (`container/list`), dibatasi `MAX_MEMORY_MB` / `-cache-mb` (default 200 MiB) | `internal/memstorage` |
| Saat budget penuh | piece paling lama dibuang, `Eviction{Hash, Index}` dikirim lewat channel `Evicted()` | `internal/memstorage` |
| Sinkronisasi torrent | `t.Piece(index).UpdateCompletion()` untuk piece yang dibuang, jadi diunduh ulang saat ffmpeg membacanya lagi | `server.syncEvictions` |
| Disk torrent | `cfg.DataDir = ""` + `DefaultStorage = memstorage`; tidak ada file data | `main.go` |
| Piece > limit | tetap disimpan (satu piece harus muat) agar progres tidak macet | `internal/memstorage` |
| Ganti magnet | piece torrent lama dilepas (`DropAll`); `cache.pieces` hanya menghitung torrent aktif (`PiecesOf`) | `main.go` |
| Eviction berulang | piece yang sudah ditandai tidak lengkap dilewati (`State()` dicek dulu) | `server.syncEvictions` |
| Banyak tab | semua tab WS berbagi satu snapshot status 500 ms | `server.cachedState` |
| Disk ffmpeg | playlist + segmen HLS di folder temp OS `zanime-hls-*`, dihapus saat sesi berhenti/ganti | `internal/hls` |

Data torrent tidak pernah menyentuh disk. Satu episode tersimpan penuh di folder temp selama diputar (event playlist menyimpan semua segmen supaya seek ke belakang tetap jalan).

## Test

Unit test (hermetic, termasuk eviction virtual):

```sh
make vet
make test
```

`tests/eviction_test.go` menjalankan seeder + leecher di proses yang sama lewat localhost, torrent 8 MiB / 8 piece, cache 2 piece. Efeknya eviction, refetch setelah seek, dan isi byte diverifikasi tanpa internet (~0,2 detik). Kedua `DataDir` dicek tetap kosong.

`internal/hls/hls_test.go` memakai ffmpeg palsu (shell script) untuk memeriksa sesi: playlist & segmen tersedia, index lain ditolak, nama tidak bisa keluar dari folder sesi, dan ganti mode transcode memulai sesi baru.

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
- Satu sesi HLS aktif pada satu waktu: hanya file terpilih yang di-segmentasi.
- Playback baru mulai setelah segmentasi selesai (setara membaca seluruh file): durasi dan seek penuh, tapi tidak instan. Mode transcode lebih lama daripada `-c copy`.
- Subtitle di dalam file belum dikirim ke player (`-sn`); subtitle eksternal bisa diunggah lewat picker di player.
- Nama rendition audio mengikuti ffmpeg (`audio_1`, `audio_2`, …) dengan tag `LANGUAGE` dari metadata file.
