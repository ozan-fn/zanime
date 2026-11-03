package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"time"

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
	// /off = soft kill tanpa kredensial: balas dulu, lalu hentikan listener
	// secara graceful (permintaan yang sedang jalan diberi waktu selesai).
	// Didaftarkan sebelum Register supaya tidak kena rate limit/SPA fallback.
	app.Get("/off", func(c *fiber.Ctx) error {
		go func() {
			time.Sleep(100 * time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := app.ShutdownWithContext(ctx); err != nil {
				log.Printf("shutdown: %v", err)
			}
		}()
		return c.SendString("off")
	})
	webapp.New(webRoot).Register(app)

	log.Printf("listening on %s", *addr)
	if err := app.Listen(*addr); err != nil {
		log.Fatal(err)
	}
}
