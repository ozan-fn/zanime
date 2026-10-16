module app

import net.http
import os
import internal.config

// SPA diuji tanpa bergantung `web/dist` hasil build: satu server memakai fixture di
// disk (`dist_dir`), satu lagi memakai peta embed (`dist`), satu lagi tanpa aset.
const spa_disk_addr = '127.0.0.1:39195'
const spa_embed_addr = '127.0.0.1:39196'
const spa_empty_addr = '127.0.0.1:39197'
const spa_fixture = '/tmp/zanime-spa-fixture'

const gz_bytes = '\x1f\x8b' + 'FAKE-GZIP'

fn write_fixture_dist() {
	os.mkdir_all('${spa_fixture}/assets') or { panic('mkdir: ${err}') }
	os.write_file('${spa_fixture}/index.html', '<!doctype html><div id="app">ZANIME</div>') or {
		panic('index: ${err}')
	}
	os.write_file('${spa_fixture}/assets/app.js', 'console.log("app");') or {
		panic('app.js: ${err}')
	}
	os.write_file('${spa_fixture}/assets/other.js', 'console.log("other");') or {
		panic('other.js: ${err}')
	}
	os.write_file('${spa_fixture}/assets/app.js.gz', gz_bytes) or { panic('app.js.gz: ${err}') }
}

fn serve(app &App, addr string) &http.Server {
	mut srv := &http.Server{
		addr:                 addr
		handler:              app
		show_startup_message: false
	}
	spawn srv.listen_and_serve()
	srv.wait_till_running() or { panic('server ${addr}: ${err}') }
	return srv
}

fn get(url string) http.Response {
	return http.get(url) or { panic('GET ${url}: ${err}') }
} // get_h: GET dengan header tambahan (mis. Accept-Encoding / If-None-Match).

fn get_h(url string, headers map[string]string) http.Response {
	mut h := http.new_header()
	for k, v in headers {
		h.add_custom(k, v) or {}
	}
	return http.fetch(url: url, method: .get, header: h) or { panic('GET ${url}: ${err}') }
}

fn test_spa_from_disk_fixture() {
	write_fixture_dist()
	mut a := &App{
		cfg:      config.Config{}
		dist_dir: spa_fixture
	}
	mut srv := serve(a, spa_disk_addr)
	base := 'http://${spa_disk_addr}'

	// index + fallback routing sisi klien
	r := get('${base}/')
	assert r.status_code == 200
	assert (r.header.get(.content_type) or { '' }).starts_with('text/html')
	assert (r.header.get(.cache_control) or { '' }) == 'no-cache'
	assert r.body.contains('ZANIME')
	rf := get('${base}/watch/abc/1')
	assert rf.status_code == 200
	assert rf.body.contains('ZANIME')
	assert get('${base}/browse/trending?page=2').status_code == 200

	// aset ber-hash: immutable
	ra := get('${base}/assets/app.js')
	assert ra.status_code == 200
	assert (ra.header.get(.content_type) or { '' }).starts_with('text/javascript')
	assert (ra.header.get(.cache_control) or { '' }).contains('immutable')
	assert ra.body == 'console.log("app");'
	assert (ra.header.get(.content_encoding) or { '' }) == ''

	// precompression: klien minta gzip + `.gz` ada → kirim byte .gz apa adanya
	rg := get_h('${base}/assets/app.js', {
		'Accept-Encoding': 'gzip, deflate'
	})
	assert rg.status_code == 200
	assert (rg.header.get(.content_encoding) or { '' }) == 'gzip'
	assert (rg.header.get(.vary) or { '' }) == 'Accept-Encoding'
	assert rg.body == gz_bytes // byte .gz, bukan hasil kompresi ulang
	etag := rg.header.get(.etag) or { '' }
	assert etag.ends_with('-gz"')

	// 304 memakai ETag varian gzip (ETag tergantung negosiasi → kirim keduanya)
	r304 := get_h('${base}/assets/app.js', {
		'Accept-Encoding': 'gzip'
		'If-None-Match':   etag
	})
	assert r304.status_code == 304
	assert r304.body == ''

	// tanpa `.gz` → tanpa content-encoding dan tanpa vary
	ro := get_h('${base}/assets/other.js', {
		'Accept-Encoding': 'gzip'
	})
	assert ro.status_code == 200
	assert (ro.header.get(.content_encoding) or { '' }) == ''
	assert (ro.header.get(.vary) or { '' }) == ''

	// klien tak menerima gzip tapi `.gz` ada → vary tetap dikirim (cache benar)
	assert (ra.header.get(.vary) or { '' }) == 'Accept-Encoding'

	// HEAD: header saja, tanpa body
	rh := http.head('${base}/assets/app.js') or { panic('head: ${err}') }
	assert rh.status_code == 200
	assert rh.body == ''

	srv.stop()
}

fn test_spa_from_embedded_map() {
	// Jalur embed: peta nama → isi (produksi, tanpa berkas di disk).
	mut a := &App{
		cfg:  config.Config{}
		dist: {
			'index.html':     '<!doctype html><div id="app">EMBED</div>'
			'assets/x.js':    'X-JS'
			'assets/x.js.gz': gz_bytes
		}
	}
	mut srv := serve(a, spa_embed_addr)
	base := 'http://${spa_embed_addr}'

	r := get('${base}/')
	assert r.status_code == 200
	assert r.body.contains('EMBED')
	assert get('${base}/anime/abc').body.contains('EMBED') // fallback tanpa berkas di disk

	rx := get('${base}/assets/x.js')
	assert rx.status_code == 200
	assert rx.body == 'X-JS'

	rg := get_h('${base}/assets/x.js', {
		'Accept-Encoding': 'gzip'
	})
	assert (rg.header.get(.content_encoding) or { '' }) == 'gzip'
	assert rg.body == gz_bytes

	srv.stop()
}

fn test_spa_without_assets_is_404() {
	mut a := &App{
		cfg:      config.Config{}
		dist_dir: '/tmp/zanime-spa-tidak-ada'
	}
	mut srv := serve(a, spa_empty_addr)
	assert get('http://${spa_empty_addr}/').status_code == 404
	srv.stop()
}
