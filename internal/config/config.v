module config

import os

// browser_ua: UA browser untuk request keluar (upstream REST/GraphQL + CDN/proxy).
// CDN segmen & subtitle menolak request tanpa UA browser (403 HTML).
pub const browser_ua = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36'

// Config: konstanta + env yang dibaca sekali saat startup.
pub struct Config {
pub mut:
	bind        string
	user_agent  string
	origin      string
	referer     string
	graphql_url string
	api_base    string
	provider    string
	// Satu atau banyak key (pisah koma/baris) → dipakai round-robin.
	gemini_keys []string
	// Model Gemini (env GEMINI_MODEL). Default flash-lite terbaru: murah + free tier.
	gemini_model string
	// Cache Redis (opsional): `rediss://default:<pass>@<host>:6379`. Kosong = tanpa cache.
	redis_url string
}

// load Config dari .env + environment (tanpa lib dotenv). Key yang sudah ada di
// lingkungan menang (.env hanya mengisi yang belum ada) — seperti dotenvy.
pub fn load() Config {
	dotenv()
	mut port := os.getenv('PORT').trim_space()
	if port == '' {
		port = '3000'
	}
	keys := os.getenv('GEMINI_API_KEY').split_any(',\n').map(it.trim_space()).filter(it != '')
	mut model := os.getenv('GEMINI_MODEL').trim_space()
	if model == '' {
		model = 'gemini-3.5-flash-lite'
	}
	return Config{
		bind:         '0.0.0.0:${port}'
		user_agent:   browser_ua
		origin:       'https://anistream.one'
		referer:      'https://anistream.one/'
		graphql_url:  'https://graphql.animex.one/graphql'
		api_base:     'https://api.anistream.one/rest/api'
		provider:     'yuki'
		gemini_keys:  keys
		gemini_model: model
		redis_url:    os.getenv('REDIS_URL').trim_space()
	}
}

// .env proyek (jika ada): KEY=VAL tiap baris; yang sudah di env tidak ditimpa.
fn dotenv() {
	txt := os.read_file('.env') or { return }
	for line in txt.split('\n') {
		l := line.trim_space()
		if l == '' || l.starts_with('#') {
			continue
		}
		i := l.index_u8(`=`)
		if i < 1 {
			continue
		}
		key := l[..i].trim_space()
		if key.contains(' ') {
			continue
		}
		val := l[i + 1..].trim_space()
		if os.getenv(key) == '' {
			os.setenv(key, val, false)
		}
	}
}
