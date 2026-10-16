module cache

// Tanpa redis (REDIS_URL kosong, belum init) hasil upstream harus lewat apa adanya.
fn test_tanpa_redis_lewat_langsung() {
	v := cached_json('kunci', 1, fn () !string {
		return '9'
	}) or { panic('harus lewat tanpa cache') }
	assert v == '9'
	// Miss tanpa redis tetap miss (tak ada yang tersimpan).
	miss := get('kunci') or { '' }
	assert miss == ''
}
