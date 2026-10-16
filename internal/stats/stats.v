@[has_globals]
module stats

import os
import sync
import time

// Resource proses untuk navbar: diisi tiap 2 dtk oleh loop di main, dibaca /api/stats.
pub struct Snapshot {
pub:
	mem_mb   u64
	peak_mem u64
	cpu      f64
	peak_cpu f64
}

struct State {
mut:
	mu         shared sync.Mutex
	mem        u64
	peak_mem   u64
	cpu10      u64
	peak_cpu   u64
	last_ticks u64
	last_ms    i64
}

__global g_st State

// set isi mem (byte) + cpu (persen); puncak dilacak juga.
pub fn set(mem_bytes u64, cpu f64) {
	c := if cpu > 0.0 && cpu < 100000.0 { u64(cpu * 10.0) } else { 0 }
	lock g_st.mu {
		g_st.mem = mem_bytes
		g_st.cpu10 = c
		if mem_bytes > g_st.peak_mem {
			g_st.peak_mem = mem_bytes
		}
		if c > g_st.peak_cpu {
			g_st.peak_cpu = c
		}
	}
}

// snapshot: mem + puncak mem (MB), cpu + puncak cpu (%).
pub fn snapshot() Snapshot {
	lock g_st.mu {
		return Snapshot{
			mem_mb:   g_st.mem / 1024 / 1024
			peak_mem: g_st.peak_mem / 1024 / 1024
			cpu:      f64(g_st.cpu10) / 10.0
			peak_cpu: f64(g_st.peak_cpu) / 10.0
		}
	}
}

// sample baca /proc/self (Linux) → RSS + CPU delta, lalu set.
// ponytail: Linux /proc saja, cukup untuk deploy; sysinfo setara bila perlu portabel.
pub fn sample() {
	mem := rss_bytes()
	ticks := cpu_ticks()
	now := time.now().unix_milli()
	mut cpu := 0.0
	lock g_st.mu {
		if g_st.last_ms > 0 && now > g_st.last_ms && ticks >= g_st.last_ticks {
			dt := f64(now - g_st.last_ms) / 1000.0
			// HZ asumdi 100 tick/detik; cpu% = tick_delta / HZ / dt * 100.
			if dt > 0 {
				cpu = f64(ticks - g_st.last_ticks) / 100.0 / dt * 100.0
			}
		}
		g_st.last_ticks = ticks
		g_st.last_ms = now
	}
	set(mem, cpu)
}

// rss_bytes: /proc/self/statm field ke-2 (resident) × 4096 (page size).
fn rss_bytes() u64 {
	txt := os.read_file('/proc/self/statm') or { return 0 }
	parts := txt.trim_space().split(' ')
	if parts.len < 2 {
		return 0
	}
	return u64(parts[1].int()) * 4096
}

// cpu_ticks: utime+stime (tick) dari /proc/self/stat.
fn cpu_ticks() u64 {
	txt := os.read_file('/proc/self/stat') or { return 0 }
	rp := txt.last_index(')') or { return 0 }
	rest := txt[rp + 1..].trim_space().split(' ')
	// field keseluruhan 14 (utime) & 15 (stime) → indeks 11 & 12 setelah ') '.
	if rest.len < 13 {
		return 0
	}
	return u64(rest[11].u64()) + u64(rest[12].u64())
}
