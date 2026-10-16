module app

import net.http
import internal.config
import internal.upstream

const test_addr = '127.0.0.1:39177'

// Server end-to-end: routing + error yang tak butuh jaringan/berkas.
fn test_server_end_to_end() {
	mut a := &App{
		cfg: config.Config{
			provider: 'yuki'
		}
	}
	mut srv := &http.Server{
		addr:    test_addr
		handler: a
	}
	spawn srv.listen_and_serve()
	srv.wait_till_running() or { panic('server tak jalan: ${err}') }
	base := 'http://${test_addr}'

	r := http.get('${base}/api/stats') or { panic('stats: ${err}') }
	assert r.status_code == 200
	assert (r.header.get(.content_type) or { '' }) == 'application/json'
	assert r.body.contains('memMb')

	r2 := http.get('${base}/api/search?query=') or { panic('search: ${err}') }
	assert r2.status_code == 400
	assert r2.body.contains('query kosong')

	r3 := http.get('${base}/api/fetch?u=${upstream.b64('http://127.0.0.1/x')}') or {
		panic('fetch: ${err}')
	}
	assert r3.status_code == 400
	assert r3.body.contains('host tidak diizinkan')

	r4 := http.get('${base}/api/subid/status?id=nope') or { panic('status: ${err}') }
	assert r4.status_code == 404
	assert r4.body.contains('job tak ada')

	r5 := http.post('${base}/api/subid/batch', '{"id":"nope"}') or { panic('batch: ${err}') }
	assert r5.status_code == 400
	assert r5.body.contains('GEMINI_API_KEY')

	srv.stop()
}
