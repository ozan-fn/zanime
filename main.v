module main

import net.http
import time
import internal.app
import internal.cache
import internal.config
import internal.stats

// resource_log: catat mem + cpu proses tiap 2 dtk (nilai sama dipakai /api/stats).
fn resource_log() {
	for {
		time.sleep(2 * time.second)
		stats.sample()
		sn := stats.snapshot()
		eprintln('res proc_mem=${sn.mem_mb}MB peak_mem=${sn.peak_mem}MB proc_cpu=${sn.cpu:.1}% peak_cpu=${sn.peak_cpu:.1}%')
	}
}

// dist_map: isi web/dist yang di-embed (`web_dist.v`, dibuat `make web` + embedfs).
// Hanya dibaca saat build `-d embed_dist`; tanpa flag itu peta kosong dan
// internal.app melayani aset dari disk (mode dev).
fn dist_map() map[string]string {
	$if embed_dist ? {
		mut m := map[string]string{}
		for k, f in web_dist {
			m[k] = f.to_string()
		}
		return m
	}
	return map[string]string{}
}

fn main() {
	cfg := config.load()
	cache.init(cfg.redis_url)
	spawn resource_log()
	mut a := &app.App{
		cfg:  cfg
		dist: dist_map()
	}
	eprintln('listen http://${cfg.bind}')
	mut server := &http.Server{
		addr:    cfg.bind
		handler: a
	}
	server.listen_and_serve()
}
