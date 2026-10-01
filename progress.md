# zanime — progress

HTTP API + SPA web player di atas hianime.at. Backend Go 1.25 + chi; frontend
React 19 + React Router 8, dibundel rsbuild dengan Tailwind 4 (plugin resmi,
bukan CDN browser) dan **React Compiler** aktif (`reactCompiler: true`). Subtitle diterjemahkan ke Indonesia lewat RPC web Google Translate
tanpa API key (job latar + progress, fallback gtx via proxy), video diputar lewat proxy HLS
dengan shaka-player (paket npm, dibundel — bukan skrip CDN). Tanpa ffmpeg sama
sekali: remux `-c copy` sudah dibuang.

Alur scrape + resolve **mengikuti `index.js` baris per baris** (lihat bagian
"Alur resolve"): search → daftar episode → servers → embed → config player →
master playlist, dengan urutan coba **sub → dub** dan **Vidstream-2 (megaplay) →
ZokoAnime**, plus fallback ke server lain kalau track subtitle-nya cuma *signs*.

## Struktur

```
main.go                   bootstrap tipis: godotenv, flag, embed web/dist, ListenAndServe
internal/hianime/         konstanta upstream, klien scraping, batas ukuran/waktu, scraping katalog
internal/stream/          resolve embed (zoko + megaplay) + proxy HLS
internal/subtitle/        terjemahan VTT (batch, split, dedup), job latar, cache, limiter
internal/server/          routing chi: /api/* + SPA dari embed
web/                      SPA React 19 (rsbuild + React Compiler)
```

Monolith `main.go` (2064 baris) dan `main_test.go` (592 baris) yang lama
**dihapus**: keduanya duplikat dari paket `internal/`, dan `main.go` sekarang
hanya bootstrap. Test yang unik di `main_test.go` dipindah, bukan dibuang:
`mostlyNonLatin` dan penolakan cache beraksara salah ke `internal/subtitle`,
`Invalidate` ke `internal/stream`.

## Status

| Bagian | Status |
|---|---|
| `go mod init zanime` + `go get chi/v5` | done |
| Scraping katalog (search / detail / episodes) | done |
| Resolve embed ala `index.js`: ZokoAnime + MegaPlay, fallback Vidstream → Zoko | done |
| Sub default (dub fallback) + pilih track dialog (bahasa lalu cue) + fallback server bila <10 cue | done |
| Prompt anti-aksara non-Latin + validasi berkas sebelum job `ready` | done |
| Parse master playlist + pilih kualitas | done |
| Terjemahan subtitle ID via RPC web Google Translate (tanpa API key) + fallback gtx via proxy | done |
| Batch ≤4500 char/RPC, baris bernomor `N\|`, mapping by prefix | done |
| Cache subtitle ke disk + job latar ber-progress | done |
| Proxy HLS hemat memori (buffer pool, 1 request/segmen) | done |
| SPA React 19 + React Router 8 (rsbuild, React Compiler) | done |
| `go vet` + `go test ./...` | pass |
| `npx tsc --noEmit` (web) | pass |

## Endpoint — semua API di bawah `/api/`

Tidak ada parameter audio: stream **sub** yang dipakai (audio Jepang + subtitle),
dan **dub** hanya fallback kalau sub tidak ada — lihat "Pilihan audio" di bawah.
Karena audio bukan pilihan pengguna, tidak ada tombol Subtitle/Dub di UI.

| Method | Path | Fungsi |
|---|---|---|
| GET | `/` | SPA (index.html + aset hashed, embed) |
| GET | `/api/healthz` | status + sisa kuota |
| GET | `/api/limits` | pacing request translate gratis |
| GET | `/api/search?q=` | cari judul (poster + sinopsis per kartu, poster di-proxy) |
| GET | `/api/img?u=` | proxy gambar CDN (base64url, allow-list host) |
| GET | `/api/anime/{id}` | detail anime: meta, genre, studio, related, recommended |
| GET | `/api/anime/{id}/episodes` | daftar episode |
| GET | `/api/episode/{id}` | master URL, referer, kualitas, subtitle |
| GET | `/api/subtitle/{id}?lang=id` | `.vtt` kalau siap, `202` + progress kalau masih dikonversi |
| GET | `/api/subtitle/{id}/status` | `{state,done,total,eta_seconds}` |
| GET | `/api/hls/{id}/master.m3u8` | master upstream, varian ditulis ulang ke `/api/hls/{id}/{label}/index.m3u8` |
| GET | `/api/hls/{id}/{quality}/index.m3u8` | playlist media, semua URI segmen ditulis ulang ke `/s/{base64url}` |
| GET | `/api/hls/{id}/{quality}/s/{seg}` | segmen absolut (base64url), content-type di-sniff dari byte |

## Alur resolve (sama dengan `index.js`)

`internal/stream/resolve.go` mengikuti urutan hop di `index.js`:

1. **servers** — `Servers(page)` memakai regex `index.js`
   (`data-type="…"\s*data-server-name="…"[\s\S]*?data-hash="…"`), hash-nya
   base64 → URL embed, duplikat dibuang; tipe audio tidak disaring di sini,
   `ordered` yang mengurutkan. Envelope JSON-nya di-`json.Unmarshal` (bukan
   di-scrub manual: scrub `\"` → `"` menyisakan `\n` **literal** di antara baris,
   yang tidak pernah cocok dengan `\s*`, sehingga daftar server terbaca kosong).
2. **embed** — `hianime.GetTiny(embed, hianime.at)`, dibaca sebagai salah satu
   dari dua player:
   - **ZokoAnime**: `window.__P` = `base64(json XOR "otaku-embed-v1")`;
     JSON-nya `{src, subtitles[]}` → master + track subtitle, satu request.
   - **MegaPlay**: `data-id` → `GET {origin}/stream/getSources?id=` dengan
     `X-Requested-With: XMLHttpRequest` + referer embed. `enc` didekripsi
     AES-256-CBC (`DecryptEnc`, key `i?LMTAx0Q6,:}50U` zero-pad 32 byte, IV
     `W0;27ToaUpl_P%'c`, PKCS#7 dibuang manual) → `{"file": …/master.m3u8}`.
     Kalau `enc` tidak ada, jatuh ke `sources` (string atau `[{file}]`) lalu
     `file` — sama seperti `index.js`.
3. **master** — `Qualities` mengambil master dan `ParseMaster` menyortir varian
   per tinggi. Kalau URL itu ternyata playlist media (ada `#EXTINF`, tanpa
   varian), ia dipakai sebagai satu kualitas `auto` — `index.js` juga
   memperlakukannya sebagai playlist yang bisa diputar. Kalau benar-benar tidak
   ada yang bisa diputar, resolve **gagal** supaya jatuh ke server berikutnya.
4. **fallback otomatis** — `ordered()` mengurutkan embed: sub+megaplay, sub+zoko,
   dub+megaplay, dub+zoko (urutan upstream dipertahankan di dalam grup), lalu
   `Resolve` mencoba satu per satu. Embed mati / getSources gagal / master mati =
   server berikutnya, bukan episode yang gagal. `index.js` menyerahkan pilihan
   ini ke manusia; di sini urutannya sama, cuma tanpa prompt.
5. **track subtitle** — `pickDialogue` menandai default dalam dua langkah:
   bahasa dulu (`hianime.LangRank`: Indonesian → English → lainnya), lalu cue
   terbanyak di antara track berbahasa itu. Semua track tetap ditawarkan ke
   player, jadi menu subtitle tidak kosong.

Dibuang karena tidak dipakai `index.js`: filter `megaplay.buzz` di daftar
server (host tidak difilter, urutan yang menentukan), `NormMode`/parameter
mode, pembacaan `data-realid`/`data-mediaid` di halaman embed (hanya `data-id`
yang dipakai), dan aturan `kind: caption|sub` milik megaplay — diganti jumlah cue,
lihat "Mengapa bukan track pertama".

Bukti probe langsung ke upstream (Sep 2026, episode 37108): servers →
`ZokoAnime [dub] https://zokoanime.video/stream/mal/59970/1/dub` +
`Vidstream-2 [dub] https://megaplay.buzz/stream/s-2/169702/dub`; resolve →
master `https://hls.dramahot.top/…/master.m3u8` (referer
`https://zokoanime.video/`, sama seperti HAR), varian 1080p/720p/360p;
`/api/hls/37108/360p/index.m3u8` 200 (50 KB, semua URI ditulis ulang), segmen
200 `Content-Type: video/mp2t` 277 KB (0x47 di byte pertama) → satu request
upstream per segmen.

### Pilihan audio: sub, dub sebagai fallback

Keputusan awal "dub saja" (ikut `chisle: dub aja` di `index.js`) **salah untuk
app ini**, dan `har.har` yang membuktikannya: embed yang benar-benar dibuka sesi
itu `zokoanime.video/stream/mal/58514/1/sub` — sub, dan `58514` justru anime yang
sedang ditonton. Yang diload cuma satu file subtitle, 21 176 B, referer
`zokoanime.video/`: track dialog, bukan yang 510 B.

Diukur pada episode 6647 (Sep 2026):

| Stream | Track | Cue |
|---|---|---|
| sub Zoko | 10 track, terpadat `en` | **363** |
| sub MegaPlay | 10 track (ada `Indonesian`, `Japanese`, `Chinese`…) | — |
| dub Zoko / MegaPlay | satu track *signs* | **7** |

Stream dub cuma membawa title card dan nama tempat; dialognya ada di stream sub.
Karena itu `ordered()` menaruh sub di depan, dan dub tetap ada sebagai fallback
(ada dub berlisensi yang tidak punya stream sub).

### Mengapa bukan track pertama

`index.js` membaca `subtitles[0]` / track `kind=caption` pertama — cukup untuk
mencetak URL ke manusia, tapi bukan track dialog. Di zoko, `subtitles[0]`
labelnya "English" dengan `default: true` dan isinya 8 cue *signs*, sementara
track kedua berisi 553 cue dialog.

Bahasa dan kepadatan dua hal berbeda, dan keduanya pernah salah:

- **Jumlah cue saja tidak cukup.** Di MegaPlay, daftarnya berisi Simplified,
  Traditional, English, Indonesian, Japanese, Korean…, semuanya track dialog
  yang padat. Memilih yang terpadat berarti memilih track Cina paling awal, dan
  berkas `（本作所有人物、组织名均为虚构）` yang keluar di `player` adalah
  hasilnya: sumber Han → jawaban Han.
- **Label saja juga tidak cukup.** Track signs yang 8 cue juga berlabel
  "English".

`pickDialogue` karena itu menyaring dengan `hianime.LangRank` dulu (cuma track
berbahasa terbaik yang di-probe), lalu mengambil **head 64 KB tiap kandidat**
(maks 3), menghitung `-->`, dan menjadikan yang terbanyak sebagai default;
jumlahnya disimpan di `hianime.Source.DialogueCues` (tidak ikut di JSON). `Resolve` memakai
angka itu: server yang track terbaiknya **di bawah `hianime.DenseCues` (10)**
tidak langsung diterima — ia disimpan sebagai cadangan dan server berikutnya
dicoba dulu, jadi episode yang dialog-nya cuma ada di provider lain tetap dapat
subtitle utuh tanpa kehilangan video. Track yang gagal di-fetch dihitung 0, dan
kalau tidak ada yang bisa dibandingkan, track pertama tetap dapat default
(perilaku `index.js`).

`hianime.MatchesLang` menyamakan `id` dengan label yang dipakai provider
(`id`, `ind`, `Indonesian`, `Bahasa Indonesia`): megaplay kadang sudah
menyediakan track Indonesia, dan track itu tidak perlu diterjemahkan — dulu
perbandingannya `HasPrefix(label, "id")`, yang gagal untuk label bernama
"Indonesian" sehingga file Indonesia yang sudah bagus malah diterjemahkan mesin.
Tabel alias yang sama dipakai `LangRank` saat memilih track default.

Bukti setelah perbaikan (episode 6647): master resolve dari zoko **sub**
(`…/1pebmnrx09/5oncdnrmc7gyow/master.m3u8`), default = track `91fum5o8jyevwkno.vtt`,
source 20 566 B / **363 cue** → `ParseVTT` 363 blok → `RebuildVTT` 363 cue
(tidak ada yang hilang).

Menambah pemilih audio (kalau nanti perlu dub manual): kirim tipe yang diminta ke
`Resolve`/`ordered` dan tambahkan tombolnya di `Watch.tsx`.

## Video (proxy hemat memori)

Browser tidak bisa mengirim `Referer` yang diminta host stream, jadi tiga rute
mem-proxy playlist + segmen.

- Segmen **di-stream** dengan buffer dari `sync.Pool` (`copyUpstream`, 32 KB):
  `io.Copy` biasa mengalokasikan buffer baru per request, dan player yang
  mengambil selusin segmen paralel berarti selusin buffer. Pool ini yang menjaga
  memori proxy tetap datar.
- **Satu request upstream per segmen**: 512 byte pertama dibaca untuk men-sniff
  content-type (`0x47` → TS, `ftyp` → MP4), lalu byte itu ditulis dan sisanya
  dialirkan. Dulu ada request probe terpisah, jadi beban upstream tiap segmen
  dua kali lipat.
- Playlist dibaca dengan `io.LimitReader` 64 KB — host yang menjawab dengan
  body raksasa tidak bisa menahan memori.
- Transport sendiri: `MaxIdleConnsPerHost: 16`. Default Go (2) membuat unduhan
  segmen paralel saling mengantre.
- Segmen `max-age=3600` (immutable), playlist `no-cache` (token upstream
  berumur pendek), `Invalidate` + retry sekali di `servePlaylist` saat upstream
  menolak URL lama (error 1001 di player).

Batas scrape **diukur, bukan ditebak**: halaman search/detail ~0,3 MB, daftar
episode One Piece 1,1 MB, servers 2 KB, embed 4,5 KB, getSources ~0,5 KB,
master ~2 KB. Karena itu `hianime.Get` 4 MB + 10 s dan `hianime.GetTiny` 64 KB +
5 s; hop resolve gagal cepat lalu pindah server, halaman besar tetap muat.

Aplikasi tanpa ffmpeg: remux `-c copy` tidak ada, jadi tidak ada `os/exec`.

## Subtitle Indonesia

1. Ambil `.vtt` asli (hampir selalu Inggris) pakai referer yang sama.
2. Kalau sumbernya belum `id`, parse blok cue WebVTT.
3. Pack cue sampai ~4500 char per panggilan RPC `MkEWBc` (batchexecute web,
   bentuk disalin dari traffic browser): baris bernomor `N|teks`, jawaban
   dipetakan balik by prefix (terbukti 60/60 utuh). Multiline/`N|` gagal di
   `AVdN8`, satu teks per call di `gtx`, multiplex RPC di-drop server — jadi
   satu batch = satu RPC multiline.
4. Deduplikasi: cue dengan teks identik dikirim sekali, hasilnya disalin ke
   semua kemunculannya. Payload mengecil dan konsistensi jadi gratis.
5. Cue yang tidak dijawab (atau dijawab dalam aksara non-Latin) diulang sekali
   (retry pass); mismatch jumlah baris → batch dipecah (retryable), bukan
   ditebak — cue tak pernah nyasar.

Track yang dipakai adalah track dialog hasil `pickDialogue` (lihat "Mengapa bukan
track pertama"); kalau track itu sudah berlabel Indonesia, berkasnya dipakai apa
adanya tanpa memanggil Google Translate.

### Job latar + progress

- `subtitle.Status` membuat job (`sync.Map` per `episode|lang`) dan
  menjalankannya di goroutine dengan `context.Background()`.
- `done`/`total` ditulis bersama oleh `SetProgress` = jumlah baris unik.
- `/api/subtitle/{id}` → `202` + progress selama `converting`, lalu `.vtt`
  dengan `no-store` setelah `ready`.
- `MaxConcurrentJobs = 4` (semaphore `jobSlots`); kelebihannya menunggu.
- Hasil akhir masuk cache disk `.cache/subtitles`, jadi pemutaran kedua instan.
- Log live: `translating` saat mulai, `cue N: <teks ID>` per cue, progress
  `done/total` tiap 5 dtk + saat selesai.

### Konsistensi (tanpa prompt — RPC web, bukan model)

Penjaga di kode:

- `mostlyNonLatin` menolak jawaban beraksara salah; baris itu diperlakukan
  seperti baris yang dijatuhkan (masuk pass retry) dan `SubtitleCache.Get`
  menolak entri lama beraksara salah (dianggap cache miss).
- `RunSubtitleJob` memvalidasi berkas sebelum job dinyatakan `ready`: hasil
  beraksara bukan target membuat job `error` dengan pesan jelas. Sebelum ini,
  job bisa `ready` dengan berkas yang cache-nya tolak, dan `/api/subtitle/{id}`
  menjawab `finished subtitle is missing from the cache` terus-menerus.
- `/api/subtitle/{id}` sekarang melihat cache lebih dulu: tidak ada di cache =
  progress (`202` + `{state,done,total,eta_seconds}`), bukan `502`.

### Sesi, fallback, dan 429

- RPC web butuh `f.sid` + `bl` dari halaman Translate (`FdrFJe`/`cfb2h`);
  `refreshGoogleSession` mengambilnya otomatis saat jawaban kosong, sekali
  per kegagalan. Tanpa `at` (yang basi justru ditolak); override manual via
  env `GOOGLE_FSID`/`GOOGLE_BL`/`GOOGLE_AT`.
- Primer web gagal (retryable/token) → fallback `gtx` via proxy
  `minky.anistream.one/fetch` (IP egress diblokir di googleapis langsung,
  IP proxy bersih): teks polos, mapping posisional, di-chunk ≤1200 char/GET
  (proxy menjawab 431 kalau URL kepanjangan).
- Paralel `MaxTranslateWorkers = 4`, pacing `WaitForBudget` per batch +
  jeda 500 ms; `requestsPerMinute = 120`.
- 429/5xx/sorry-block → `APIError` retryable: split batch + backoff (`Retry-After`
  dipakai). 5×429 beruntun = IP kena sorry-block → circuit breaker: cooldown
  10 mnt fail-fast (`errCooling`, proxy tetap dicoba), bukan hammer.
- 302 ke `/sorry` (Google mem-follow redirect jadi 200) dideteksi dari body
  dan diperlakukan sama dengan 429.

## Kuota

RPC web + gtx pakai endpoint publik tanpa API key, jadi tidak ada secret yang
perlu disembunyikan. Keduanya tidak terdokumentasi dan bisa rate-limit /
berubah tanpa peringatan; pakai sebagai terjemahan gratis best-effort.
IP yang kena sorry-block pulih sendiri (menit–jam); selama diblokir jangan
tembak request — tiap hit berpotensi memperpanjangnya.

## Frontend

React 19 + `react-router` 8 (mode *declarative*: `BrowserRouter` →
`<Routes>/<Route>`), dibundel rsbuild dengan React Compiler. Tidak ada skrip
CDN: shaka-player dari npm.

```
web/src/
  index.tsx          entry render
  App.tsx            Route: /, /s/:q?, /a/:animeId, /w/:animeId/:epId, 404
  components/        Header (pencarian), Player (shaka), NotFound, ui/Icon
  features/search/   Search.tsx + api.ts
  features/watch/    Watch.tsx, Anime.tsx, player.ts, catalog.ts, api.ts
  lib/               api.ts, urls.ts, types.ts
```

| Path | Isi |
|---|---|
| `/` | form cari |
| `/s/{q}` | hasil pencarian (`/s/:q?`) |
| `/a/{animeId}` | detail + daftar episode |
| `/w/{animeId}/{epId}` | player |
| `*` | 404 |

Audio tidak dipilih pengguna (selalu sub), jadi tidak ada kontrol Subtitle/Dub.

`KeyedPage` (`App.tsx`) membungkus halaman dengan `<div key={pathname}>` dari
`useLocation()` supaya subtree remount tiap URL berubah — cara docs React
("Resetting state with a key"), jadi tidak perlu lagi trik muat-ulang penuh
(`data-native` di preact-router) untuk membuang MSE/shaka lama saat pindah
episode; cleanup effect `Player` yang membongkarnya. `Player.tsx` memakai
`shaka.ui.Overlay` (quality + captions di control panel), subtitle dipasang
`addTextTrackAsync` hanya saat status `ready`, dan `Watch.tsx` mem-poll
`/api/subtitle/{id}/status` tiap 2 s selama `converting` (4 s kalau satu
jawaban gagal — job tetap jalan di server).

### Migrasi dari Preact (Sep 2026)

Seluruh SPA dipindah dari Preact 10 + preact-router ke React 19 + React Router,
sekaligus menghapus `lib/nav.ts`:

- **Navigasi** — `<Link to=...>` di semua tautan (hasil pencarian, daftar
  episode, prev/next, terkait/rekomendasi) dan `useNavigate()` untuk form
  pencarian. Helper `navigate()`/`goTo()` yang memanggil `route()` preact-router
  dan jatuh ke `location.assign` **dihapus** — itu sumber bug "Enter di kolom
  pencarian tidak mengubah apa pun": submit form lolos ke navigasi bawaan
  browser, halaman dimuat ulang di path yang sama, jadi halaman tonton tetap
  tampil. React Router menangani klik modifier/tengah dan submit secara penuh di
  klien, tanpa muat ulang.
- **React Compiler** aktif lewat `pluginReact({ reactCompiler: true })`. Rsbuild
  2.1+ menjalankannya sebagai Rust compiler di `jsc.transform.reactCompiler`
  (SWC), bukan plugin Babel terpisah; React 19 tidak butuh
  `react-compiler-runtime`. Bukti build: bundle memuat
  `react.memo_cache_sentinel`.
- **Atribut JSX** — `class`→`className`, `for`→`htmlFor`,
  `stroke-width`→`strokeWidth`, `playsinline`→`playsInline`,
  `enterkeyhint`→`enterKeyHint`, `autocomplete`→`autoComplete`; input
  terkendali pakai `onChange` (React), bukan `onInput`.
- **Ref** — `useRef<T>(null)` lalu dijaga null (React 19 tidak lagi memberi
  `current` non-null tanpa alasan). `createRoot` + `StrictMode` di
  `index.tsx`; effect `Player` sudah punya cleanup, jadi double-invoke dev
  aman.
- Docs: [React Compiler](https://react.dev/learn/react-compiler/installation),
  [Rsbuild React plugin](https://rsbuild.rs/plugins/list/plugin-react),
  [React Router](https://reactrouter.com/start/declarative/routing).

## Toolchain

- Go: `go.mod` menyebut 1.25.1; toolchain di mesin ini via mise (1.27).
- Web: node + rsbuild (React 19, React Compiler); typecheck
  `npx tsc --noEmit`, lint `pnpm run lint` dari `web/`.

## Menjalankan

```bash
go run . -addr :8080        # API + SPA di :8080 (web/dist hasil build, di-embed)
cd web && npm run dev       # dev server rsbuild + proxy /api → :8080
cd web && npm run build     # hasil ke web/dist (di-embed oleh go:embed)
go test ./...               # test semua paket
```

`go:embed` mengunci `web/dist` saat kompilasi: ubah frontend → `npm run build`
dulu, lalu **compile ulang + restart** binary.

## Dilewati

- **Range request** — segmen di-proxy apa adanya, tanpa dukungan `Range`.
- **Track audio di dalam satu file** — audio ditentukan di level server
  (dub), bukan track terpisah.
- **Cloudflare bypass** — `net/http` bisa kena blok; `ErrCloudflare` → 502
  dengan pesan jelas. Solusinya `curl-impersonate`, bukan regex.
- **Picture-in-picture** — tidak ada di layout kontrol.
