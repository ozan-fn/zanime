# zanime

Server nonton anime: proxy API + SPA. Satu biner V yang menyajikan seluruh API
sekaligus frontend Svelte — build produksi **meng-embed `web/dist`** ke dalam biner
(modul [embedfs](https://github.com/gechandesu/embedfs)), jadi tidak perlu folder
statis di sampingnya. Mode dev membaca `web/dist` dari disk.

Semua data diambil dari sumber pihak ketiga dan **tidak pernah diekspos langsung** ke
browser — gambar, subtitle, playlist, dan segmen video ditulis ulang jadi URL `/api/*`
(base64), jadi klien tidak bisa menembak sumbernya sendiri.

- Upstream katalog/detail: `https://graphql.animex.one/graphql` (GraphQL)
- Upstream episode/server/sumber: `https://api.anistream.one/rest/api` (provider `yuki`)
- Skip-time: `https://api.aniskip.com`, komentar & metadata episode: `theanimecommunity.com`
- Terjemahan subtitle: Gemini (`gemini-3.5-flash-lite`)

---

## Fitur

| Fitur | Keterangan |
| --- | --- |
| Katalog & pencarian | Filter status/format/season/tahun/genre + sort apa pun dari skema upstream, limit ≤ 30 |
| Detail anime | Base/seasons/relations/recommendations/characters |
| Episode & server | Daftar episode (dengan gambar), daftar server per episode |
| Stream | Resolve sumber termurah-yang-hidup: probe 64 KB ke CDN → URL proxy `/api/hls` |
| Proxy HLS penuh | Master/varian playlist ditulis ulang; segmen `.ts`/`.mp4` diambil lewat `/api/fetch/{nama}.{ext}` |
| Sniff MIME | Tipe media ditentukan dari magic bytes (CDN sering salah label: segmen TS dinamai `.jpg`) |
| Anti-SSRF | Host privat/loopback/metadata diblokir di `decode_target` |
| Subtitle Indonesia | Terjemahan progresif per 10 cue saat video diputar, cache di disk |
| Cache Redis | Respons upstream yang aman di-cache (opsional, aktif bila `REDIS_URL` diisi) |
| SPA | Aset + gzip precompress + ETag/304 + `Cache-Control` per jenis file dari `web/dist` |
| Judul EN + romaji + JP | Halaman detail, halaman putar, dan tiap baris episode menampilkan judul English, romaji, dan Jepang (kanji) |
| Monitor resource | Navbar menampilkan mem, puncak mem, cpu, dan puncak cpu proses ini (poll `/api/stats` tiap 3 dtk) |

---

## Struktur proyek

```
main.v            bootstrap: load config, init cache, log resource 2 dtk, HTTP :3000
                  + dist_map() — isi web/dist ter-embed (dipakai `-d embed_dist`)
gen_embed.vsh     script generator: web/dist → web_dist.v lewat embedfs (dipanggil `make web`)
internal/
  app/            App.handle — routing /api/* + fallback SPA, validasi query, bentuk respons
                  + penyajian SPA: embed atau disk, gzip precompress, ETag/304
  config/         Config::load() + pembacaan .env (tanpa lib dotenv)
  errors/         AppErr → JSON (400/404/429/502)
  models/         bentuk keluaran web (AnimeOut, EpisodeItem, StreamResponse, SubStatus)
  upstream/       semua panggilan keluar (upstream.v), proxy (proxy.v), VTT (vtt.v),
                  pipeline subtitle + Gemini (subs.v)
  cache/          pembungkus Redis (best-effort, opsional)
  stats/          snapshot mem/cpu proses dari /proc (Linux)
web/              SPA Svelte 5 + Vite + Tailwind 4 (+ hls.js via @videojs/hlsjs-video)
```

Alur request: `app` (validasi + bentuk keluaran) → `upstream` (fetch, cache) →
`json2` → JSON. Playlist HLS melewati `upstream.rewrite_playlist` agar semua URI
menunjuk balik ke `/api/*`.

---

## Menjalankan

Butuh V 0.5.2+ dan, untuk frontend, `pnpm`. State global dideklarasikan dengan
atribut `@[has_globals]`, jadi tidak ada flag `-enable-globals`.

```bash
# 1) sekali saja: modul generator embed
make deps        # v install gechandesu.embedfs

# 2) .env (lihat tabel di bawah)
printf 'GEMINI_API_KEY=...\nREDIS_URL=rediss://...\n' >> .env

# 3) build biner tunggal (web/dist ter-embed) + jalankan di :3000
make build && ./zanime

# atau langsung
make server      # pnpm build + v run . (aset dari disk)
make test        # v test internal/

# mode dev frontend (Vite :5173, proxy /api → 127.0.0.1:3000)
make client
```

| Target | Efek |
| --- | --- |
| `make deps` | `v install gechandesu.embedfs` (generator embed) |
| `make web` | `pnpm --dir web build` → `web/dist`, lalu `v run gen_embed.vsh` → `web_dist.v` |
| `make embed` | `make web` + `v -d embed_dist -o zanime .` (biner tunggal, dist di dalam) |
| `make build` | alias `make embed` |
| `make server` | `make web` lalu `v run .` — aset dibaca dari `web/dist` di disk |
| `make client` | `pnpm --dir web install` + `pnpm --dir web dev` (:5173) |
| `make test` | `v test internal/` |

- **Produksi** (`make build`): `web_dist.v` (dihasilkan `gen_embed.vsh`) memuat
  `$embed_file` untuk semua berkas `web/dist` (termasuk `.gz`); `-d embed_dist`
  membuat `main.dist_map()` mengisinya. Biner bisa dijalankan dari mana saja.
- **Dev** (`make server`): tanpa `-d embed_dist` → peta kosong → aset dibaca dari
  `web/dist` relatif ke direktori kerja (jalankan dari root proyek).
- `web_dist.v` digenerate dan masuk `.gitignore`; tanpa berkas itu build **tanpa**
  `-d embed_dist` tetap jalan (referensinya ada di dalam `$if embed_dist ?`).

---

## Konfigurasi

Dibaca dari `.env` (tanpa dependensi) atau environment. `.env`, `.cache/`, `zanime`,
dan `web/dist` sudah masuk `.gitignore`. Key yang sudah ada di environment menang.

| Variabel | Wajib | Default | Keterangan |
| --- | --- | --- | --- |
| `GEMINI_API_KEY` | hanya untuk subtitle ID | kosong | Bisa banyak key (pisah koma/baris) → round-robin. Kosong → `POST /api/subid/batch` balas 400 |
| `GEMINI_MODEL` | tidak | `gemini-3.5-flash-lite` | Model Gemini `generateContent` |
| `REDIS_URL` | tidak | kosong | `rediss://default:<pass>@<host>:6379`. Kosong/gagal → cache mati, API tetap jalan |
| `PORT` | tidak | `3000` | Port bind (host `0.0.0.0`) |

Nilai lain (UA, `Origin`/`Referer` upstream, base URL GraphQL/REST, provider `yuki`,
TTL cache) di-hardcode di `internal/config` dan `internal/upstream`.

---

## API

Semua endpoint JSON ada di bawah `/api`; sisanya dilayani SPA (fallback `index.html`).
Parameter `u` pada endpoint proxy adalah base64url (tanpa padding) dari URL asli.

| Method | Path | Parameter | Cache |
| --- | --- | --- | --- |
| GET | `/api/search` | `query` (wajib) | 1 hari |
| GET | `/api/catalog` | `sort`, `direction=ASC\|DESC`, `limit` (1–30, def 18), `offset`, `status`, `format`, `season`, `year`, `genre`, `query` | 1 hari |
| GET | `/api/anime` | `id` **atau** `anilistId`; `section=base\|seasons\|relations\|recommendations\|characters` | 1 hari |
| GET | `/api/episodes` | `id` (wajib) | 1 hari |
| GET | `/api/servers` | `id`, `ep` | 1 hari |
| GET | `/api/sources` | `id`, `ep`, `type=sub`, `provider=yuki` | – |
| GET | `/api/stream` | `id`, `ep` | – |
| GET | `/api/skiptimes` | `mal`, `ep`, `len` | 1 hari |
| GET | `/api/comments` | `media`, `offset`, `sort=Top`, `limit=10` | 1 hari |
| GET | `/api/episode-meta` | `anilistId`, `ep` | 1 hari |
| GET | `/api/stats` | – | – |
| POST | `/api/anilist` | body `{query, variables}` | – |
| GET | `/api/subid/status` | `id`, `at` | – |
| POST | `/api/subid/batch` | body `{id, at}` | – |
| GET | `/api/subid/result` | `id`, `only=id` | – |
| GET | `/api/fetch`, `/api/fetch/{nama}.{ext}` | `u` (wajib), `origin`, `referer`; meneruskan `Range` | – |
| GET | `/api/hls` | `u` (wajib), `origin`, `referer` | – |
| GET, HEAD | `/{filename:.*}` | – | SPA |

Catatan bentuk:
- `/api/anime?section=base` memuat `titleRomaji`, `titleEnglish`, dan `titles`
  (JSON scalar berisi judul semua bahasa, termasuk `ja`). `titles` **bukan** objek
  GraphQL — minta apa adanya (`titles`), `titles{ja}` bikin seluruh query gagal.
- `/api/episodes` mengembalikan `{number, title (English), titleRomaji (x-jat), titleJp (kanji), img}`.
- Halaman detail dan halaman putar menampilkan judul English (utama), lalu romaji, lalu kanji
  (`titles.ja`) — yang kosong/duplikat dilewati (`altTitles()` di `features/episodes/types.ts`).
- `/api/stats` = resource proses ini: `{memMb, peakMemMb, cpu, peakCpu}` (dipakai navbar).
- Item katalog (`/api/search`, `/api/catalog`) memuat `id`, `anilistId`, `titleRomaji`,
  `titleEnglish`, `episodeCount`, `cover`, `banner`, `format`, `seasonYear`,
  `averageScore`, dan `status`.
- `cover`/`banner`/`img`/`sub_en` selalu URL proxy `/api/fetch?u=…` (bukan URL CDN).
- `/api/stream` → `{stream: "/api/hls?u=…", sub_en: "/api/fetch?u=…", sub_id: "<job>"}`.
- `/api/subid/result` selalu `text/vtt`; job belum siap → `WEBVTT` kosong (bukan error),
  supaya entri menu subtitle di player tetap ada.
- Ekstensi di `/api/fetch/{nama}.{ext}` penting: player web memilih jalur demux dari
  ekstensi URL (`.ts` → `video/mp2t`).
- `sort`/`status`/`format`/`season` divalidasi terhadap whitelist → 400 jelas, bukan
  diteruskan buta ke upstream.
- `limit` di-clamp ke 1–30 (`UPSTREAM_MAX_PAGE`): `catalogAnime` membalas `items: null`
  di atas 30, jadi permintaan lebih besar dipotong, bukan diteruskan lalu 502.
- Halaman **Lihat semua** (`/browse/:key?page=N`) memakai `offset`, 30 judul per
  halaman, dengan pagination bernomor (prev/next + elipsis) di halaman itu sendiri.
- Upstream tak punya `total`/`pageInfo`, jadi jumlah halaman bersifat heuristik:
  halaman penuh (30 item) berarti ada halaman berikutnya. Tombol nomor tumbuh saat
  halaman baru terbukti ada.

---

## Cache Redis

`internal/cache` adalah satu-satunya lapisan cache HTTP. Pola pakainya:

```v
cached_json(key, ttl, fn () !string { /* fetch upstream */ })!
```

- **Miss / error / redis mati** → jalankan `f`, lalu simpan hasil (`SET key value EX ttl`).
- Semua operasi bersifat best-effort: redis mati, URL salah, atau value bukan JSON valid
  **tidak pernah** menghasilkan 5xx — request jatuh ke jalur upstream biasa.
- Nilai disimpan sebagai JSON; key memuat seluruh argumen yang menentukan
  hasil (`catalog:<sort>:<direction>:<limit>:<offset>:<filter>`).
- Koneksi Redis dibuka sekali saat startup; pesan startup: `cache: redis siap` atau
  `cache: REDIS_URL kosong → tanpa cache` / `cache: redis gagal (…) → tanpa cache`.
- Mengubah **bentuk** respons sebuah endpoint (mis. menambah field) membuat entri lama
  tetap tersaji sampai TTL sehari habis. Buang key terkait saat deploy, mis.
  `redis-cli --scan --pattern 'anime:*' | xargs redis-cli del`.
- Nilai `null` **tidak** disimpan: dipakai endpoint untuk "tak ada data", dan query
  upstream yang rusak juga jatuh ke sana — kalau ikut di-cache, hasilnya lengket
  sepanjang TTL.

TTL: satu nilai, `86400` (sehari). Konten anime berubah (episode baru tayang, peringkat,
komentar), jadi tiap entri kedaluwarsa sendiri dalam sehari dan diisi ulang dari upstream.

**Sengaja tidak di-cache:**

| Endpoint | Alasan |
| --- | --- |
| `/api/sources`, `/api/stream` | URL m3u8 dari CDN bertanda tangan + berkedaluwarsa (`?token=<exp>.<hmac>`); entri basi = playback mati |
| `/api/hls`, `/api/fetch` | Playlist/segmen/gambar: besar, bervariasi per `Range` |
| `/api/anilist` | Query GraphQL bebas dari klien → jumlah key tak terbatas (cache poisoning/DOS) |
| `/api/subid/*` | Stateful (job in-memory + cache disk `.cache/subtitles/<hash>.vtt`) |

---

## Subtitle Indonesia

Worker hanya mengunduh VTT EN lalu mem-parse (tanpa LLM). Terjemahan diambil
**on-demand** tiap 10 cue saat video berjalan, lewat `POST /api/subid/batch`:

1. `/api/stream` mendaftarkan job (dedup per URL) → `sub_id`.
2. Worker isi state `queued → working → ready → done` (+`failed`).
3. Player memanggil `batch` saat `next_at` terlewati; hasil per batch dikembalikan
   sebagai delta VTT di respons `status`, dan job yang selesai penuh ditulis ke disk.
4. `GET /api/subid/result` merakit VTT: cue yang belum diterjemahkan jatuh ke teks EN.

Detail yang perlu diketahui saat mengubah bagian ini:
- Throttle 1,1 dtk/call; banyak key berputar round-robin dan key yang kena limit
  dilompati. Semua key cooldown → balas 429 (gagal-cepat), frontend yang retry.
- Antrean dibatasi 100 job; yang tertua dibuang beserta state-nya.
- `next_at` = timestamp cue ke-5 dari belakang yang sudah diterjemahkan minus 2 dtk; `-1` = selesai.

---

## Frontend (`web/`)

Svelte 5 + Vite + Tailwind 4, tanpa env: same-origin, dan di mode dev `/api` di-proxy
Vite ke `127.0.0.1:3000`.

- Halaman: `/` (home + 6 section katalog), `/browse/:key?page=N` (satu section penuh
  satu halaman sendiri: 30 judul + pagination bernomor, nomor halaman ada di URL
  sehingga refresh/tombol back tetap di halaman yang sama), `/anime/:id`
  (detail + episode + terkait), `/watch/:id/:ep` (player), `*` (404).
- `AnimeCard` menampilkan tahun rilis (tebal), format, jumlah episode, skor, dan chip
  status (`Tayang`/`Tamat`/`Akan datang`/`Hiatus`/`Dibatalkan`). Kartu **Terkait**
  pakai pola yang sama (tahun + tipe + eps + chip status) supaya urutan seri terlihat;
  entri tanpa halaman anime tetap tampil sebagai kartu biasa, tanpa link.
- Definisi section (judul + parameter katalog) tinggal di `features/home/hooks.ts`
  (`SECTIONS`), dipakai bareng oleh baris home dan halaman lihat-semua.
- Router `svelte-spa-router`; setiap fitur (`search`, `episodes`, `home`, `watch`)
  berisi `api / hooks / stores / types / components / index`.
- Player: `<hlsjs-video>` dari `@videojs/html` + adapter `@videojs/hlsjs-video`
  (hls.js) — wajib, karena MSE browser tidak menerima MPEG-TS dan bundel video.js
  tidak punya transmuxer TS.
- Cek: `pnpm --dir web check` (svelte-check + tsc), build produksi: `pnpm --dir web build`.

---

## Catatan & batasan

- Server bind ke `0.0.0.0:3000` tanpa autentikasi — jangan diekspos ke internet
  tanpa reverse proxy + autentikasi.
- `/api/anilist` meneruskan GraphQL apa pun ke `graphql.anilist.co` (butuh untuk
  endpoint yang belum dipetakan), jadi jangan dipublikasikan apa adanya.
- Log `res proc_mem=… peak_mem=… proc_cpu=… peak_cpu=…` dicetak tiap 2 dtk; `stats.sample()`
  membaca `/proc/self` (Linux).
- Kegagalan upstream yang tidak bisa diparse selalu jadi 502 dengan pesan asli dari
  upstream (mis. Cloudflare memblokir playlist → "upstream bukan playlist").
- `/api/fetch` menahan body upstream di memori sebelum dikirim ke klien (belum streaming
  per-chunk) — cukup untuk gambar/segmen kecil; streaming
  per-chunk belum diimplementasikan.
- Embed memakai modul pihak ketiga `gechandesu.embedfs` (`make deps`); file `web/dist`
  harus ada saat build ter-embed, kalau tidak `web_dist.v` berisi peta kosong.
- SPA menyajikan `.gz` hasil precompress vite apa adanya (level 9) bila klien menerima
  gzip; tanpa `.gz` berkas dikirim tanpa compression. `Vary: Accept-Encoding` selalu ada
  bila `.gz` tersedia.
- Playback + subtitle di browser sudah dikonfirmasi manual (video lancar, subtitle EN &
  terjemahan Indonesia muncul). Verifikasi otomatis tetap berhenti di level HTTP (status,
  tipe MIME, byte awal segmen, durasi playlist) karena tidak ada browser di CI.
