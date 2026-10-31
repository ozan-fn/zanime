// Package em menjalankan pipeline scraping embedmaster: halaman player ->
// welcome gate -> kredensial segar (ticket/pk/scrapeToken) -> feed NDJSON
// /api/sources -> resolve+dekripsi semua server (EMCrypto). Seluruh
// network/dekripsi terjadi di dalam `bun -e` (script di bawah); Go hanya
// menjalankan proc dan membaca baris JSON. Flow-nya sama dengan pipeline
// yang diuji di internal/embedmaster.
package em

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// URL stream hasil resolve punya masa berlaku; cache ./tmp pegang
	// hasilnya cuma selama ini.
	cacheDir   = "tmp/em"
	cacheTTL   = 15 * time.Minute
	scrapeSecs = 120
)

// TVEpisode memilih satu episode tv (season 0 = specials).
type TVEpisode struct {
	Season  int
	Episode int
}

// Quality adalah satu pilihan resolusi milik server progressive (mp4 per
// resolusi; URL server sudah mewakili kualitas tertingginya).
type Quality struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Sub adalah satu track subtitle VTT (diakses lewat proxy /fetch).
type Sub struct {
	Label string `json:"label"`
	Lang  string `json:"lang"`
	URL   string `json:"url"`
}

// Server adalah satu sumber stream ter-resolve.
type Server struct {
	Name      string    `json:"name"`
	Type      string    `json:"type"` // mp4 (progressive) | hls (m3u8)
	Quality   string    `json:"quality"`
	Qualities []Quality `json:"qualities,omitempty"`
	Subs      []Sub     `json:"subs,omitempty"`
	URL       string    `json:"url"`
	Err       string    `json:"err,omitempty"`
}

// raw adalah satu baris output pipeline bun.
type raw struct {
	Server string `json:"server"`
	Data   *struct {
		OK        bool      `json:"ok"`
		Kind      string    `json:"kind"`
		URL       string    `json:"url"`
		Type      string    `json:"type"`
		Quality   string    `json:"quality"`
		Qualities []Quality `json:"qualities"`
		Subtitles []Sub     `json:"subtitles"`
	} `json:"data"`
	Err string `json:"err"`
}

// bunFreshScript: pipeline lengkap sesi embedmaster (satu baris JSON per
// server + baris ringkasan {"done":true}). Menyalin perilaku player
// browser: GET embed membawa Referer embedmaster.com; welcome gate POST tr=;
// config obfuscated halaman player dieval di sandbox vm untuk mint
// kredensial; resolve.php didekripsi ulang dengan EMCrypto halaman itu.
const bunFreshScript = `(async () => {
  const { runInNewContext } = require("vm");
  const UA = "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0";
  const kl = String.fromCharCode(10);
  const B = String.fromCharCode(92);
  // argv layout: bun -e script a b → argv = [bun, a, b] (script tidak
  // masuk argv); bila dijalankan dari file (bun em_fresh.js a b), argv[1]
  // adalah file .js itu, mulai dari argv[2].
  const first = process.argv[1];
  const isFile = /^[/\w.-]+\.js$/.test(first || "");
  const args = process.argv.slice(isFile ? 2 : 1).filter(Boolean);
  const tmdb = args[0];
  const mode = args[1] === "tv" ? "tv" : "movie";
  const se = mode === "tv" ? (args[2] || "1-1") : "";
  const season = parseInt(se.split("-")[0], 10) || 1;
  const episode = parseInt(se.split("-")[1], 10) || 1;
  if (!tmdb) throw new Error("usage: bun -e script <tmdbId> [tv <season-episode>]");
  const jar = new Map();
  const cookieHeader = () => [...jar.entries()].map(([k, v]) => k + "=" + v).join("; ");
  const storeCookies = (res) => {
    for (const c of (res.headers.getSetCookie ? res.headers.getSetCookie() : [])) {
      const [pair] = c.split(";");
      const i = pair.indexOf("=");
      if (i > 0) jar.set(pair.slice(0, i).trim(), pair.slice(i + 1).trim());
    }
  };
  const scriptRe = new RegExp("<script" + B + "b[^>]*>([" + B + "s" + B + "S]*?)<" + B + "/script>", "g");
  const cryptoRe = new RegExp("src=\"([^\"]*em-crypto" + B + ".min" + B + ".js[^\"]*)\"");
  const stubEl = () => ({ style: {}, setAttribute() {}, appendChild() {}, remove() {}, addEventListener() {} });
  const mkDoc = (html) => ({
    querySelectorAll() { return []; },
    querySelector() { return null; },
    getElementById() { return null; },
    createElement: stubEl,
    body: { innerHTML: html, appendChild() {} },
    documentElement: {},
    addEventListener() {},
  });

  // Eval halaman player di sandbox: config obfuscated -> window.__EM_PLAYER__
  // (kredensial segar) dan, untuk tv, window.__EM_EPISODES__.
  const evalPages = (html, playerURL) => {
    const scripts = [...html.matchAll(scriptRe)].map((m) => m[1]);
    const cfgScript = scripts.find(
      (s) => s && s.indexOf("atob(") !== -1 && s.indexOf("(0,eval)(decodeURIComponent(escape(") !== -1,
    );
    if (!cfgScript) throw new Error("no obfuscated credential script");
    const w = {
      location: { href: playerURL },
      navigator: { userAgent: UA },
      document: mkDoc(html),
      innerWidth: 1920,
      innerHeight: 1080,
      addEventListener() {},
      requestAnimationFrame() {},
      setTimeout() {},
      setInterval() {},
      console,
    };
    w.fetch = () => {}; // halaman jangan melakukan fetch sendiri
    w.self = w;
    const ctx = require("vm").createContext({
      window: w, self: w, document: w.document, location: w.location,
      navigator: w.navigator, atob, escape, unescape, decodeURIComponent,
      encodeURIComponent, setTimeout() {}, setInterval() {},
      requestAnimationFrame() {}, console,
    });
    try { runInNewContext(cfgScript, ctx); } catch (e) {
      if (!w.__EM_PLAYER__) throw e;
    }
    if (mode === "tv") {
      const epsScript = scripts.find((s) => s && s.indexOf("__EM_EPISODES__") !== -1);
      if (!epsScript) throw new Error("tv player page has no __EM_EPISODES__");
      try { runInNewContext(epsScript, { window: w, self: w, console }); } catch (e) {}
      if (!w.__EM_EPISODES__) throw new Error("no __EM_EPISODES__ parsed");
    }
    return w;
  };

  // Satu embed URL -> welcome gate -> kredensial segar + crypto src halaman.
  const playerFor = async (embedURL) => {
    const r1 = await fetch(embedURL, {
      headers: { "User-Agent": UA, Referer: "https://embedmaster.com/" },
    });
    storeCookies(r1);
    const embedHost = new URL(embedURL).origin;
    const playerURL = r1.url;
    let html = await r1.text();
    if (html.indexOf("atob(") === -1 || html.indexOf("v2/assets") === -1) {
      const r2 = await fetch(playerURL, {
        method: "POST",
        headers: {
          "User-Agent": UA,
          Cookie: cookieHeader(),
          "Content-Type": "application/x-www-form-urlencoded",
          Origin: embedHost,
          Referer: "https://embedmaster.com/",
        },
        body: "tr=https%3A%2F%2Fembedmaster.com%2F",
      });
      storeCookies(r2);
      html = await r2.text();
    }
    const win = evalPages(html, playerURL);
    const cryptoPath = (html.match(cryptoRe) || [])[1];
    if (!cryptoPath) throw new Error("em-crypto src not found");
    const cryptoSrc = await (await fetch(new URL(cryptoPath, playerURL).href, {
      headers: { "User-Agent": UA, Referer: playerURL },
    })).text();
    return { playerURL, win, cryptoSrc };
  };

  // Feed + resolve+dekripsi semua server. Satu baris JSON per server.
  const resolveAll = async (p) => {
    const cfg = p.win.__EM_PLAYER__;
    const fr = await fetch(new URL(cfg.feedUrl + cfg.scrapeToken).href, {
      headers: {
        "User-Agent": UA, Accept: "application/x-ndjson",
        Origin: "https://embdmstrplayer.com", Referer: p.playerURL,
      },
    });
    const feed = await fr.text();
    const servers = feed.split(kl).map((s) => s.trim()).filter(Boolean).map(JSON.parse)
      .filter((r) => r.type === "server");
    const dw = {
      location: { protocol: "https:" }, navigator: { userAgent: UA },
      Uint8Array, Uint16Array, Uint32Array, Int8Array, Int16Array, Int32Array,
      ArrayBuffer, DataView, TextDecoder, TextEncoder, atob, btoa, Math, Error,
      Promise, JSON, String, Object, Date, Array, RegExp, Symbol, BigInt,
      performance, console,
    };
    dw.self = dw; dw.window = dw;
    require("vm").createContext(dw);
    runInNewContext(p.cryptoSrc, dw);
    const c = dw.EMCrypto;
    if (!c) throw new Error("no EMCrypto");
    const key = c.hexToBytes(cfg.pk);
    const rURL = new URL(cfg.resolveUrl, p.playerURL).href.replace(new RegExp(B + B + "/$"), "");
    for (const s of servers) {
      let data = null;
      let err = null;
      const fetchResolve = async () => {
        const rs = await fetch(rURL + "?src=" + s.source_url, {
          headers: {
            "User-Agent": UA, Referer: p.playerURL,
            "X-EM-Ticket": cfg.ticket, "X-Em-Player": "1",
          },
        });
        const blob = await rs.text();
        return JSON.parse(c.decrypt(blob, key));
      };
      // resolve.php kadang gagal pada langkah atob EMCrypto; sekali retry
      // dengan kredensial yang sama biasanya berhasil (reload browser).
      try { data = await fetchResolve(); }
      catch (e1) {
        err = e1.message;
        try { data = await fetchResolve(); err = null; } catch (e2) { err = e2.message; }
      }
      process.stdout.write(JSON.stringify({
        server: s.source_name,
        data: data && data.ok ? {
          ok: data.ok, kind: data.kind, url: data.url, type: data.type,
          quality: data.quality || "", qualities: data.qualities || [],
          subtitles: data.subtitles || [],
        } : null, err,
      }) + kl);
    }
    return { resolved: servers.filter((s) => s.source_url).length, total: servers.length };
  };

  const embed =
    (mode === "tv"
      ? "https://embedmaster.link/tv/" + tmdb + "/" + season + "/" + episode
      : "https://embedmaster.link/movie/" + tmdb) +
    "?skin=onyx&welcome_page=on&autoplay=off";

  let meta;
  if (mode === "movie") {
    const p = await playerFor(embed);
    const r = await resolveAll(p);
    meta = { mode, tmdb, playerURL: p.playerURL };
    process.stdout.write(JSON.stringify({ done: true, resolved: r.resolved, total: r.total, meta }) + kl);
  } else {
    // tv: halaman player pertama membawa daftar episode lengkap; hanya
    // episode terpilih yang di-resolve (lazy).
    const p0 = await playerFor(embed);
    const list = p0.win.__EM_EPISODES__;
    const eps = [];
    for (const sn of list.seasons) {
      for (const ep of sn.episodes || []) {
        eps.push({ season: sn.season, episode: ep.episode, title: ep.title, url: ep.url });
      }
    }
    if (!eps.length) throw new Error("episode list is empty");
    const selURL = (eps.find((e) => String(e.season) === String(season) && String(e.episode) === String(episode)) || {}).url;
    if (!selURL) throw new Error("episode not in list: S" + season + "E" + episode);
    const p1 = await playerFor(selURL);
    const r = await resolveAll(p1);
    meta = { mode, tmdb, playerURL: p1.playerURL, episodes: { showTitle: list.showTitle, current: list.current, count: eps.length } };
    process.stdout.write(JSON.stringify({ done: true, resolved: r.resolved, total: r.total, meta }) + kl);
  }
})().catch((e) => {
  console.error("FATAL", e.message);
  process.exit(1);
});`

// Client menjalankan pipeline (bun) dan menyimpan hasilnya di ./tmp/em.
type Client struct {
	// Run menjalankan pipeline; default exec bun -e <script> args...
	// Field bukan interface untuk test: pasang stub di sini.
	Run func(ctx context.Context, argv []string) ([]byte, string, error)
}

func New() *Client {
	return &Client{Run: runBun}
}

// runBun menjalankan `bun -e <script> <args...>`, mengembalikan stdout plus
// kata di baris "FATAL ..." stderr bila proc kelainan (untuk pesan error).
func runBun(ctx context.Context, argv []string) ([]byte, string, error) {
	var stderr bytes.Buffer
	// argv[0] bun -e: script, lalu arg milik script mulai argv[2].
	cmd := exec.CommandContext(ctx, "bun", argv...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && stderr.Len() > 0 {
		err = fmt.Errorf("%v: %s", err, firstLine(stderr.String()))
	}
	return out, stderr.String(), err
}

// Streams mengembalikan daftar server stream untuk movie atau satu episode
// tv. id adalah TMDB id (moviedb_id). Cache ./tmp/em disentuh hanya bila
// belum basi (cacheTTL): URL stream terbatas waktu.
func (c *Client) Streams(ctx context.Context, tab, id string, tv *TVEpisode) ([]Server, error) {
	key := tab + "-" + id
	if tv != nil {
		key += fmt.Sprintf("-%d-%d", tv.Season, tv.Episode)
	}
	if servers, ok := readCache(key); ok {
		return servers, nil
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	runCtx, cancel := context.WithTimeout(context.Background(), scrapeSecs*time.Second)
	defer cancel()
	argv := []string{"-e", bunFreshScript}
	if tab == "tv" && tv != nil {
		argv = append(argv, id, "tv", fmt.Sprintf("%d-%d", tv.Season, tv.Episode))
	} else {
		argv = append(argv, id)
	}
	out, stderr, err := c.Run(runCtx, argv)
	if err != nil {
		if !errors.Is(err, context.DeadlineExceeded) && stderr != "" {
			err = fmt.Errorf("%v: %s", err, firstLine(stderr))
		}
		return nil, fmt.Errorf("pipeline scrape: %w", err)
	}

	var servers []Server
	for sc := bufio.NewScanner(strings.NewReader(string(out))); sc.Scan(); {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.Contains(line, `"done":true`) {
			continue
		}
		var r raw
		if uerr := json.Unmarshal([]byte(line), &r); uerr != nil {
			continue
		}
		s := Server{Name: r.Server}
		if r.Data != nil && r.Data.OK {
			s.Type = typeOf(r.Data.Type)
			s.Quality = r.Data.Quality
			s.Qualities = r.Data.Qualities
			s.Subs = r.Data.Subtitles
			s.URL = r.Data.URL
		} else if r.Err != "" {
			s.Err = r.Err
		} else {
			s.Err = "server tidak resolve"
		}
		servers = append(servers, s)
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("embedmaster tidak mengembalikan server (id %s)", id)
	}
	playable := false
	for _, s := range servers {
		if s.URL != "" {
			playable = true
		}
	}
	if !playable {
		return nil, fmt.Errorf("tidak ada server yang bisa diputar (id %s)", id)
	}
	writeCache(key, servers)
	return servers, nil
}

func typeOf(t string) string {
	if t == "hls" || t == "m3u8" {
		return "hls"
	}
	return "mp4"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// ---- cache ./tmp/em ----

func cachePath(key string) string {
	sum := sha1.Sum([]byte(key))
	return filepath.Join(cacheDir, fmt.Sprintf("%x.json", sum))
}

func readCache(key string) ([]Server, bool) {
	info, err := os.Stat(cachePath(key))
	if err != nil || time.Since(info.ModTime()) > cacheTTL {
		return nil, false
	}
	servers := readServers(cachePath(key))
	return servers, servers != nil
}

func readServers(path string) []Server {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []Server
	if err := json.Unmarshal(file, &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func writeCache(key string, servers []Server) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(servers, "", " ")
	if err != nil {
		return
	}
	_ = os.WriteFile(cachePath(key), data, 0o644)
}
