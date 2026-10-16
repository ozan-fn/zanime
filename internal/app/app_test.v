module app

import x.json2
import internal.models
import internal.upstream

// pick: filter enum valid → key:[VALUE]; nilai asing → error; kosong → skip.
fn test_catalog_pick() {
	mut f := map[string]json2.Any{}
	pick('releasing', upstream.statuses, 'statusIn', mut f) or { assert false }
	got := f['statusIn'] or { json2.null }
	assert upstream.val_str(got.as_array()[0]) == 'RELEASING'
	if _ := pick('ngawur', upstream.statuses, 'statusIn', mut f) {
		assert false
	}
	pick(none, upstream.statuses, 'statusIn', mut f) or { assert false }
}

// to_out: gambar katalog lewat proxy /api/fetch.
fn test_to_out_proxy() {
	a := json2.decode[json2.Any]('{"id":"x","anilistId":1,"titleRomaji":"T","coverImage":{"large":"https://cdn/c.jpg"},"bannerImage":"https://cdn/b.jpg"}') or {
		panic('decode')
	}
	o := to_out(a)
	assert o.cover.starts_with('/api/fetch?u=')
	assert o.banner.starts_with('/api/fetch?u=')
}

// episode_item: judul en > x-jat > ja, img kosong → tanpa proxy.
fn test_episode_item() {
	v := json2.decode[json2.Any]('[{"number":2,"titles":{"en":"Two","x-jat":"Ni","ja":"二"},"img":"https://cdn/2.jpg"},{"number":1,"titles":{"en":null,"x-jat":"Ichi","ja":"一"},"img":""},{"number":3,"titles":{},"img":""}]') or {
		panic('decode')
	}
	mut out := []models.EpisodeItem{}
	for e in v.as_array() {
		out << episode_item(e)
	}
	out.sort_with_compare(fn (x &models.EpisodeItem, y &models.EpisodeItem) int {
		return int(x.number - y.number)
	})
	assert out[0].title == 'Ichi'
	assert out[1].title == 'Two'
	assert out[2].title == '?'
	assert out[0].img == ''
	assert out[1].img.starts_with('/api/fetch?u=')
}

// pick_en_sub: lang upstream tak pernah bersih ("english (cr english)").
fn test_pick_en_sub() {
	a := json2.decode[json2.Any]('{"tracks":[{"lang":"portuguese (cr portuguese(brazil))","url":"pt.vtt"},{"lang":"english (cr english)","url":"en.vtt"},{"lang":"spanish (cr spanish)","url":"es.vtt"}]}') or {
		panic('decode')
	}
	assert pick_en_sub(a) == 'en.vtt'
	b := json2.decode[json2.Any]('{"tracks":[{"lang":"English","url":"en.vtt"}]}') or {
		panic('decode')
	}
	assert pick_en_sub(b) == 'en.vtt'
	c := json2.decode[json2.Any]('{"tracks":[{"lang":"en","url":"en.vtt"}]}') or { panic('decode') }
	assert pick_en_sub(c) == ''
	d := json2.decode[json2.Any]('{}') or { panic('decode') }
	assert pick_en_sub(d) == ''
	// Tanpa Inggris → kosong (bukan salah pilih trek).
	e := json2.decode[json2.Any]('{"tracks":[{"lang":"spanish (cr spanish)","url":"es.vtt"}]}') or {
		panic('decode')
	}
	assert pick_en_sub(e) == ''
}
