# zanime

Torrent video streaming server: HTTP + Range/seek, cache piece di RAM saja,
zero disk, dan UI web untuk menempel magnet serta memilih video.

Player memakai [MoviPlayer](https://moviplayer.com/) dari CDN (`<movi-player>`),
jadi MKV/HEVC/AV1 dan subtitle di dalam file diputar di browser lewat WebCodecs
+ FFmpeg WASM. Server hanya mengirim byte lewat HTTP Range; tidak ada transcoding
di server. Kalau MKV tidak punya index `Cues` (pemutar biasanya harus memindai
seluruh file dulu), server bisa **remux cepat** (`-c copy`, tanpa transkode) ke
RAM sehingga player dapat versi berindex — lihat [Remux](#remux).

## Stack

- **Rust** + **axum** (HTTP, WebSocket) + **tokio**.
- **librqbit** untuk BitTorrent. Salinan di `vendor/librqbit` dipatch dengan
  `forget_piece`: piece yang dibuang dari RAM ditandai "belum punya" sehingga
  bisa diunduh ulang saat player seek kembali.
- Storage piece: `src/memstore.rs` (LRU, batas `MAX_MEMORY_MB`).
- Remux opsional: ffmpeg sistem / statis di `./tmp/ffmpeg` (`src/remux.rs`).

## Menjalankan

```bash
cp .env.example .env      # PORT, MAX_MEMORY_MB
cargo run --release
# atau
cargo run --release -- --addr 0.0.0.0:8080 --cache-mb 200 --magnet "magnet:?xt=..."
```

Buka `http://localhost:8080`, tempel magnet, lalu klik salah satu file video.
`/api/ws` mendorong status tiap 0,7 detik; UI tidak memakai polling.

## Konfigurasi

| Sumber | Kunci | Arti |
|---|---|---|
| `.env` / env proses | `PORT` | port HTTP (default 8080) |
| `.env` / env proses | `MAX_MEMORY_MB` | batas RAM total: cache piece + buffer remux (default 200 MiB) |
| argumen | `--addr`, `--cache-mb`, `--magnet` | menimpa nilai di atas |

Magnet awal bisa dikosongkan (`--magnet ""`) supaya server mulai tanpa torrent.

## Endpoint

| Endpoint | Fungsi |
|---|---|
| `GET /` | UI (index.html di-embed ke binary) |
| `GET /healthz` | cek hidup, balas `ok` |
| `GET /info` | ringkasan siap/nama/ukuran/cache |
| `GET /api/ws` | WebSocket: dorongan status tiap 0,7 detik (sumber tunggal angka di UI) |
| `POST /api/magnet` | `{"magnet": "...", "fileIndex": N}`; `fileIndex` opsional (urutan file di torrent) |
| `POST /api/cancel` | batalkan pemuatan magnet yang sedang jalan |
| `POST /api/select` | `{"index": N}` pada daftar video yang sudah diurutkan |
| `GET/HEAD /stream` | file terpilih; dukung `Range` (206) dan 416 |
| `GET/HEAD /stream/{index}/{name}` | file tertentu; `name` dipakai untuk MIME |
| `GET/HEAD /remux/{index}/{name}` | keluaran remux (RAM); `Range` didukung; 503 saat masih diproses |

## Remux

Setiap episode terpilih diremux **ffmpeg** untuk menjamin file punya index
(`Cues`) — tanpa itu player bisa dipaksa memindai seluruh file sebelum mutar:

- Sumber ffmpeg: `./tmp/ffmpeg` → ffmpeg dari `PATH` → build statis yang
  diunduh otomatis ke `./tmp/ffmpeg` (~35 MB, sekali saja).
- Perintah: `ffmpeg -i http://127.0.0.1:<port>/stream/… -map 0 -c copy -f matroska pipe:1`
  — semua track (audio/subtitle/font) disalin bit-per-bit, input dibaca lewat
  endpoint stream sendiri secara streaming, keluaran dipipe langsung ke RAM:
  **tetap zero disk**.
- Setiap byte keluaran memotong budget `MAX_MEMORY_MB` yang sama dengan cache
  piece (cache otomatis meng-LRU menyusut), jadi total RAM tidak pernah
  melewati batas env.
- UI otomatis memakai `/remux/...` hanya kalau player belum sempat memutar;
  playback yang sudah jalan tidak pernah disela.
- Ganti episode: job remux lama dihentikan, budgetnya dipulihkan. Job untuk
  file yang sama tidak dijalankan ulang. Remux gagal (ffmpeg error/batas
  memori) → player tetap dilayani `/stream` seperti biasa.

## Cara kerja singkat

- Satu torrent aktif. Magnet baru tidak mengunduh seluruh file: librqbit diberi
  `only_files = []`, jadi piece hanya diambil saat stream memintanya.
- Pemuatan magnet adalah satu job milik server (bukan request), bisa dibatalkan
  lewat `/api/cancel`, dibatasi 45 detik menunggu metadata, dan job dengan magnet
  yang sama dipakai bersama.
- Setiap torrent aktif memegang satu stream "penjaga" di EOF supaya peer tidak
  dilepas saat player belum membaca apa pun.
- Cache RAM: piece di atas batas dibuang (LRU), indeksnya dikirim ke evictor,
  lalu `forget_piece` menandainya belum punya agar bisa diunduh ulang.
- File video disaring berdasarkan ekstensi, diurutkan menurut path huruf kecil;
  indeks di API/stream mengikuti urutan itu, sedangkan `fileIndex` mengikuti
  urutan file di dalam torrent.

## Test

```bash
cargo test        # unit: range, urutan file, URL, config, memstore, router
cargo fmt
cargo clippy --all-targets
```

Test router memakai `tower::ServiceExt::oneshot`, jadi route diuji tanpa membuka
port dan tanpa torrent. Test streaming end-to-end tidak dijalankan otomatis
karena butuh swarm nyata.

## Layout

```
src/main.rs        entry point (tipis)
src/lib.rs         config, app state, job magnet, route, stream Range, remux
src/remux.rs       deteksi/unduh ffmpeg, kebutuhan remux
src/memstore.rs    storage piece RAM + LRU eviction (budget dibagi dengan remux)
vendor/librqbit    librqbit 8.1.1 yang dipatch (forget_piece)
web/index.html     UI yang di-embed
```
