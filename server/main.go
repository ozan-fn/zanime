package main

import (
	"log"
	"os"

	"miruro-proxy/internal/anilist"
	"miruro-proxy/internal/api"
	"miruro-proxy/internal/miruro"
	"miruro-proxy/internal/proxy"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	app := api.New(anilist.New(), miruro.New(), proxy.New())
	log.Printf("miruro proxy listening on http://%s", addr)
	log.Fatal(app.Listen(addr))
}
