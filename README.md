# Zanime

Aplikasi Flutter untuk streaming anime subtitle Indonesia. Data katalog dari
AniList, halaman tonton dari miruro.cx, dan server tambahan dari sokuja.

> Nama paket/pubspec masih `test1` (warisan scaffold). Ganti nama paket akan
> mengubah semua import `package:test1/...`, jadi belum dilakukan.

## Fitur

- Banner anime trending, daftar episode terbaru, dan pencarian dengan debounce.
- Riwayat pencarian dalam sesi (maks. 10, tanpa penyimpanan permanen).
- Tab **Watching** menampilkan anime berstatus CURRENT/REPEATING dari akun
  AniList (progress + episode berikutnya, tap untuk memutar). Tab **Profile**
  berisi login/profil AniList, total per status (grid yang bisa dibuka menjadi
  daftar), pengaturan subtitle, cache, dan cek update.
- Halaman tonton: grid episode, ganti provider/server, kualitas video, subtitle.
  Provider terakhir yang dipilih diingat selama sesi; kalau tidak tersedia,
  dipakai server paling atas.
- **PiP dalam aplikasi**: menekan back dari player menyisakan player kecil di
  pojok kanan bawah di semua halaman; tap player kecil membuka halaman tonton
  lagi dan melanjutkan dari posisi terakhir.
- Player `media_kit`/libmpv + libass (ASS/SSA dipakai apa adanya agar posisi
  subtitle `\an8` dihormati; VTT/SRT dibersihkan dari cue kosong/duplikat).
  Entitas HTML (`&amp;`, `&#39;`, `&#x266A;`) dan escape ASS (`\N`,
  `\h`) diterjemahkan supaya tidak muncul simbol aneh.
- Ukuran subtitle diatur lewat slider 12–32 px (default 16 px), tersimpan dan
  hanya memengaruhi subtitle, bukan font UI. Font fallback (Noto Sans/Symbols)
  dipakai agar glyph simbol tetap dirender.
- Cek update di **Profile** → membandingkan versi aplikasi dengan GitHub
  Release terbaru (hanya major.minor.patch) dan menautkan ke halaman rilis.
- Cache gambar (`cached_network_image` + `flutter_cache_manager`) dengan
  pengaturan ukuran dan hapus cache.
- Hitung mundur episode berikutnya yang belum tayang.
- APK release dipecah per ABI (`arm64-v8a`, `armeabi-v7a`, `x86_64`) dengan
  `flutter build apk --split-per-abi`.

## Struktur

```
lib/
  main.dart                     Home shell, navigasi 4 tab, tema gelap
  components/
    anime_banner.dart           Banner trending
    latest_episodes.dart        Grid episode terbaru (satu halaman)
    search_screen.dart          Pencarian + riwayat
    watching_screen.dart        Anime CURRENT/REPEATING dari AniList
    watch_screen.dart           Detail anime, episode, pemilihan server
    video_player_widget.dart    Player media_kit, kontrol, subtitle
    mini_player.dart            Overlay PiP dalam aplikasi
    update_screen.dart          Halaman cek update (list)
    upcoming_countdown.dart     Hitung mundur tayang
    settings_screen.dart        Profil, subtitle, cache, cek update
  models/models.dart            Model data + label/format helper
  services/
    api.dart                    AniList + miruro.cx (parser SvelteKit)
    anilist_auth.dart           OAuth AniList, profil, daftar watching
    app_settings.dart           Ukuran subtitle (px) + persistensi
    app_update.dart             Release GitHub + perbandingan versi
    mini_player.dart            Status playback PiP + navigator key
    sokuja.dart                 Provider sokuja + pencocokan anime
    http_client.dart            Cronet di Android, dart:io di desktop
    subtitle_format.dart        Normalisasi subtitle
    image_cache.dart            Ukuran/hapus cache gambar
```

## Sumber data

- **AniList GraphQL** — banner, pencarian, metadata anime.
- **miruro.cx** — data tonton (`/watch/<id>/x/__data.json`), server/stream,
  subtitle. Endpoint API miruro memblokir klien non-HTTP/2, jadi dipakai
  payload SvelteKit.
- **sokuja (x6.sokuja.uk)** — server tambahan:

  | Endpoint | Fungsi |
  | --- | --- |
  | `GET /api/search/?q=..&limit=20` | Cari anime (prefix judul) |
  | `GET /anime/<slug>/` | Metadata + daftar episode (id ada di payload RSC Next.js) |
  | `GET /api/video-mirrors/?e=<episodeId>` | Mirror mp4 480p/720p/1080p |

### Pencocokan sokuja

Judul saja tidak cukup karena banyak season/remake memakai judul sama. Skor
gabungan (ambang 0.55, maksimal 3 kandidat diperiksa detailnya):

| Komponen | Bobot |
| --- | --- |
| Kemiripan judul (Dice per kata, partikel dibuang) | 0.50 |
| Tahun sama / beda ≤1 tahun | 0.20 / 0.10 |
| Studio utama sama | 0.15 |
| Overlap genre | 0.10 |
| Jumlah episode sama | 0.10 |
| Overlap sinopsis (kecil; bahasa beda) | 0.05 |

Kalau skor terbaik di bawah ambang atau nomor episode tidak ada, server sokuja
tidak ditampilkan — lebih baik tidak ada server daripada memutar anime/episode
yang salah. Provider ini tampil sebagai `skj` di dropdown.

## Menjalankan

```bash
flutter pub get
flutter run -d <device>
flutter analyze
flutter test
```

Build APK release per ABI:

```bash
flutter build apk --release --split-per-abi
```

Menghasilkan `app-arm64-v8a-release.apk`, `app-armeabi-v7a-release.apk`, dan
`app-x86_64-release.apk` di `build/app/outputs/flutter-apk/`. Tanpa
`--split-per-abi`, Flutter membuat satu APK universal berisi semua ABI.

### Rilis otomatis

`.github/workflows/release-apk.yml` berjalan setiap push ke `main` (atau
`workflow_dispatch`). Versi diambil dari `version:` di `pubspec.yaml`:

- Jika **major/minor/patch** di pubspec berubah, rilis memakai versi baru itu.
- Build number (`+N`) selalu naik satu dari tag rilis terakhir, sehingga
  `versionCode` Android tetap monoton dan setiap rilis punya tag unik
  (contoh: `v1.0.0+1`, `v1.0.0+2`, lalu `v1.1.0+3` bila pubspec jadi `1.1.0`).
- Versi yang lebih tua dari tag terakhir akan menggagalkan workflow.

APK dibangun dengan `--build-name`/`--build-number` dari versi tersebut dan
`--split-per-abi`, dinamai `zanime-v<versi>+<build>-<abi>.apk` untuk tiap ABI,
diunggah sebagai artifact, lalu otomatis dibuatkan GitHub Release. Workflow
memakai aksi pihak ketiga (`subosito/flutter-action`,
`gradle/actions/setup-gradle`, `softprops/action-gh-release`) dengan cache
Flutter SDK, cache pub, dan cache Gradle; tidak ada `apt install`.

## PiP dalam aplikasi

Back dari halaman tonton tidak menutup player. `PopScope` menangkap back,
membuat `MiniPlayback` (stream/headers/subtitle/episode + posisi terakhir),
lalu menaruh player kecil (≈176 px, 16:9) lewat `OverlayEntry` di atas semua
route. Pemutaran berlanjut karena widget player baru dibuat dengan `startAt`;
posisi berjalan dilaporkan balik lewat `positionSink`.

- Tap player kecil → overlay ditutup dan `WatchScreen(startAt: ...)` dibuka
  kembali, resume dari posisi saat itu (data server diambil ulang).
- Tombol tutup (×) menghentikan dan membuang player kecil.
- Membuka halaman tonton mana pun otomatis menutup player kecil.
- Hanya PiP dalam aplikasi, bukan PiP sistem operasi.

## Cek update

Profile → **Cek update** membuka halaman list (tanpa dialog) yang mengambil
`GET https://api.github.com/repos/ozan-fn/zanime/releases/latest`.

- Versi terpasang dibaca lewat `package_info_plus`.
- Perbandingan hanya **major.minor.patch**; build metadata (`+8`) dan awalan
  `v` diabaikan, jadi `v1.0.1+8` dianggap sama dengan `1.0.1`.
- Jika lebih baru, muncul status **Update tersedia** beserta baris
  **Buka halaman rilis** yang membuka
  `https://github.com/ozan-fn/zanime/releases/latest` di browser
  (`url_launcher`).
- Gagal jaringan menampilkan tile error dan tombol coba lagi; versi terpasang
  tetap ditampilkan.

## Login AniList

Tab **Profile** punya tombol login dan profil AniList (nama, ID, jumlah anime,
episode ditonton, tombol keluar). Token disimpan di `shared_preferences`.

- Client ID: `18615` (hardcoded, aman karena publik).
- Redirect URL yang **wajib** diisi di pengaturan aplikasi AniList:
  `zanime://auth`
- Flow: **implicit grant** (`response_type=token`) lewat
  `flutter_web_auth_2`; token dikembalikan di fragment URL dan tidak butuh
  server maupun client secret.
- `AndroidManifest.xml` mendaftarkan `zanime` untuk menangkap callback.

Client secret **tidak dipakai** dan tidak boleh ditanam di APK: APK bisa
dibongkar, sehingga secret harus dianggap publik. Kalau secret pernah
dibagikan, rotasi/revoke di pengaturan AniList.

## Masalah: HTTP 403 dari sokuja

### Gejala

Permintaan ke `x6.sokuja.uk` (khususnya `/api/search/`) bisa menerima respons
Cloudflare `403` dengan body HTML `Just a moment...` alih-alih JSON.

### Temuan

- `curl` dengan User-Agent Chrome desktop mendapat `200` untuk endpoint yang
  sama, sedangkan klien Dart (`IOClient`/`dart:io`) di desktop mendapat `403`.
- Jadi pemblokiran bukan soal URL/parameter, melainkan fingerprint klien
  (TLS/JA3, header, dan reputasi IP). Cloudflare menantang koneksi yang tidak
  terlihat seperti browser.
- Di Android aplikasi memakai Cronet (network stack Chromium) lewat
  `cronet_http`, yang fingerprint-nya lebih dekat ke Chrome. Karena itu aplikasi
  memakai `createHttpClient()` → `CronetClient`; desktop memakai `IOClient`.
- `fetchSokujaServer()` menangani kegagalan dengan aman: status non-200
  dilewati, exception ditangkap, dan server sokuja tidak ditambahkan. Miruro
  tetap bisa diputar.

### Catatan / kemungkinan lanjutan

- 403 Cloudflare tidak dapat dipastikan dari desktop; harus diuji di device.
- Kalau device juga kena 403, opsi yang sudah dipertimbangkan:
  - `ok_http` (Conscrypt) — bukan stack Chrome, dan plugin `jni`-nya masih
    memaksa `compileSdk 31` (butuh workaround Gradle).
  - `rhttp` (reqwest/rustls) — menambah toolchain Rust saat build.
  - Proxy/Worker yang sudah lolos challenge Cloudflare (menambah dependensi
    eksternal).
- Jangan mengandalkan `User-Agent` saja; Cloudflare juga memeriksa TLS dan
  pola header.

## Keterbatasan

- Riwayat pencarian hilang saat aplikasi ditutup.
- Subtitle hanya dari miruro; mirror sokuja berupa mp4 mandiri tanpa track
  subtitle terpisah.
- Cek update memakai GitHub API tanpa token, jadi tunduk pada rate limit
  60 permintaan/jam per IP (cukup untuk pengecekan manual).
- Warning Flutter: `cronet_http` masih memakai Kotlin Gradle Plugin (non-fatal
  saat ini, upstream: dart-lang/http#1920).
- Test tidak memutar video sungguhan; `libmpv` tidak tersedia di lingkungan
  test (`Cannot find libmpv` hanya log, bukan kegagalan test).
