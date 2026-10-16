module app

import net.http
import os
import time
import x.json2
import internal.config
import internal.upstream

// Test LIVE: menyentuh upstream nyata (animex/anistream/CDN/aniskip/TAC/AniList).
// Default dilewati supaya `v test internal/` tetap deterministik & offline.
// Jalankan: ZANIME_LIVE=1 v test internal/app/live_test.v
//
// Menutup celah yang tak bisa diuji offline: /api/hls & /api/fetch lewat handler
// (host CDN publik → tidak diblok anti-SSRF), plus endpoint yang URL-nya di-hardcode
// (skiptimes/comments/episode-meta/anilist).
const live_addr = '127.0.0.1:39194'

fn live_get(url string) http.Response {
	return http.get(url) or { panic('GET ${url}: ${err}') }
}

fn live_map(body string) map[string]json2.Any {
	return (json2.decode[json2.Any](body) or { panic('json: ${body}') }).as_map()
}

fn first_line_with(text string, prefix string) string {
	for l in text.split('\n') {
		t := l.trim_space()
		if t.starts_with(prefix) {
			return t
		}
	}
	return ''
}

fn test_live_api_flow_streams() {
	if os.getenv('ZANIME_LIVE') == '' {
		eprintln('live_test dilewati (set ZANIME_LIVE=1 untuk mengaktifkan)')
		return
	}
	mut a := &App{
		cfg: config.load()
	}
	mut srv := &http.Server{
		addr:                 live_addr
		handler:              a
		show_startup_message: false
	}
	spawn srv.listen_and_serve()
	srv.wait_till_running() or { panic('app: ${err}') }
	base := 'http://${live_addr}'

	// --- katalog & pencarian (upstream nyata) ---
	rs := live_get('${base}/api/search?query=naruto')
	assert rs.status_code == 200
	items := (json2.decode[json2.Any](rs.body) or { panic('search json') }).as_array()
	assert items.len > 0
	first := items[0].as_map()
	id := upstream.val_str(first['id'] or { json2.null })
	cover := upstream.val_str(first['cover'] or { json2.null })
	assert id != ''
	assert cover.starts_with('/api/fetch?u=')
	assert live_get('${base}/api/catalog?sort=TRENDING&limit=3').status_code == 200

	// --- detail / episode / server ---
	ra := live_get('${base}/api/anime?id=${id}&section=base')
	assert ra.status_code == 200
	assert ra.body.contains('titleRomaji')
	re := live_get('${base}/api/episodes?id=${id}')
	assert re.status_code == 200
	assert (json2.decode[json2.Any](re.body) or { panic('episodes json') }).as_array().len > 0
	assert live_get('${base}/api/servers?id=${id}&ep=1').status_code == 200
	rso := live_get('${base}/api/sources?id=${id}&ep=1&type=sub&provider=yuki')
	assert rso.status_code == 200
	assert rso.body.contains('sources')

	// --- stream: resolve sumber hidup → URL proxy ---
	rst := live_get('${base}/api/stream?id=${id}&ep=1')
	assert rst.status_code == 200
	stream := live_map(rst.body)
	stream_url := upstream.val_str(stream['stream'] or { json2.null })
	sub_id := upstream.val_str(stream['sub_id'] or { json2.null })
	assert stream_url.starts_with('/api/hls?u=')

	// --- /api/hls: playlist master ditulis ulang (URI → /api/*) ---
	rm := live_get('${base}${stream_url}')
	assert rm.status_code == 200
	assert (rm.header.get(.content_type) or { '' }).contains('mpegurl')
	assert rm.body.starts_with('#EXTM3U')
	assert rm.body.contains('/api/hls?u=')
	variant := first_line_with(rm.body, '/api/hls')
	assert variant != ''

	// --- varian → segmen, lalu /api/fetch: byte video nyata lewat proxy ---
	rv := live_get('${base}${variant}')
	assert rv.status_code == 200
	assert rv.body.contains('#EXTM3U')
	seg := first_line_with(rv.body, '/api/fetch')
	assert seg != '', 'varian harus memuat segmen /api/fetch'
	rseg := live_get('${base}${seg}')
	assert rseg.status_code == 200
	assert (rseg.header.get(.content_type) or { '' }).starts_with('video/')
	assert rseg.body.len > 0
	assert rseg.body[0] == 0x47 || rseg.body.len > 1000 // TS sync byte (atau fMP4 besar)

	// --- gambar juga lewat proxy ---
	rimg := live_get('${base}${cover}')
	assert rimg.status_code == 200
	assert (rimg.header.get(.content_type) or { '' }).starts_with('image/')

	// --- subtitle: worker EN → status → result (VTT) ---
	assert sub_id != ''
	mut state := ''
	for _ in 0 .. 80 {
		rstt := live_get('${base}/api/subid/status?id=${sub_id}')
		assert rstt.status_code == 200
		state = upstream.val_str(live_map(rstt.body)['state'] or { json2.null })
		if state in ['ready', 'done', 'failed'] {
			break
		}
		time.sleep(250 * time.millisecond)
	}
	assert state in ['ready', 'done']
	rr := live_get('${base}/api/subid/result?id=${sub_id}')
	assert rr.status_code == 200
	assert (rr.header.get(.content_type) or { '' }).starts_with('text/vtt')
	assert rr.body.trim_space().starts_with('WEBVTT')
	assert rr.body.contains('-->')

	// --- terjemahan Gemini (opsional; memakai kuota → ZANIME_LIVE_GEMINI=1) ---
	if os.getenv('ZANIME_LIVE_GEMINI') != '' {
		rbat := http.post('${base}/api/subid/batch', '{"id":"${sub_id}","at":0.0}') or {
			panic('batch: ${err}')
		}
		assert rbat.status_code == 200
		bm := live_map(rbat.body)
		assert upstream.val_str(bm['error'] or { json2.null }) == ''
		assert (bm['done'] or { json2.null }).i64() == 10 // 1 batch = 10 cue
		delta := upstream.val_str(bm['vtt'] or { json2.null })
		assert delta.contains('-->')
		assert !delta.contains('[0]') && !delta.contains('[9]') // penanda [N] habis dikonsumsi
	}

	// --- endpoint ber-URL hardcode ---
	assert live_get('${base}/api/skiptimes?mal=20&ep=1').status_code == 200
	assert live_get('${base}/api/episode-meta?anilistId=20&ep=1').status_code == 200
	assert live_get('${base}/api/comments?media=20&limit=5').status_code == 200
	ran := http.post('${base}/api/anilist',
		'{"query":"{ Page(page: 1, perPage: 1) { media { id } } }"}') or {
		panic('anilist: ${err}')
	}
	assert ran.status_code == 200
	assert ran.body.contains('data')
	assert live_get('${base}/api/stats').status_code == 200

	srv.stop()
}
