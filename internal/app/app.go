package app

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/goceleris/celeris"
	"github.com/goceleris/celeris/middleware/recovery"

	"zanime/internal/em"
	"zanime/internal/logbus"
	"zanime/internal/resolve"
	"zanime/internal/subdl"
	"zanime/internal/tmdb"
	"zanime/internal/translate"
)

// Streamer adalah irisan em.Client yang dipakai server (mudah di-stub test).
type Streamer interface {
	Streams(ctx context.Context, tab, id string, tv *em.TVEpisode) ([]em.Server, error)
}

// Server menyatukan dependensi handler; handler-nya ada di web.go, api.go, dan
// proxy.go supaya tiap berkas tetap kecil.
type Server struct {
	web  fs.FS
	tmdb *tmdb.Client
	em   Streamer
	sub  *subdl.Client
	res  *resolve.Pool
	logs *logbus.Hub
	tr   *translate.Client
}

// New menyiapkan dependensi produksi: TMDB, embedmaster, subdl, dan pool slug.
func New(webRoot fs.FS) *Server {
	res := resolve.New()
	res.Tidy() // buang slug lama warisan run sebelumnya (./tmp/resolve rapi)
	logs := logbus.New()
	emc := em.New()
	emc.SetLogger(hubLogger{hub: logs})
	return &Server{web: webRoot, tmdb: tmdb.New(), em: emc, res: res, logs: logs, sub: subdl.New(), tr: translate.New()}
}

// Register memasang seluruh route. Semua yang menyentuh jaringan ditandai
// Async: handler inline berjalan di event loop, jadi panggilan TMDB/scrape
// tidak boleh memblokirnya.
func (s *Server) Register(mux *celeris.Server) {
	mux.Use(recovery.New(), rateLimit(maxRPS))

	mux.GET("/", s.index)
	mux.GET("/assets/*filepath", s.webFile)
	for _, p := range staticFiles {
		mux.GET(p, s.webFile)
	}
	// SPA fallback: React Router punya route `/` dan `*` (404), jadi path lain
	// tetap dapat index.html; endpoint API yang salah tulis tetap JSON 404.
	mux.NotFound(s.spaFallback)

	mux.GET("/healthz", func(c *celeris.Context) error { return c.String(http.StatusOK, "ok") })

	mux.GET("/api/search", s.apiSearch).Async()
	mux.GET("/api/meta/:tab/:id", s.apiMeta).Async()
	mux.GET("/api/streams", s.apiStreams).Async()
	mux.GET("/api/subtitles", s.apiSubtitles).Async()
	mux.GET("/api/subtitles/file", s.apiSubtitleFile).Async()
	mux.GET("/api/translate", s.apiTranslate).Async()
	mux.GET("/api/translate/file", s.apiTranslateFile).Async()
	mux.GET("/api/logs", s.logsRoute())

	mux.GET("/s/:slug", s.streamProxy).Async()
	mux.HEAD("/s/:slug", s.streamProxy).Async()
}

// staticFiles adalah berkas non-aset yang disajikan dari root web/dist.
var staticFiles = []string{
	"/favicon.ico", "/favicon.svg", "/icon-180.png", "/icon-512.png", "/og.png", "/site.webmanifest",
}

// maxRPS membatasi total permintaan per detik. Sengaja dipatok di kode (bukan
// env): cukup untuk banyak segmen HLS paralel, dan lonjakan tetap tertahan.
const maxRPS = 200

// rateLimit adalah token bucket sederhana: bucket penuh = rps (burst maksimal
// satu detik), isi ulang mengikuti waktu. Kelebihan dibalas 429.
func rateLimit(rps float64) celeris.HandlerFunc {
	var (
		mu     sync.Mutex
		tokens = rps
		last   = time.Now()
	)
	return func(c *celeris.Context) error {
		now := time.Now()
		mu.Lock()
		tokens += now.Sub(last).Seconds() * rps
		if tokens > rps {
			tokens = rps
		}
		last = now
		ok := tokens >= 1
		if ok {
			tokens--
		}
		mu.Unlock()
		if !ok {
			c.SetHeader("Retry-After", "1")
			return errJSON(c, http.StatusTooManyRequests, "terlalu banyak permintaan")
		}
		return c.Next()
	}
}

// errJSON mengirim {"error": "..."} seperti gin.H{"error": ...} dulu.
func errJSON(c *celeris.Context, status int, msg string) error {
	return c.JSON(status, map[string]string{"error": msg})
}

// fail memetakan error handler ke status 502/504.
func (s *Server) fail(c *celeris.Context, err error) error {
	status := http.StatusBadGateway
	if errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusGatewayTimeout
	}
	return errJSON(c, status, err.Error())
}
