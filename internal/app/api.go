package app

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/gofiber/fiber/v2"

	"zanime/internal/em"
	"zanime/internal/logbus"
	"zanime/internal/miruro"
	"zanime/internal/resolve"
	"zanime/internal/translate"
)

// subHTTP mengambil zip subtitle subdl (ukuran kecil, bukan streaming).
var subHTTP = resty.New().SetTimeout(30 * time.Second)

// hubLogger menjembatani em.Logger (baris pipeline scrape) ke logbus.
type hubLogger struct{ hub *logbus.Hub }

func (l hubLogger) Log(id, level, msg string) {
	l.hub.Emit(logbus.Event{ID: id, Level: level, Text: msg})
}

// apiSearch: pencarian judul. Backend yang menghubungi sumber catalog; hasil
// membawa id TMDB (moviedb_id) yang dibutuhkan embedmaster.
func (s *Server) apiSearch(c *fiber.Ctx) error {
	q := strings.TrimSpace(c.Query("q"))
	tab := c.Query("tab")
	if q == "" || (tab != "movie" && tab != "tv") {
		return errJSON(c, http.StatusBadRequest, "query harus ?q=<judul>&tab=movie|tv")
	}
	res, err := s.tmdb.Search(c.UserContext(), tab, q)
	if err != nil {
		log.Printf("api:search gagal: %v", err)
		return s.fail(c, err)
	}
	return c.Status(http.StatusOK).JSON(map[string]any{"results": res})
}

// apiMeta: detail satu judul (movie: metadata; tv: metadata + daftar episode).
func (s *Server) apiMeta(c *fiber.Ctx) error {
	tab, id := c.Params("tab"), c.Params("id")
	if (tab != "movie" && tab != "tv") || !strings.HasPrefix(id, "tt") {
		return errJSON(c, http.StatusBadRequest, "meta harus /api/meta/movie|tv/<imdb-id>")
	}
	res, err := s.tmdb.Meta(c.UserContext(), tab, id)
	if err != nil {
		log.Printf("api:meta gagal: %v", err)
		return s.fail(c, err)
	}
	return c.Status(http.StatusOK).JSON(res)
}

// apiStreams: daftar stream hasil scraping embedmaster untuk satu TMDB id
// (movie) atau satu episode tv. Fetch/dekripsi terjadi di Go (internal/em).
func (s *Server) apiStreams(c *fiber.Ctx) error {
	tab := c.Query("tab")
	id := strings.TrimSpace(c.Query("id"))
	if (tab != "movie" && tab != "tv") || id == "" {
		return errJSON(c, http.StatusBadRequest, "streams harus ?tab=movie|tv&id=<tmdb-id>")
	}
	var tv *em.TVEpisode
	if tab == "tv" {
		ses, err1 := strconv.Atoi(c.Query("s"))
		epi, err2 := strconv.Atoi(c.Query("e"))
		if err1 != nil || err2 != nil || ses < 0 || epi < 1 {
			return errJSON(c, http.StatusBadRequest, "tv butuh ?s=<season>&e=<episode>")
		}
		tv = &em.TVEpisode{Season: ses, Episode: epi}
	}

	// embedmaster dan miruro diambil serentak: miruro best-effort, kegagalannya
	// tidak boleh menutup sumber utama. miruro tidak menerima id, jadi imdb
	// dari klien dipakai untuk mencocokkan hasil pencarian berjudul sama.
	imdb := strings.TrimSpace(c.Query("imdb"))
	if !strings.HasPrefix(imdb, "tt") {
		imdb = ""
	}
	useMiruro := imdb != "" && s.mir != nil

	emCh := make(chan struct {
		servers []em.Server
		err     error
	}, 1)
	go func() {
		servers, err := s.em.Streams(c.UserContext(), tab, id, tv)
		emCh <- struct {
			servers []em.Server
			err     error
		}{servers, err}
	}()
	mirCh := make(chan []miruro.Server, 1)
	if useMiruro {
		go func() {
			meta, err := s.tmdb.Meta(c.UserContext(), tab, imdb)
			if err != nil {
				log.Printf("api:streams miruro meta: %v", err)
				mirCh <- nil
				return
			}
			season, episode := 0, 0
			if tv != nil {
				season, episode = tv.Season, tv.Episode
			}
			servers, err := s.mir.Streams(c.UserContext(), tab, imdb, id, meta.Name, season, episode)
			if err != nil {
				log.Printf("api:streams miruro: %v", err)
				mirCh <- nil
				return
			}
			mirCh <- servers
		}()
	}

	got := <-emCh
	var mir []miruro.Server
	if useMiruro {
		mir = <-mirCh
	}
	if got.err != nil && len(mir) == 0 {
		log.Printf("api:streams gagal: %v", got.err)
		return s.fail(c, got.err)
	}
	res := got.servers

	// Semua URL didaftarkan ke pool slug; player/subtitle hanya melihat
	// /s/<slug> pendek, tidak ada URL ter-encode panjang di network tab.
	// Ekstensi slug mengikuti jenis hasil verifikasi (hls→.m3u8) supaya player
	// memilih jalur adaptif walau URL stream tanpa ekstensi.
	for i := range res {
		srv := &res[i]
		hint := strings.ToLower(srv.Name)
		kind := resolve.ExtForKind(srv.Type)
		if srv.URL != "" {
			srv.URL = "/s/" + s.res.RegisterKind(srv.URL, hint, kind)
		}
		for j := range srv.Qualities {
			q := &srv.Qualities[j]
			if q.URL != "" {
				q.URL = "/s/" + s.res.Register(q.URL, hint+"-"+q.Label)
			}
		}
		for j := range srv.Subs {
			sb := &srv.Subs[j]
			if sb.URL != "" {
				sb.URL = "/s/" + s.res.Register(sb.URL, hint+"-sub-"+sb.Lang)
			}
		}
	}
	// miruro membawa header sendiri (Referer/Origin) yang wajib ikut proxy,
	// termasuk untuk file subtitle-nya. Subtitle didaftarkan berekstensi .vtt
	// supaya proxy menyajikannya sebagai text/vtt (host bisa mengirim
	// application/octet-stream atau .jpeg).
	for _, m := range mir {
		subs := make([]em.Sub, 0, len(m.Subs))
		for _, sb := range m.Subs {
			subs = append(subs, em.Sub{
				Label: sb.Label,
				Lang:  sb.Lang,
				URL:   "/s/" + s.res.RegisterHeaders(sb.URL, "miruro-sub-"+m.Name+"-"+sb.Lang, ".vtt", m.Headers),
			})
		}
		res = append(res, em.Server{
			Name:    m.Name,
			Source:  "miruro",
			Type:    m.Type,
			Quality: m.Quality,
			Subs:    subs,
			URL:     "/s/" + s.res.RegisterHeaders(m.URL, "miruro-"+m.Name, resolve.ExtForKind(m.Type), m.Headers),
		})
	}
	playable := 0
	for i := range res {
		if res[i].URL != "" {
			playable++
		}
	}
	s.logs.OKf("Daftar stream siap · %d dari %d server bisa diputar", playable, len(res))
	return c.Status(http.StatusOK).JSON(map[string]any{"id": id, "tab": tab, "servers": res})
}

// apiSubtitles: cari subtitle subdl untuk judul yang sedang dibuka. Nama+tahun
// diambil dari meta supaya autocomplete subdl dapat query yang tepat; tv ikut
// memilih musim + episode. Tiap permintaan fresh (tanpa cache): tautan unduh
// yang dikembalikan berasal dari halaman yang baru diambil.
func (s *Server) apiSubtitles(c *fiber.Ctx) error {
	tab, id := c.Query("tab"), strings.TrimSpace(c.Query("id"))
	if (tab != "movie" && tab != "tv") || !strings.HasPrefix(id, "tt") {
		return errJSON(c, http.StatusBadRequest, "subtitles harus ?tab=movie|tv&id=<imdb-id>")
	}
	season, episode := 0, 0
	if tab == "tv" {
		ses, err1 := strconv.Atoi(c.Query("s"))
		epi, err2 := strconv.Atoi(c.Query("e"))
		if err1 != nil || err2 != nil || ses < 0 || epi < 1 {
			return errJSON(c, http.StatusBadRequest, "tv butuh ?s=<season>&e=<episode>")
		}
		season, episode = ses, epi
	}
	if s.sub == nil {
		return errJSON(c, http.StatusServiceUnavailable, "pencarian subtitle tidak aktif")
	}
	meta, err := s.tmdb.Meta(c.UserContext(), tab, id)
	if err != nil {
		return s.fail(c, err)
	}
	search := s.sub.Search
	if c.Query("refresh") != "" {
		search = s.sub.SearchFresh
	}
	subs, err := search(c.UserContext(), tab, meta.Name, meta.Year, season, episode)
	if err != nil {
		log.Printf("api:subtitles gagal: %v", err)
		return s.fail(c, err)
	}
	// Zip didaftarkan ke pool slug: endpoint /api/subtitles/file hanya bisa
	// mengambil URL yang memang didaftarkan di sini (bukan sembarang alamat).
	// Baris tanpa zip dibuang — slug dengan URL kosong hanya menghasilkan 404/502
	// saat tombol "pakai" ditekan.
	out := subs[:0]
	for _, sub := range subs {
		if sub.Zip == "" {
			continue
		}
		sub.URL = "/api/subtitles/file?u=" + s.res.RegisterKind(sub.Zip, "sub-"+sub.Lang, ".zip")
		out = append(out, sub)
	}
	subs = out
	s.logs.OKf("Subtitle siap · %d pilihan", len(subs))
	return c.Status(http.StatusOK).JSON(map[string]any{"subtitles": subs})
}

// apiSubtitleFile mengubah zip subtitle subdl jadi VTT siap track: entri
// .srt/.vtt pertama, header WEBVTT, lalu timestamp dinormalkan.
func (s *Server) apiSubtitleFile(c *fiber.Ctx) error {
	raw, ok := s.res.Load(c.Query("u"))
	if !ok {
		return errJSON(c, http.StatusNotFound, "subtitle tidak ada")
	}
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		return errJSON(c, http.StatusBadRequest, "URL subtitle tidak valid")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 30*time.Second)
	defer cancel()
	res, err := subHTTP.R().SetContext(ctx).SetDoNotParseResponse(true).
		SetHeader("User-Agent", "Mozilla/5.0 zanime").Get(raw)
	if err != nil {
		return errJSON(c, http.StatusBadGateway, "gagal menghubungi sumber subtitle")
	}
	body := res.RawBody()
	defer func() { _ = body.Close() }()
	if res.StatusCode() != http.StatusOK {
		return errJSON(c, http.StatusBadGateway, fmt.Sprintf("sumber subtitle HTTP %s", res.Status()))
	}
	data, err := io.ReadAll(io.LimitReader(body, 20<<20))
	if err != nil {
		return errJSON(c, http.StatusBadGateway, err.Error())
	}
	text, err := firstSubFile(data)
	if err != nil {
		return errJSON(c, http.StatusBadGateway, err.Error())
	}
	out := srtToVTT(text)
	s.logs.OKf("Subtitle subdl siap · %d cue", strings.Count(out, "-->"))
	c.Set("Content-Type", "text/vtt; charset=utf-8")
	return c.Status(http.StatusOK).Send([]byte(out))
}

// apiTranslate menerjemahkan subtitle milik player (slug /s/…) ke bahasa lain.
// Browser tidak pernah menghubungi penyedia terjemahan: permintaan dan cache
// ada di server, jadi hasil yang sama kembali seketika.
func (s *Server) apiTranslate(c *fiber.Ctx) error {
	u := strings.TrimSpace(c.Query("u"))
	if u == "" {
		return errJSON(c, http.StatusBadRequest, "butuh ?u=<slug subtitle>")
	}
	slug := subSlug(u)
	if slug == "" {
		return errJSON(c, http.StatusBadRequest, "URL subtitle tidak dikenal")
	}
	raw, ok := s.res.Load(slug)
	if !ok {
		return errJSON(c, http.StatusNotFound, "subtitle tidak ada")
	}
	to := c.Query("to", "id")
	from := c.Query("from", "en")
	ctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Minute)
	defer cancel()
	body, err := fetchSubtitleText(ctx, raw, s.res.Headers(slug))
	if err != nil {
		log.Printf("api:translate sumber gagal: %v", err)
		return errJSON(c, http.StatusBadGateway, err.Error())
	}
	res, err := s.tr.TranslateVTT(ctx, srtToVTT(body), from, to)
	if err != nil {
		log.Printf("api:translate gagal: %v", err)
		return errJSON(c, http.StatusBadGateway, err.Error())
	}
	note := ""
	if res.Cached {
		note = " · cache"
	}
	s.logs.OKf("Terjemahan %s siap · %d cue%s", to, res.Cues, note)
	return c.Status(http.StatusOK).JSON(map[string]any{
		"url":    "/api/translate/file?c=" + res.Key,
		"cues":   res.Cues,
		"cached": res.Cached,
		"lang":   to,
	})
}

// subSlug mengambil slug dari URL subtitle yang dikenal aplikasi: /s/<slug>
// (track bawaan stream) atau /api/subtitles/file?u=<slug> (hasil subdl).
func subSlug(u string) string {
	if rest, ok := strings.CutPrefix(u, "/s/"); ok {
		return rest
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Path != "/api/subtitles/file" {
		return ""
	}
	return parsed.Query().Get("u")
}

// apiTranslateFile menyajikan VTT hasil terjemahan yang tersimpan di server.
func (s *Server) apiTranslateFile(c *fiber.Ctx) error {
	vtt, ok := translate.Cached(c.Query("c"))
	if !ok {
		return errJSON(c, http.StatusNotFound, "terjemahan tidak ada")
	}
	c.Set("Content-Type", "text/vtt; charset=utf-8")
	return c.Status(http.StatusOK).Send([]byte(vtt))
}

// fetchSubtitleText mengambil subtitle dari host video lewat slug pool sebagai
// teks (dipakai jalur terjemahan). Header milik slug (mis. Referer/Origin
// miruro) ikut dikirim, sama seperti proxy, supaya host yang butuh kredensial
// tidak menolak permintaan.
func fetchSubtitleText(ctx context.Context, raw string, headers map[string]string) (string, error) {
	req := proxyClient.R().SetContext(ctx).SetDoNotParseResponse(true).
		SetHeader("User-Agent", mediaUA).SetHeader("Referer", videoReferer)
	for k, v := range headers {
		req.SetHeader(k, v)
	}
	res, err := req.Get(raw)
	if err != nil {
		return "", fmt.Errorf("gagal menghubungi sumber subtitle: %w", err)
	}
	body := res.RawBody()
	defer func() { _ = body.Close() }()
	if res.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("sumber subtitle HTTP %s", res.Status())
	}
	data, err := io.ReadAll(io.LimitReader(body, 20<<20))
	if err != nil {
		return "", err
	}
	// Subdl mengirim zip: terjemahan butuh teks .srt/.vtt di dalamnya.
	if bytes.HasPrefix(data, []byte("PK")) {
		if text, err := firstSubFile(data); err == nil {
			return text, nil
		}
	}
	return string(data), nil
}

// firstSubFile mengambil berkas .srt/.vtt pertama dari zip subtitle.
func firstSubFile(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("zip subtitle rusak: %w", err)
	}
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		if !strings.HasSuffix(name, ".srt") && !strings.HasSuffix(name, ".vtt") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(rc, 8<<20))
		_ = rc.Close()
		if err != nil {
			continue
		}
		return string(body), nil
	}
	return "", fmt.Errorf("zip subtitle tanpa .srt/.vtt")
}

// srtToVTT memberi header WEBVTT dan memakai titik pada baris waktu (SRT
// memakai koma). Timestamp tidak digeser/dinormalkan.
func srtToVTT(text string) string {
	body := strings.TrimSpace(strings.TrimPrefix(text, "\uFEFF"))
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if !strings.HasPrefix(body, "WEBVTT") {
		body = "WEBVTT\n\n" + body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.Contains(line, "-->") {
			lines[i] = strings.ReplaceAll(line, ",", ".")
		}
	}
	return strings.Join(lines, "\n")
}
