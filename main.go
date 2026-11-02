package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"

	"github.com/goceleris/celeris"

	"zanime/internal/app"
	"zanime/internal/config"
)

//go:embed web/dist
var webFiles embed.FS

func main() {
	if err := config.LoadDotEnv(".env"); err != nil {
		log.Printf(".env: %v", err)
	}
	webRoot, err := fs.Sub(webFiles, "web/dist")
	if err != nil {
		log.Fatalf("embedded web files: %v", err)
	}

	addr := flag.String("addr", "0.0.0.0:"+config.String("PORT", "8080"), "HTTP listen address")
	flag.Parse()

	mux := celeris.New(celeris.Config{Addr: *addr})
	app.New(webRoot).Register(mux)

	log.Printf("listening on %s", *addr)
	if err := mux.Start(); err != nil {
		log.Fatal(err)
	}
}
