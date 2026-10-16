module upstream

import net.http
import x.json2
import internal.config
import internal.errors

// Fallback ekstensi bila sniff tak kenal isi. Label CDN sering bohong
// (segmen TS dinamai .jpg) → sniff selalu menang.
pub fn mime_for(url string) ?string {
	path := url.split_any('?#')[0]
	ext := path.all_after_last('.').to_lower()
	return match ext {
		'ts' { 'video/mp2t' }
		'm4s' { 'video/iso.segment' }
		'mp4' { 'video/mp4' }
		'webm' { 'video/webm' }
		'vtt' { 'text/vtt' }
		'm3u8' { 'application/vnd.apple.mpegurl' }
		'mpd' { 'application/dash+xml' }
		'jpg', 'jpeg' { 'image/jpeg' }
		'png' { 'image/png' }
		'webp' { 'image/webp' }
		'gif' { 'image/gif' }
		'avif' { 'image/avif' }
		'aac' { 'audio/aac' }
		'mp3' { 'audio/mpeg' }
		else { none }
	}
}

// sniff: tebak tipe dari magic bytes (menang atas ekstensi/label CDN).
pub fn sniff(b []u8) ?string {
	if b.len >= 6 && b[..6] == 'WEBVTT'.bytes() {
		return 'text/vtt'
	} else if b.len >= 7 && b[..7] == '#EXTM3U'.bytes() {
		return 'application/vnd.apple.mpegurl'
	} else if b.len >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF {
		return 'image/jpeg'
	} else if b.len >= 4 && b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4E && b[3] == 0x47 {
		return 'image/png'
	} else if b.len > 12 && b[..4] == 'RIFF'.bytes() && b[8..12] == 'WEBP'.bytes() {
		return 'image/webp'
	} else if b.len >= 4 && b[..4] == 'GIF8'.bytes() {
		return 'image/gif'
	} else if b.len > 12 && b[4..8] == 'ftyp'.bytes() && (b[8..12] == 'avif'.bytes()
		|| b[8..12] == 'avis'.bytes()) {
		return 'image/avif' // AVIF dicek dulu: brand-nya juga ftyp
	} else if b.len > 8 && b[4..8] == 'ftyp'.bytes() {
		return 'video/mp4'
	} else if b.len > 0 && b[0] == 0x47 && (b.len < 189 || b[188] == 0x47) {
		return 'video/mp2t' // sync-byte TS; chunk pertama bisa <188B
	}
	return none
}

fn def_headers(origin ?string, referer ?string) (string, string) {
	o := if v := origin { v } else { 'https://megaplay.buzz' }
	r := if v := referer { v } else { 'https://megaplay.buzz/' }
	return o, r
}

pub struct Fetched {
pub:
	status int
	ctype  string
	body   []u8
}

pub struct Streamed {
pub:
	status int
	ctype  string
	crange ?string
	clen   ?string
	body   []u8
}

// fetch_with: GET dengan header Origin/Referer (+Range/identity) tetap.
fn fetch_with(url string, origin string, referer string, range ?string, identity bool) !http.Response {
	mut h := http.new_header()
	h.add_custom('Origin', origin) or {}
	h.add_custom('Referer', referer) or {}
	if identity {
		h.add_custom('Accept-Encoding', 'identity') or {}
	}
	if g := range {
		h.add_custom('Range', g) or {}
	}
	return http.fetch(url: url, method: .get, header: h, user_agent: config.browser_ua) or {
		errors.upstream('${err}')
	}
}

// fetch_upstream: unduh penuh, ctype apa adanya dari upstream (dipakai hls + sub).
pub fn fetch_upstream(cfg config.Config, url string, origin ?string, referer ?string, range ?string) !Fetched {
	o, f := def_headers(origin, referer)
	res := fetch_with(url, o, f, range, false)!
	ctype := res.header.get(.content_type) or { 'application/octet-stream' }
	return Fetched{
		status: res.status_code
		ctype:  ctype
		body:   res.body.bytes()
	}
}

// fetch_proxied: minta identity (sniff byte asli),
// sniff isi dulu, fallback ekstensi → label upstream → octet-stream.
pub fn fetch_proxied(url string, origin ?string, referer ?string, range ?string) !Streamed {
	o, f := def_headers(origin, referer)
	res := fetch_with(url, o, f, range, true)!
	body := res.body.bytes()
	uct := res.header.get(.content_type)
	t := if s := sniff(body) {
		s
	} else if s := mime_for(url) {
		s
	} else if c := uct {
		c
	} else {
		'application/octet-stream'
	}
	return Streamed{
		status: res.status_code
		ctype:  t
		crange: res.header.get(.content_range)
		clen:   res.header.get(.content_length)
		body:   body
	}
}

// seg_path: path proxy segmen berakhiran ekstensi (player deteksi tipe dari ext).
fn seg_path(abs string, ext string) string {
	name := abs.split_any('?#')[0].all_after_last('/')
	mut stem := name.all_before('.')
	if stem == '' {
		stem = 'seg'
	}
	return '/api/fetch/${stem}${ext}?u=${b64(abs)}'
}

// resolve: URI relatif → absolut terhadap base.
pub fn resolve(base string, r string) string {
	if r.starts_with('http') {
		return r
	}
	return '${base}/${r.trim_left('/')}'
}

// is_m3u8: cek ekstensi abaikan query/fragment (?token=…).
pub fn is_m3u8(u string) bool {
	return u.split_any('?#')[0].ends_with('.m3u8')
}

// playlist_urls: URI dari playlist (master: setelah EXT-X-STREAM-INF; media: non-#).
pub fn playlist_urls(t string) []string {
	l := t.split('\n').map(it.trim_space()).filter(it != '')
	mut o := []string{}
	for i in 0 .. l.len - 1 {
		if l[i].starts_with('#EXT-X-STREAM-INF') && !l[i + 1].starts_with('#') {
			o << l[i + 1]
		}
	}
	if o.len == 0 {
		o = l.filter(!it.starts_with('#')).clone()
	}
	return o
}

// rewrite_playlist: semua URI → /api/hls atau /api/fetch (base64).
pub fn rewrite_playlist(base string, t string, origin ?string, referer ?string) string {
	mut suf := ''
	if o := origin {
		if r := referer {
			if !o.contains('&') && !o.contains('?') && !r.contains('&') && !r.contains('?') {
				suf = '&origin=${o}&referer=${r}'
			}
		}
	}
	ext := if t.contains('#EXT-X-MAP') { '.mp4' } else { '.ts' }
	mut out := []string{}
	for line in t.split('\n') {
		l := line.trim_space()
		if l == '' {
			out << ''
			continue
		}
		if l.starts_with('#') {
			mut o := ''
			mut rest := l
			for {
				i := rest.index('URI="') or { break }
				start := i + 5
				e := rest[start..].index('"') or { break }
				abs := resolve(base, rest[start..start + e])
				o += rest[..start]
				if is_m3u8(abs) {
					o += '/api/hls?u=${b64(abs)}${suf}'
				} else {
					o += '/api/fetch?u=${b64(abs)}${suf}'
				}
				o += '"'
				rest = rest[start + e + 1..]
			}
			o += rest
			out << o
		} else {
			abs := resolve(base, l)
			if is_m3u8(abs) {
				out << '/api/hls?u=${b64(abs)}${suf}'
			} else {
				out << seg_path(abs, ext)
			}
		}
	}
	return out.join('\n')
}

// base_of: direktori dari sebuah URL.
pub fn base_of(u string) string {
	return u.all_before_last('/')
}

fn sstr(v json2.Any, d string) string {
	if v is string {
		return v
	}
	return d
}

// probe_stream: master m3u8 → resolve ke segmen video, cuplik 64KB buktikan hidup.
// Sekuensial + early-exit (request CDN paling sedikit). Gagal semua → NotFound.
pub fn probe_stream(s json2.Any) !string {
	m := s.as_map()
	hdr := jget(m, 'headers').as_map()
	origin := sstr(jget(hdr, 'Origin'), 'https://megaplay.buzz')
	referer := sstr(jget(hdr, 'Referer'), 'https://megaplay.buzz/')
	for x in jget(m, 'sources').as_array() {
		u := sstr(jget(x.as_map(), 'url'), '')
		if u == '' {
			continue
		}
		mt := fetch_text(u, origin, referer, none) or { continue }
		if !mt.contains('#EXTM3U') {
			continue
		}
		urls := playlist_urls(mt)
		media := if urls.len > 0 { resolve(base_of(u), urls.last()) } else { u }
		mut seg := media
		if is_m3u8(media) {
			t := fetch_text(media, origin, referer, none) or { continue }
			su := playlist_urls(t)
			if su.len > 0 {
				seg = resolve(base_of(media), su[0])
			}
		}
		if probe_bytes(seg, origin, referer) > 0 {
			return u
		}
	}
	return errors.not_found('no playable source')
}

// fetch_text: GET teks (status harus 2xx), gagal → error.
fn fetch_text(url string, origin string, referer string, range ?string) !string {
	res := fetch_with(url, origin, referer, range, false)!
	if res.status_code < 200 || res.status_code >= 300 {
		return error('status ${res.status_code}')
	}
	return res.body
}

// probe_bytes: Range 64KB → jumlah byte (0 = mati).
fn probe_bytes(url string, origin string, referer string) int {
	res := fetch_with(url, origin, referer, 'bytes=0-65535', false) or { return 0 }
	if res.status_code != 200 && res.status_code != 206 {
		return 0
	}
	return res.body.len
}
