module app

import net.http
import time
import x.json2
import internal.config
import internal.upstream

// Alur API penuh (search → katalog → detail → episode → server → sumber → stream →
// subtitle) diuji deterministik dengan upstream tiruan. Endpoint yang URL-nya
// di-hardcode (skiptimes/comments/episode-meta/anilist) diuji di live_test.v.
const flow_up_addr = '127.0.0.1:39192'
const flow_app_addr = '127.0.0.1:39193'

fn mock_anime_json(id string, i int) string {
	return '{"id":"${id}","anilistId":${i},"titleRomaji":"Romaji ${i}","titleEnglish":"English ${i}","episodeCount":2,"coverImage":{"large":"https://cdn.example.com/c${i}.jpg"},"bannerImage":"https://cdn.example.com/b${i}.jpg","format":"TV","seasonYear":2002,"averageScore":80,"status":"FINISHED"}'
}

// mock_graphql: balas sesuai query yang dikirim handler (CatalogAnime / detail).
fn mock_graphql(data string) http.Response {
	// id tak dikenal → anime null (handler memetakannya ke 404).
	if data.contains('tidak-ada') {
		return mock_json('{"data":{"anime":null}}')
	}
	if data.contains('CatalogAnime') {
		// Emulasi batas keras upstream: `items: null` (→ 502) bila limit > 30.
		if data.contains('"limit":999') {
			return mock_json('{"data":{"catalogAnime":{"items":null}}}')
		}
		// offset diteruskan apa adanya → halaman 2 dapat item berbeda (/browse?page=N).
		mut page := 'p1'
		if data.contains('"offset":30') {
			page = 'p2'
		}
		return mock_json('{"data":{"catalogAnime":{"items":[${mock_anime_json(page, 1)},${mock_anime_json('id2',
			2)}]}}}')
	}
	if data.contains('AnimeDetailSeasons') {
		return mock_json('{"data":{"anime":{"id":"id1","seasons":[{"title":"S1"}]}}}')
	}
	if data.contains('AnimeDetailRelations') {
		return mock_json('{"data":{"anime":{"id":"id1","relations":[{"title":"R1","seasonYear":2003,"status":"FINISHED"}]}}}')
	}
	if data.contains('AnimeDetailRecommendations') {
		return mock_json('{"data":{"anime":{"id":"id1","recommendations":[{"title":"REC1"}]}}}')
	}
	if data.contains('AnimeDetailCharacters') {
		return mock_json('{"data":{"anime":{"id":"id1","characters":[{"name":"CH1"}]}}}')
	}
	return mock_json('{"data":{"anime":{"id":"id1","anilistId":1,"titleRomaji":"Romaji 1","titleEnglish":"English 1","titles":"{\\"ja\\":\\"T JP\\"}","coverImage":{"large":"https://cdn.example.com/c.jpg"},"bannerImage":"https://cdn.example.com/b.jpg","episodeCount":2,"status":"FINISHED"}}}')
}

// 2 dulu, 1 kemudian → membuktikan handler mengurutkan per nomor.
fn mock_episodes_json() string {
	return '[{"number":2,"titles":{"en":"Two","x-jat":"Ni","ja":"二"},"img":"https://cdn.example.com/2.jpg"},{"number":1,"titles":{"en":"One","x-jat":"Ichi","ja":"一"},"img":""}]'
}

fn mock_servers_json() string {
	return '{"subProviders":[{"id":"yuki","default":true}],"dubProviders":[]}'
}

// Sumber + track EN + header; URL CDN menunjuk mock ini juga (master → varian → segmen).
fn mock_sources_json() string {
	return '{"sources":[{"url":"http://${flow_up_addr}/cdn/master.m3u8","quality":"auto"}],"tracks":[{"lang":"english (cr english)","url":"http://${flow_up_addr}/cdn/subs.vtt"}],"headers":{"Origin":"http://127.0.0.1","Referer":"http://127.0.0.1/"}}'
}

// MockUp: upstream tiruan — GraphQL animex + REST anistream + CDN (master/varian/segmen/VTT).
struct MockUp {}

fn (mut m MockUp) handle(req http.Request) http.Response {
	path := req.url.all_before('?')
	return match path {
		'/graphql' {
			mock_graphql(req.data)
		}
		'/rest/api/episodes' {
			mock_json(mock_episodes_json())
		}
		'/rest/api/servers' {
			mock_json(mock_servers_json())
		}
		'/rest/api/sources' {
			mock_json(mock_sources_json())
		}
		'/cdn/master.m3u8' {
			mock_playlist('#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1920x1080\nindex-f1-v1-a1.m3u8\n')
		}
		'/cdn/index-f1-v1-a1.m3u8' {
			mock_playlist('#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\nseg-1.ts\n#EXTINF:4.0,\nseg-2.ts\n')
		}
		'/cdn/seg-1.ts', '/cdn/seg-2.ts' {
			mock_segment(req)
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

fn mock_segment(req http.Request) http.Response {
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

fn mock_json(body string) http.Response {
	mut r := http.Response{
		status_code: 200
		body:        body
	}
	r.header.add_custom('content-type', 'application/json') or {}
	return r
}

fn mock_playlist(body string) http.Response {
	mut r := http.Response{
		status_code: 200
		body:        body
	}
	r.header.add_custom('content-type', 'application/vnd.apple.mpegurl') or {}
	return r
}

fn get(url string) http.Response {
	return http.get(url) or { panic('GET ${url}: ${err}') }
}

fn as_map(body string) map[string]json2.Any {
	return (json2.decode[json2.Any](body) or { panic('json: ${body}') }).as_map()
}

fn test_api_flow_search_to_stream() {
	mut up := &MockUp{}
	mut up_srv := &http.Server{
		addr:                 flow_up_addr
		handler:              up
		show_startup_message: false
	}
	spawn up_srv.listen_and_serve()
	up_srv.wait_till_running() or { panic('mock upstream: ${err}') }

	mut a := &App{
		cfg: config.Config{
			graphql_url: 'http://${flow_up_addr}/graphql'
			api_base:    'http://${flow_up_addr}/rest/api'
			origin:      'http://127.0.0.1'
			referer:     'http://127.0.0.1/'
			provider:    'yuki'
		}
	}
	mut app_srv := &http.Server{
		addr:                 flow_app_addr
		handler:              a
		show_startup_message: false
	}
	spawn app_srv.listen_and_serve()
	app_srv.wait_till_running() or { panic('app: ${err}') }
	base := 'http://${flow_app_addr}'

	// --- katalog & pencarian ---
	rs := get('${base}/api/search?query=test')
	assert rs.status_code == 200
	items := (json2.decode[json2.Any](rs.body) or { panic('search json') }).as_array()
	assert items.len == 2
	out := items[0].as_map()
	assert upstream.val_str(out['id'] or { json2.null }) == 'p1'
	assert upstream.val_str(out['titleRomaji'] or { json2.null }) == 'Romaji 1'
	assert upstream.val_str(out['cover'] or { json2.null }).starts_with('/api/fetch?u=')
	assert upstream.val_str(out['banner'] or { json2.null }).starts_with('/api/fetch?u=')
	assert upstream.val_str(out['status'] or { json2.null }) == 'FINISHED'

	assert get('${base}/api/search?query=').status_code == 400

	// katalog: validasi filter + clamp limit + offset (halaman "lihat semua")
	assert get('${base}/api/catalog?sort=NGAWUR').status_code == 400
	assert get('${base}/api/catalog?direction=NAIK').status_code == 400
	assert get('${base}/api/catalog?status=ngawur').status_code == 400
	assert get('${base}/api/catalog?format=ngawur').status_code == 400
	assert get('${base}/api/catalog?season=ngawur').status_code == 400
	rc := get('${base}/api/catalog?sort=TRENDING&limit=3')
	assert rc.status_code == 200
	assert (json2.decode[json2.Any](rc.body) or { panic('catalog json') }).as_array().len == 2
	// limit > 30 di-clamp: kalau diteruskan, upstream tiruan balas items null → 502.
	assert get('${base}/api/catalog?sort=TRENDING&limit=999').status_code == 200
	rpage := get('${base}/api/catalog?sort=TRENDING&limit=30&offset=30')
	assert rpage.status_code == 200
	assert upstream.val_str((json2.decode[json2.Any](rpage.body) or { panic('page json') }).as_array()[0].as_map()['id'] or {
		json2.null
	}) == 'p2'
	// semua filter sekaligus (dipakai section home)
	assert get('${base}/api/catalog?sort=POPULARITY&direction=ASC&limit=18&offset=0&status=RELEASING&format=TV&season=FALL&year=2026&genre=Action&query=test').status_code == 200

	// --- detail (base + seasons + relations) ---
	ra := get('${base}/api/anime?id=id1&section=base')
	assert ra.status_code == 200
	assert ra.body.contains('titleRomaji') && ra.body.contains('titles')
	rsea := get('${base}/api/anime?id=id1&section=seasons')
	assert rsea.status_code == 200
	assert rsea.body.contains('seasons')
	rrel := get('${base}/api/anime?id=id1&section=relations')
	assert rrel.status_code == 200
	assert rrel.body.contains('relations')
	rrec := get('${base}/api/anime?id=id1&section=recommendations')
	assert rrec.status_code == 200
	assert rrec.body.contains('recommendations')
	rch := get('${base}/api/anime?id=id1&section=characters')
	assert rch.status_code == 200
	assert rch.body.contains('characters')
	assert get('${base}/api/anime').status_code == 400 // tanpa id/anilistId
	assert get('${base}/api/anime?id=tidak-ada').status_code == 404

	// --- episode & server ---
	re := get('${base}/api/episodes?id=id1')
	assert re.status_code == 200
	eps := (json2.decode[json2.Any](re.body) or { panic('episodes json') }).as_array()
	assert eps.len == 2
	assert (eps[0].as_map()['number'] or { json2.null }).i64() == 1 // terurut naik
	e0 := eps[0].as_map()
	e1 := eps[1].as_map()
	assert upstream.val_str(e0['title'] or { json2.null }) == 'One'
	assert upstream.val_str(e1['titleJp'] or { json2.null }) == '二'
	assert upstream.val_str(e0['titleRomaji'] or { json2.null }) == 'Ichi'
	assert upstream.val_str(e0['img'] or { json2.null }) == '' // img kosong → tanpa proxy
	assert upstream.val_str(e1['img'] or { json2.null }).starts_with('/api/fetch?u=')

	assert get('${base}/api/servers?id=id1&ep=1').status_code == 200

	// --- sumber & stream ---
	rso := get('${base}/api/sources?id=id1&ep=1&type=sub&provider=yuki')
	assert rso.status_code == 200
	assert rso.body.contains('sources')

	rst := get('${base}/api/stream?id=id1&ep=1')
	assert rst.status_code == 200
	stream := as_map(rst.body)
	stream_url := upstream.val_str(stream['stream'] or { json2.null })
	sub_en := upstream.val_str(stream['sub_en'] or { json2.null })
	sub_id := upstream.val_str(stream['sub_id'] or { json2.null })
	assert stream_url.starts_with('/api/hls?u=') // sumber hidup di-probe → ditulis ulang ke proxy
	assert sub_en.starts_with('/api/fetch?u=')
	assert sub_id != ''

	// --- subtitle: worker ambil VTT EN → status → result ---
	mut state := ''
	mut total := i64(0)
	for _ in 0 .. 100 {
		rstt := get('${base}/api/subid/status?id=${sub_id}')
		assert rstt.status_code == 200
		st := as_map(rstt.body)
		state = upstream.val_str(st['state'] or { json2.null })
		total = (st['total'] or { json2.null }).i64()
		if state in ['ready', 'done'] {
			break
		}
		time.sleep(50 * time.millisecond)
	}
	assert state in ['ready', 'done']
	assert total == 2

	rr := get('${base}/api/subid/result?id=${sub_id}')
	assert rr.status_code == 200
	assert (rr.header.get(.content_type) or { '' }).starts_with('text/vtt')
	assert rr.body.contains('Hello') && rr.body.contains('World')
	only := get('${base}/api/subid/result?id=${sub_id}&only=id')
	assert only.status_code == 200
	assert only.body.contains('WEBVTT')

	assert get('${base}/api/subid/status?id=tidak-ada').status_code == 404

	// --- guard: proxy menolak host privat (anti-SSRF), termasuk CDN mock ---
	assert get('${base}/api/fetch?u=${upstream.b64('http://127.0.0.1:1/x')}').status_code == 400
	assert get('${base}/api/hls?u=${upstream.b64('http://${flow_up_addr}/cdn/master.m3u8')}').status_code == 400

	// --- validasi + stats ---
	assert get('${base}/api/comments?media=').status_code == 400
	assert (http.post('${base}/api/anilist', '{"query":""}') or { panic('anilist post') }).status_code == 400
	rstats := get('${base}/api/stats')
	assert rstats.status_code == 200
	assert rstats.body.contains('memMb')

	app_srv.stop()
	up_srv.stop()
}
