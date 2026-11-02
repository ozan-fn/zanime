package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/goceleris/celeris"
	"github.com/goceleris/celeris/middleware/websocket"

	"zanime/internal/logbus"
)

// proxyClient streams remote media through RAM — no overall timeout, playback
// lasts as long as the player keeps reading.
//
// Transport di-tune untuk segmen HLS yang banyak dan sering paralel: HTTP/2
// diaktifkan supaya banyak segmen di-multiplex lewat satu koneksi, dan batas
// idle per host dinaikkan supaya tidak membuka TLS handshake baru tiap segmen.
var proxyClient = resty.NewWithClient(&http.Client{
	Transport: &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        256,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		// Media tidak perlu dikompresi; hindari negosiasi gzip di jalur panas.
		DisableCompression: true,
	},
})

const (
	// mediaUA: UA browser penuh ke host video. UA pendek ("Mozilla/5.0 zanime")
	// sering ditolak edge CDN dengan 5xx karena terlihat seperti bot.
	mediaUA = "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0"
	// videoReferer: host video embedmaster menolak request tanpa Referer
	// halaman player mereka; Referer ini juga membuka CORS.
	videoReferer = "https://embdmstrplayer.com/"
)

// streamProxy menyalurkan satu URL media terdaftar lewat RAM (tanpa disk),
// tetap meneruskan Range supaya player bisa seek. Playlist teks (m3u8)
// ditulis ulang: setiap URI (playlist turun / segmen / URI= di tag)
// didaftarkan jadi slug /s/<slug>, karena player tidak bisa meminta playlist
// turunan langsung ke host video tanpa Referer.
func (s *Server) streamProxy(c *celeris.Context) error {
	slug := c.Param("slug")
	raw, ok := s.res.Load(slug)
	if !ok {
		return errJSON(c, http.StatusNotFound, "slug tidak ada")
	}
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		return errJSON(c, http.StatusNotFound, "slug tidak ada")
	}
	px := "px:" + slug

	req := proxyClient.R().SetContext(c.Context()).SetDoNotParseResponse(true).
		SetHeader("User-Agent", mediaUA).
		SetHeader("Referer", videoReferer)
	if rng := c.Header("Range"); rng != "" {
		req.SetHeader("Range", rng)
	}

	res, err := req.Execute(c.Method(), target.String())
	if err != nil {
		// Player membatalkan permintaan (mis. ganti server) — bukan kegagalan.
		if errors.Is(err, context.Canceled) {
			s.logs.Emit(logbus.Event{ID: px, Level: logbus.LevelInfo, Text: "Permintaan dibatalkan browser"})
			return nil
		}
		log.Printf("%s: media gagal dihubungi · %v", px, err)
		return errJSON(c, http.StatusBadGateway, "gagal menghubungi upstream: "+err.Error())
	}
	body := res.RawBody()
	defer func() { _ = body.Close() }()
	if res.StatusCode() < 200 || res.StatusCode() >= 400 {
		log.Printf("%s: media ditolak server sumber · HTTP %s", px, res.Status())
		return errJSON(c, http.StatusBadGateway, fmt.Sprintf("upstream HTTP %s", res.Status()))
	}

	out := c.StreamWriter()
	defer func() { _ = out.Close() }()

	if isPlaylist(res.Header().Get("Content-Type"), target.Path) {
		data, err := io.ReadAll(body)
		if err != nil {
			return errJSON(c, http.StatusBadGateway, err.Error())
		}
		renditions, segments, vod := playlistStats(string(data))
		s.logs.Emit(logbus.Event{ID: px, Level: logbus.LevelOK, Text: fmt.Sprintf(
			"Playlist HLS dibaca · %d rendition · %d segmen · %s", renditions, segments, vodLabel(vod))})
		// VOD (ada #EXT-X-ENDLIST) tidak berubah → biarkan browser meng-cache;
		// playlist live tetap no-store. Master playlist bisa 1+ MB.
		cache := "no-store"
		if vod {
			cache = "public, max-age=600"
		}
		headers := [][2]string{
			{"Content-Type", "application/vnd.apple.mpegurl"},
			{"Cache-Control", cache},
		}
		if err := out.WriteHeader(http.StatusOK, headers); err != nil {
			return err
		}
		_, err = out.Write([]byte(s.rewritePlaylist(target, string(data))))
		return err
	}

	// Subtitle diteruskan apa adanya: timestamp dari sumber dipakai langsung,
	// tidak ada normalisasi/geser di server.
	if isSubtitle(res.Header().Get("Content-Type"), target.Path) {
		data, err := io.ReadAll(body)
		if err != nil {
			return errJSON(c, http.StatusBadGateway, err.Error())
		}
		s.logs.Emit(logbus.Event{ID: px, Level: logbus.LevelOK, Text: fmt.Sprintf(
			"Subtitle VTT siap · %d cue", strings.Count(string(data), "-->"))})
		if err := out.WriteHeader(http.StatusOK, [][2]string{{"Content-Type", "text/vtt; charset=utf-8"}}); err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	}

	headers := make([][2]string, 0, 4)
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := res.Header().Get(h); v != "" {
			headers = append(headers, [2]string{h, v})
		}
	}
	s.logs.Emit(logbus.Event{ID: px, Level: logbus.LevelOK, Text: fmt.Sprintf(
		"Media terkirim · HTTP %s · %s", res.Status(), sizeLabel(res.Header().Get("Content-Length")))})
	if err := out.WriteHeader(res.StatusCode(), headers); err != nil {
		return err
	}
	if c.Method() == http.MethodHead {
		return nil
	}
	if _, err := io.Copy(out, body); err != nil {
		return err
	}
	return nil
}

// logsRoute menyiarkan log realtime ke browser lewat websocket: riwayat
// terakhir dulu, lalu baris baru. Dipakai panel log di depan player.
func (s *Server) logsRoute() celeris.HandlerFunc {
	return websocket.New(websocket.Config{
		// Aplikasi satu-origin; dev Vite juga boleh menyambung lewat proxy.
		CheckOrigin: func(*celeris.Context) bool { return true },
		Handler: func(conn *websocket.Conn) {
			if s.logs == nil {
				return
			}
			ch, cancel := s.logs.Subscribe()
			defer cancel()
			// Pembaca: mendeteksi klien putus supaya langganan dilepas dan
			// handler tidak menggantung saat server dimatikan.
			go func() {
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						cancel()
						return
					}
				}
			}()
			for ev := range ch {
				if err := conn.WriteJSON(ev); err != nil {
					return
				}
			}
		},
	})
}

// playlistStats menghitung rendition (master), segmen (#EXTINF), dan apakah
// playlist sudah tuntas (VOD) — untuk baris log.
func playlistStats(body string) (renditions, segments int, vod bool) {
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF"):
			renditions++
		case strings.HasPrefix(line, "#EXTINF"):
			segments++
		case strings.Contains(line, "#EXT-X-ENDLIST"):
			vod = true
		}
	}
	return
}

func vodLabel(vod bool) string {
	if vod {
		return "VOD"
	}
	return "live"
}

// sizeLabel merapikan Content-Length untuk teks log.
func sizeLabel(n string) string {
	v, err := strconv.ParseInt(n, 10, 64)
	if err != nil || v <= 0 {
		return "ukuran tidak disebut"
	}
	if v >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(v)/(1<<20))
	}
	return fmt.Sprintf("%.0f KB", float64(v)/1024)
}

func isPlaylist(ct, p string) bool {
	return strings.Contains(ct, "mpegurl") || strings.HasSuffix(p, ".m3u8")
}

// isSubtitle: track teks VTT/SRT (host subtitle mengirim text/vtt, kadang
// extension .vtt/.srt tanpa content-type yang jelas).
func isSubtitle(ct, p string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "text/vtt") || strings.Contains(ct, "/vtt") ||
		strings.Contains(ct, "x-subrip") || strings.Contains(ct, "/srt") ||
		strings.HasSuffix(p, ".vtt") || strings.HasSuffix(p, ".srt")
}

// rewritePlaylist menulis ulang satu playlist m3u8 ke slug pool. Baris yang
// bukan komentar dan bukan tag (#) adalah URI; URI="..." di dalam tag
// (#EXT-X-KEY, #EXT-X-MEDIA) juga ditulis ulang.
func (s *Server) rewritePlaylist(base *url.URL, body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if idx := strings.Index(trimmed, `URI="`); idx != -1 {
				rest := trimmed[idx+len(`URI="`):]
				if end := strings.Index(rest, `"`); end > 0 {
					abs := resolveRef(base, rest[:end])
					lines[i] = trimmed[:idx] + `URI="` + "/s/" + s.res.RegisterTemp(abs) + `"` + rest[end+1:]
				}
			}
			continue
		}
		lines[i] = "/s/" + s.res.RegisterTemp(resolveRef(base, trimmed))
	}
	return strings.Join(lines, "\n")
}

func resolveRef(base *url.URL, ref string) string {
	if u, err := url.Parse(ref); err == nil {
		return base.ResolveReference(u).String()
	}
	return ref
}
