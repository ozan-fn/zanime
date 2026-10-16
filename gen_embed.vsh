#!/usr/bin/env -S v run

import os
import gechandesu.embedfs

// gen_embed.vsh menghasilkan `web_dist.v` (root): peta nama relatif → isi berkas
// `web/dist` (termasuk `.gz` precompress vite), memakai modul embedfs.
//
// Dipanggil otomatis oleh `make web`. Sekali di awal: `v install gechandesu.embedfs`.
// Bila `web/dist` belum dibangun, peta kosong — build biasa tetap jalan dan
// `internal.app` membaca aset dari disk (lihat main.v `dist_map`).
const dist_dir = 'web/dist'

fn main() {
	mut files := []string{}
	collect(dist_dir, dist_dir, mut files)
	files.sort()
	code := embedfs.generate_with(files, embedfs.EmbedFsParams{
		module_name:      'main'
		const_name:       'web_dist'
		const_public:     true
		file_path_prefix: dist_dir
		notice:           'web_dist.v — DIHASILKAN `make web` (gen_embed.vsh); jangan diedit manual.'
	}) or { panic('embedfs.generate_with gagal: ${err}') }
	os.write_file('web_dist.v', code) or { panic('gagal tulis web_dist.v: ${err}') }
	println('web_dist.v: ${files.len} berkas dari ${dist_dir}')
}

// collect mengumpulkan nama berkas relatif terhadap `base` (rekursif).
fn collect(base string, dir string, mut out []string) {
	for name in os.ls(dir) or { []string{} } {
		p := os.join_path(dir, name)
		if os.is_dir(p) {
			collect(base, p, mut out)
		} else {
			out << p.all_after('${base}/').replace('\\', '/')
		}
	}
}
