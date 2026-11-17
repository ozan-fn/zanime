import 'dart:io';

import 'package:flutter_cache_manager/flutter_cache_manager.dart';
import 'package:flutter/painting.dart';
import 'package:path_provider/path_provider.dart';

/// Bytes used by the on-disk image cache (CachedNetworkImage /
/// flutter_cache_manager's default cache folder).
Future<int> imageCacheSize() async {
  final dir = Directory(
    '${(await getTemporaryDirectory()).path}/${DefaultCacheManager.key}',
  );
  if (!await dir.exists()) return 0;
  var bytes = 0;
  await for (final entity in dir.list(recursive: true)) {
    if (entity is File) bytes += await entity.length();
  }
  return bytes;
}

/// Deletes the image cache from disk and drops decoded images from memory.
Future<void> clearImageCache() async {
  await DefaultCacheManager().emptyCache();
  PaintingBinding.instance.imageCache
    ..clear()
    ..clearLiveImages();
}

String formatCacheSize(int bytes) {
  if (bytes < 1024) return '$bytes B';
  final kb = bytes / 1024;
  if (kb < 1024) return '${kb.toStringAsFixed(1)} KB';
  return '${(kb / 1024).toStringAsFixed(1)} MB';
}
