package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
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

	app := fiber.New()
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

	// Banner Fiber dicetak dari ln.Addr(); laporkan localhost supaya bisa
	// diklik, sementara socket tetap mendengarkan di alamat asli (0.0.0.0).
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	if err := app.Listener(localhostAddr(ln)); err != nil {
		log.Fatal(err)
	}
}

// localhostAddr membungkus listener agar Addr() melaporkan localhost:<port>,
// bukan alamat bind sebenarnya.
func localhostAddr(ln net.Listener) net.Listener {
	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return ln
	}
	return &addrListener{Listener: ln, addr: textAddr(fmt.Sprintf("localhost:%d", tcp.Port))}
}

type addrListener struct {
	net.Listener
	addr net.Addr
}

func (l *addrListener) Addr() net.Addr { return l.addr }

type textAddr string

func (a textAddr) Network() string { return "tcp" }
func (a textAddr) String() string  { return string(a) }
