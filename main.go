// Command zanime: serve aplikasi web (frontend React ter-embed + API) di :8080
// (atau $PORT), stream torrent → mp4 progresif. Mode CLI: go run . --cli
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"zanime/internal/meta"
	"zanime/internal/tmpdir"
	"zanime/internal/torrent"
	"zanime/internal/web"
)

func main() {
	cliFlag := flag.Bool("cli", false, "jalankan mode CLI interaktif di terminal")
	webAddr := flag.String("addr", "", "alamat listen mode web (default :$PORT, atau :8080)")
	flag.Parse()
	if *cliFlag {
		if err := runCLI(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	runWeb(listenAddr(*webAddr))
}

// listenAddr: -addr menang, lalu env PORT ("3000" atau ":3000"), fallback :8080.
func listenAddr(flagAddr string) string {
	if flagAddr != "" {
		return flagAddr
	}
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		if strings.Contains(p, ":") {
			return p
		}
		return ":" + p
	}
	return ":8080"
}

func runWeb(addr string) {
	tm, err := torrent.New()
	if err != nil {
		log.Fatalf("torrent client: %v", err)
	}
	defer tm.Close()
	stop := make(chan struct{})
	tm.RunPruneLoop(stop)
	srv := &http.Server{Addr: addr, Handler: web.New(tm).Routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if !web.HasFrontend() {
			log.Printf("frontend belum dibangun: cd web && pnpm install && pnpm run build")
		}
		log.Printf("folder kerja: %s", tmpdir.Base())
		log.Printf("web listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	close(stop)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func runCLI() error {
	in := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("cari anime: ")
		line, _ := in.ReadString('\n')
		q := strings.TrimSpace(line)
		if q == "" {
			continue
		}
		if q == "q" || q == "exit" {
			return nil
		}
		metas, err := meta.SearchAll(q)
		if err != nil {
			return err
		}
		if len(metas) == 0 {
			fmt.Println("tidak ada hasil.")
			continue
		}
		for i, m := range metas {
			typ := "movie"
			if m.Type == "series" {
				typ = "TV"
			}
			fmt.Printf("%3d. [%s] %s (%s)  %s\n", i+1, typ, m.Name, m.Year, m.ID)
		}
		pick, err := pickInt(in, "pilih nomor (0 kembali)", len(metas))
		if err != nil || pick < 0 {
			continue
		}
		m := metas[pick]
		vidID, name := m.ID, m.Name
		if m.Type == "series" {
			full, err := meta.GetMeta(m.Type, m.ID)
			if err != nil {
				return err
			}
			vids := full.Meta.Videos
			for i, v := range vids {
				sp := fmt.Sprintf("S%02dE%02d", v.Season, v.Episode)
				if v.Season == 0 {
					sp = fmt.Sprintf("SP%02d", v.Episode)
				}
				fmt.Printf("%3d. %s  %s\n", i+1, sp, v.Title)
			}
			pi, err := pickInt(in, "pilih episode", len(vids))
			if err != nil || pi < 0 {
				continue
			}
			v := vids[pi]
			vidID = fmt.Sprintf("%s:%d:%d", m.ID, v.Season, v.Episode)
			name = m.Name + " " + v.Title
		}
		items, err := meta.GetStreams(m.Type, vidID)
		if err != nil {
			return err
		}
		if len(items) == 0 && m.Type == "series" {
			// Season 0 / OVA sering kosong di Torrentio — fallback semua torrent serial.
			items, _ = meta.GetStreams(m.Type, strings.Split(vidID, ":")[0])
			if len(items) > 0 {
				fmt.Println("tidak ada stream untuk episode spesifik — semua torrent serial:")
			}
		}
		if len(items) == 0 {
			fmt.Println("tidak ada stream tersedia.")
			continue
		}
		for i, s := range items {
			fmt.Printf("%3d. %s\n     %s\n", i+1, s.Name, s.Title)
		}
		si, err := pickInt(in, "pilih stream", len(items))
		if err != nil || si < 0 {
			continue
		}
		fmt.Println(meta.BuildMagnet(items[si], name))
	}
}

func pickInt(in *bufio.Reader, prompt string, max int) (int, error) {
	fmt.Printf("%s: ", prompt)
	line, _ := in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		return -1, err
	}
	if n < 1 || n > max {
		return -1, fmt.Errorf("di luar rentang")
	}
	return n - 1, nil
}
