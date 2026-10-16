module upstream

import time
import x.json2

// parse_vtt + cue_lines + build_vtt/build_vtt_id (fallback EN).
fn test_parse_build_vtt() {
	t := 'WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello\n\nNOTE komentar\n\n00:00:03.000 --> 00:00:04.000\nWorld\n'
	head, cues := parse_vtt(t)
	assert head == 'WEBVTT'
	assert cues.len == 3
	ls := cue_lines(cues[0]) or { panic('cue 0 harus punya baris') }
	assert ls == ['Hello']
	if _ := cue_lines(cues[1]) {
		assert false
	}
	mut ids := map[int]string{}
	ids[0] = 'Halo'
	full := build_vtt(head, cues, ids)
	assert full.contains('Halo')
	assert full.contains('World') // cue belum diterjemahkan → fallback EN
	only := build_vtt_id(head, cues, ids)
	assert only.contains('Halo')
	assert !only.contains('World')
}

// ts_start/ts_end parse HH:MM:SS.mmm.
fn test_ts() {
	line := '00:01:02.500 --> 00:01:03.750'
	assert ts_start(line) == 62.5
	assert ts_end(line) == 63.75
}

// next_at_of: done penuh / kosong → -1.
fn mkjob(ids []int, n int) SubJob {
	mut cues := [][]string{}
	mut dlg := []int{}
	for i in 0 .. n {
		cues << ['00:00:${i:02}.000 --> 00:00:${i + 1:02}.000', 'L${i}']
		dlg << i
	}
	mut idm := map[int]string{}
	for i in ids {
		idm[i] = 'ID${i}'
	}
	return SubJob{
		state: 'ready'
		head:  'WEBVTT'
		cues:  cues
		dlg:   dlg
		ids:   idm
	}
}

fn test_next_at_sintetis() {
	assert next_at_of(mkjob([0, 1], 2), 0.0) == -1.0
	assert next_at_of(mkjob([], 0), 0.0) == -1.0
}

// decode_target: blok SSRF + hanya http(s).
fn test_decode_target_ssrf() {
	if _ := decode_target(b64('http://127.0.0.1/x')) {
		assert false
	}
	if _ := decode_target(b64('http://169.254.169.254/latest')) {
		assert false
	}
	if _ := decode_target(b64('ftp://example.com/y')) {
		assert false
	}
	s := decode_target(b64('https://example.com/a.m3u8')) or { '' }
	assert s == 'https://example.com/a.m3u8'
}

// playlist_urls: master (EXT-X-STREAM-INF) vs media (baris non-#).
fn test_playlist_urls() {
	master := '#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nindex-1.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2\nindex-2.m3u8\n'
	assert playlist_urls(master) == ['index-1.m3u8', 'index-2.m3u8']
	media := '#EXTM3U\n#EXTINF:6.0,\nseg-1.ts\n#EXTINF:6.0,\nseg-2.ts\n'
	assert playlist_urls(media) == ['seg-1.ts', 'seg-2.ts']
}

// img_url: string langsung, atau object {large,medium,url,...}; kosong → ''.
fn test_img_url_shapes() {
	assert img_url(jdec('"https://cdn/x.jpg"')) == 'https://cdn/x.jpg'
	assert img_url(jdec('{"large":"https://cdn/l.jpg"}')) == 'https://cdn/l.jpg'
	assert img_url(jdec('{"medium":"","url":"https://cdn/u.jpg"}')) == 'https://cdn/u.jpg'
	assert img_url(jdec('{}')) == ''
	assert img_url(json2.null) == ''
}

// rewrite_playlist: varian → /api/hls; segmen → /api/fetch/<nama>.ts|mp4; URI= → /api/fetch.
fn test_rewrite_playlist() {
	base := 'https://cdn.example.com/hls'
	master := '#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100\nlow.m3u8\n'
	out := rewrite_playlist(base, master, none, none)
	assert out.contains('/api/hls?u=')
	media := '#EXTM3U\n#EXTINF:6,\nseg0.ts\n#EXT-X-MAP:URI="init.mp4"\n'
	out2 := rewrite_playlist(base, media, none, none)
	assert out2.contains('/api/fetch/seg0.mp4?u=')
	assert out2.contains('URI="/api/fetch?u=')
}

// sniff: magic bytes menang atas ekstensi CDN yang bohong; mime_for fallback ekstensi.
fn test_sniff_mime() {
	assert (sniff('WEBVTT\n\n'.bytes()) or { '' }) == 'text/vtt'
	assert (sniff('#EXTM3U\n'.bytes()) or { '' }) == 'application/vnd.apple.mpegurl'
	assert (sniff([u8(0x47), 0x00]) or { '' }) == 'video/mp2t'
	assert (sniff([u8(0), 0, 0, 24, `f`, `t`, `y`, `p`, `m`, `p`, `4`, `2`]) or { '' }) == 'video/mp4'
	assert (sniff([u8(0xFF), 0xD8, 0xFF, 0x00]) or { '' }) == 'image/jpeg'
	assert (sniff('GIF89a'.bytes()) or { '' }) == 'image/gif' // regresi: 'G'=0x47 jangan sampai kena cabang TS
	assert (sniff('GIF87a'.bytes()) or { '' }) == 'image/gif'
	// AVIF = box ftyp + brand avif/avis → jangan sampai dikira video/mp4.
	assert (sniff([u8(0), 0, 0, 32, `f`, `t`, `y`, `p`, `a`, `v`, `i`, `f`, 0, 0]) or { '' }) == 'image/avif'
	assert (sniff([u8(0), 0, 0, 32, `f`, `t`, `y`, `p`, `a`, `v`, `i`, `s`, 0, 0]) or { '' }) == 'image/avif'
	if _ := sniff('acak'.bytes()) {
		assert false
	}
	assert (mime_for('https://x/seg0.ts?t=1') or { '' }) == 'video/mp2t'
	assert (mime_for('https://x/a.m3u8') or { '' }) == 'application/vnd.apple.mpegurl'
	assert (mime_for('https://x/cover.avif') or { '' }) == 'image/avif'
	assert (mime_for('https://x/audio.aac') or { '' }) == 'audio/aac'
	assert (mime_for('https://x/audio.mp3') or { '' }) == 'audio/mpeg'
	assert is_m3u8('https://x/low.m3u8?token=abc')
	assert !is_m3u8('https://x/seg0.ts')
}

// pick_key: round-robin berputar, lompati key cooldown, gagal-cepat bila semua cooldown.
fn test_gemini_pick_key_round_robin() {
	keys := ['a', 'b', 'c']
	mut got := []string{}
	for _ in 0 .. 3 {
		got << (pick_key(keys) or { '' })
	}
	assert got == ['a', 'b', 'c'] // kursor mulai dari 0
	// Key cooldown dilompati (kursor lanjut → "a", dilewati → "b").
	lock g_gem.mu {
		g_gem.cd['a'] = time.now().unix_milli() + 30000
	}
	assert (pick_key(keys) or { '' }) == 'b'
	// Semua key cooldown → gagal-cepat RateLimited (429).
	xy := ['x', 'y']
	lock g_gem.mu {
		for k in xy {
			g_gem.cd[k] = time.now().unix_milli() + 30000
		}
	}
	if _ := pick_key(xy) {
		assert false
	} else {
		assert err.code() == 429
	}
	if _ := pick_key([]) {
		assert false
	} else {
		assert err.code() == 400
	}
	lock g_gem.mu {
		g_gem.cd.clear()
	}
}

// gemini_text / gemini_cooldown_secs: bentuk respons generateContent + RESOURCE_EXHAUSTED.
fn test_gemini_shapes_sintetis() {
	// 200: teks = gabungan semua part.
	ok :=
		jdec('{"candidates":[{"content":{"parts":[{"text":"[0] Halo"},{"text":"\\n[1] Pagi"}]}}]}')
	assert (gemini_text(ok) or { '' }) == '[0] Halo\n[1] Pagi'
	if _ := gemini_text(jdec('{}')) {
		assert false
	}
	// 429 + RESOURCE_EXHAUSTED + retryDelay → cooldown ikut RetryInfo.
	rl :=
		jdec('{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[{"retryDelay":"35s"}]}}')
	assert (gemini_cooldown_secs(429, rl) or { 0 }) == 35
	assert (gemini_cooldown_secs(200, rl) or { 0 }) == 35 // status bohong, body jujur
	assert (gemini_cooldown_secs(429, jdec('{}')) or { 0 }) == 20
	if _ := gemini_cooldown_secs(200, jdec('{"candidates":[]}')) {
		assert false
	}
	if _ := gemini_cooldown_secs(503, jdec('{"error":{"status":"UNAVAILABLE"}}')) {
		assert false
	}
}
