package app

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"zanime/internal/em"
	"zanime/internal/logbus"
	"zanime/internal/miruro"
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
	mir  *miruro.Client
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
	return &Server{web: webRoot, tmdb: tmdb.New(), em: emc, mir: miruro.New(), res: res, logs: logs, sub: subdl.New(), tr: translate.New()}
}

// Register memasang seluruh route. Fiber menjalankan tiap permintaan di
// goroutine sendiri, jadi panggilan TMDB/scrape tidak memblokir permintaan lain.
func (s *Server) Register(app *fiber.App) {
	app.Use(recover.New(), rateLimit(maxRPS))

	app.Get("/", s.index)
	app.Get("/assets/*", s.webFile)
	for _, p := range staticFiles {
		app.Get(p, s.webFile)
	}

	app.Get("/healthz", func(c *fiber.Ctx) error { return c.SendString("ok") })

	app.Get("/api/search", s.apiSearch)
	app.Get("/api/meta/:tab/:id", s.apiMeta)
	app.Get("/api/streams", s.apiStreams)
	app.Get("/api/subtitles", s.apiSubtitles)
	app.Get("/api/subtitles/file", s.apiSubtitleFile)
	app.Get("/api/translate", s.apiTranslate)
	app.Get("/api/translate/file", s.apiTranslateFile)
	app.Get("/api/logs", s.logsRoute())

	app.Get("/s/:slug", s.streamProxy)
	app.Head("/s/:slug", s.streamProxy)
	// SPA fallback: React Router punya route `/` dan `*` (404), jadi path lain
	// tetap dapat index.html; endpoint API yang salah tulis tetap JSON 404.
	// Didaftarkan terakhir: middleware yang tidak memanggil Next menghentikan
	// rantai, jadi route di atasnya harus sudah terpasang.
	app.Use(s.spaFallback)
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
func rateLimit(rps float64) fiber.Handler {
	var (
		mu     sync.Mutex
		tokens = rps
		last   = time.Now()
	)
	return func(c *fiber.Ctx) error {
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
			c.Set("Retry-After", "1")
			return errJSON(c, http.StatusTooManyRequests, "terlalu banyak permintaan")
		}
		return c.Next()
	}
}

// errJSON mengirim {"error": "..."} seperti gin.H{"error": ...} dulu.
func errJSON(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(map[string]string{"error": msg})
}

// fail memetakan error handler ke status 502/504.
func (s *Server) fail(c *fiber.Ctx, err error) error {
	status := http.StatusBadGateway
	if errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusGatewayTimeout
	}
	return errJSON(c, status, err.Error())
}
