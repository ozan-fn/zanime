import 'package:material_ui/material_ui.dart';

import '../services/image_cache.dart';

/// Settings tab: image cache usage and a clear button.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    this.cacheSize = imageCacheSize,
    this.clearCache = clearImageCache,
  });

  /// Injected in tests.
  final Future<int> Function() cacheSize;
  final Future<void> Function() clearCache;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  int? _size;
  bool _clearing = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final size = await widget.cacheSize();
    if (mounted) setState(() => _size = size);
  }

  Future<void> _clear() async {
    setState(() => _clearing = true);
    await widget.clearCache();
    if (!mounted) return;
    setState(() {
      _size = 0;
      _clearing = false;
    });
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('Cache dihapus')));
  }

  @override
  Widget build(BuildContext context) {
    final size = _size;
    return Scaffold(
      appBar: AppBar(title: const Text('Setting')),
      body: ListView(
        children: [
          ListTile(
            leading: const Icon(Icons.image_outlined),
            title: const Text('Cache gambar'),
            subtitle: Text(size == null ? 'Menghitung...' : formatCacheSize(size)),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
            child: FilledButton(
              onPressed: _clearing || size == null ? null : _clear,
              child: Text(_clearing ? 'Menghapus...' : 'Hapus cache'),
            ),
          ),
        ],
      ),
    );
  }
}
