package app

import (
	"context"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// pageOG adalah tag og:/twitter: yang diisi saat HTML dilayani. Crawler
// (WhatsApp, X, Discord) tidak menjalankan JS, jadi judul + poster harus sudah
// ada di HTML mentah.
type pageOG struct {
	title string
	desc  string
	image string
}

const siteDescription = "Cari judul, pilih server, lalu putar film dan serial langsung di browser. Tanpa unduhan dan tanpa aplikasi tambahan."

func (s *Server) index(c *fiber.Ctx) error {
	// Not FileFromFS: http.FileServer redirects *index.html to ".", which
	// loops forever on "/". Serve the bytes directly instead.
	data, err := fs.ReadFile(s.web, "index.html")
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, "index.html tidak ditemukan")
	}
	// og:url / og:image harus absolut; origin diisi saat halaman dilayani
	// (X-Forwarded-Proto dipakai kalau ada reverse proxy seperti cPanel).
	scheme := "http"
	if c.Secure() || strings.EqualFold(strings.Split(c.Get("X-Forwarded-Proto"), ",")[0], "https") {
		scheme = "https"
	}
	origin := scheme + "://" + c.Hostname()
	uri := c.OriginalURL()

	og := pageOG{title: "kafuu — Streaming Film & Seri", desc: siteDescription, image: origin + "/og.png"}
	if t := s.titleOG(c); t != nil {
		og = *t
		if og.image == "" {
			og.image = origin + "/og.png"
		}
	}
	for _, v := range []struct{ ph, val string }{
		{"__PAGE_TITLE__", og.title},
		{"__PAGE_DESC__", og.desc},
		{"__PAGE_IMAGE__", og.image},
		{"__PAGE_URL__", origin + uri},
	} {
		data = []byte(strings.ReplaceAll(string(data), v.ph, html.EscapeString(v.val)))
	}
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Status(http.StatusOK).Send(data)
}

// titleOG menyusun tag OG dari judul di URL (?tab=&id=&s=&e=); nil bila tidak
// ada judul valid atau meta gagal diambil (halaman memakai tag default).
func (s *Server) titleOG(c *fiber.Ctx) *pageOG {
	tab, id := c.Query("tab"), strings.TrimSpace(c.Query("id"))
	if s.tmdb == nil || (tab != "movie" && tab != "tv") || !strings.HasPrefix(id, "tt") {
		return nil
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 5*time.Second)
	defer cancel()
	m, err := s.tmdb.Meta(ctx, tab, id)
	if err != nil || m.Name == "" {
		return nil
	}
	title := m.Name
	if m.Year != "" {
		title += " (" + m.Year + ")"
	}
	if tab == "tv" && c.Query("s") != "" && c.Query("e") != "" {
		title += fmt.Sprintf(" S%sE%s", c.Query("s"), c.Query("e"))
	}
	desc := strings.TrimSpace(m.Description)
	if desc == "" {
		desc = "Tonton " + m.Name + " di kafuu: cari judul, pilih server, lalu putar langsung di browser."
	}
	if runes := []rune(desc); len(runes) > 200 {
		desc = string(runes[:200]) + "…"
	}
	return &pageOG{title: title + " — kafuu", desc: desc, image: posterOG(m.Poster)}
}

// posterOG memakai poster yang cukup besar untuk pratinjau tautan: Metahub
// mengirim /poster/small/ (terlalu kecil untuk kartu WhatsApp/X), jadi diganti
// ke ukuran medium bila polanya cocok.
func posterOG(poster string) string {
	if strings.Contains(poster, "/poster/small/") {
		return strings.Replace(poster, "/poster/small/", "/poster/medium/", 1)
	}
	return poster
}

// spaFallback: path GET yang tak dikenal dapat index.html (React Router yang
// menampilkan 404), kecuali path API/fetch yang salah tulis — itu tetap JSON
// 404 supaya error di sisi client mudah dibedakan.
func (s *Server) spaFallback(c *fiber.Ctx) error {
	p := c.Path()
	if strings.Contains(p, "/api/") || p == "/healthz" || strings.HasPrefix(p, "/s/") {
		return errJSON(c, http.StatusNotFound, "not found")
	}
	return s.index(c)
}

// webFile menyajikan berkas statis dist. Varian praskompresi
// (<nama>.br/.zst/.gz) dipakai bila klien menerimanya, jadi server tidak perlu
// mengompres di jalur permintaan.
func (s *Server) webFile(c *fiber.Ctx) error {
	name := strings.TrimPrefix(c.Path(), "/")
	if name == "" || name == "index.html" {
		return s.index(c)
	}
	// index.html tidak memakai varian praskompresi: isinya diisi per
	// permintaan (judul/poster OG), jadi versi statisnya selalu basi.
	data, enc, err := s.readAsset(c, name)
	if err != nil {
		return c.Status(http.StatusNotFound).SendString("tidak ada")
	}
	// Vary selalu dikirim: respons berbeda menurut Accept-Encoding, jadi
	// cache perantara tidak boleh mencampur keduanya.
	c.Set("Vary", "Accept-Encoding")
	if enc != "" {
		c.Set("Content-Encoding", enc)
	}
	c.Set("Content-Type", contentType(name))
	return c.Status(http.StatusOK).Send(data)
}

// precompressedEncodings: urutan varian yang dipakai. Brotli dulu (paling
// kecil dan didukung browser modern), lalu zstd, gzip sebagai cadangan.
var precompressedEncodings = []struct{ suffix, enc string }{
	{".br", "br"},
	{".zst", "zstd"},
	{".gz", "gzip"},
}

// readAsset membaca aset dist, memilih varian praskompresi yang tersedia dan
// didukung klien. index.html tidak lewat sini (isinya diisi per permintaan).
func (s *Server) readAsset(c *fiber.Ctx, name string) (data []byte, enc string, err error) {
	accept := c.Get("Accept-Encoding")
	for _, v := range precompressedEncodings {
		if !acceptsEncoding(accept, v.enc) {
			continue
		}
		if data, err := fs.ReadFile(s.web, name+v.suffix); err == nil {
			return data, v.enc, nil
		}
	}
	data, err = fs.ReadFile(s.web, name)
	return data, "", err
}

// acceptsEncoding melaporkan apakah Accept-Encoding klien memuat `enc` dengan
// q>0 (atau "*"). Token dibandingkan tanpa peduli huruf besar/kecil.
func acceptsEncoding(header, enc string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		token := strings.ToLower(strings.TrimSpace(fields[0]))
		if token != enc && token != "*" {
			continue
		}
		q := 1.0
		for _, f := range fields[1:] {
			if v, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(f)), "q="); ok {
				if parsed, err := strconv.ParseFloat(v, 64); err == nil {
					q = parsed
				}
			}
		}
		return q > 0
	}
	return false
}

// contentType memetakan ekstensi aset ke Content-Type-nya. Nama yang dipakai
// adalah nama asli, bukan varian .br/.gz/.zst (yang dikirim isi berkas aslinya).
func contentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".ico"):
		return "image/x-icon"
	case strings.HasSuffix(name, ".webmanifest"):
		return "application/manifest+json"
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	}
	return "application/octet-stream"
}
