package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"

	"github.com/gofiber/fiber/v2"

	webapp "zanime/internal/app"
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

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	webapp.New(webRoot).Register(app)

	log.Printf("listening on %s", *addr)
	if err := app.Listen(*addr); err != nil {
		log.Fatal(err)
	}
}
