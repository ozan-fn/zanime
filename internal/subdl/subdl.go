// Package subdl mencari subtitle di subdl.com. Alurnya: autocomplete
// api3.subdl.com/auto (JSON berisi link judul) → halaman judul/seasons → baris
// subtitle (data-row) yang membawa tautan unduh zip. Tidak ada cache: tiap
// pencarian mengambil halaman baru, jadi tautan unduh yang dipakai selalu yang
// berlaku sekarang (subdl merotasi id berkas).
package subdl

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	defaultSite = "https://subdl.com"
	defaultAPI  = "https://api3.subdl.com"
	userAgent   = "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0"
	httpTimeout = 20 * time.Second
	maxPage     = 8 << 20
	// Daftar subtitle jarang berubah dalam hitungan menit; cache menahan
	// autocomplete + halaman judul agar pindah tab/kartu tidak menembak ulang.
	cacheDir = "tmp/subdl"
	cacheTTL = 10 * time.Minute
	// searchTimeout membatasi satu pencarian penuh (autocomplete + halaman
	// judul, tv: + halaman musim).
	searchTimeout = 20 * time.Second
)

// Result satu baris autocomplete /auto.
type Result struct {
	Type   string `json:"type"` // movie | tv
	Name   string `json:"name"`
	Year   int    `json:"year"`
	Link   string `json:"link"` // /subtitle/sd…/slug
	Poster string `json:"poster_url,omitempty"`
}

// Subtitle satu baris subtitle untuk movie atau episode tv.
type Subtitle struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Author      string `json:"author,omitempty"`
	Lang        string `json:"lang"`     // slug bahasa, mis. "indonesian"
	Language    string `json:"language"` // nama tampilan, mis. "Indonesian"
	Downloads   int    `json:"downloads"`
	EpisodeFrom int    `json:"episodeFrom,omitempty"`
	EpisodeTo   int    `json:"episodeTo,omitempty"`
	Zip         string `json:"-"`             // URL zip asli, hanya dipakai server
	URL         string `json:"url,omitempty"` // diisi pemanggil (slug proxy)
}

type Client struct {
	site string
	api  string
	http *resty.Client
}

func New() *Client {
	return &Client{site: defaultSite, api: defaultAPI, http: resty.New().SetTimeout(httpTimeout)}
}

// SetBase menimpa kedua host (site + API); dipakai test dengan httptest.Server.
func (c *Client) SetBase(site, api string) { c.site, c.api = site, api }

// Search mencari subtitle untuk satu judul (cache ./tmp/subdl, TTL 10 menit).
// tv memakai season/episode untuk memilih halaman musim dan menyaring baris
// episode; movie mengabaikannya.
func (c *Client) Search(ctx context.Context, tab, name, year string, season, episode int) ([]Subtitle, error) {
	return c.search(ctx, tab, name, year, season, episode, false)
}

// SearchFresh sama seperti Search tapi melewati cache (tombol "cari ulang").
func (c *Client) SearchFresh(ctx context.Context, tab, name, year string, season, episode int) ([]Subtitle, error) {
	return c.search(ctx, tab, name, year, season, episode, true)
}

func (c *Client) search(ctx context.Context, tab, name, year string, season, episode int, fresh bool) ([]Subtitle, error) {
	key := cacheKey(tab, name, year, season, episode)
	if !fresh {
		if subs, ok := readCache(key); ok {
			return subs, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	hits, err := c.autocomplete(ctx, name)
	if err != nil {
		return nil, err
	}
	hit, ok := pick(hits, tab, name, year)
	if !ok {
		return nil, fmt.Errorf("judul tidak ditemukan di subdl: %s", name)
	}
	base := strings.TrimSuffix(c.site, "/") + hit.Link
	page, err := c.get(ctx, base)
	if err != nil {
		return nil, err
	}
	if tab == "tv" && season >= 0 {
		if slug, found := seasonSlug(page, season); found {
			if page, err = c.get(ctx, base+"/"+slug); err != nil {
				return nil, err
			}
		}
	}
	subs := relevant(parseRows(page, tab == "tv", episode), name)
	writeCache(key, subs)
	return subs, nil
}

// relevant menyaring baris: hanya Indonesian + English, urut Indonesian dulu,
// dan nama rilis harus memuat kata kunci judul (kata terpanjang) supaya
// subtitle judul lain yang ikut di halaman tidak muncul.
func relevant(subs []Subtitle, name string) []Subtitle {
	re := anchor(name)
	out := make([]Subtitle, 0, len(subs))
	var eng []Subtitle
	for _, s := range subs {
		if s.Lang != "indonesian" && s.Lang != "english" {
			continue
		}
		if re != nil && !re.MatchString(s.Name) {
			continue
		}
		if s.Lang == "indonesian" {
			out = append(out, s)
			continue
		}
		eng = append(eng, s)
	}
	return append(out, eng...)
}

// anchor = regexp kata terpanjang dari judul (mis. "Frieren: Beyond Journey's
// End" → /frieren/i). Nil bila judul tidak punya kata ASCII ≥3 huruf (judul
// non-latin): baris tidak disaring nama, hanya bahasanya.
func anchor(name string) *regexp.Regexp {
	best := ""
	for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r > 127 || r < '0' || r > '9' && r < 'a' || r > 'z'
	}) {
		if len(w) > len(best) {
			best = w
		}
	}
	if len(best) < 3 {
		return nil
	}
	return regexp.MustCompile(`(?i)` + regexp.QuoteMeta(best))
}

func (c *Client) autocomplete(ctx context.Context, query string) ([]Result, error) {
	endpoint := strings.TrimSuffix(c.api, "/") + "/auto?query=" + url.QueryEscape(query)
	body, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Results []Result `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, fmt.Errorf("autocomplete subdl: %w", err)
	}
	return payload.Results, nil
}

func (c *Client) get(ctx context.Context, endpoint string) (string, error) {
	res, err := c.http.R().SetContext(ctx).
		SetHeader("User-Agent", userAgent).
		SetHeader("Accept", "application/json, text/html").
		SetHeader("Referer", defaultSite+"/").
		Get(endpoint)
	if err != nil {
		return "", err
	}
	if res.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("subdl HTTP %s", res.Status())
	}
	return res.String(), nil
}

var (
	langRe   = regexp.MustCompile(`data-language="([a-z0-9_]+)"\s+data-language-name="([^"]*)"`)
	rowRe    = regexp.MustCompile(`(?s)<li([^>]*\bdata-row\b[^>]*)>(.*?)</li>`)
	idRe     = regexp.MustCompile(`data-id="(\d+)"`)
	dlsRe    = regexp.MustCompile(`data-downloads="(\d+)"`)
	fromRe   = regexp.MustCompile(`data-episode-from="(\d+)"`)
	toRe     = regexp.MustCompile(`data-episode-to="(\d+)"`)
	titleRe  = regexp.MustCompile(`(?s)data-subtitle-title[^>]*>\s*<h4>(.*?)</h4>`)
	authorRe = regexp.MustCompile(`(?s)data-subtitle-author[^>]*>\s*\((.*?)\)\s*</a>`)
	zipRe    = regexp.MustCompile(`data-subtitle-download\s+href="([^"]+)"`)
	// seasons tertanam di atribut Astro (JSON ter-escape): number = slug musim,
	// name = "Season N" atau "Specials".
	seasonRe = regexp.MustCompile(`"number":\[0,"([a-z0-9-]+-season)"\],"name":\[0,"(?:Specials|Season (\d+))"\]`)
	nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
)

// seasonSlug memetakan nomor musim ke slug halaman musim subdl.
func seasonSlug(page string, season int) (string, bool) {
	for _, m := range seasonRe.FindAllStringSubmatch(html.UnescapeString(page), -1) {
		if m[2] == "" && season == 0 {
			return m[1], true
		}
		if m[2] != "" && atoi(m[2]) == season {
			return m[1], true
		}
	}
	return "", false
}

// parseRows membaca baris subtitle. tv dengan episode > 0 hanya menyisakan
// baris episode itu plus pack yang mencakupnya (pack tanpa batas episode).
func parseRows(page string, tv bool, episode int) []Subtitle {
	page = html.UnescapeString(page)
	langs := langRe.FindAllStringSubmatchIndex(page, -1)
	rows := rowRe.FindAllStringSubmatchIndex(page, -1)
	out := make([]Subtitle, 0, len(rows))
	lang := 0
	for _, m := range rows {
		attrs, body := page[m[2]:m[3]], page[m[4]:m[5]]
		for lang+1 < len(langs) && langs[lang+1][0] < m[0] {
			lang++
		}
		var s Subtitle
		if lang < len(langs) && langs[lang][0] < m[0] {
			s.Lang = page[langs[lang][2]:langs[lang][3]]
			s.Language = page[langs[lang][4]:langs[lang][5]]
		}
		s.ID = firstMatch(idRe, attrs)
		s.Downloads = atoi(firstMatch(dlsRe, attrs))
		s.EpisodeFrom = atoi(firstMatch(fromRe, attrs))
		s.EpisodeTo = atoi(firstMatch(toRe, attrs))
		if tv && episode > 0 && s.EpisodeTo > 0 &&
			(episode < s.EpisodeFrom || episode > s.EpisodeTo) {
			continue
		}
		s.Name = strings.TrimSpace(firstMatch(titleRe, body))
		s.Author = strings.TrimSpace(firstMatch(authorRe, body))
		s.Zip = firstMatch(zipRe, body)
		if s.ID == "" || s.Name == "" || s.Zip == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// pick memilih hasil autocomplete yang paling cocok: tipe wajib sama dan nama
// harus cocok (sama atau salah satu memuat yang lain — Cinemeta kadang memakai
// judul pendek); tahun hanya memutuskan antar-kandidat. Tanpa kecocokan nama,
// judul dianggap tidak ditemukan: subtitle judul lain lebih buruk dari kosong.
func pick(hits []Result, tab, name, year string) (Result, bool) {
	want := norm(name)
	best, bestScore := Result{}, -1
	for _, h := range hits {
		if h.Type != tab || h.Link == "" || want == "" {
			continue
		}
		hn := norm(h.Name)
		if hn != want && !strings.Contains(hn, want) && !strings.Contains(want, hn) {
			continue
		}
		score := 2
		if year != "" && strconv.Itoa(h.Year) == year {
			score++
		}
		if score > bestScore {
			best, bestScore = h, score
		}
	}
	return best, bestScore >= 0
}

func norm(s string) string {
	return strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(s), " "), " ")
}

func firstMatch(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return html.UnescapeString(m[1])
	}
	return ""
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// ---- cache ./tmp/subdl ----

func cacheKey(tab, name, year string, season, episode int) string {
	return fmt.Sprintf("%s|%s|%s|%d|%d", tab, strings.ToLower(name), year, season, episode)
}

func cachePath(key string) string {
	sum := sha1.Sum([]byte(key))
	return filepath.Join(cacheDir, fmt.Sprintf("%x.json", sum))
}

// cachedSub: Subtitle.Zip sengaja `json:"-"` (tautan unduh asli tidak boleh
// ikut ke klien), jadi zip disimpan di field terpisah di cache. Tanpa ini
// daftar yang dibaca dari cache kehilangan tautan unduh dan /api/subtitles/file
// hanya menghasilkan slug kosong (404/502 saat tombol "pakai" ditekan).
type cachedSub struct {
	Subtitle
	Zip string `json:"zip"`
}

func readCache(key string) ([]Subtitle, bool) {
	path := cachePath(key)
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > cacheTTL {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cached []cachedSub
	if json.Unmarshal(data, &cached) != nil || len(cached) == 0 {
		return nil, false
	}
	out := make([]Subtitle, 0, len(cached))
	for _, c := range cached {
		if c.Zip == "" {
			continue // cache lama (sebelum zip ikut disimpan): anggap basi
		}
		c.Subtitle.Zip = c.Zip
		out = append(out, c.Subtitle)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func writeCache(key string, subs []Subtitle) {
	if len(subs) == 0 {
		return
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}
	cached := make([]cachedSub, len(subs))
	for i, s := range subs {
		cached[i] = cachedSub{Subtitle: s, Zip: s.Zip}
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return
	}
	_ = os.WriteFile(cachePath(key), data, 0o644)
}
