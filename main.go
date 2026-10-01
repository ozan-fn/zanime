// Command main serves the zanime HTTP API and SPA around hianime.at.
//
// It is deliberately thin: everything it needs lives under internal/ —
// hianime (scraping), stream (embed resolve + HLS proxy), subtitle (Indonesian
// translation jobs) and server (routing). This file only wires those together
// and owns the process lifetime.
package main

import (
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"

	"zanime/internal/server"
	"zanime/internal/subtitle"
)

//go:embed all:web/dist
var distFS embed.FS

// webRoot is the built SPA served at /: index.html plus hashed assets.
var webRoot, _ = fs.Sub(distFS, "web/dist")

func main() {
	// .env di root proyek (gitignored) mengisi ADDR/CACHE_DIR
	// untuk `go run .`; environment yang sudah diset tidak ditimpa.
	_ = godotenv.Load()

	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	cacheDir := flag.String("cache", envOr("CACHE_DIR", ".cache/subtitles"), "subtitle cache directory")
	flag.Parse()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.New(subtitle.NewSubtitleCache(*cacheDir), webRoot),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: streams are long-lived and the server must not cut them.
	}
	log.Printf("listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
