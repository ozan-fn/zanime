package embedmaster

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Result models of the scrape — kept next to the test because the package
// .go file is model-of-HAR only.

type emCredential struct {
	Ticket        string            `json:"ticket"`      // X-EM-Ticket header value
	PK            string            `json:"pk"`          // hex payload key for EMCrypto.decrypt
	ScrapeToken   string            `json:"scrapeToken"` // suffix of the /api/sources/ feed URL
	FeedURL       string            `json:"feedUrl"`
	FeedURLBackup string            `json:"feedUrlBackup"`
	ResolveURL    string            `json:"resolveUrl"`
	Order         []string          `json:"order"`
	Kinds         map[string]string `json:"kinds"`
}

type videoQuality struct {
	Label string `json:"label"` // "size video": 1080p / 480p / FULL HD
	URL   string `json:"url"`
}

type subtitleTrack struct {
	Label string `json:"label"`
	Lang  string `json:"lang"`
	URL   string `json:"url"`
	Kind  string `json:"kind"`
}

type resolvedServer struct {
	OK        bool            `json:"ok"`
	Kind      string          `json:"kind"`
	URL       string          `json:"url"` // playable video URL
	Type      string          `json:"type"`
	Quality   string          `json:"quality"`
	Qualities []videoQuality  `json:"qualities"`
	Subtitles []subtitleTrack `json:"subtitles"`
}

type resolvedEnvelope struct {
	Src  string          `json:"src"`
	Data *resolvedServer `json:"data"`
}

// ScrapeResult is what the test produces from the HAR for one tmdb id.
type ScrapeResult struct {
	TMDBID       string
	EmbedURL     string
	PlayerURL    string
	Credential   emCredential               // auto-resolved credentials
	Servers      map[string]*resolvedServer // key = resolve.php src token
	ServerOrder  []string
	SubtitleBody string // recorded VTT body found in the HAR
}

// readHAREntries streams the 257MB HAR without holding the whole file JSON in
// one buffer: walk tokens to log.entries and decode each entry individually.
func readHAREntries(t *testing.T, path string) []Entry {
	t.Helper()
	if path == "" {
		t.Skip("HAR tidak diset (EMBEDMASTER_HAR); record one with the browser first")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("HAR not available (%v); record one with the browser first", err)
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))

	var skipValue func(dec *json.Decoder) error
	skipValue = func(dec *json.Decoder) error {
		v, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := v.(json.Delim); ok && (d == '{' || d == '[') {
			for dec.More() {
				if err := skipValue(dec); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil { // closing delim
				return err
			}
		}
		return nil
	}

	var entries []Entry

	var walkObject func(dec *json.Decoder, depth int) error
	walkObject = func(dec *json.Decoder, depth int) error {
		if v, err := dec.Token(); err != nil { // '{'
			return err
		} else if d, ok := v.(json.Delim); !ok || d != '{' {
			return fmt.Errorf("expected object, got %v", v)
		}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			if key == "log" {
				if err := walkObject(dec, depth+1); err != nil {
					return err
				}
				continue
			}
			if key == "entries" && depth >= 1 {
				if v, err := dec.Token(); err != nil { // '['
					return err
				} else if d, ok := v.(json.Delim); !ok || d != '[' {
					return fmt.Errorf("expected entries array, got %v", v)
				}
				for dec.More() {
					var e Entry
					if err := dec.Decode(&e); err != nil {
						return err
					}
					entries = append(entries, e)
				}
				if _, err := dec.Token(); err != nil { // ']'
					return err
				}
				continue
			}
			if err := skipValue(dec); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil { // closing '}'
			return err
		}
		return nil
	}

	if err := walkObject(dec, 0); err != nil {
		t.Fatalf("walk HAR: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("HAR parsed but no entries found")
	}
	return entries
}

const bunCredentialScript = `const s=require("fs").readFileSync(0,"utf8");
const {runInNewContext}=require("vm");
const w={};
runInNewContext(s,{window:w,atob,escape,encodeURIComponent});
process.stdout.write(JSON.stringify(w.__EM_PLAYER__||{}));`

// bunFreshScript is the whole fresh scrape pipeline in one bun -e run: it
// walks the live embed->player flow like the browser does (incl. the welcome
// gate POST tr= with the session cookie), auto-resolves a brand-new
// credential (window.__EM_PLAYER__ = {ticket, pk, scrapeToken, ...}) by
// executing the page's obfuscated config script in a bare sandbox, then uses
// that credential to fetch the /api/sources NDJSON feed and resolve+decrypt
// every server via the page's own em-crypto. One JSON line per server, then a
// {"done":...} summary. Only bun touches the network; Go never does.
const bunFreshScript = `// Fresh (live) embedmaster scrape pipeline.


// Movie: embed -> player -> fresh credential -> feed -> resolve every server,
// one JSON line per server.
// TV: same up to the player page, which carries the full episode list
// (window.__EM_EPISODES__); only the SELECTED season-episode is then walked
// again through its own player page and resolved. Episode list is parsed from
// the player page (lazy: nothing per-episode is fetched until selection).
// Output: one JSON line per resolved server, final line
//   {done:true,resolved:N,total:M,meta:{...}} (meta.episodes for tv).
(async () => {
  const { runInNewContext } = require('vm');
  const log = (...a) => console.error('[fresh]', ...a);
  const UA =
    'Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0';
  const kl = String.fromCharCode(10);
  const B = String.fromCharCode(92); // regex hands built without escapes
  // argv layout: robust to both 'bun -e script id ...' (user args start at
  // argv[1]).
  const first = process.argv[1];
  const isFile = first && first.endsWith('.js') && require('fs').existsSync(first);
  const args = process.argv.slice(isFile ? 2 : 1).filter(Boolean);
  const tmdb = args[0];
  const mode = args[1] === 'tv' ? 'tv' : 'movie';
  const se = mode === 'tv' ? (args[2] || '1-1') : '';
  const season = parseInt(se.split('-')[0], 10) || 1;
  const episode = parseInt(se.split('-')[1], 10) || 1;
  if (!tmdb) throw new Error('usage: bun -e script <tmdbId> [tv <season-episode>]');
  const jar = new Map();
  const cookieHeader = () =>
    [...jar.entries()].map(([k, v]) => k + '=' + v).join('; ');
  const storeCookies = (res) => {
    for (const c of (res.headers.getSetCookie ? res.headers.getSetCookie() : [])) {
      const [pair] = c.split(';');
      const i = pair.indexOf('=');
      if (i > 0) jar.set(pair.slice(0, i).trim(), pair.slice(i + 1).trim());
    }
  };
  const scriptRe = new RegExp('<script' + B + 'b[^>]*>([' + B + 's' + B + 'S]*?)<' + B + '/script>', 'g');
  const cryptoRe = new RegExp('src="([^"]*em-crypto' + B + '.min' + B + '.js[^"]*)"');
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

  // Eval the player page scripts in a bare sandbox: obfuscated config ->
  // window.__EM_PLAYER__ (fresh credential) and, for tv, the plain
  // window.__EM_EPISODES__ = {seasons:[{season,episodes:[{episode,url,...}]}]}.
  const evalPages = (html, playerURL) => {
    const scripts = [...html.matchAll(scriptRe)].map((m) => m[1]);
    const cfgScript = scripts.find(
      (s) => s && s.indexOf('atob(') !== -1 && s.indexOf('(0,eval)(decodeURIComponent(escape(') !== -1,
    );
    if (!cfgScript) throw new Error('no obfuscated credential script');
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
    w.fetch = () => log('page fetch shim called');
    w.self = w;
    const ctx = require('vm').createContext({
      window: w, self: w, document: w.document, location: w.location,
      navigator: w.navigator, atob, escape, unescape, decodeURIComponent,
      encodeURIComponent, setTimeout() {}, setInterval() {},
      requestAnimationFrame() {}, console,
    });
    try { runInNewContext(cfgScript, ctx); } catch (e) {
      log('cfg script err (continued):', e.message);
    }
    if (!w.__EM_PLAYER__) {
      const ctx2 = require('vm').createContext({
        self: w, window: w, document: w.document, location: w.location,
        navigator: w.navigator, atob, escape, unescape, decodeURIComponent,
        encodeURIComponent, setTimeout() {}, setInterval() {}, console,
      });
      runInNewContext(cfgScript, ctx2);
    }
    if (mode === 'tv') {
      const epsScript = scripts.find((s) => s && s.indexOf('__EM_EPISODES__') !== -1);
      if (!epsScript) throw new Error('tv player page has no __EM_EPISODES__');
      try { runInNewContext(epsScript, { window: w, self: w, console }); } catch (e) { log('eps err', e.message); }
      if (!w.__EM_EPISODES__) throw new Error('no __EM_EPISODES__ parsed');
    }
    return w;
  };

  // Walk one embed url through the welcome gate to a fresh credential.
  const playerFor = async (embedURL) => {
    const r1 = await fetch(embedURL, {
      headers: { 'User-Agent': UA, Referer: 'https://embedmaster.com/' },
    });
    storeCookies(r1);
    const embedHost = new URL(embedURL).origin;
    const playerURL = r1.url;
    let html = await r1.text();
    log('player', r1.status, html.length);
    if (html.indexOf('atob(') === -1 || html.indexOf('v2/assets') === -1) {
      const r2 = await fetch(playerURL, {
        method: 'POST',
        headers: {
          'User-Agent': UA,
          Cookie: cookieHeader(),
          'Content-Type': 'application/x-www-form-urlencoded',
          Origin: embedHost,
          Referer: 'https://embedmaster.com/',
        },
        body: 'tr=https%3A%2F%2Fembedmaster.com%2F',
      });
      storeCookies(r2);
      html = await r2.text();
      log('player html POST', r2.status, html.length);
    }
    const w = evalPages(html, playerURL);
    const cfg = w.__EM_PLAYER__;
    if (!cfg) throw new Error('no __EM_PLAYER__ credential');
    const cryptoPath = (html.match(cryptoRe) || [])[1];
    if (!cryptoPath) throw new Error('em-crypto src not found');
    const rr = await fetch(new URL(cryptoPath, playerURL).href, {
      headers: { 'User-Agent': UA, Referer: playerURL },
    });
    return { playerURL, cfg, win: w, cryptoSrc: await rr.text() };
  };

  // Feed + resolve+decrypt every server of a credential. One JSON line each.
  const resolveAll = async (p) => {
    const cfg = p.cfg;
    const fr = await fetch(new URL(cfg.feedUrl + cfg.scrapeToken).href, {
      headers: {
        'User-Agent': UA, Accept: 'application/x-ndjson',
        Origin: 'https://embdmstrplayer.com', Referer: p.playerURL,
      },
    });
    const feed = await fr.text();
    const rows = feed.split(kl).map((s) => s.trim()).filter(Boolean).map(JSON.parse);
    const servers = rows.filter((r) => r.type === 'server');
    log('feed', fr.status, 'servers', servers.length, servers.map((s) => s.source_name).join('|'));
    const dw = {
      location: { protocol: 'https:' }, navigator: { userAgent: UA },
      Uint8Array, Uint16Array, Uint32Array, Int8Array, Int16Array, Int32Array,
      ArrayBuffer, DataView, TextDecoder, TextEncoder, atob, btoa, Math, Error,
      Promise, JSON, String, Object, Date, Array, RegExp, Symbol, BigInt,
      performance, console,
    };
    dw.self = dw; dw.window = dw;
    require('vm').createContext(dw);
    runInNewContext(p.cryptoSrc, dw);
    const c = dw.EMCrypto;
    if (!c) throw new Error('no EMCrypto');
    const key = c.hexToBytes(cfg.pk);
    const rURL = new URL(cfg.resolveUrl, p.playerURL).href.replace(new RegExp(B + B + '/$'), '');
    let resolved = 0;
    for (const s of servers) {
      let data = null;
      let err = null;
      const fetchResolve = async () => {
        const rs = await fetch(rURL + '?src=' + s.source_url, {
          headers: {
            'User-Agent': UA, Referer: p.playerURL,
            'X-EM-Ticket': cfg.ticket, 'X-Em-Player': '1',
          },
        });
        const blob = await rs.text();
        return JSON.parse(c.decrypt(blob, key));
      };
      // resolve.php responses occasionally fail the EMCrypto atob step
      // ('Invalid character') — a single retry with the same credential
      // routinely succeeds, mirrors what a browser reload would do.
      try {
        data = await fetchResolve();
      } catch (e1) {
        err = e1.message;
        try { data = await fetchResolve(); err = null; } catch (e2) { err = e2.message; }
      }
      if (data && data.ok) {
        resolved++;
        log('  =>', s.source_name, data.kind, data.quality || '', 'subs', (data.subtitles || []).length);
      } else {
        log('  !!', s.source_name, String(err || (data && data.message) || 'decrypt').slice(0, 80));
      }
      process.stdout.write(JSON.stringify({
        server: s.source_name,
        feed: { quality: s.source_quality, type: s.source_type },
        data, err,
      }) + kl);
    }
    return { resolved, total: servers.length };
  };

  const embed =
    (mode === 'tv'
      ? 'https://embedmaster.link/tv/' + tmdb + '/' + season + '/' + episode
      : 'https://embedmaster.link/movie/' + tmdb) +
    '?skin=onyx&welcome_page=on&autoplay=off';

  let meta;
  if (mode === 'movie') {
    log('movie', tmdb);
    const p = await playerFor(embed);
    const r = await resolveAll(p);
    meta = { mode, tmdb, playerURL: p.playerURL, embed, ticketLen: p.cfg.ticket.length, pkLen: p.cfg.pk.length, order: p.cfg.order || [] };
    confirm(r);
  } else {
    // TV: parse the episode list off the player page ("before streaming").
    log('tv', tmdb, season + '-' + episode);
    const p0 = await playerFor(embed);
    const list = p0.win.__EM_EPISODES__;
    const epsOut = [];
    for (const sn of list.seasons) {
      log('season', sn.season, ':', (sn.episodes || []).length, 'episodes');
      for (const ep of sn.episodes || []) {
        epsOut.push({ season: sn.season, episode: ep.episode, title: ep.title, url: ep.url });
      }
    }
    if (!epsOut.length) throw new Error('episode list is empty');
    // Selected episode, lazily: only this one gets player+credential+resolve.
    const selURL = (epsOut.find((e) => String(e.season) === String(season) && String(e.episode) === String(episode)) || {}).url;
    if (!selURL) throw new Error('episode not in list: S' + season + 'E' + episode);
    log('selected S' + season + 'E' + episode);
    const p1 = await playerFor(selURL);
    const r = await resolveAll(p1);
    meta = {
      mode, tmdb, playerURL: p1.playerURL, embed: selURL,
      ticketLen: p1.cfg.ticket.length, pkLen: p1.cfg.pk.length,
      order: p1.cfg.order || [],
      episodes: { showTitle: list.showTitle, current: list.current, seasons: list.seasons.length, count: epsOut.length, sample: epsOut.slice(0, 3) },
    };
    confirm(r);
  }
  function confirm(r) {
    process.stdout.write(JSON.stringify({
      done: true, resolved: r.resolved, total: r.total, meta,
    }) + kl);
  }
})().catch((e) => {
  console.error('FATAL', e.message);
  process.exit(1);
});`

type freshRow struct {
	Server string `json:"server"`
	Feed   struct {
		Quality string `json:"quality"`
		Type    int    `json:"type"`
	} `json:"feed"`
	Data *resolvedServer `json:"data"`
	Err  string          `json:"err"`
}

type freshResult struct {
	Rows      []freshRow
	Resolved  int
	Total     int
	PlayerURL string
	EmbedURL  string
	TicketLen int
	PKLen     int
	Order     []string
	Mode      string
	Episodes  struct {
		ShowTitle string `json:"showTitle"`
		Seasons   int    `json:"seasons"`
		Count     int    `json:"count"`
		Current   struct {
			Season  int `json:"season"`
			Episode int `json:"episode"`
		} `json:"current"`
	} `json:"episodes"`
}

const bunDecryptScript = `const inp=JSON.parse(require("fs").readFileSync(0,"utf8"));
const {runInNewContext}=require("vm");
const w={location:{protocol:"https:"},navigator:{userAgent:"Mozilla/5.0"},
	Uint8Array,TextDecoder,atob,Math,Error,Promise,JSON,String,Object,Date};
runInNewContext(inp.crypto,{window:w,self:w});
const c=w.EMCrypto, key=c.hexToBytes(inp.pk);
for (const b of inp.blobs) {
	let parsed=null;
	try { parsed=JSON.parse(c.decrypt(b.text,key)); } catch (e) {}
	process.stdout.write(JSON.stringify({src:b.src,data:parsed})+"\n");
}`

func runNode(t *testing.T, script, stdin string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "bun", "-e", script)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("bun -e failed: %v\nstderr:\n%s", err, stderr.String())
	}
	return stdout.String()
}

func contentText(c Content) string {
	if c.Encoding == "base64" && c.Text != "" {
		if b, err := base64.StdEncoding.DecodeString(c.Text); err == nil {
			return string(b)
		}
	}
	return c.Text
}

func header(name string, hs []Header) string {
	for _, h := range hs {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

var inlineScriptRe = regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)

// NOTE: returns every inline script body (src-less) in document order.
func inlineScripts(doc string) []string {
	var out []string
	for _, m := range inlineScriptRe.FindAllStringSubmatch(doc, -1) {
		body := strings.TrimSpace(m[1])
		if body != "" {
			out = append(out, body)
		}
	}
	return out
}

// TestScrapeServersVideosSubtitlesFromHAR replays a recorded embedmaster.com
// session from a HAR set via $EMBEDMASTER_HAR (optional): given nothing but a
// tmdb id it resolves the embed URL, auto-resolves the player credentials
// (ticket+pk+scrapeToken), decrypts every recorded resolve.php payload via the
// page's own em-crypto (run with `bun -e`), and produces the server list
// (sizes), video URLs and subtitles. Skips when the HAR is not provided.
func TestScrapeServersVideosSubtitlesFromHAR(t *testing.T) {
	t.Parallel()
	tmdbID := "1434581" // default: the id used as the recorded reference
	if env := os.Getenv("TMDB_ID"); env != "" {
		tmdbID = env
	}

	harPath := os.Getenv("EMBEDMASTER_HAR")
	if harPath == "" {
		t.Skip("EMBEDMASTER_HAR tidak diset; rekaman HAR tidak wajib ada")
	}
	entries := readHAREntries(t, harPath)
	if len(entries) < 250 {
		t.Fatalf("expected a full recording, got only %d entries", len(entries))
	}

	// 1. tmdb id -> embed URL (recorded embedmaster.link/movie/<id> request).
	embedIdx := -1
	for i, e := range entries {
		if strings.HasPrefix(e.Request.URL, "https://embedmaster.link/movie/"+tmdbID) {
			embedIdx = i
			break
		}
	}
	if embedIdx < 0 {
		t.Fatalf("tmdb id %s: no embedmaster.link/movie request recorded", tmdbID)
	}
	embedURL := entries[embedIdx].Request.URL
	if entries[embedIdx].Response.Status != 302 {
		t.Fatalf("embed request expected 302, got %d", entries[embedIdx].Response.Status)
	}

	// 2. embed -> player page: auto-resolve the redirect (location header).
	playerURL := header("location", entries[embedIdx].Response.Headers)
	if !strings.HasPrefix(playerURL, "https://embdmstrplayer.com/v2/") {
		t.Fatalf("unexpected player redirect: %q", playerURL)
	}

	var playerHTML, cryptoSrc string
	var resolveSrcs []string
	var vttBody string
	var scrapeFeedURL string

	for _, e := range entries {
		url := e.Request.URL
		text := contentText(e.Response.Content)
		switch {
		case strings.HasPrefix(url, playerURL) && e.Response.Status == 200 && text != "":
			if strings.Contains(text, "__EM_I18N__") { // leading GET/POST player page
				playerHTML += text
			}
		case strings.Contains(url, "em-crypto.min.js") && text != "":
			cryptoSrc = text
		case strings.Contains(url, "/v2/api/resolve.php?src=") && text != "":
			resolveSrcs = append(resolveSrcs, url)
		case strings.HasPrefix(url, "https://scrapemaster.net/subtitles/") && text != "":
			vttBody = text
		case strings.Contains(url, "scrapemaster.net/api/sources/"):
			scrapeFeedURL = url
		}
	}
	if playerHTML == "" {
		t.Fatalf("player page not found for %s", playerURL)
	}
	if cryptoSrc == "" {
		t.Fatal("em-crypto.min.js not found in the recording")
	}

	// 3. Credential auto-resolve: the obfuscated inline config decrypts to
	// window.__EM_PLAYER__ = {ticket, pk, scrapeToken, ...}. Extract that one
	// inline script and let bun -e run it in a bare sandbox (no network).
	var credScript string
	for _, s := range inlineScripts(playerHTML) {
		if strings.Contains(s, "_krmx=") {
			credScript = s
			break
		}
	}
	if credScript == "" {
		t.Fatal("recorded obfuscated credential script (_krmx) not found in player page")
	}
	var cred emCredential
	if err := json.Unmarshal([]byte(runNode(t, bunCredentialScript, credScript)), &cred); err != nil {
		t.Fatalf("credential JSON: %v", err)
	}
	if len(cred.PK) != 64 {
		t.Fatalf("unexpected pk credential: %q", cred.PK)
	}
	if len(cred.Ticket) == 0 {
		t.Fatal("empty ticket credential")
	}

	// 4. The recorded sources feed URL must exactly be feedUrl + scrapeToken,
	// i.e. the credentials the page handed out re-derive the /api/sources URL.
	want := cred.FeedURL + cred.ScrapeToken
	if scrapeFeedURL != want {
		t.Fatalf("feed URL mismatch:\n recorded: %s\n resolved: %s", scrapeFeedURL, want)
	}

	// 5. Auto-resolve every recorded resolve.php payload with the page's own
	// crypto (bun -e decrypts, Go never touches the network).
	if len(resolveSrcs) == 0 {
		t.Fatal("no resolve.php responses recorded")
	}
	type blob struct {
		Src  string `json:"src"`
		Text string `json:"text"`
	}
	var blobs []blob
	seen := map[string]bool{}
	for _, rURL := range resolveSrcs {
		src := strings.SplitN(rURL, "src=", 2)[1]
		if seen[src] {
			continue
		}
		seen[src] = true
		// find the recorded response text for this src (re-iterates entries)
		var respText string
		for _, e := range entries {
			if strings.Contains(e.Request.URL, "resolve.php?src="+src) && e.Response.Content.Text != "" {
				respText = contentText(e.Response.Content)
				break
			}
		}
		if respText == "" {
			t.Fatalf("resolve src %s... requested but no recorded response body", first40(src))
		}
		blobs = append(blobs, blob{Src: src, Text: respText})
	}
	decryptIn, err := json.Marshal(map[string]interface{}{
		"crypto": cryptoSrc,
		"pk":     cred.PK,
		"blobs":  blobs,
	})
	if err != nil {
		t.Fatalf("marshal decrypt input: %v", err)
	}

	result := &ScrapeResult{
		TMDBID:       tmdbID,
		EmbedURL:     embedURL,
		PlayerURL:    playerURL,
		Credential:   cred,
		Servers:      map[string]*resolvedServer{},
		ServerOrder:  cred.Order,
		SubtitleBody: vttBody,
	}
	var envelopes []resolvedEnvelope
	for _, line := range strings.Split(runNode(t, bunDecryptScript, string(decryptIn)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var env resolvedEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatalf("decrypt output %q: %v", line[:min(len(line), 120)], err)
		}
		envelopes = append(envelopes, env)
		if env.Data != nil && env.Data.OK {
			result.Servers[env.Src] = env.Data
		}
	}
	if len(envelopes) != len(blobs) {
		t.Fatalf("bun decrypted %d of %d blobs", len(envelopes), len(blobs))
	}
	if len(result.Servers) < 2 {
		t.Fatalf("auto-resolve produced %d playable servers, want >=2", len(result.Servers))
	}

	// 6. Assertions every call site consumes: sizes, playable video, subs.
	var qualities, subtitleLangs = map[string]bool{}, map[string]bool{}
	playable, withSubs := false, false
	for src, s := range result.Servers {
		if s.URL == "" || s.Type == "" {
			t.Fatalf("server %s resolves without playable url/type", src[:16])
		}
		if !strings.HasPrefix(s.URL, "https://") {
			t.Fatalf("server %s: non-https video url", src[:16])
		}
		playable = true
		if s.Quality != "" {
			qualities[s.Quality] = true
		}
		for _, q := range s.Qualities {
			qualities[q.Label] = true
		}
		for _, st := range s.Subtitles {
			subtitleLangs[st.Lang] = true
		}
		if len(s.Subtitles) > 0 {
			withSubs = true
		}
	}
	if !playable {
		t.Fatal("no playable video resolved")
	}
	if !withSubs {
		t.Fatal("no server exposes subtitles")
	}
	for _, want := range []string{"1080p", "480p", "360p", "FULL HD"} {
		if !qualities[want] {
			t.Errorf("video size/quality %q missing (have %v)", want, keys(qualities))
		}
	}
	for _, want := range []string{"en", "id", "tl", "ar"} {
		if !subtitleLangs[want] {
			t.Errorf("subtitle language %q missing (have %v)", want, keys(subtitleLangs))
		}
	}

	// 7. The player's own server list (ids in preferred order + kind).
	if len(result.ServerOrder) == 0 || len(result.Credential.Kinds) == 0 {
		t.Fatal("credential contains no server list (order/kinds)")
	}

	// 8. Recorded subtitle body must be a real VTT we did not have to fetch.
	if !strings.HasPrefix(result.SubtitleBody, "WEBVTT") {
		t.Errorf("recorded subtitle body is not a VTT: %q", first40(result.SubtitleBody))
	}

	t.Logf("OK id=%s embed=%s servers=%d sizes=%v subs=%v ticketLen=%d",
		tmdbID, embedURL, len(result.Servers), keys(qualities), keys(subtitleLangs), len(cred.Ticket))
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func first40(s string) string {
	if len(s) > 40 {
		return s[:40]
	}
	return s
}

// TestFreshScrapeServersVideosSubtitles runs the whole scrape pipeline live
// (network happens only inside `bun -e`): given nothing but a tmdb id it
// walks embed -> player -> welcome gate, mints a brand-new credential
// (ticket+pk+scrapeToken) by executing the player page's obfuscated config in
// a sandbox, fetches the NDJSON sources feed and resolves+decrypts every
// server. Works for any tmdb id whose content exists on embedmaster —
// unlike the HAR replay above, which is bound to the recorded session.
func TestFreshScrapeServersVideosSubtitles(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun not installed; fresh scrape needs it (Go itself never touches the network)")
	}
	tmdbID := "1434581" // default: known content (3 servers, 7 subtitle langs)
	if env := os.Getenv("TMDB_ID"); env != "" {
		tmdbID = env
	}

	res := runFreshPipeline(t, tmdbID, "movie")

	// Credential was minted fresh: ticket/pk must be present (pk=64 hex).
	if res.TicketLen == 0 || res.PKLen != 64 {
		t.Fatalf("credential not minted: ticketLen=%d pkLen=%d", res.TicketLen, res.PKLen)
	}
	if res.PlayerURL == "" || !strings.HasPrefix(res.PlayerURL, "https://embdmstrplayer.com/v2/") {
		t.Fatalf("unexpected player url %q", first40(res.PlayerURL))
	}

	// Row shape: every requested server produced a decrypt result.
	if len(res.Rows) == 0 || len(res.Rows) != res.Total {
		t.Fatalf("got %d rows for total %d", len(res.Rows), res.Total)
	}
	if res.Resolved < 1 {
		t.Fatalf("fresh scrape resolved %d of %d servers", res.Resolved, res.Total)
	}

	// Assertions every call site consumes: sizes, playable video, subtitles.
	var qualities, subtitleLangs = map[string]bool{}, map[string]bool{}
	playable, withSubs := false, false
	for _, row := range res.Rows {
		d := row.Data
		if d == nil || !d.OK {
			if row.Err != "" {
				t.Logf("server %s not resolved: %s", row.Server, first40(row.Err))
			}
			continue
		}
		if d.URL == "" || d.Kind == "" {
			t.Fatalf("server %s resolves without kind/url", first40(row.Server))
		}
		if d.Kind == "direct" && d.Type == "" {
			t.Fatalf("server %s: direct resolve without type", first40(row.Server))
		}
		if !strings.HasPrefix(d.URL, "https://") {
			t.Fatalf("server %s: non-https video url", first40(row.Server))
		}
		playable = true
		if d.Quality != "" {
			qualities[d.Quality] = true
		}
		for _, q := range d.Qualities {
			qualities[q.Label] = true
		}
		for _, st := range d.Subtitles {
			if st.Lang == "" || st.URL == "" {
				t.Fatalf("server %s: subtitle track missing lang/url: %+v", first40(row.Server), st)
			}
			subtitleLangs[st.Lang] = true
		}
		if len(d.Subtitles) > 0 {
			withSubs = true
		}
		t.Logf("OK server=%s kind=%s quality=%s qualities=%d subs=%d", row.Server, d.Kind, d.Quality, len(d.Qualities), len(d.Subtitles))
	}
	if !playable {
		t.Fatal("no playable video resolved")
	}
	if len(qualities) == 0 {
		t.Fatal("no video size/quality label resolved")
	}
	t.Logf("resolved fresh: id=%s player=%s... servers=%d/%d sizes=%v subs=%v ticketLen=%d pkLen=%d",
		tmdbID, first40(res.PlayerURL), res.Resolved, res.Total, keys(qualities), keys(subtitleLangs), res.TicketLen, res.PKLen)
	_ = withSubs // content-dependent: some titles expose subtitles, some do not
}

// runFreshPipeline runs bunFreshScript for one pipeline invocation and
// parses its output: one JSON line per resolved server row plus a final
// {"done":...} summary. mode is "movie" or ("tv" + season-episode); all
// network traffic happens inside `bun -e`.
func runFreshPipeline(t *testing.T, args ...string) freshResult {
	t.Helper()
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun not installed; fresh scrape needs it (Go itself never touches the network)")
	}
	// argv layout matches bunFreshScript: tmdbId [movie|tv [season-episode]].
	if len(args) > 1 && args[1] == "tv" {
		t.Logf("fresh scrape: tv id=%s season-episode=%s", args[0], args[2])
	} else {
		t.Logf("fresh scrape: movie id=%s", args[0])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	argv := append([]string{"-e", bunFreshScript}, args...)
	cmd := exec.CommandContext(ctx, "bun", argv...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("bun -e fresh scrape failed: %v\nstderr:\n%s", err, stderr.String())
	}
	// The [fresh] progress lines come on stderr; surface them in the test log.
	for _, line := range strings.Split(stderr.String(), "\n") {
		if strings.TrimSpace(line) != "" {
			t.Log(line)
		}
	}

	var res freshResult
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, `"done":true`) {
			var done struct {
				Done     bool `json:"done"`
				Resolved int  `json:"resolved"`
				Total    int  `json:"total"`
				Meta     struct {
					PlayerURL string   `json:"playerURL"`
					EmbedURL  string   `json:"embed"`
					TicketLen int      `json:"ticketLen"`
					PKLen     int      `json:"pkLen"`
					Order     []string `json:"order"`
					Mode      string   `json:"mode"`
					Episodes  struct {
						ShowTitle string `json:"showTitle"`
						Seasons   int    `json:"seasons"`
						Count     int    `json:"count"`
						Current   struct {
							Season  int `json:"season"`
							Episode int `json:"episode"`
						} `json:"current"`
					} `json:"episodes"`
				} `json:"meta"`
			}
			if err := json.Unmarshal([]byte(line), &done); err != nil {
				t.Fatalf("summary line: %v", err)
			}
			res.Resolved, res.Total = done.Resolved, done.Total
			res.PlayerURL, res.EmbedURL = done.Meta.PlayerURL, done.Meta.EmbedURL
			res.TicketLen, res.PKLen = done.Meta.TicketLen, done.Meta.PKLen
			res.Order = done.Meta.Order
			res.Mode = done.Meta.Mode
			res.Episodes = done.Meta.Episodes
			if !done.Done {
				t.Fatal("summary line without done=true")
			}
			continue
		}
		var row freshRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("row %s: %v", first40(line), err)
		}
		res.Rows = append(res.Rows, row)
	}
	return res
}

// TestFreshTVScrapeEpisodes handles tv series: first entry point tv/<id>/<s>/<e>
// player page carries the WHOLE episode list (window.__EM_EPISODES__), parsed
// up-front. Only the SELECTED season-episode is then walked again through its
// own player page (fresh credential) and resolved to playable urls —
// episodes are parsed lazily, nothing else is fetched per-episode.
func TestFreshTVScrapeEpisodes(t *testing.T) {
	tvID := "283938" // default: "Even the Student Council Has Its Holes!" (S1 = 4 eps)
	// Defaults match the recorded reference session:
	// the session watched S1E4 and its resolved feed+resolve are the
	// ground truth for the tv flow being re-driven live here.
	season, episode := 1, 4 // S1E4 default: S01E04 "Even School Staff Have Their Holes…"
	df := tvID == "283938" && os.Getenv("TMDB_TV_ID") == ""
	if env := os.Getenv("TMDB_TV_ID"); env != "" {
		tvID = env
	}
	if env := os.Getenv("TMDB_TV_SEASON"); env != "" {
		season, _ = strconv.Atoi(env)
	}
	if env := os.Getenv("TMDB_TV_EPISODE"); env != "" {
		episode, _ = strconv.Atoi(env)
	}
	se := fmt.Sprintf("%d-%d", season, episode)
	res := runFreshPipeline(t, tvID, "tv", se)

	// Fresh credential for the selected episode.
	if res.TicketLen == 0 || res.PKLen != 64 {
		t.Fatalf("credential not minted: ticketLen=%d pkLen=%d", res.TicketLen, res.PKLen)
	}
	if res.PlayerURL == "" || !strings.HasPrefix(res.PlayerURL, "https://embdmstrplayer.com/v2/") {
		t.Fatalf("unexpected player url %q", first40(res.PlayerURL))
	}

	// Episode list was parsed: show title, >=1 season, >=1 episode, and the
	// current selection must match the request.
	if res.Mode != "tv" {
		t.Fatalf("unexpected pipeline mode %q", res.Mode)
	}
	if res.Episodes.ShowTitle == "" || res.Episodes.Seasons < 1 || res.Episodes.Count < 1 {
		t.Fatalf("episode list not parsed: showTitle=%q seasons=%d count=%d",
			res.Episodes.ShowTitle, res.Episodes.Seasons, res.Episodes.Count)
	}
	if df {
		// Reference facts from the recorded session.
		const refTitle = "Even the Student Council Has Its Holes!"
		if res.Episodes.ShowTitle != refTitle {
			t.Fatalf("default tv id but showTitle %q != %q", res.Episodes.ShowTitle, refTitle)
		}
		if res.Episodes.Count < 4 { // S1 has exactly 4 episodes in the reference list
			t.Fatalf("unexpected episode list size: %d < 4", res.Episodes.Count)
		}
	}
	if res.Episodes.Current.Season != season || res.Episodes.Current.Episode != episode {
		t.Fatalf("current S%dE%d in list, want S%dE%d",
			res.Episodes.Current.Season, res.Episodes.Current.Episode, season, episode)
	}
	if !strings.Contains(res.EmbedURL, "/tv/"+tvID+"/") {
		t.Fatalf("selected episode url is not a tv episode url: %s", first40(res.EmbedURL))
	}

	// Only the selected episode got its own feed+resolve.
	if len(res.Rows) == 0 || len(res.Rows) != res.Total {
		t.Fatalf("got %d rows for total %d", len(res.Rows), res.Total)
	}
	if res.Resolved < 1 {
		t.Fatalf("selected episode resolved %d of %d servers", res.Resolved, res.Total)
	}
	sizes := map[string]bool{}
	playable := false
	for _, row := range res.Rows {
		d := row.Data
		if d == nil || !d.OK {
			t.Logf("server %s not resolved: %s", row.Server, first40(row.Err))
			continue
		}
		if d.URL == "" || d.Kind == "" {
			t.Fatalf("server %s resolves without kind/url", first40(row.Server))
		}
		if d.Kind == "direct" && d.Type == "" {
			t.Fatalf("server %s: direct resolve without type", first40(row.Server))
		}
		if !strings.HasPrefix(d.URL, "https://") {
			t.Fatalf("server %s: non-https video url", first40(row.Server))
		}
		playable = true
		if d.Quality != "" {
			sizes[d.Quality] = true
		}
		for _, q := range d.Qualities {
			sizes[q.Label] = true
		}
		t.Logf("OK server=%s kind=%s quality=%s qualities=%d subs=%d", row.Server, d.Kind, d.Quality, len(d.Qualities), len(d.Subtitles))
	}
	if !playable || len(sizes) == 0 {
		t.Fatal("episode not playable: no direct/embed url with sizes")
	}
	t.Logf("resolved fresh tv: id=%s S%dE%d show=%q player=%s... servers=%d/%d sizes=%v ticketLen=%d pkLen=%d",
		tvID, season, episode, res.Episodes.ShowTitle, first40(res.PlayerURL), res.Resolved, res.Total, keys(sizes), res.TicketLen, res.PKLen)
}
