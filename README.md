# Test, progres & kekurangan

## Flow streaming (detail — silakan koreksi)

Ini urutan yang benar-benar dijalankan kode sekarang. Nomor biaya ditulis supaya jelas mana
yang makan kuota/CPU.

### 1. Pilih judul → mulai sesi torrent

- `GET /api/search?q=…` → `meta.SearchAll`. Pilih judul → `GET /api/meta` (detail + daftar
  episode), lalu `GET /api/streams` (Torrentio → magnet per rilis).
- Frontend membuka `/watch/:sid` dengan magnet di query →
  `GET /api/torrent/start?hash=…&tr=…` → `Manager.Start`:
  - magnet dibangun dari hash + tracker (default tracker publik ditambah, kecuali
    `ZANIME_TORRENT_NO_DHT=1`);
  - `waitInfo` async menunggu metadata torrent (maks 90 dtk), lalu memilih file video
    terbesar (atau `fileIdx` dari request);
  - **biaya: metadata saja (KB)**.

### 2. Status poll → probe (header file, bukan "selalu 5 MB")

- Frontend memanggil `GET /api/torrent/status/{sid}` tiap 800 ms selama belum siap, lalu 2 dtk.
- Begitu info torrent siap, status memicu `startProbe`:
  - **MKV: parser EBML sendiri** (`internal/mkv`) — membaca header sampai daftar Tracks,
    **berhenti sebelum lampiran font**, jadi ±32 KB dan <1 dtk (log: `probe mkv …: 32768 byte`);
  - kalau header itu tidak terbaca, fallback `ffprobe` `-analyzeduration 2s -probesize 1M`
    (diulang 5M/5s kalau kosong). Untuk rilis fansub berlampiran font (neoDESU/Okay-Subs)
    biaya ffprobe = panjang header, terukur **38,6 MB / 48 dtk** — inilah yang dulu terasa lambat.
- Kalau probe >8 dtk, UI tetap memutar video (`forcePlay`), daftar track menyusul.
- **biaya: KB (MKV) atau header file (KB–40 MB kalau fallback), CPU kecil, tanpa transcode**.

### 3. Player mulai (gate)

- Status dikirim dua jalur: polling `GET /api/torrent/status/{sid}` (fallback) dan
  **WebSocket `GET /api/torrent/ws/{sid}`** yang push tiap **100 ms** — indikator alur
  (Peer/Server/Kamu) bergerak real-time dan label berubah `realtime · WS` saat tersambung;
  garis putus-putus hanya beranimasi kalau ada aliran (idle = diam).
- `<video src="/stream/{sid}/{fileIdx}?audio={index}">` baru dipasang saat
  `status.ready && status.probe.done` — jadi butuh durasi/track dulu, **tidak menunggu
  subtitle**. Kartu "Menyiapkan info video…" hilang begitu probe selesai.
- Subtitle tidak ditunggu: `<track>` dirender untuk ref yang sudah punya isi —
  `subsCached` (lengkap) **atau** `subsPartial` (masih mengalir dari pass video). Versi URL
  ikut ukuran byte, jadi cue baru termuat tanpa reload halaman.

### 4. Jalur `/stream`

- **Direct** (mp4/m4v/webm/ogv/mov): `http.ServeContent` membaca `torrent.File.NewReader()`
  → didukung `Range`/seek, byte dikirim sambil torrent mengunduh.
- **Remux** (mkv/ts/avi/wmv/dll): `streamRemux` menjalankan ffmpeg satu kali:
  - video **selalu copy** (`-c:v copy`), audio **copy dulu**;
  - audio di-encode AAC **hanya kalau** header mp4 ditolak ffmpeg (AC3/E-AC3/DTS) — percobaan
    pertama selalu copy supaya CPU tidak dipakai kalau tidak perlu;
  - output fragmented mp4 (`empty_moov`, fragmen ~2 dtk) dipipe langsung ke response, tanpa
    file cache di disk;
  - 64 KiB pertama ditahan di memori (`holdWriter`) supaya percobaan yang gagal di header
    bisa diulang tanpa mengirim data rusak ke player;
  - **subtitle tertanam ikut pass yang sama**: ffmpeg diberi output tambahan
    `-map 0:{idx} -c:s webvtt -f webvtt <cache>/subs/t{idx}.vtt.part`, jadi file dibaca
    sekali saja (video + subtitle sekaligus) — tidak ada unduh ulang untuk subtitle;
  - saat pass selesai, `.part` dirapikan jadi `.vtt` final; sementara berjalan, `/subs/…`
    menyajikan potongan VTT yang cue-nya sudah utuh (dipotong di akhir cue terakhir);
  - **durasi dikunci di header mvhd**: fragmented mp4 bawaan ffmpeg menulis `mvhd=0`
    (durasi tidak bisa diketahui di awal) sehingga player menampilkan durasi yang terus
    membesar. Header moov ditahan sebentar lalu durasi hasil probe ditulis ke
    `mvhd`/`tkhd`/`mdhd` → total terkunci (mis. `23:00`) — cek ada di
    `TestRemuxDurasiTerkunci` + `TestRemuxMkvStreams`;
  - konsekuensi: `Range`/seek belum ada, durasi asli tetap dikirim di status dan ditampilkan
    di header UI.

### 5. Penyimpanan data torrent (berbatas)

- Data potongan disimpan sebagai cache LRU di `./tmp/zanime-data`, capacity
  `ZANIME_CACHE_LIMIT_MB` (default 512 MB). Potongan tertua dibuang otomatis.
- Request strategy juga diberi `Capacity` dan reader diberi readahead terbatas (1/8 capacity,
  maks 16 MB) supaya pembuangan tidak memotong data yang belum dibaca (kalau tidak: macet).
- Sisa unduhan versi lama yang melebihi batas dibersihkan sekali saat server start.
- **biaya: disk ≤ batas, tapi data yang sudah dibuang akan diunduh lagi kalau dibaca ulang.**

### 6. Subtitle (ikut pass video, tanpa unduh penuh)

- **Tertanam (di MKV):** tidak pernah diekstrak sendiri. Semua track subtitle ditulis
  bersamaan dengan transmux saat diputar (`SubFiles` di `ffmpeg.RemuxProgressive`).
  Kalau belum ada yang memutar, `/subs/{ref=t…}` membalas `202 pending` (VTT kosong).
- Hasilnya VTT kecil (beberapa KB) di `./tmp/zanime-cache/<sid>/subs/t{idx}.vtt`; tontonan
  berikutnya memakai file itu (tidak ada pass tambahan sama sekali).
- **Subtitle eksternal** (file `.srt/.ass/.vtt` di torrent): diekstrak saat diminta
  (`?warm=f{idx}`), praktis instan karena file-nya kecil, lalu di-cache.
- **biaya: nol tambahan** — menumpang pass video yang memang sudah membaca file..

### 7. Berhenti & bersih-bersih

- `GET /api/torrent/stop/{sid}` → sesi dihapus, cache subtitle sesi dihapus, torrent di-drop.
- Prune tiap 5 menit: sesi idle >30 menit dibuang (cache subtitle ikut dihapus; data torrent
  dibatasi oleh cache LRU, tidak lagi menumpuk).

### 8. Env yang berpengaruh

| Env | Arti |
| --- | --- |
| `PORT` / `-addr` | alamat web (default `:8080`) |
| `ZANIME_TMP_DIR` | folder kerja (default `./tmp`, fallback temp sistem) |
| `ZANIME_CACHE_LIMIT_MB` | batas cache data torrent (default 512) |
| `ZANIME_TORRENT_DATA_DIR` | lokasi cache torrent (default `./tmp/zanime-data`) |
| `ZANIME_CACHE_DIR` | lokasi cache subtitle VTT (default `./tmp/zanime-cache`) |
| `ZANIME_TORRENT_NO_DHT` | mode hemat kuota: tanpa DHT/PEX/tracker publik |
| `ZANIME_TORRENT_DEBUG` | log anacrolix penuh |

### Titik yang paling mungkin perlu dikoreksi

1. Tanpa cache MP4: seek pada mkv/ts belum ada. Kalau seek penting, harus ada disk lebih besar
   (naikkan `ZANIME_CACHE_LIMIT_MB`) atau kembali ke cache MP4.
2. Subtitle tertanam baru ada setelah video diputar (karena menumpang pass video). Kalau
   diinginkan muncul sebelum play, satu-satunya cara = baca file terpisah (biaya kuota lagi).
3. MKV quick-probe membaca header sampai Tracks; kalau ada rilis yang menaruh Tracks setelah
   lampiran font, parser berhenti di batas 1 MB lalu fallback ffprobe (lambat lagi).

## Cara menjalankan test

```bash
# semua test Go (unit + integrasi + e2e, tanpa browser/Chrome)
go test ./... -timeout 300s

# verbose, lihat detail streaming/kecepatan/subtitle
go test ./internal/torrent/ -v -timeout 300s

# hanya e2e pemutaran video
go test ./internal/torrent/ -run TestEndToEndVideoLoad -v

# hanya e2e remux MKV + subtitle tertanam
go test ./internal/torrent/ -run TestRemuxMkvStreams -v

# tes integrasi (butuh jaringan internet, memanggil API katalog/Torrentio)
INTEGRATION=1 go test ./... -timeout 300s

# ukur kuota per tahap dari satu magnet nyata (metadata → durasi/track → subtitle → video)
MAGNET='magnet:?xt=urn:btih:…&tr=…' go test ./internal/torrent/ -run TestKuotaMagnet -v -timeout 20m
# contoh keluaran (Big Buck Bunny, ~220 peer, file 158 MB tanpa subtitle tertanam):
#   1. metadata              0.00 MB   6.2 dtk
#   2. durasi + track        0.03 MB   0.3 dtk   (mkv quick-probe EBML)
#   3. video                 1.30 MB   10.0 dtk
#   4. subtitle              —          (tidak ada subtitle tertanam; kalau ada, ikut pass video)
#   TOTAL sampai video mulai: 1.30 MB

# frontend
cd web && npx tsc --noEmit && npx rsbuild build

# statis
go vet ./... && golangci-lint run ./...
```

### Sifat test

- **Hermetik / 0 kuota internet** untuk `TestEndToEndVideoLoad` dan `TestRemuxMkvStreams`:
  tracker & seeder lokal, video dibuat ffmpeg lokal, DHT/PEX/tracker publik dimatikan
  (`ZANIME_TORRENT_NO_DHT=1`), data & cache di folder sementara
  (`ZANIME_TORRENT_DATA_DIR`, `ZANIME_CACHE_DIR`) supaya tidak memakai unduhan lama.
- **Tanpa Chrome.** Verifikasi UI dilakukan lewat jalur HTTP yang benar-benar dipakai
  browser (`/api/torrent/*`, `/stream`, `/subs`), bukan otomasi browser.
- Butuh `ffmpeg` + `ffprobe` di `PATH`; kalau tidak ada, test remux/subtitle di-skip.

## Hasil test terakhir

| Pemeriksaan | Hasil |
| --- | --- |
| `go test ./...` | PASS (torrent ±15 dtk) |
| `INTEGRATION=1 go test ./...` | PASS |
| `go vet ./...` | bersih |
| `golangci-lint run ./...` | 0 issues |
| `gofmt -l .` | kosong |
| `tsc --noEmit` | 0 error |

Cakupan yang dibuktikan test:

1. Torrent siap di bawah 20 detik lewat alur HTTP asli (`start` → `status`).
2. Video mulai terlayani **sebelum unduhan selesai** (progresif, bukan unduh penuh dulu).
3. Kecepatan `downBps` / `upBps` terukur > 0 dan konsisten dengan byte yang dikirim.
4. Subtitle eksternal diambil dengan **kuota kecil** (jauh di bawah 1 MB).
5. MKV ditransmux langsung ke client: byte pertama keluar saat ffmpeg masih jalan, hasil
   stream adalah mp4 valid (h264 + aac), dan **tidak ada file cache MP4** di folder kerja.
6. Subtitle **tertanam** (MKV, bahasa `ind`) dikenali probe, diekstrak otomatis di latar
   belakang, lalu tersaji sebagai WebVTT yang valid (header tunggal, tanpa baris kosong
   beruntun, timestamp cue benar).
7. MKV ber-audio **AC3/E-AC3**: remux tidak lagi gagal (`Cannot write moov atom before
   EAC3 packets parsed`); video tetap copy, audio di-encode AAC agar bisa diputar browser.

## Progres

- [x] Streaming progresif + indikator kecepatan (peer → server → client, animasi 2 dtk).
- [x] Transmux MKV → mp4 langsung ke player, tanpa cache video di disk (penyimpanan terbatas).
- [x] Audio copy dulu; encode AAC hanya kalau copy ditolak ffmpeg (AC3/E-AC3/DTS).
- [x] Probe audio/subtitle/durasi (`ffprobe` tanpa `-nostdin` — penyebab lama selalu gagal).
- [x] Daftar subtitle: Indonesia diprioritaskan & otomatis terpilih.
- [x] Ekstraksi subtitle di latar belakang + cache VTT (tidak bergantung request browser).
- [x] Pemilihan audio memetakan index stream global → urutan track.
- [x] Error subtitle ditampilkan di UI (bukan spinner tanpa akhir).
- [x] Player mulai setelah probe selesai (durasi + daftar track, hanya ~5 MB); subtitle
  menyusul di latar belakang dan terpasang otomatis begitu VTT-nya siap.

## Build (frontend ter-embed)

Frontend hasil build di-embed ke binary, jadi deploy cukup satu file:

```bash
cd web && pnpm install && pnpm run build   # output → internal/web/dist (rsbuild)
cd .. && go build -o zanime .              # UI + API dalam satu binary
```

Kalau `internal/web/dist` belum dibangun, server tetap jalan tapi `/` membalas HTTP 503
berisi pesan cara build. Tidak perlu lagi folder `web/dist` saat runtime.

## Perilaku torrent (klien saja)

Proses ini hanya mengunduh: `NoUpload` (tidak pernah mengirim chunk) dan
`AcceptPeerConnections=false` (tidak membuka/menerima koneksi peer masuk), jadi tidak
berperan sebagai server atau seed. Konsekuensinya sebagian peer leecher yang menuntut
timbal-balik bisa lebih pelit, tapi seeder tetap mengirim.

Batas buffer diturunkan dari bawaan library supaya RAM tidak membengkak saat streaming:
30 koneksi/torrent (bawaan 50), buffer peer 256 KiB (bawaan 1 MiB), dan chunk belum
terverifikasi 16 MiB (bawaan 64 MiB).

### Batas penyimpanan (penting untuk panel kecil)

Data torrent disimpan sebagai cache berbatas (LRU) di `./tmp/zanime-data`:

- Batas default **512 MB**, ubah dengan `ZANIME_CACHE_LIMIT_MB` (mis. `ZANIME_CACHE_LIMIT_MB=256`).
- Potongan lama dibuang otomatis, jadi satu episode 2+ GiB **tidak** menumpuk di disk.
- Sisa unduhan versi lama yang melebihi batas dibersihkan sekali saat server start.
- Readahead reader dibatasi (1/8 batas cache, maks 16 MB) supaya pembuangan potongan tidak
  membuat pemutaran macet.

Konsekuensi: cache tidak menyimpan seluruh episode. Karena subtitle diekstrak dengan satu
pass baca penuh sebelum diputar, pass pemutaran berikutnya membaca ulang dari awal dan
sebagian data terpaksa diunduh lagi (kuota). Naikkan `ZANIME_CACHE_LIMIT_MB` kalau disk
mencukupi agar pass kedua tidak perlu unduh ulang.

## Folder sementara

Data torrent, cache subtitle (VTT kecil), dan binary ffmpeg cadangan ditaruh di folder kerja:
`ZANIME_TMP_DIR` (kalau diisi) → `./tmp` → `os.TempDir()`. `./tmp` didahulukan karena di
container (mis. Pterodactyl) `/tmp` bisa read-only, kecil, dan hilang saat restart,
sedangkan folder server tetap. Server mencatat foldernya saat start (`folder kerja: …`).

ffmpeg: kalau `ffmpeg`/`ffprobe` ada di `PATH` (atau di-install lewat image panel) itu yang
dipakai; kalau tidak ada, build statis diunduh sekali ke `./tmp/zanime-bin/`.

## Kekurangan / belum dikerjakan

- **Tanpa cache MP4**, seek/Range pada jalur transmux (mkv/ts/avi) belum tersedia dan
  durasi di player mengikuti stream yang sedang berjalan; memuat ulang halaman memulai
  ffmpeg dari awal. Konsekuensi langsung dari tidak menyimpan file besar di disk.
- **Subtitle tertanam** perlu satu pass baca seluruh berkas video, jadi ekstraksinya
  berjalan di latar belakang sambil video diputar. Di koneksi lambat subtitle muncul
  terlambat (bukan di awal), dan pass itu menambah kuota karena membaca ulang data yang
  mungkin sudah terbuang dari cache berbatas.
- **Subtitle bitmap** (PGS/DVD `xsub`) tidak ditawarkan karena tidak bisa dikonversi ke WebVTT.
- **Subtitle eksternal** hanya dideteksi dari ekstensi file di torrent (`.srt/.ass/.vtt`),
  termasuk yang namanya tidak menyebut bahasa.
- **Frontend belum punya test otomatis** (hanya typecheck + build); verifikasi UI lewat HTTP.
- **Satu ffmpeg per request stream**: dua penonton pada file yang sama menjalankan dua
  ffmpeg (video tetap copy, jadi bebannya ringan), tanpa berbagi buffer.
- Kualitas pemilihan rilis/subtitle masih bergantung metadata rilis (label bahasa rilis
  yang salah tidak bisa dideteksi).
