// Package tmdb: pencarian + detail judul. API publik TMDB minta kunci jadi
// client ini pakai Cinemeta (keyless) yang katalognya TMDB-backed dan meta-nya
// membawa `moviedb_id` — id TMDB yang dibutuhkan embedmaster. Hasil dip-
// cache di ./tmp/tmdb (judul jarang berubah, TTL panjang).
package tmdb

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	base        = "https://v3-cinemeta.strem.io"
	cacheDir    = "tmp/tmdb"
	searchTTL   = 24 * time.Hour
	metaTTL     = 24 * time.Hour
	httpTimeout = 20 * time.Second
)

// FlexString menerima JSON string maupun number: Cinemeta kadang mengirim
// moviedb_id sebagai angka (557) bukan string, dan unmarshal ke string
// langsung gagal.
type FlexString string

func (f *FlexString) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		return nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	*f = FlexString(s)
	return nil
}

// Item adalah satu hasil pencarian.
type Item struct {
	ID     string `json:"id"`   // imdb id (tt...)
	TMDB   string `json:"tmdb"` // moviedb_id, kosong bila meta belum diambil
	Name   string `json:"name"`
	Year   string `json:"year,omitempty"`
	Type   string `json:"type"` // movie | tv
	Poster string `json:"poster,omitempty"`
}

// Episode sindikat pra-terlebih dari metadata Cinemeta (id `tt:artis:s:ne`).
type Episode struct {
	Season    int    `json:"season"`
	Episode   int    `json:"episode"`
	Title     string `json:"title,omitempty"`
	Thumbnail string `json:"thumbnail,omitempty"`
	Released  string `json:"released,omitempty"`
}

// Meta adalah detail satu judul.
type Meta struct {
	ID          string    `json:"id"`
	TMDB        string    `json:"tmdb"`
	Name        string    `json:"name"`
	Year        string    `json:"year,omitempty"`
	Description string    `json:"description,omitempty"`
	Runtime     string    `json:"runtime,omitempty"`
	Genres      []string  `json:"genres,omitempty"`
	Rating      string    `json:"rating,omitempty"`
	Poster      string    `json:"poster,omitempty"`
	Episodes    []Episode `json:"episodes,omitempty"` // tv saja
}

type Client struct {
	http *http.Client
	base string
}

func New() *Client {
	return &Client{http: &http.Client{Timeout: httpTimeout}, base: base}
}

// SetBase mengganti endpoint sumber catalog (dipakai test dengan
// httptest.Server; produksi selalu memakai default).
func (c *Client) SetBase(u string) { c.base = u }

// Search mengembalikan hasil pencarian judul (tab: movie | tv).
func (c *Client) Search(ctx context.Context, tab, query string) ([]Item, error) {
	if tab != "movie" && tab != "tv" {
		return nil, fmt.Errorf("tab salah: %s", tab)
	}
	key := "search-" + tab + "-" + strings.ToLower(query)
	if data, ok := readCache(key, searchTTL); ok {
		return data, nil
	}
	endpoint := fmt.Sprintf("%s/catalog/%s/top/search=%s.json", c.base, tabCinemaType(tab), urlEscape(query))
	body, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Metas []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			ReleaseInfo string `json:"releaseInfo"`
			Poster      string `json:"poster"`
		} `json:"metas"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(payload.Metas))
	for _, m := range payload.Metas {
		if !strings.HasPrefix(m.ID, "tt") {
			continue
		}
		items = append(items, Item{
			ID:     m.ID,
			Name:   m.Name,
			Year:   firstYear(m.ReleaseInfo),
			Type:   tab,
			Poster: m.Poster,
		})
	}
	writeCache(key, items)
	return items, nil
}

// Meta mengambil detail satu judul (tab movie|tv). Untuk tv, meta Cinemeta
// membawa semua episode (season, number, thumbnail).
func (c *Client) Meta(ctx context.Context, tab, id string) (*Meta, error) {
	if tab != "movie" && tab != "tv" {
		return nil, fmt.Errorf("tab salah: %s", tab)
	}
	key := "meta-" + tab + "-" + id
	if data, ok := readMetaCache(key, metaTTL); ok {
		return data, nil
	}
	endpoint := fmt.Sprintf("%s/meta/%s/%s.json", c.base, tabCinemaType(tab), id)
	body, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Meta struct {
			ID          string     `json:"id"`
			MoviedbID   FlexString `json:"moviedb_id"`
			Name        string     `json:"name"`
			ReleaseInfo string     `json:"releaseInfo"`
			Description string     `json:"description"`
			Runtime     string     `json:"runtime"`
			Genres      []string   `json:"genres"`
			ImdbRating  FlexString `json:"imdbRating"`
			Poster      string     `json:"poster"`
			Videos      []struct {
				Season    int    `json:"season"`
				Episode   int    `json:"episode"`
				Name      string `json:"name"`
				Title     string `json:"title"`
				Thumbnail string `json:"thumbnail"`
				Released  string `json:"released"`
			} `json:"videos"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	m := &Meta{
		ID:          payload.Meta.ID,
		TMDB:        string(payload.Meta.MoviedbID),
		Name:        payload.Meta.Name,
		Year:        firstYear(payload.Meta.ReleaseInfo),
		Description: payload.Meta.Description,
		Runtime:     payload.Meta.Runtime,
		Genres:      payload.Meta.Genres,
		Rating:      string(payload.Meta.ImdbRating),
		Poster:      payload.Meta.Poster,
	}
	if tab == "tv" {
		for _, v := range payload.Meta.Videos {
			title := v.Title
			if title == "" {
				title = v.Name
			}
			m.Episodes = append(m.Episodes, Episode{
				Season:    v.Season,
				Episode:   v.Episode,
				Title:     title,
				Thumbnail: v.Thumbnail,
				Released:  v.Released,
			})
		}
	}
	writeCache(key, m)
	return m, nil
}

func (c *Client) get(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cinemeta HTTP %s", res.Status)
	}
	return body, nil
}

func tabCinemaType(tab string) string {
	if tab == "tv" {
		return "series"
	}
	return "movie"
}

// firstYear: "2023-10-10"|"2016–2021"|"2021" -> "2023" dsb.
func firstYear(releaseInfo string) string {
	if releaseInfo == "" {
		return ""
	}
	sep := releaseInfo
	if i := strings.IndexAny(sep, "-–"); i > 0 {
		sep = sep[:i]
	}
	sep = strings.TrimSpace(sep)
	if len(sep) > 4 {
		sep = sep[:4]
	}
	return sep
}

func urlEscape(s string) string { return url.QueryEscape(s) }

// ---- cache ./tmp/tmdb ----

func cachePath(key string) string {
	sum := sha1.Sum([]byte(key))
	return filepath.Join(cacheDir, fmt.Sprintf("%x.json", sum))
}

func readCache(key string, ttl time.Duration) ([]Item, bool) {
	path := cachePath(key)
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > ttl {
		return nil, false
	}
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var out []Item
	if err := json.Unmarshal(file, &out); err != nil || len(out) == 0 {
		return nil, false
	}
	return out, true
}

func readMetaCache(key string, ttl time.Duration) (*Meta, bool) {
	path := cachePath(key)
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > ttl {
		return nil, false
	}
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var out Meta
	if err := json.Unmarshal(file, &out); err != nil || out.ID == "" {
		return nil, false
	}
	return &out, true
}

func writeCache(key string, v any) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return
	}
	_ = os.WriteFile(cachePath(key), data, 0o644)
}
