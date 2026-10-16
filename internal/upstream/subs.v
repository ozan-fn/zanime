@[has_globals]
module upstream

import hash
import math
import net.http
import os
import sync
import time
import x.json2
import internal.config
import internal.errors
import internal.models

// --- Subtitle ID via Gemini (default flash-lite, generateContent) ---
// System prompt subtitle (dipertahankan utuh, warisan era Mistral).
const gemini_sys = 'Kamu adalah penerjemah subtitle profesional Inggris → Indonesia untuk anime. Standar: akurat, natural, mudah dibaca (FAR Pedersen 2017). Maksimal 20 CPS, 42 karakter per baris, maksimal 2 baris per subtitle. Pertahankan honorifik Jepang (san, chan, kun, sama, senpai, sensei) dan nama asli. Jaga register bicara tiap karakter. Adaptasi wordplay, jangan literal. Italic untuk lagu, telepon, dan bisikan. Jangan menambah atau mengurangi makna, jangan spoiler, hindari bahasa gaul usang dan kalimat kaku.'

pub const sub_batch_size = 10

// SubJob: status pekerjaan terjemahan satu trek subtitle.
pub struct SubJob {
pub mut:
	state string // queued|ready|working|done|failed
	err   ?string
	head  string
	cues  [][]string
	dlg   []int          // indeks cue dialog (ada "-->")
	ids   map[int]string // cue idx → teks ID
	cache string         // path cache disk (.cache/subtitles)
}

// BatchItem: satu cue yang dikirim ke Gemini.
pub struct BatchItem {
pub:
	idx   int
	lines []string
}

struct SubStore {
mut:
	mu    shared sync.Mutex
	jobs  map[string]SubJob
	order []string
}

struct GemState {
mut:
	mu   shared sync.Mutex
	cd   map[string]i64 // key → unix-ms sampai cooldown
	last i64            // unix-ms panggilan terakhir (throttle RPM)
	next int            // kursor round-robin
}

// g_subs/g_gem: state global job subtitle + Gemini.
__global g_subs SubStore
__global g_gem GemState

// enqueue_sub: daftarkan job subtitle (dedup per URL) + spawn worker.
pub fn enqueue_sub(url string) string {
	id := b64(url)
	mut fresh := false
	lock g_subs.mu {
		if id !in g_subs.jobs {
			g_subs.jobs[id] = SubJob{
				state: 'queued'
			}
			g_subs.order << id
			for g_subs.order.len > 100 {
				old := g_subs.order[0]
				g_subs.order.delete(0)
				g_subs.jobs.delete(old)
			}
			fresh = true
		}
	}
	if fresh {
		spawn sub_worker(id, url)
	}
	return id
}

fn set_state(id string, state string) {
	lock g_subs.mu {
		mut j := g_subs.jobs[id] or { return }
		j.state = state
		g_subs.jobs[id] = j
	}
}

fn fail(id string, msg string) {
	lock g_subs.mu {
		mut j := g_subs.jobs[id] or { return }
		j.state = 'failed'
		j.err = msg
		g_subs.jobs[id] = j
	}
}

// sub_worker: fetch VTT EN + parse + simpan (tanpa LLM). Batch diterjemahkan
// on-demand via sub_batch.
fn sub_worker(id string, url string) {
	set_state(id, 'working')
	// Header sama seperti jalur proxy (UA browser + Origin/Referer megaplay):
	// CDN subtitle menolak request tanpa itu (403 HTML → "bukan VTT").
	f := fetch_upstream(config.Config{}, url, none, none, none) or {
		fail(id, '${err}')
		return
	}
	text := f.body.bytestr()
	if !text.trim_space_left().starts_with('WEBVTT') {
		fail(id, 'bukan VTT')
		return
	}
	path := sub_cache_path(text)
	mut head := ''
	mut cues := [][]string{}
	mut cached := false
	if hit := os.read_file(path) {
		if hit != '' {
			head, cues = parse_vtt(hit)
			cached = true
		}
	}
	// Tanpa cache: parse EN apa adanya.
	if cues.len == 0 {
		head, cues = parse_vtt(text)
	}
	mut dlg := []int{}
	mut ids := map[int]string{}
	for i, c in cues {
		if c.any(it.contains('-->')) {
			dlg << i
		}
		if cached {
			if ls := cue_lines(c) {
				ids[i] = ls.join('\n')
			}
		}
	}
	lock g_subs.mu {
		mut j := g_subs.jobs[id] or { return }
		j.head = head
		j.cues = cues
		j.dlg = dlg
		j.ids = ids.clone()
		j.cache = path
		j.state = if dlg.len > 0 && ids.len >= dlg.len { 'done' } else { 'ready' }
		g_subs.jobs[id] = j
	}
}

// sub_cache_path: .cache/subtitles/<hash>.vtt → tonton ulang instan, hemat limit.
fn sub_cache_path(text string) string {
	return '.cache/subtitles/${hash.sum64_string(text, 0):016x}.vtt'
}

// sub_status: status job + posisi antre + next_at.
pub fn sub_status(id string, at f64) !models.SubStatus {
	lock g_subs.mu {
		j := g_subs.jobs[id] or { return errors.not_found('job tak ada') }
		mut pos := 0
		mut total := 0
		for x in g_subs.order {
			if x in g_subs.jobs {
				if g_subs.jobs[x].state == 'queued' {
					total++
					if x == id {
						pos = total
					}
				}
			}
		}
		return models.SubStatus{
			state:       j.state
			queue:       pos
			queue_total: total
			eta_sec:     if pos > 0 { u64(pos - 1) * 120 } else { u64(0) }
			done:        j.ids.len
			total:       j.dlg.len
			next_at:     next_at_of(j, at)
			error:       j.err
		}
	}
	return error('unreachable')
}

// next_at_of: detik pemicu batch berikut (saat cue ID tersisa di depan posisi
// `at` menyentuh 5). Semua sudah / kosong → -1.
fn next_at_of(j SubJob, at f64) f64 {
	if j.ids.len >= j.dlg.len || j.dlg.len == 0 {
		return -1.0
	}
	mut ahead := []f64{}
	for i in j.dlg {
		if i in j.ids {
			continue
		}
		if c := j.cues[i] {
			for l in c {
				if l.contains('-->') {
					t := ts_start(l)
					if t >= at {
						ahead << t
					}
					break
				}
			}
		}
	}
	if ahead.len < 5 {
		return at
	}
	ahead.sort()
	v := ahead[ahead.len - 5] - 2.0
	return if v > 0.0 { v } else { 0.0 }
}

// sub_batch: 1 batch cue berikut yang belum diterjemahkan → terjemahkan + simpan.
pub fn sub_batch(cfg config.Config, id string, at f64) !models.SubStatus {
	if cfg.gemini_keys.len == 0 {
		return errors.bad_request('GEMINI_API_KEY kosong (isi .env)')
	}
	mut batch := []BatchItem{}
	lock g_subs.mu {
		j := g_subs.jobs[id] or { return errors.not_found('job tak ada') }
		if j.state == 'failed' {
			return errors.upstream(j.err or { 'gagal' })
		}
		if j.cues.len == 0 {
			return errors.upstream('belum siap')
		}
		mut pos := j.dlg.len
		for k, i in j.dlg {
			mut t := 0.0
			if c := j.cues[i] {
				for l in c {
					if l.contains('-->') {
						t = ts_start(l)
						break
					}
				}
			}
			if t >= at {
				pos = k
				break
			}
		}
		begin := if pos > 0 { pos - 1 } else { 0 }
		for i in j.dlg[begin..] {
			if i !in j.ids {
				batch << BatchItem{
					idx:   i
					lines: cue_lines(j.cues[i]) or { []string{} }
				}
			}
			if batch.len >= sub_batch_size {
				break
			}
		}
		if batch.len < sub_batch_size {
			for i in j.dlg {
				if i !in j.ids && batch.all(it.idx != i) {
					batch << BatchItem{
						idx:   i
						lines: cue_lines(j.cues[i]) or { []string{} }
					}
				}
				if batch.len >= sub_batch_size {
					break
				}
			}
		}
		batch = batch.filter(it.lines.len > 0)
	}
	mut delta := ''
	if batch.len > 0 {
		m := translate_batch(cfg, batch)!
		lock g_subs.mu {
			mut j := g_subs.jobs[id] or { return errors.not_found('job tak ada') }
			for k, v in m {
				j.ids[k] = v
			}
			for item in batch {
				txt := m[item.idx] or { '' }
				if txt == '' {
					continue
				}
				if c := j.cues[item.idx] {
					for l in c {
						if l.contains('-->') {
							delta += '${l}\n${txt}\n\n'
							break
						}
					}
				}
			}
			if j.ids.len >= j.dlg.len {
				j.state = 'done'
				done := build_vtt(j.head, j.cues, j.ids)
				if j.cache != '' {
					dir := j.cache.all_before_last('/')
					os.mkdir_all(dir) or {}
					os.write_file(j.cache, done) or {}
				}
			}
			g_subs.jobs[id] = j
		}
	}
	mut st := sub_status(id, at)!
	st.vtt = delta
	return st
}

// sub_result: VTT Indonesia, parsial selama jalan, penuh saat done.
pub fn sub_result(id string, only string) !string {
	lock g_subs.mu {
		j := g_subs.jobs[id] or { return errors.not_found('job tak ada') }
		if j.state == 'failed' {
			return errors.upstream(j.err or { 'gagal' })
		}
		if j.cues.len == 0 {
			return 'WEBVTT\n\n'
		}
		if only == 'id' {
			return build_vtt_id(j.head, j.cues, j.ids)
		}
		return build_vtt(j.head, j.cues, j.ids)
	}
	return error('unreachable')
}

// --- Gemini ---

// throttle: jeda antar panggilan (batas RPM paket free).
fn throttle() {
	mut wait := i64(0)
	lock g_gem.mu {
		now := time.now().unix_milli()
		if g_gem.last > 0 && now - g_gem.last < 1100 {
			wait = 1100 - (now - g_gem.last)
		}
		g_gem.last = now + wait
	}
	if wait > 0 {
		time.sleep(time.Duration(wait) * time.millisecond)
	}
}

// pick_key: key berikut round-robin, lompati yang cooldown. Semua cooldown →
// gagal-cepat (frontend sudah retry).
pub fn pick_key(keys []string) !string {
	if keys.len == 0 {
		return errors.bad_request('GEMINI_API_KEY kosong (isi .env)')
	}
	now := time.now().unix_milli()
	lock g_gem.mu {
		start := g_gem.next
		g_gem.next++
		for off in 0 .. keys.len {
			k := keys[(start + off) % keys.len]
			if (g_gem.cd[k] or { i64(0) }) <= now {
				return k
			}
		}
		mut wait := i64(20)
		mut mn := i64(0)
		for _, u in g_gem.cd {
			if u > now && (mn == 0 || u < mn) {
				mn = u
			}
		}
		if mn > 0 {
			wait = (mn - now) / 1000 + 1
		}
		return errors.rate_limited('semua key gemini rate limit, coba lagi dalam ${wait} dtk')
	}
	return error('unreachable')
}

// gemini_text: gabungan semua part kandidat pertama.
fn gemini_text(v json2.Any) ?string {
	cands := jget(v.as_map(), 'candidates').as_array()
	if cands.len == 0 {
		return none
	}
	parts := jget(jget(cands[0].as_map(), 'content').as_map(), 'parts').as_array()
	mut t := ''
	for p in parts {
		t += sstr(jget(p.as_map(), 'text'), '')
	}
	if t == '' {
		return none
	}
	return t
}

// gemini_cooldown_secs: detik cooldown bila RESOURCE_EXHAUSTED/429 (retryDelay bila ada).
pub fn gemini_cooldown_secs(status int, v json2.Any) ?u64 {
	e := jget(v.as_map(), 'error').as_map()
	estatus := sstr(jget(e, 'status'), '')
	code := val_i64(jget(e, 'code'))
	exhausted := status == 429 || estatus == 'RESOURCE_EXHAUSTED' || code == 429
	if !exhausted {
		return none
	}
	mut delay := u64(0)
	for d in jget(e, 'details').as_array() {
		rd := sstr(jget(d.as_map(), 'retryDelay'), '')
		if rd.ends_with('s') {
			n := rd[..rd.len - 1].f64()
			if n > 0 {
				delay = u64(math.ceil(n))
			}
			break
		}
	}
	if delay == 0 {
		delay = 20
	}
	if delay < 5 {
		delay = 5
	}
	if delay > 60 {
		delay = 60
	}
	return delay
}

fn gemini_chat(cfg config.Config, key string, user string) !string {
	throttle()
	mut syspart := map[string]json2.Any{}
	syspart['text'] = json2.Any(gemini_sys)
	mut sys := map[string]json2.Any{}
	sys['parts'] = json2.Any([json2.Any(syspart)])
	mut part := map[string]json2.Any{}
	part['text'] = json2.Any(user)
	mut content := map[string]json2.Any{}
	content['parts'] = json2.Any([json2.Any(part)])
	mut gen := map[string]json2.Any{}
	gen['temperature'] = json2.Any(i64(0))
	gen['maxOutputTokens'] = json2.Any(i64(1024))
	mut body := map[string]json2.Any{}
	body['system_instruction'] = json2.Any(sys)
	body['contents'] = json2.Any([json2.Any(content)])
	body['generationConfig'] = json2.Any(gen)
	url := 'https://generativelanguage.googleapis.com/v1beta/models/${cfg.gemini_model}:generateContent'
	mut h := http.new_header(key: .content_type, value: 'application/json')
	h.add_custom('x-goog-api-key', key) or {}
	res := http.fetch(
		url:          url
		method:       .post
		data:         jenc(body)
		header:       h
		read_timeout: 30 * time.second
	) or { return errors.upstream('${err}') }
	status := res.status_code
	v := jdec(res.body)
	if wait := gemini_cooldown_secs(status, v) {
		lock g_gem.mu {
			g_gem.cd[key] = time.now().unix_milli() + i64(wait) * 1000
		}
		return errors.rate_limited('gemini rate limit, coba lagi dalam ${wait} dtk')
	}
	if status != 200 {
		msg := sstr(jget(jget(v.as_map(), 'error').as_map(), 'message'), 'unknown')
		tmsg := if msg.len > 200 { msg[..200] } else { msg }
		return errors.upstream('gemini: ${tmsg}')
	}
	return gemini_text(v) or { errors.upstream('gemini tanpa content: ${res.body}') }
}

// translate_batch: terjemahkan sekumpulan cue; balas map idx → teks ID.
pub fn translate_batch(cfg config.Config, jobs []BatchItem) !map[int]string {
	mut q := ''
	for j in jobs {
		q += '[${j.idx}] ${j.lines.join('\n')}\n'
	}
	key := pick_key(cfg.gemini_keys)!
	user := 'Translate each cue to Indonesian, keeping its line breaks. Reply ONLY as [N] <indonesian>, same order:\n${q}'
	a := gemini_chat(cfg, key, user)!
	mut out := map[int]string{}
	mut cur := -1
	for line in a.split('\n').map(it.trim_space()).filter(it != '') {
		if line.starts_with('[') {
			if rb := line.index(']') {
				if rb > 1 {
					k := line[1..rb].int()
					txt := line[rb + 1..].trim_space()
					cur = k
					if txt != '' {
						out[k] = txt
					}
					continue
				}
			}
		}
		if cur >= 0 {
			out[cur] = (out[cur] or { '' }) + '\n' + line
		}
	}
	return out
}
