module app

import hash
import net.http
import net.urllib
import os
import time
import x.json2
import internal.config
import internal.errors
import internal.models
import internal.stats
import internal.upstream

// dist_root: root SPA hasil vite build (.gz precompress di sisi file).
const dist_root = 'web/dist'

// App: handler HTTP stateless (config dipegang, state global di internal/*).
pub struct App {
pub mut:
	cfg config.Config
	// dist: isi web/dist yang di-embed (nama relatif → isi berkas, termasuk `.gz`).
	// Kosong → aset dibaca dari disk (mode dev, `web/dist` di samping biner).
	dist map[string]string
	// dist_dir: root aset SPA di disk saat tidak di-embed. Kosong = dist_root.
	dist_dir string
}

// handle routes /api/* ke handler-nya masing-masing, sisanya dilayani SPA.
pub fn (mut a App) handle(req http.Request) http.Response {
	raw := req.url
	path := raw.all_before('?')
	qs := if raw.contains('?') { raw.all_after('?') } else { '' }
	q := parse_query(qs)
	if !path.starts_with('/api/') {
		return spa(req, path, a.dist, a.dist_dir)
	}
	route := path[5..]
	if route.starts_with('fetch/') || route == 'fetch' {
		return a.h_fetch(req, q)
	}
	return match route {
		'search' { a.h_search(q) }
		'catalog' { a.h_catalog(q) }
		'anime' { a.h_anime(q) }
		'episodes' { a.h_episodes(q) }
		'servers' { a.h_servers(q) }
		'sources' { a.h_sources(q) }
		'stream' { a.h_stream(q) }
		'skiptimes' { a.h_skiptimes(q) }
		'comments' { a.h_comments(q) }
		'episode-meta' { a.h_episode_meta(q) }
		'anilist' { a.h_anilist(req) }
		'stats' { h_stats() }
		'subid/batch' { a.h_sub_batch(req) }
		'subid/status' { a.h_sub_status(q) }
		'subid/result' { a.h_sub_result(q) }
		'hls' { a.h_hls(q) }
		else { err_resp(errors.not_found('tidak ada')) }
	}
}

// --- query helpers ---

fn parse_query(qs string) map[string]string {
	mut m := map[string]string{}
	if qs == '' {
		return m
	}
	vals := urllib.parse_query(qs) or { urllib.new_values() }
	for k, arr in vals.to_map() {
		if arr.len > 0 {
			m[k] = arr[0]
		}
	}
	return m
}

fn qstr(q map[string]string, k string) string {
	return q[k] or { '' }
}

fn qopt(q map[string]string, k string) ?string {
	if v := q[k] {
		if v != '' {
			return v
		}
	}
	return none
}

// --- response helpers ---

fn err_resp(err IError) http.Response {
	return json_resp(err.code(), jenc_error(err.msg()))
}

fn json_resp(status int, body string) http.Response {
	mut res := http.Response{
		status_code: status
		body:        body
	}
	res.header.add_custom('content-type', 'application/json') or {}
	return res
}

fn jenc_error(msg string) string {
	mut o := map[string]json2.Any{}
	o['error'] = json2.Any(msg)
	return json2.encode(o, escape_unicode: true)
}

fn enc[T](v T) string {
	return json2.encode(v, escape_unicode: true)
}

fn ok_json(body string) http.Response {
	return json_resp(200, body)
}

// --- mapping upstream → web ---

fn to_out(a json2.Any) models.AnimeOut {
	m := a.as_map()
	return models.AnimeOut{
		id:            upstream.val_str(jg(m, 'id'))
		anilist_id:    jg(m, 'anilistId').i64()
		title_romaji:  upstream.val_str(jg(m, 'titleRomaji'))
		title_english: opt_str(jg(m, 'titleEnglish'))
		episode_count: opt_i64(jg(m, 'episodeCount'))
		cover:         upstream.px(upstream.img_url(jg(m, 'coverImage')))
		banner:        upstream.px(upstream.img_url(jg(m, 'bannerImage')))
		format:        opt_str(jg(m, 'format'))
		season_year:   opt_i64(jg(m, 'seasonYear'))
		average_score: opt_i64(jg(m, 'averageScore'))
		status:        opt_str(jg(m, 'status'))
	}
}

fn outs(items []json2.Any) []models.AnimeOut {
	mut o := []models.AnimeOut{}
	for it in items {
		o << to_out(it)
	}
	return o
}

fn jg(m map[string]json2.Any, k string) json2.Any {
	return m[k] or { json2.null }
}

fn opt_str(v json2.Any) ?string {
	if v is string {
		if v == '' {
			return none
		}
		return v
	}
	return none
}

fn opt_i64(v json2.Any) ?i64 {
	if v is f64 {
		return i64(v)
	}
	if v is i64 {
		return v
	}
	return none
}

// episode_item: upstream /episodes → baris web (judul en > x-jat > ja).
fn episode_item(e json2.Any) models.EpisodeItem {
	t := jg(e.as_map(), 'titles').as_map()
	en := upstream.val_str(jg(t, 'en'))
	ja_romaji := upstream.val_str(jg(t, 'x-jat'))
	ja := upstream.val_str(jg(t, 'ja'))
	mut title := '?'
	if en != '' {
		title = en
	} else if ja_romaji != '' {
		title = ja_romaji
	} else if ja != '' {
		title = ja
	}
	return models.EpisodeItem{
		number:       jg(e.as_map(), 'number').i64()
		title:        title
		title_romaji: ja_romaji
		title_jp:     ja
		img:          upstream.px(upstream.val_str(jg(e.as_map(), 'img')))
	}
}

// --- handlers ---

fn (mut a App) h_search(q map[string]string) http.Response {
	query := qstr(q, 'query').trim_space()
	if query == '' {
		return err_resp(errors.bad_request('query kosong'))
	}
	items := upstream.search(a.cfg, query) or { return err_resp(err) }
	if items.len == 0 {
		return err_resp(errors.not_found('tidak ketemu'))
	}
	return ok_json(enc(outs(items)))
}

fn (mut a App) h_catalog(q map[string]string) http.Response {
	mut sort := qstr(q, 'sort').to_upper()
	if sort == '' {
		sort = 'POPULARITY'
	}
	if sort !in upstream.sort_fields {
		return err_resp(errors.bad_request('sort tidak dikenal'))
	}
	mut dir := qstr(q, 'direction').to_upper()
	if dir == '' {
		dir = 'DESC'
	}
	if dir != 'ASC' && dir != 'DESC' {
		return err_resp(errors.bad_request('direction harus ASC/DESC'))
	}
	mut f := map[string]json2.Any{}
	pick(qopt(q, 'status'), upstream.statuses, 'statusIn', mut f) or { return err_resp(err) }
	pick(qopt(q, 'format'), upstream.formats, 'formatIn', mut f) or { return err_resp(err) }
	pick(qopt(q, 'season'), upstream.seasons, 'seasonIn', mut f) or { return err_resp(err) }
	if v := qopt(q, 'year') {
		y := v.i64()
		f['seasonYearMin'] = json2.Any(y)
		f['seasonYearMax'] = json2.Any(y)
	}
	if g := qopt(q, 'genre') {
		gt := g.trim_space()
		if gt != '' {
			f['genres'] = json2.Any([json2.Any(gt)])
		}
	}
	if s := qopt(q, 'query') {
		st := s.trim_space()
		if st != '' {
			f['query'] = json2.Any(st)
		}
	}
	mut limit := if v := qopt(q, 'limit') { v.int() } else { 18 }
	if limit < 1 {
		limit = 1
	}
	if limit > 30 {
		limit = 30
	}
	mut offset := if v := qopt(q, 'offset') { v.int() } else { 0 }
	if offset < 0 {
		offset = 0
	}
	items := upstream.catalog(a.cfg, f, sort, dir, i64(limit), i64(offset)) or {
		return err_resp(err)
	}
	return ok_json(enc(outs(items)))
}

fn pick(v ?string, allowed []string, key string, mut f map[string]json2.Any) ! {
	s := v or { return }
	u := s.to_upper()
	if u !in allowed {
		return errors.bad_request('${key} tidak dikenal')
	}
	f[key] = json2.Any([json2.Any(u)])
}

fn (mut a App) h_anime(q map[string]string) http.Response {
	id := qopt(q, 'id')
	anilist_id := if ai := qopt(q, 'anilistId') { ?i64(ai.i64()) } else { ?i64(none) }
	if id == none && anilist_id == none {
		return err_resp(errors.bad_request('id atau anilistId wajib'))
	}
	mut section := qstr(q, 'section')
	if section == '' {
		section = 'base'
	}
	v := upstream.anime(a.cfg, id, anilist_id, section) or { return err_resp(err) }
	if v is json2.Null {
		return err_resp(errors.not_found('anime tidak ketemu'))
	}
	return ok_json(json2.encode(v, escape_unicode: true))
}

fn (mut a App) h_episodes(q map[string]string) http.Response {
	id := qstr(q, 'id').trim_space()
	if id == '' {
		return err_resp(errors.bad_request('id kosong'))
	}
	v := upstream.episodes(a.cfg, id) or { return err_resp(err) }
	mut out := []models.EpisodeItem{}
	for e in v.as_array() {
		out << episode_item(e)
	}
	if out.len == 0 {
		return err_resp(errors.not_found('episode kosong'))
	}
	out.sort_with_compare(fn (x &models.EpisodeItem, y &models.EpisodeItem) int {
		return int(x.number - y.number)
	})
	return ok_json(enc(out))
}

fn (mut a App) h_servers(q map[string]string) http.Response {
	id := qstr(q, 'id').trim_space()
	if id == '' {
		return err_resp(errors.bad_request('id kosong'))
	}
	ep := qstr(q, 'ep').i64()
	v := upstream.servers(a.cfg, id, ep) or { return err_resp(err) }
	return ok_json(json2.encode(v, escape_unicode: true))
}

fn (mut a App) h_sources(q map[string]string) http.Response {
	id := qstr(q, 'id').trim_space()
	if id == '' {
		return err_resp(errors.bad_request('id kosong'))
	}
	ep := qstr(q, 'ep').i64()
	mut typ := qstr(q, 'type')
	if typ == '' {
		typ = 'sub'
	}
	mut provider := qstr(q, 'provider')
	if provider == '' {
		provider = a.cfg.provider
	}
	v := upstream.sources(a.cfg, id, ep, typ, provider) or { return err_resp(err) }
	return ok_json(json2.encode(v, escape_unicode: true))
}

fn (mut a App) h_stream(q map[string]string) http.Response {
	id := qstr(q, 'id').trim_space()
	if id == '' {
		return err_resp(errors.bad_request('id kosong'))
	}
	ep := qstr(q, 'ep').i64()
	s, url := a.sources_probe(id, ep) or { return err_resp(err) }
	sub := pick_en_sub(s)
	sub_id := if sub == '' { '' } else { upstream.enqueue_sub(sub) }
	return ok_json(enc(models.StreamResponse{
		stream: '/api/hls?u=${upstream.b64(url)}'
		sub_en: upstream.px(sub)
		sub_id: sub_id
	}))
}

// sources_probe: 3 percobaan (token m3u8 berumur pendek, upstream sesaat 5xx).
fn (mut a App) sources_probe(id string, ep i64) !(json2.Any, string) {
	mut last := IError(errors.upstream('stream gagal'))
	for attempt in 0 .. 3 {
		if attempt > 0 {
			time.sleep(time.Duration(400 * attempt) * time.millisecond)
		}
		s := upstream.sources(a.cfg, id, ep, 'sub', a.cfg.provider) or {
			last = err
			continue
		}
		url := upstream.probe_stream(s) or {
			last = err
			continue
		}
		return s, url
	}
	return last, ''
}

// pick_en_sub: URL VTT trek Inggris (lang bisa "english (cr english)").
fn pick_en_sub(s json2.Any) string {
	for t in jg(s.as_map(), 'tracks').as_array() {
		lang := upstream.val_str(jg(t.as_map(), 'lang')).to_lower()
		if lang.contains('english') {
			return upstream.val_str(jg(t.as_map(), 'url'))
		}
	}
	return ''
}

fn (mut a App) h_skiptimes(q map[string]string) http.Response {
	mal := qstr(q, 'mal').i64()
	ep := qstr(q, 'ep').i64()
	mut length := ?f64(none)
	if l := qopt(q, 'len') {
		length = l.f64()
	}
	v := upstream.skiptimes(a.cfg, mal, ep, length) or { return err_resp(err) }
	return ok_json(json2.encode(v, escape_unicode: true))
}

fn (mut a App) h_comments(q map[string]string) http.Response {
	media := qstr(q, 'media').trim_space()
	if media == '' {
		return err_resp(errors.bad_request('media kosong'))
	}
	offset := u32(qstr(q, 'offset').int())
	mut sort := qstr(q, 'sort')
	if sort == '' {
		sort = 'Top'
	}
	limit := if v := qopt(q, 'limit') { u32(v.int()) } else { u32(10) }
	v := upstream.comments(a.cfg, media, offset, sort, limit) or { return err_resp(err) }
	return ok_json(json2.encode(v, escape_unicode: true))
}

fn (mut a App) h_episode_meta(q map[string]string) http.Response {
	anilist_id := qstr(q, 'anilistId').i64()
	ep := qstr(q, 'ep').i64()
	v := upstream.episode_meta(a.cfg, anilist_id, ep) or { return err_resp(err) }
	return ok_json(json2.encode(v, escape_unicode: true))
}

fn (mut a App) h_anilist(req http.Request) http.Response {
	b := upstream.jdec(req.data)
	m := b.as_map()
	query := upstream.val_str(jg(m, 'query')).trim_space()
	if query == '' {
		return err_resp(errors.bad_request('query kosong'))
	}
	mut vars := ?json2.Any(none)
	if v := m['variables'] {
		vars = v
	}
	v := upstream.anilist(a.cfg, query, vars) or { return err_resp(err) }
	return ok_json(json2.encode(v, escape_unicode: true))
}

fn h_stats() http.Response {
	sn := stats.snapshot()
	mut o := map[string]json2.Any{}
	o['memMb'] = json2.Any(sn.mem_mb)
	o['peakMemMb'] = json2.Any(sn.peak_mem)
	o['cpu'] = json2.Any(sn.cpu)
	o['peakCpu'] = json2.Any(sn.peak_cpu)
	return ok_json(json2.encode(o, escape_unicode: true))
}

fn (mut a App) h_sub_batch(req http.Request) http.Response {
	b := upstream.jdec(req.data)
	m := b.as_map()
	id := upstream.val_str(jg(m, 'id'))
	at := jg(m, 'at').f64()
	st := upstream.sub_batch(a.cfg, id, at) or { return err_resp(err) }
	return ok_json(enc(st))
}

fn (mut a App) h_sub_status(q map[string]string) http.Response {
	id := qstr(q, 'id')
	at := qstr(q, 'at').f64()
	st := upstream.sub_status(id, at) or { return err_resp(err) }
	return ok_json(enc(st))
}

fn (mut a App) h_sub_result(q map[string]string) http.Response {
	id := qstr(q, 'id')
	only := qstr(q, 'only')
	body := upstream.sub_result(id, only) or { return err_resp(err) }
	mut res := http.Response{
		status_code: 200
		body:        body
	}
	res.header.add_custom('content-type', 'text/vtt') or {}
	return res
}

// h_fetch: proxy generik /api/fetch[/name]?u=<base64url> (gambar, sub, segmen).
fn (mut a App) h_fetch(req http.Request, q map[string]string) http.Response {
	url := upstream.decode_target(qstr(q, 'u')) or { return err_resp(err) }
	rng := req.header.get(.range)
	f := upstream.fetch_proxied(url, qopt(q, 'origin'), qopt(q, 'referer'), rng) or {
		return err_resp(err)
	}
	mut res := http.Response{
		status_code: f.status
		body:        f.body.bytestr()
	}
	res.header.add_custom('content-type', f.ctype) or {}
	if cr := f.crange {
		res.header.add_custom('content-range', cr) or {}
	}
	if cl := f.clen {
		res.header.add_custom('content-length', cl) or {}
	}
	if f.ctype.starts_with('image/') {
		res.header.add_custom('cache-control', 'public, max-age=31536000, immutable') or {}
	}
	return res
}

// h_hls: proxy playlist — isi ditulis ulang ke /api/* (base64).
fn (mut a App) h_hls(q map[string]string) http.Response {
	url := upstream.decode_target(qstr(q, 'u')) or { return err_resp(err) }
	origin := qopt(q, 'origin')
	referer := qopt(q, 'referer')
	f := upstream.fetch_upstream(a.cfg, url, origin, referer, none) or { return err_resp(err) }
	text := f.body.bytestr()
	if !text.contains('#EXTM3U') {
		return err_resp(errors.upstream('upstream bukan playlist (kemungkinan diblokir)'))
	}
	base := upstream.base_of(url)
	out := upstream.rewrite_playlist(base, text, origin, referer)
	mut res := http.Response{
		status_code: 200
		body:        out
	}
	res.header.add_custom('content-type', 'application/vnd.apple.mpegurl') or {}
	return res
}

// --- SPA ---

// spa melayani aset SPA: dari peta embed (produksi) atau dari disk (dev), dengan
// negosiasi gzip (`.gz` precompress vite) + ETag/304. Path tak dikenal → index.html.
fn spa(req http.Request, path string, dist map[string]string, dir string) http.Response {
	root := if dir == '' { dist_root } else { dir }
	mut key := path.trim_left('/')
	if key == '' {
		key = 'index.html'
	}
	if !asset_exists(dist, root, key) {
		key = 'index.html' // fallback routing sisi klien
		if !asset_exists(dist, root, key) {
			return http.Response{
				status_code: 404
				body:        'not found'
			}
		}
	}
	ae := req.header.get(.accept_encoding) or { '' }
	want_gzip := ae.split(',').any(it.trim_space().all_before(';').trim_space() == 'gzip')
	has_gz := asset_exists(dist, root, '${key}.gz')
	use_gz := want_gzip && has_gz
	data := if use_gz { asset_bytes(dist, root, '${key}.gz') } else { asset_bytes(dist, root, key) }
	etag := '"${u64(hash.sum64(data, 0)):x}-${data.len}${if use_gz { '-gz' } else { '' }}"'
	mut res := http.Response{}
	if (req.header.get(.if_none_match) or { '' }) == etag {
		res.status_code = 304
		res.header.add_custom('etag', etag) or {}
		if has_gz {
			res.header.add_custom('vary', 'Accept-Encoding') or {}
		}
		return res
	}
	res.status_code = 200
	res.header.add_custom('content-type', spa_mime(key)) or {}
	res.header.add_custom('etag', etag) or {}
	ctl := if key.starts_with('assets/') {
		'public, max-age=31536000, immutable'
	} else {
		'no-cache'
	}
	res.header.add_custom('cache-control', ctl) or {}
	if use_gz {
		res.header.add_custom('content-encoding', 'gzip') or {}
	}
	if has_gz {
		res.header.add_custom('vary', 'Accept-Encoding') or {}
	}
	if req.method != .head {
		res.body = data.bytestr()
	}
	return res
}

// asset_exists: berkas SPA ada di peta embed, atau (bila peta kosong) di disk.
fn asset_exists(dist map[string]string, root string, key string) bool {
	if dist.len > 0 {
		return key in dist
	}
	return os.exists('${root}/${key}')
}

// asset_bytes: isi berkas SPA (embed menang; kalau kosong → disk).
fn asset_bytes(dist map[string]string, root string, key string) []u8 {
	if dist.len > 0 {
		return (dist[key] or { '' }).bytes()
	}
	return os.read_bytes('${root}/${key}') or { []u8{} }
}

fn spa_mime(p string) string {
	ext := p.all_after_last('.').to_lower()
	return match ext {
		'html' { 'text/html; charset=utf-8' }
		'js', 'mjs' { 'text/javascript; charset=utf-8' }
		'css' { 'text/css; charset=utf-8' }
		'json' { 'application/json' }
		'svg' { 'image/svg+xml' }
		'png' { 'image/png' }
		'jpg', 'jpeg' { 'image/jpeg' }
		'webp' { 'image/webp' }
		'gif' { 'image/gif' }
		'ico' { 'image/x-icon' }
		'woff2' { 'font/woff2' }
		'woff' { 'font/woff' }
		'ttf' { 'font/ttf' }
		'txt' { 'text/plain; charset=utf-8' }
		'webmanifest', 'map' { 'application/json' }
		else { 'application/octet-stream' }
	}
}
