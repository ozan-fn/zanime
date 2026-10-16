module upstream

import encoding.base64
import net.http
import x.json2
import internal.cache
import internal.config
import internal.errors

const magic_catalog = 'query CatalogAnime($filter: AnimeCatalogFilterInput $sort: [AnimeSortInput!] $limit: Int $offset: Int){catalogAnime(filter:$filter sort:$sort limit:$limit offset:$offset){items{id anilistId titleRomaji titleEnglish episodeCount coverImage bannerImage format seasonYear averageScore status}}}'

// TTL cache Redis (detik): sehari. Konten yang tampil tak boleh basi, jadi semua
// entri kedaluwarsa sendiri dan diisi ulang dari upstream.
pub const ttl = u64(86400)

// Nilai sah `AnimeSortField` (dari introspeksi skema upstream).
pub const sort_fields = ['POPULARITY', 'TRENDING', 'AVERAGE_SCORE', 'MEAN_SCORE', 'FAVOURITES',
	'SEASON_YEAR', 'UPDATED_AT', 'CREATED_AT', 'EPISODE_COUNT', 'DURATION', 'TITLE_ENGLISH',
	'TITLE_ROMAJI', 'NEXT_AIRING_AT', 'SUB_COUNT', 'DUB_COUNT']

pub const statuses = ['FINISHED', 'RELEASING', 'NOT_YET_RELEASED', 'CANCELLED', 'HIATUS']

pub const formats = ['TV', 'TV_SHORT', 'MOVIE', 'SPECIAL', 'OVA', 'ONA', 'MUSIC']

pub const seasons = ['WINTER', 'SPRING', 'SUMMER', 'FALL']

pub const anime_base = 'query AnimeDetailBase($id: String, $anilistId: Int){anime(id:$id anilistId:$anilistId){id anilistId malId titleRomaji titleEnglish titles coverImage bannerImage backdropUrl description episodeCount status duration genres source format seasonYear season averageScore popularity studios}}'

pub const anime_seasons = 'query AnimeDetailSeasons($id: String, $anilistId: Int){anime(id:$id anilistId:$anilistId){id seasons{animeId anilistId title image coverImage episodeCount type rating}}}'

pub const anime_relations = 'query AnimeDetailRelations($id: String, $anilistId: Int){anime(id:$id anilistId:$anilistId){id relations{animeId anilistId title image coverImage episodeCount type rating seasonYear status}}}'

pub const anime_recs = 'query AnimeDetailRecommendations($id: String, $anilistId: Int){anime(id:$id anilistId:$anilistId){id recommendations{animeId anilistId title image coverImage episodeCount type rating}}}'

pub const anime_chars = 'query AnimeDetailCharacters($id: String, $anilistId: Int){anime(id:$id anilistId:$anilistId){id characters}}'

// b64: base64url tanpa padding (URL_SAFE_NO_PAD).
pub fn b64(u string) string {
	return base64.url_encode_str(u)
}

// jenc / jdec: bungkus encode/decode json2.Any.
pub fn jenc(v json2.Any) string {
	return json2.encode(v, escape_unicode: true)
}

pub fn jdec(s string) json2.Any {
	return json2.decode[json2.Any](s) or { json2.null }
}

// sjson: build json2.Any dari literal JSON yang kita tulis sendiri.
fn sjson(literal string) json2.Any {
	return json2.decode[json2.Any](literal) or { json2.null }
}

// jget: indeks objek JSON; hilang → null.
fn jget(m map[string]json2.Any, key string) json2.Any {
	return m[key] or { json2.null }
}

// img_url: string langsung, atau object {extraLarge,large,medium,...}.
pub fn img_url(v json2.Any) string {
	if v is string {
		return v
	}
	if v is json2.Null {
		return ''
	}
	m := v.as_map()
	for k in ['extraLarge', 'large', 'medium', 'image', 'url', 'src', 'cover', 'banner'] {
		x := jget(m, k)
		if x is string {
			s := x
			if s != '' {
				return s
			}
		}
	}
	return ''
}

pub fn px(u string) string {
	if u == '' {
		return ''
	}
	return '/api/fetch?u=${b64(u)}'
}

// blocked_host: privat/loopback/metadata dilarang (anti-SSRF).
fn blocked_host(h string) bool {
	hl := h.to_lower()
	if hl == 'localhost' || hl.ends_with('.localhost') || hl.ends_with('.local')
		|| hl.ends_with('.internal') || hl == '0.0.0.0' || hl == '::1' || hl == '[::1]'
		|| hl.contains('metadata.google') {
		return true
	}
	p := hl.split('.')
	if p.len != 4 {
		return false
	}
	if p[0] == '127' || p[0] == '10' {
		return true
	}
	if p[0] == '192' && p[1] == '168' {
		return true
	}
	if p[0] == '169' && p[1] == '254' {
		return true
	}
	if p[0] == '172' && p[1].int() >= 16 && p[1].int() <= 31 {
		return true
	}
	return false
}

// decode_target mengubah base64url `u` jadi URL asli, dengan blok SSRF.
pub fn decode_target(u string) !string {
	s := base64.url_decode_str(u)
	if !(s.starts_with('https://') || s.starts_with('http://')) {
		return errors.bad_request('host tidak diizinkan')
	}
	mut host := ''
	rest := s.all_after('://')
	if j := rest.index('/') {
		host = rest[..j]
	} else {
		host = rest
	}
	if k := host.last_index('@') {
		host = host[k + 1..]
	}
	if host == '' || blocked_host(host) {
		return errors.bad_request('host tidak diizinkan')
	}
	return s
}

fn headers_with(cfg config.Config, extra map[string]string) http.Header {
	mut h := http.new_header()
	h.add_custom('Referer', cfg.referer) or {}
	h.add_custom('Origin', cfg.origin) or {}
	for k, v in extra {
		h.add_custom(k, v) or {}
	}
	return h
}

// do_get: GET upstream dengan UA/Referer/Origin tetap + header ekstra.
fn do_get(cfg config.Config, url string, extra map[string]string) !http.Response {
	return http.fetch(
		url:        url
		method:     .get
		header:     headers_with(cfg, extra)
		user_agent: cfg.user_agent
	) or { errors.upstream('${err}') }
}

// do_post_json: POST JSON ke upstream.
fn do_post_json(cfg config.Config, url string, body string) !http.Response {
	mut h := headers_with(cfg, map[string]string{})
	h.add_custom('Content-Type', 'application/json') or {}
	return http.fetch(
		url:        url
		method:     .post
		data:       body
		header:     h
		user_agent: cfg.user_agent
	) or { errors.upstream('${err}') }
}

// gql: POST GraphQL ke upstream animex, balas seluruh respons JSON.
fn gql(cfg config.Config, query string, vars map[string]json2.Any) !json2.Any {
	mut body := map[string]json2.Any{}
	body['query'] = json2.Any(query)
	body['variables'] = json2.Any(vars)
	r := do_post_json(cfg, cfg.graphql_url, jenc(body))!
	return jdec(r.body)
}

// get_json: GET upstream JSON.
fn get_json(cfg config.Config, url string) !json2.Any {
	r := do_get(cfg, url, map[string]string{})!
	return jdec(r.body)
}

// val_str: string dari JSON (angka ikut dikonversi).
pub fn val_str(v json2.Any) string {
	if v is string {
		return v
	}
	if v is f64 {
		return v.str()
	}
	if v is i64 {
		return v.str()
	}
	return ''
}

fn val_i64(v json2.Any) i64 {
	return v.i64()
}

fn val_opt_str(v json2.Any) ?string {
	if v is string {
		if v == '' {
			return none
		}
		return v
	}
	return none
}

fn val_opt_i64(v json2.Any) ?i64 {
	if v is f64 {
		return i64(v)
	}
	if v is i64 {
		return v
	}
	return none
}

// items_of: blok items katalog → banyak objek item (null = skip). null di sini
// tanda upstream 502 (query rusak / limit > 30).
pub fn items_of(v json2.Any) ![]json2.Any {
	if v is json2.Null {
		return errors.upstream('invalid type: null')
	}
	mut out := []json2.Any{}
	for x in v.as_array() {
		if x is json2.Null {
			continue
		}
		out << x
	}
	return out
}

// catalog: filter/sort upstream apa adanya.
pub fn catalog(cfg config.Config, filter json2.Any, sort string, direction string, limit i64, offset i64) ![]json2.Any {
	key := 'catalog:${sort}:${direction}:${limit}:${offset}:${jenc(filter)}'
	if h := cache.get(key) {
		return items_of(jdec(h))!
	}
	mut vars := map[string]json2.Any{}
	vars['filter'] = filter
	vars['sort'] = sjson('[{"field":"${sort}","direction":"${direction}"}]')
	vars['limit'] = json2.Any(limit)
	vars['offset'] = json2.Any(offset)
	r := gql(cfg, magic_catalog, vars)!
	o := r.as_map()
	d := jget(o, 'data').as_map()
	col := jget(d, 'catalogAnime').as_map()
	items := jget(col, 'items')
	hits := items_of(items)!
	cache.set(key, jenc(items), ttl)
	return hits
}

pub fn search(cfg config.Config, title string) ![]json2.Any {
	mut f := map[string]json2.Any{}
	f['query'] = json2.Any(title)
	return catalog(cfg, json2.Any(f), 'POPULARITY', 'DESC', 24, 0)
}

pub fn episodes(cfg config.Config, id string) !json2.Any {
	key := 'episodes:${id}'
	if h := cache.get(key) {
		return jdec(h)
	}
	v := get_json(cfg, '${cfg.api_base}/episodes?id=${id}')!
	cache.set(key, jenc(v), ttl)
	return v
}

// sources sengaja **tidak** di-cache: URL m3u8 dari CDN bertanda tangan +
// berkedaluwarsa (`?token=<exp>.<hmac>`), entri cache basi = playback mati.
pub fn sources(cfg config.Config, id string, ep i64, typ string, provider string) !json2.Any {
	return get_json(cfg,
		'${cfg.api_base}/sources?id=${id}&epNum=${ep}&type=${typ}&providerId=${provider}')
}

pub fn servers(cfg config.Config, id string, ep i64) !json2.Any {
	key := 'servers:${id}:${ep}'
	if h := cache.get(key) {
		return jdec(h)
	}
	v := get_json(cfg, '${cfg.api_base}/servers?id=${id}&epNum=${ep}')!
	cache.set(key, jenc(v), ttl)
	return v
}

pub fn anime(cfg config.Config, id ?string, anilist_id ?i64, section string) !json2.Any {
	mut sid := '-'
	mut sal := '-'
	if x := id {
		sid = x
	}
	if y := anilist_id {
		sal = y.str()
	}
	key := 'anime:${section}:${sid}:${sal}'
	if h := cache.get(key) {
		return jdec(h)
	}
	q := match section {
		'seasons' { anime_seasons }
		'relations' { anime_relations }
		'recommendations' { anime_recs }
		'characters' { anime_chars }
		else { anime_base }
	}

	mut vars := map[string]json2.Any{}
	vars['id'] = opt_str(id)
	vars['anilistId'] = opt_i64(anilist_id)
	r := gql(cfg, q, vars)!
	o := r.as_map()
	d := jget(o, 'data').as_map()
	an := jget(d, 'anime')
	errs := jget(o, 'errors')
	if an is json2.Null && errs !is json2.Null {
		eprintln('graphql anime gagal: ${jenc(errs)}')
	}
	if an !is json2.Null {
		cache.set(key, jenc(an), ttl)
	}
	return an
}

pub fn skiptimes(cfg config.Config, mal i64, ep i64, len ?f64) !json2.Any {
	mut lens := ''
	if l := len {
		lens = '&episodeLength=${l}'
	}
	key := 'skiptimes:${mal}:${ep}:${len or { 0.0 }}'
	if h := cache.get(key) {
		return jdec(h)
	}
	v := get_json(cfg,
		'https://api.aniskip.com/v2/skip-times/${mal}/${ep}?types[]=op&types[]=ed&types[]=mixed-op&types[]=mixed-ed&types[]=recap${lens}')!
	cache.set(key, jenc(v), ttl)
	return v
}

pub fn comments(cfg config.Config, media string, offset u32, sort string, limit u32) !json2.Any {
	key := 'comments:${media}:${offset}:${sort}:${limit}'
	if h := cache.get(key) {
		return jdec(h)
	}
	v := get_json(cfg,
		'https://theanimecommunity.com/api/v1/comments/${media}?offset=${offset}&sortBy=${sort}&limit=${limit}')!
	cache.set(key, jenc(v), ttl)
	return v
}

pub fn episode_meta(cfg config.Config, anilist_id i64, ep i64) !json2.Any {
	key := 'episode-meta:${anilist_id}:${ep}'
	if h := cache.get(key) {
		return jdec(h)
	}
	v := get_json(cfg,
		'https://theanimecommunity.com/api/v1/episodes/mediaItemID?AniList_ID=${anilist_id}&mediaType=anime&episodeChapterNumber=${ep}')!
	cache.set(key, jenc(v), ttl)
	return v
}

pub fn anilist(cfg config.Config, query string, variables ?json2.Any) !json2.Any {
	mut body := map[string]json2.Any{}
	body['query'] = json2.Any(query)
	body['variables'] = opt_val(variables)
	r := do_post_json(cfg, 'https://graphql.anilist.co/', jenc(body))!
	return jdec(r.body)
}

fn opt_str(x ?string) json2.Any {
	if v := x {
		return json2.Any(v)
	}
	return json2.null
}

fn opt_i64(x ?i64) json2.Any {
	if v := x {
		return json2.Any(v)
	}
	return json2.null
}

fn opt_val(x ?json2.Any) json2.Any {
	if v := x {
		return v
	}
	return json2.null
}
