# Zanime

Aplikasi Flutter untuk streaming anime subtitle Indonesia. Data katalog dari
AniList, halaman tonton dari miruro.cx, dan server tambahan dari sokuja.

> Nama paket/pubspec masih `test1` (warisan scaffold). Ganti nama paket akan
> mengubah semua import `package:test1/...`, jadi belum dilakukan.

## Fitur

- Banner anime trending, daftar episode terbaru, dan pencarian dengan debounce.
- Riwayat pencarian tersimpan di storage (maks. 10, terbaru dulu, muncul lagi
  saat aplikasi dibuka ulang); hanya disimpan saat Enter, bukan saat mengetik.
  Ada tombol silang di kolom pencarian dan di setiap item riwayat.
- Tab **Watching** menampilkan anime berstatus CURRENT/REPEATING dari akun
  AniList atau MyAnimeList yang sedang aktif (progress + episode berikutnya,
  tap untuk memutar). Tab **Profile** berisi login/profil akun, total per
  status (grid yang bisa dibuka menjadi daftar), pengaturan subtitle, cache,
  dan cek update.
- Halaman tonton: grid episode, ganti provider/server, kualitas video, subtitle.
  Provider terakhir yang dipilih diingat selama sesi; kalau tidak tersedia,
  dipakai server paling atas. Dialog status list: tap status yang sedang
  aktif sekali lagi untuk menghapus anime dari list. Kalau episode yang diminta
  belum ada/gagal dimuat, halaman otomatis memakai episode 1, bukan layar
  "Gagal memuat anime".
- **PiP dalam aplikasi**: menekan back dari player menyisakan player kecil di
  pojok kanan bawah di semua halaman; player bisa digeser seperti PiP YouTube
  dan tap membuka halaman tonton lagi tanpa memulai ulang pemutaran.
- Player `media_kit`/libmpv + libass (ASS/SSA dipakai apa adanya agar posisi
  subtitle `\an8` dihormati; VTT/SRT dibersihkan dari cue kosong/duplikat).
  Entitas HTML (`&amp;`, `&#39;`, `&#x266A;`) dan escape ASS (`\N`,
  `\h`) diterjemahkan supaya tidak muncul simbol aneh.
- Ukuran subtitle diatur lewat slider 12–32 px (default 16 px), tersimpan dan
  hanya memengaruhi subtitle, bukan font UI. Font fallback (Noto Sans/Symbols)
  dipakai agar glyph simbol tetap dirender. Track ASS/SSA dirender libass dan
  mengabaikan style Flutter, jadi ukurannya diteruskan lewat properti mpv
  `sub-scale` (16 px sebagai acuan). Jarak bawah subtitle ikut membesar bersama
  ukuran font, jadi subtitle besar tidak menempel terlalu rendah.
- Cek update di **Profile**: mengecek saat halaman dibuka dan setiap 1 menit
  selama terbuka; membandingkan versi aplikasi dengan GitHub Release terbaru
  (hanya major.minor.patch). Kalau ada update, baris **Cek update** langsung
  menuju halaman rilis; catatan rilis dirender sebagai Markdown.
- Cache aplikasi (`cached_network_image` + `flutter_cache_manager`) dengan
  ukuran dan tombol hapus di tile yang sama.
- Hitung mundur episode berikutnya yang belum tayang.
- APK release dipecah per ABI (`arm64-v8a`, `armeabi-v7a`, `x86_64`) dengan
  `flutter build apk --split-per-abi`.

## Struktur

```
lib/
  main.dart                       Bootstrap, tema gelap, MaterialApp
  models/models.dart              Model data + label/format helper
  screens/
    home/
      home_shell.dart             Bottom navigation 4 tab
      home_tab.dart               Banner trending + episode terbaru
      widgets/
        anime_banner.dart         Banner trending
        latest_episodes.dart      Grid episode terbaru (satu halaman)
    search/search_screen.dart     Pencarian + riwayat tersimpan
    watching/watching_screen.dart Anime CURRENT/REPEATING dari layanan aktif
    detail/watch_screen.dart      Detail anime, episode, pemilihan server
    player/video_player_widget.dart Player media_kit, kontrol, subtitle
    profile/
      settings_screen.dart        Profil, subtitle, cache, cek update
      anilist_list_screen.dart    Satu daftar status (completed, planning, ...)
      update_screen.dart          Halaman cek update (list, catatan Markdown)
  widgets/
    mini_player.dart              Overlay PiP dalam aplikasi
    upcoming_countdown.dart       Hitung mundur tayang
  services/
    api.dart                      AniList + miruro.cx (parser SvelteKit)
    anilist_auth.dart             OAuth AniList, profil, daftar watching
    mal_auth.dart                 OAuth MyAnimeList, profil, daftar, status
    list_service.dart             Layanan list aktif (AniList/MyAnimeList)
    app_settings.dart             Ukuran subtitle (px) + persistensi
    app_update.dart               Release GitHub + perbandingan versi
    mini_player.dart              Status playback PiP + navigator key
    playback_session.dart         Player libmpv yang dipakai bersama PiP
    sokuja.dart                   Provider sokuja + pencocokan anime
    http_client.dart              Cronet di Android, dart:io di desktop
    subtitle_format.dart          Normalisasi subtitle
    image_cache.dart              Ukuran/hapus cache gambar
```

State halaman masih memakai `StatefulWidget`/`Future` lokal, jadi belum ada
folder `providers`.

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

Back dari halaman tonton tidak menutup player. `PopScope` menangkap back, lalu
`PlaybackSession` (Player + VideoController yang sama) diserahkan ke PiP, jadi
pemutaran berjalan terus tanpa seek/restart. Overlay baru dipasang setelah
halaman tonton selesai keluar agar hanya satu `Video` memakai controller.

- Player kecil bisa **digeser** (drag) ke mana saja, seperti PiP YouTube;
  tap tanpa geser membuka halaman tonton lagi dengan session yang sama.
- Subtitle di player kecil memakai ukuran tetap 18 px (sedikit lebih besar dari
  default) dan **tidak** ikut slider ukuran subtitle.
- Tombol tutup (×) menghentikan dan membuang session PiP.
- Membuka halaman tonton mana pun otomatis menutup player kecil.
- Hanya PiP dalam aplikasi, bukan PiP sistem operasi.

## Cek update

Profile → **Cek update**. Pengecekan berjalan saat halaman dibuka dan diulang
tiap 1 menit selama halaman terbuka; hasil terakhir dipakai bersama oleh baris
di Profile.

- Baris **Cek update** di Profile: kalau ada versi lebih baru, tampil ikon
  update + label **Update** dan tap langsung membuka
  `https://github.com/ozan-fn/zanime/releases/latest`; kalau tidak, tap membuka
  halaman list cek update seperti biasa.
- Sumber: `GET https://api.github.com/repos/ozan-fn/zanime/releases/latest`.
- Versi terpasang dibaca lewat `package_info_plus`. Perbandingan hanya
  **major.minor.patch**; build metadata (`+8`) dan awalan `v` diabaikan.
- Catatan rilis dirender sebagai **Markdown** dan mengikuti scroll halaman
  (tidak dipotong).
- Gagal jaringan menampilkan tile error dan tombol coba lagi; versi terpasang
  tetap ditampilkan.

## Login AniList dan MyAnimeList

Tab **Profile** punya dua tombol login bersebelahan dengan logo aslinya: **ani**
(AniList) dan **mal** (MyAnimeList). Setelah login, tab itu menampilkan profil
akun aktif (nama, ID, jumlah anime, episode ditonton, tombol keluar solid
merah). Token disimpan di `shared_preferences`.

- **Keluar** dari akun dan **hapus cache aplikasi** meminta konfirmasi lewat
  dialog sebelum dijalankan.
- Hanya **satu layanan aktif** pada satu waktu. Untuk pindah, keluar dulu lalu
  login lewat tombol layanan lain; layanan aktif disimpan di
  `shared_preferences` (`list_service`).
- Tab **Watching**, status di halaman tonton, simpan, dan hapus list semuanya
  mengikuti layanan aktif.
- AniList memakai **AniList ID** dari miruro; MyAnimeList tidak memakai ID itu,
  jadi anime dicari lewat judul di MAL. Hanya hasil yang judulnya terverifikasi
  mirip (skor kemiripan ≥ 0.55, termasuk judul alternatif) yang dipakai dan
  di-cache; hitungan pertama tidak pernah ditebak, jadi status tidak pernah
  tertulis ke entry MAL yang salah.
- Daftar MAL mengambil field anime (`num_episodes`, `media_type`,
  `start_season`) di level atas request, bukan di dalam `node{...}`, supaya
  total episode, format, dan tahun terisi seperti AniList. Episode `0` dari MAL
  (belum diketahui) ditampilkan sebagai `?`, sama seperti AniList.

### AniList

- Client ID: `18615` (hardcoded, aman karena publik).
- Redirect URL yang **wajib** diisi di pengaturan aplikasi AniList:
  `zanime://auth`
- Flow: **implicit grant** (`response_type=token`) lewat
  `flutter_web_auth_2`; token dikembalikan di fragment URL dan tidak butuh
  server maupun client secret.
- `AndroidManifest.xml` mendaftarkan `zanime` untuk menangkap callback.

### MyAnimeList

- Client ID: `9de8db2034d559e70de97b61528a9692`
- Client secret ditanam di aplikasi (diminta eksplisit). APK bisa dibongkar,
  jadi secret harus dianggap publik dan dirotasi bila bocor.
- Redirect URL yang **wajib** didaftarkan persis di pengaturan aplikasi MAL:
  `zanime://auth`
- Flow: **authorization code + PKCE** (`code_challenge_method=plain`) lewat
  `flutter_web_auth_2`; kode ditukar ke token di
  `https://myanimelist.net/v1/oauth2/token`.
- Access token diperbarui otomatis dengan refresh token sebelum kedaluwarsa
  (`mal_token`, `mal_refresh_token`, `mal_expiry_ms`).
- Status MAL dipetakan ke kode gaya AniList agar UI yang sama bisa dipakai:
  `watching→CURRENT`, `completed→COMPLETED`, `on_hold→PAUSED`,
  `dropped→DROPPED`, `plan_to_watch→PLANNING`, rewatch→`REPEATING`.

Client secret AniList **tidak dipakai** dan tidak boleh ditanam di APK: APK
bisa dibongkar, sehingga secret harus dianggap publik. Kalau secret pernah
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

- Subtitle hanya dari miruro; mirror sokuja berupa mp4 mandiri tanpa track
  subtitle terpisah.
- Cek update memakai GitHub API tanpa token, jadi tunduk pada rate limit
  60 permintaan/jam per IP; pengecekan otomatis dibatasi sekali per menit.
- Warning Flutter: `cronet_http` masih memakai Kotlin Gradle Plugin (non-fatal
  saat ini, upstream: dart-lang/http#1920).
- Test tidak memutar video sungguhan; `libmpv` tidak tersedia di lingkungan
  test (`Cannot find libmpv` hanya log, bukan kegagalan test).
