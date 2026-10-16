module upstream

import net.http
import internal.config

// Jalur streaming diuji lawan CDN tiruan: probe sumber hidup → tulis ulang playlist
// → unduh segmen (MIME dari sniff) + Range. Handler /api/fetch & /api/hls memakai
// fungsi yang sama, tapi menolak host privat (anti-SSRF) sehingga di sini diuji
// langsung di level modul.
const cdn_addr = '127.0.0.1:39191'

struct MockCdn {}

fn (mut m MockCdn) handle(req http.Request) http.Response {
	path := req.url.all_before('?')
	return match path {
		'/cdn/master.m3u8' {
			cdn_playlist('#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1920x1080\nindex-f1-v1-a1.m3u8\n')
		}
		'/cdn/index-f1-v1-a1.m3u8' {
			cdn_playlist('#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\nseg-1.ts\n#EXTINF:4.0,\nseg-2.ts\n')
		}
		'/cdn/seg-1.ts', '/cdn/seg-2.ts' {
			cdn_segment(req)
		}
		'/cdn/subs.vtt' {
			mut r := http.Response{
				status_code: 200
				body:        'WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello\n\n00:00:03.000 --> 00:00:04.000\nWorld\n'
			}
			r.header.add_custom('content-type', 'text/vtt') or {}
			r
		}
		else {
			http.Response{
				status_code: 404
				body:        'nope'
			}
		}
	}
}

// Segmen TS tiruan: sync byte 0x47 di offset 0 dan 188. Label upstream sengaja
// `application/octet-stream` supaya terbukti sniff yang menang.
fn cdn_segment(req http.Request) http.Response {
	mut b := []u8{len: 376, init: u8(0)}
	b[0] = 0x47
	b[188] = 0x47
	mut r := http.Response{
		status_code: 200
		body:        b.bytestr()
	}
	r.header.add_custom('content-type', 'application/octet-stream') or {}
	if _ := req.header.get(.range) {
		r.status_code = 206
		r.header.add_custom('content-range', 'bytes 0-375/376') or {}
	}
	return r
}

fn cdn_playlist(body string) http.Response {
	mut r := http.Response{
		status_code: 200
		body:        body
	}
	r.header.add_custom('content-type', 'application/vnd.apple.mpegurl') or {}
	return r
}

fn test_cdn_pipeline_probe_rewrite_fetch() {
	mut cdn := &MockCdn{}
	mut srv := &http.Server{
		addr:                 cdn_addr
		handler:              cdn
		show_startup_message: false
	}
	spawn srv.listen_and_serve()
	srv.wait_till_running() or { panic('cdn: ${err}') }
	base := 'http://${cdn_addr}'
	master := '${base}/cdn/master.m3u8'

	// probe_stream: master → varian → segmen; balik URL sumber yang hidup.
	s :=
		jdec('{"sources":[{"url":"${master}"}],"headers":{"Origin":"http://127.0.0.1","Referer":"http://127.0.0.1/"}}')
	assert (probe_stream(s) or { '' }) == master

	// Playlist master: varian ditulis ulang ke /api/hls, URL CDN tak tersisa.
	master_text := fetch_text(master, 'o', 'r', none) or { panic('master: ${err}') }
	assert master_text.contains('#EXTM3U')
	rewritten := rewrite_playlist(base_of(master), master_text, none, none)
	assert rewritten.contains('/api/hls?u=')
	assert !rewritten.contains(cdn_addr)

	// Playlist media: segmen jadi /api/fetch/<nama>.ts?u=.
	media := resolve(base_of(master), playlist_urls(master_text)[0])
	media_text := fetch_text(media, 'o', 'r', none) or { panic('media: ${err}') }
	media_out := rewrite_playlist(base_of(media), media_text, none, none)
	assert media_out.contains('/api/fetch/seg-1.ts?u=')
	assert !media_out.contains(cdn_addr)

	// Segmen: MIME dari sniff (bukan label upstream) + byte asli.
	seg := resolve(base_of(media), playlist_urls(media_text)[0])
	f := fetch_proxied(seg, none, none, none) or { panic('seg: ${err}') }
	assert f.status == 200
	assert f.ctype == 'video/mp2t'
	assert f.body.len == 376
	assert f.body[0] == 0x47
	if _ := f.crange {
		assert false
	}
	// Range diteruskan; content-range upstream ikut balik.
	fr := fetch_proxied(seg, none, none, 'bytes=0-375') or { panic('seg range: ${err}') }
	assert fr.status == 206
	assert (fr.crange or { '' }) == 'bytes 0-375/376'

	// VTT EN: diunduh penuh + di-parse.
	sub := fetch_upstream(config.Config{}, '${base}/cdn/subs.vtt', none, none, none) or {
		panic('vtt: ${err}')
	}
	head, cues := parse_vtt(sub.body.bytestr())
	assert head == 'WEBVTT'
	assert cues.len == 2
	assert (cue_lines(cues[0]) or { []string{} }) == ['Hello']
	assert (cue_lines(cues[1]) or { []string{} }) == ['World']

	srv.stop()
}
