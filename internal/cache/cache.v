@[has_globals]
module cache

import db.redis
import sync

// Koneksi Redis opsional. REDIS_URL kosong / gagal sambung → cache mati,
// API tetap jalan normal (cache tak pernah jadi penyebab 5xx).
struct Store {
mut:
	mu      shared sync.Mutex
	db      redis.DB
	enabled bool
}

__global g_cache Store

// init buka koneksi sekali saat startup.
pub fn init(url string) {
	if url == '' {
		log('cache: REDIS_URL kosong → tanpa cache')
		return
	}
	cfg := parse_url(url)
	db := redis.connect(redis.Config{
		host:     cfg.host
		port:     cfg.port
		password: cfg.password
		tls:      cfg.tls
	}) or {
		log('cache: redis gagal (${err}) → tanpa cache')
		return
	}
	lock g_cache.mu {
		g_cache.db = db
		g_cache.enabled = true
	}
	log('cache: redis siap')
}

struct UrlParts {
	host     string
	port     u16
	password string
	tls      bool
}

// parse_url memecah rediss://user:pass@host:port → bagian koneksi redis.
fn parse_url(u string) UrlParts {
	mut rest := u
	tls := rest.starts_with('rediss://')
	rest = rest.all_after('://')
	mut password := ''
	if at := rest.index('@') {
		userinfo := rest[..at]
		rest = rest[at + 1..]
		if c := userinfo.index(':') {
			password = userinfo[c + 1..]
		}
	}
	mut host := rest.all_before('/')
	mut port := u16(6379)
	if c := host.index(':') {
		port = u16(host[c + 1..].int())
		host = host[..c]
	}
	return UrlParts{
		host:     host
		port:     port
		password: password
		tls:      tls
	}
}

// log ke stderr (warisan env_logger info level).
fn log(msg string) {
	eprintln(msg)
}

// get ambil key; miss / error → none.
pub fn get(key string) ?string {
	lock g_cache.mu {
		if !g_cache.enabled {
			return none
		}
		r := g_cache.db.cmd('GET', key) or {
			g_cache.enabled = false
			return none
		}
		if r is []u8 {
			return r.bytestr()
		}
	}
	return none
}

// set simpan (best-effort; gagal = cache dimatikan, request tetap jalan).
pub fn set(key string, val string, ttl u64) {
	lock g_cache.mu {
		if g_cache.enabled {
			g_cache.db.cmd('SET', key, val, 'EX', ttl.str()) or { g_cache.enabled = false }
		}
	}
}

// cached_json: baca dari cache, miss → panggil f lalu simpan. null tidak
// disimpan: dipakai endpoint untuk "tak ada data" dan query rusak — kalau ikut
// di-cache, hasilnya lengket sepanjang TTL sehari.
pub fn cached_json(key string, ttl u64, f fn () !string) !string {
	if v := get(key) {
		return v
	}
	val := f()!
	if val != 'null' {
		set(key, val, ttl)
	}
	return val
}
