# AGENTS.md — Aturan Flutter 3.50.0 Anti Memory Leak

## Prinsip Wajib
- Setiap objek disposable harus punya pemilik jelas.
- Alokasikan resource di `initState()`, bukan di `build()`.
- Buang semua resource di `dispose()`.
- Jangan simpan referensi ke objek yang sudah di-dispose.
- Untuk non-pemilik, gunakan `WeakReference` bila perlu.

## Wajib Di-dispose
- Controller: `AnimationController`, `TextEditingController`, `ScrollController`, `PageController`, `TabController`, `FocusNode`, `TransformationController`.
- `StreamSubscription.cancel()` dan `StreamController.close()`.
- `Timer.cancel()`.
- `removeListener()` dari `ChangeNotifier` / `ValueNotifier`.
- Memori FFI: pasangkan `malloc` dengan `free`.

## Larangan
- Jangan membuat controller/subscription/timer di `build()`.
- Jangan memakai `BuildContext` setelah `await` tanpa cek `mounted`.
- Jangan membiarkan Gradle daemon / emulator berjalan saat tidak dipakai.
- Di Flutter 3.50.0, hindari `MaskFilter.blur` + gradient + path besar karena berisiko lonjakan memori Impeller.
