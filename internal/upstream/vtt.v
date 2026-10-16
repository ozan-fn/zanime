module upstream

// VTT → (head, cues). Cue tanpa "-->" (NOTE/STYLE) ikut lolos utuh saat build.
pub fn parse_vtt(t string) (string, [][]string) {
	tt := t.replace('\r\n', '\n')
	blocks := tt.split('\n\n')
	head := if blocks.len > 0 { blocks[0] } else { 'WEBVTT' }
	mut cues := [][]string{}
	for i in 1 .. blocks.len {
		if blocks[i] == '' {
			continue
		}
		cues << blocks[i].split('\n')
	}
	return head, cues
}

// cue_lines: baris teks cue dialog (per cue, struktur baris dipertahankan).
pub fn cue_lines(c []string) ?[]string {
	mut p := -1
	for i, l in c {
		if l.contains('-->') {
			p = i
			break
		}
	}
	if p < 0 {
		return none
	}
	mut out := []string{}
	for l in c[p + 1..] {
		if l.trim_space() != '' {
			out << l
		}
	}
	if out.len == 0 {
		return none
	}
	return out
}

// build_vtt: rakit VTT dari map terjemahan; cue belum diterjemahkan fallback EN.
pub fn build_vtt(head string, cues [][]string, ids map[int]string) string {
	mut out := [head]
	for i, c in cues {
		mut p := -1
		for j, l in c {
			if l.contains('-->') {
				p = j
				break
			}
		}
		if p >= 0 {
			mut v := c[..p + 1].clone()
			v << (ids[i] or { c[p + 1..].join(' ') })
			out << v.join('\n')
		} else {
			out << c.join('\n')
		}
	}
	return out.join('\n\n') + '\n'
}

// build_vtt_id: hanya cue terjemahan (tanpa fallback EN).
pub fn build_vtt_id(head string, cues [][]string, ids map[int]string) string {
	mut out := [head]
	for i, c in cues {
		mut p := -1
		for j, l in c {
			if l.contains('-->') {
				p = j
				break
			}
		}
		if p < 0 {
			continue
		}
		if txt := ids[i] {
			out << [c[p], txt].join('\n')
		}
	}
	return out.join('\n\n') + '\n'
}

// parse_ts: "HH:MM:SS.mmm" (bagian boleh kurang) → detik.
fn parse_ts(s string) f64 {
	p := s.trim_space().split(':')
	mut sec := 0.0
	mut min := 0.0
	mut hr := 0.0
	if p.len >= 1 {
		sec = p[p.len - 1].f64()
	}
	if p.len >= 2 {
		min = p[p.len - 2].f64()
	}
	if p.len >= 3 {
		hr = p[p.len - 3].f64()
	}
	return hr * 3600.0 + min * 60.0 + sec
}

// ts_start: detik awal timestamp cue. Gagal parse → 0.
pub fn ts_start(line string) f64 {
	return parse_ts(line.all_before('-->'))
}

// ts_end: detik akhir timestamp cue. Gagal parse → 0.
pub fn ts_end(line string) f64 {
	return parse_ts(line.all_after('-->'))
}
