// Package em mengambil daftar stream embedmaster untuk satu judul: halaman
// player → kredensial segar → feed server → resolve+dekripsi. Semuanya HTTP
// + kripto kecil di Go (lihat crypto.go dan scrape.go); hasilnya di-cache di
// ./tmp/em karena URL stream punya masa berlaku.
package em

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// URL stream hasil resolve punya masa berlaku; cache ./tmp pegang
	// hasilnya cuma selama ini.
	cacheDir   = "tmp/em"
	cacheTTL   = 15 * time.Minute
	scrapeSecs = 120
	// resolveWorkers: berapa permintaan resolve.php serentak. Cukup untuk
	// menyembunyikan latensi (~400 ms/server) tanpa membanjiri embedmaster.
	resolveWorkers = 6
)

// Base adalah host halaman embed; dapat diganti test.
const defaultBase = "https://embedmaster.link"

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

// Sub adalah satu track subtitle VTT (diakses lewat proxy /s/<slug>).
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

// Client mengambil stream dari embedmaster. Base/http dapat ditimpa test.
type Client struct {
	Base string
	http *http.Client
	log  Logger
}

func New() *Client {
	return &Client{Base: defaultBase, http: newHTTP(), log: nopLogger{}}
}

// Streams mengembalikan daftar server stream untuk movie atau satu episode
// tv. id adalah TMDB id (moviedb_id). Cache ./tmp/em dipakai hanya bila belum
// basi (cacheTTL): URL stream terbatas waktu.
func (c *Client) Streams(ctx context.Context, tab, id string, tv *TVEpisode) ([]Server, error) {
	cacheKey := tab + "-" + id
	if tv != nil {
		cacheKey += fmt.Sprintf("-%d-%d", tv.Season, tv.Episode)
	}
	if servers, ok := readCache(cacheKey); ok {
		c.logf("cache", LogOK, "cache segar · %d server (TTL 15 menit)", len(servers))
		return servers, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Sesi sendiri per permintaan (cookie gerbang tidak boleh bocor antar judul).
	client := &Client{Base: c.Base, http: newHTTP(), log: c.log}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), scrapeSecs*time.Second)
	defer cancel()

	c.logf("scrape", LogInfo, "mulai scrape %s %s", tab, id)
	servers, err := client.scrape(runCtx, tab, id, tv)
	if err != nil {
		c.logf("scrape", LogErr, "scrape gagal: %v", err)
		return nil, fmt.Errorf("pipeline scrape: %w", err)
	}
	playable := 0
	for _, s := range servers {
		if s.URL != "" {
			playable++
		}
	}
	if playable == 0 {
		c.logf("scrape", LogErr, "tidak ada server yang bisa diputar (id %s)", id)
		return nil, fmt.Errorf("tidak ada server yang bisa diputar (id %s)", id)
	}
	c.logf("scrape", LogOK, "%d dari %d server bisa diputar", playable, len(servers))
	writeCache(cacheKey, servers)
	return servers, nil
}

// scrape menjalankan seluruh alur: kredensial → feed → resolve per server.
func (c *Client) scrape(ctx context.Context, tab, id string, tv *TVEpisode) ([]Server, error) {
	playerURL, html, err := c.playerPage(ctx, c.embedURL(tab, id, tv))
	if err != nil {
		return nil, err
	}
	if tab == "tv" && tv != nil {
		// Halaman tv pertama memuat daftar episode; halaman epilog itulah
		// yang membawa daftar server.
		ep, err := episodeURL(html, tv.Season, tv.Episode)
		if err != nil {
			c.logf("page", LogErr, "daftar episode tv: %v", err)
			return nil, err
		}
		c.logf("page", LogInfo, "pilih episode S%dE%d", tv.Season, tv.Episode)
		if playerURL, html, err = c.playerPage(ctx, ep); err != nil {
			return nil, err
		}
	}
	c.logf("creds", LogInfo, "ekstrak kredensial pemain…")
	cfg, err := creds(html)
	if err != nil {
		c.logf("creds", LogErr, "kredensial: %v", err)
		return nil, err
	}
	c.logf("creds", LogOK, "kredensial terdekode (ticket + pk + scrape token)")
	rows, err := c.feed(ctx, cfg, playerURL)
	if err != nil {
		c.logf("feed", LogErr, "feed: %v", err)
		return nil, err
	}
	c.logf("feed", LogOK, "feed: %d server (resolve %d paralel)", len(rows), resolveWorkers)

	// Resolve tiap server lewat worker pool (dibatasi resolveWorkers supaya
	// tidak membanjiri embedmaster): berurutan 15 server bisa ~6 dtk, paralel
	// ~1 dtk. Indeks dijaga supaya urutan server di UI tetap sama seperti feed.
	// Tiap URL yang berhasil di-resolve langsung diverifikasi (probe 128 KiB ke
	// host video) supaya server yang cuma halaman embed / tidak bisa diputar
	// tidak ikut ditampilkan.
	servers := make([]Server, len(rows))
	parallel(len(rows), resolveWorkers, func(i int) {
		row := rows[i]
		sid := "srv:" + row.SourceName
		s := Server{Name: row.SourceName}
		c.logf(sid+":resolve", LogInfo, "resolve %s…", row.SourceName)
		data := c.resolve(ctx, cfg, playerURL, row)
		if !data.OK {
			c.logf(sid+":resolve", LogErr, "%s: resolve gagal", row.SourceName)
			s.Err = "server gagal resolve"
			servers[i] = s
			return
		}
		c.logf(sid+":resolve", LogOK, "%s: resolve → %s", row.SourceName, orDefault(data.Type, "?"))
		// verifyStream menulis baris ujinya sendiri (info → ok/err).
		kind, ok := c.verifyStream(ctx, sid+":verify", data.URL)
		if !ok {
			c.logf(sid+":ready", LogErr, "%s disembunyikan: stream tidak bisa diputar", row.SourceName)
			s.Err = "stream tidak bisa diputar"
			servers[i] = s
			return
		}
		// Jenis dari verifikasi menimpa tebakan resolve (mis. server "mp4" yang
		// ternyata playlist → hls).
		s.Type = kind
		s.Quality = data.Quality
		s.Qualities = data.Qualities
		s.Subs = data.Subtitles
		s.URL = data.URL
		c.logf(sid+":ready", LogOK, "%s siap diputar · %s · %d kualitas · %d subtitle",
			row.SourceName, kind, len(data.Qualities), len(data.Subtitles))
		servers[i] = s
	})

	if len(servers) == 0 {
		return nil, fmt.Errorf("embedmaster tidak mengembalikan server (id %s)", id)
	}
	return servers, nil
}

// parallel menjalankan fn(i) untuk i = 0..n-1 memakai `workers` goroutine
// (worker pool): pekerjaan dibagi lewat channel, bukan satu goroutine per item.
func parallel(n, workers int, fn func(i int)) {
	if n <= 0 {
		return
	}
	if workers < 1 {
		workers = 1
	}
	if workers > n {
		workers = n
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for i := range jobs {
				fn(i)
			}
		}()
	}
	for i := range n {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

func typeOf(t string) string {
	if t == "hls" || t == "m3u8" {
		return "hls"
	}
	return "mp4"
}

// orDefault mengembalikan s bila tidak kosong, else def (untuk teks log).
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
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
