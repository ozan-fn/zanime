// Package miruro mengambil daftar server stream anime dari miruro.cx.
//
// miruro mengalamatkan judul dengan hash internal + slug, bukan id TMDB/IMDB.
// Alurnya: cari judul → cocokkan external_ids hasil pencarian → buka halaman
// watch (payload SvelteKit __data.json) → chunk sources berisi
// tracks → providers → servers → streams. Host video menolak request tanpa
// Referer/Origin yang tercantum di tiap server, jadi header ikut dikembalikan.
package miruro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	defaultBase = "https://miruro.cx"
	httpTimeout = 20 * time.Second
	userAgent   = "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0"
)

// Sub adalah satu track subtitle bawaan provider miruro.
type Sub struct {
	Label string
	Lang  string
	URL   string
}

// Server adalah satu stream miruro. Headers wajib diteruskan proxy; tanpa
// Referer/Origin yang benar host video (dan file subtitle) membalas 403.
type Server struct {
	Provider string
	Name     string
	Type     string // hls | mp4
	Quality  string
	URL      string
	Headers  map[string]string
	Subs     []Sub
}

// Client mengambil stream miruro. Base/http dapat ditimpa test.
type Client struct {
	Base string
	http *resty.Client
}

func New() *Client {
	return &Client{Base: defaultBase, http: resty.New().SetTimeout(httpTimeout).SetHeader("User-Agent", userAgent)}
}

// SetBase mengganti host miruro (dipakai test).
func (c *Client) SetBase(u string) { c.Base = u }

// Streams mencari judul di miruro lalu mengembalikan semua stream yang bisa
// diputar. title dipakai sebagai query karena miruro tidak menerima id;
// imdb/tmdb hanya dipakai untuk memastikan hasil pencarian memang judul itu.
func (c *Client) Streams(ctx context.Context, tab, imdb, tmdb, title string, season, episode int) ([]Server, error) {
	items, err := c.search(ctx, title)
	if err != nil {
		return nil, err
	}
	it, ok := match(items, imdb, tmdb)
	if !ok {
		return nil, fmt.Errorf("judul tidak ada di miruro")
	}
	hash := it.hash
	slug := slugOf(it.title)
	if tab == "tv" && season > 0 {
		// Tiap season punya hash sendiri; entri hasil pencarian bisa milik
		// season lain, jadi musim yang diminta dicari dari seasonsData.
		if p, err := c.watch(ctx, hash, slug, ""); err == nil {
			if h := seasonHash(p, season); h != "" {
				hash = h
			}
		}
	}
	query := ""
	if tab == "tv" {
		query = "?ep=" + strconv.Itoa(episode)
	}
	p, err := c.watch(ctx, hash, slug, query)
	if err != nil {
		return nil, err
	}
	tracks := p.sources()
	if len(tracks) == 0 {
		return nil, fmt.Errorf("miruro tidak mengembalikan sumber untuk episode ini")
	}
	return flatten(tracks), nil
}

// search menanyakan pencarian miruro dan mengembalikan judul-judulnya.
func (c *Client) search(ctx context.Context, query string) ([]item, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("judul kosong")
	}
	body, err := c.get(ctx, c.Base+"/search/__data.json?q="+url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	p, err := parsePage(body)
	if err != nil {
		return nil, err
	}
	items := p.list()
	if len(items) == 0 {
		return nil, fmt.Errorf("pencarian miruro kosong")
	}
	return items, nil
}

// watch membuka payload __data.json halaman watch. Slug salah dijawab dengan
// dokumen redirect; sekali diikuti memakai slug kanonik dari location.
func (c *Client) watch(ctx context.Context, hash, slug, query string) (*page, error) {
	body, err := c.get(ctx, watchURL(c.Base, hash, slug, query))
	if err != nil {
		return nil, err
	}
	p, err := parsePage(body)
	var redir redirectError
	if err != nil && !asRedirect(err, &redir) {
		return nil, err
	}
	if asRedirect(err, &redir) {
		h, s, q := splitLocation(redir.location)
		if h == "" {
			return nil, fmt.Errorf("redirect miruro tanpa judul")
		}
		if q == "" {
			q = query
		}
		body, err = c.get(ctx, watchURL(c.Base, h, s, q))
		if err != nil {
			return nil, err
		}
		return parsePage(body)
	}
	return p, nil
}

func watchURL(base, hash, slug, query string) string {
	return fmt.Sprintf("%s/watch/%s/%s/__data.json%s", base, url.PathEscape(hash), url.PathEscape(slug), query)
}

// splitLocation memecah "/watch/<hash>/<slug>?ep=1" jadi hash, slug, query.
func splitLocation(location string) (hash, slug, query string) {
	u, err := url.Parse(location)
	if err != nil {
		return "", "", ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "watch" {
		hash, slug = parts[1], parts[2]
	}
	if u.RawQuery != "" {
		query = "?" + u.RawQuery
	}
	return hash, slug, query
}

func (c *Client) get(ctx context.Context, endpoint string) ([]byte, error) {
	res, err := c.http.R().SetContext(ctx).Get(endpoint)
	if err != nil {
		return nil, err
	}
	if res.StatusCode() != 200 {
		return nil, fmt.Errorf("miruro HTTP %s", res.Status())
	}
	return res.Body(), nil
}

// ---- pencocokan judul ----

type item struct {
	hash      string
	title     string
	imdb      []string
	tmdbTV    []string
	tmdbMovie []string
}

func match(items []item, imdb, tmdb string) (item, bool) {
	if imdb != "" {
		for _, it := range items {
			if contains(it.imdb, imdb) {
				return it, true
			}
		}
	}
	if tmdb != "" {
		for _, it := range items {
			if contains(it.tmdbTV, tmdb) || contains(it.tmdbMovie, tmdb) {
				return it, true
			}
		}
	}
	return item{}, false
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// ---- payload SvelteKit ----

type page struct {
	nodes  []any
	chunks map[int]any
}

type redirectError struct{ location string }

func (e redirectError) Error() string { return "miruro redirect ke " + e.location }

func asRedirect(err error, out *redirectError) bool {
	r, ok := err.(redirectError)
	if ok {
		*out = r
	}
	return ok
}

// parsePage membaca satu respons berisi beberapa dokumen JSON berturut-turut
// (data + chunk + redirect) lalu men-decode tiap nilai devalue.
func parsePage(body []byte) (*page, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	p := &page{chunks: map[int]any{}}
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch doc["type"] {
		case "redirect":
			loc, _ := doc["location"].(string)
			return nil, redirectError{location: loc}
		case "data":
			nodes, _ := doc["nodes"].([]any)
			for _, n := range nodes {
				node, ok := n.(map[string]any)
				if !ok {
					continue
				}
				if raw, ok := node["data"].([]any); ok {
					p.nodes = append(p.nodes, hydrate(raw))
				}
			}
		case "chunk":
			id, _ := doc["id"].(float64)
			if raw, ok := doc["data"].([]any); ok {
				p.chunks[int(id)] = hydrate(raw)
			}
		}
	}
	if len(p.nodes) == 0 && len(p.chunks) == 0 {
		return nil, fmt.Errorf("payload miruro kosong")
	}
	return p, nil
}

// list mengambil daftar hasil pencarian dari node yang punya kunci items.
func (p *page) list() []item {
	for _, n := range p.nodes {
		node, ok := n.(map[string]any)
		if !ok {
			continue
		}
		raw, ok := node["items"].([]any)
		if !ok {
			continue
		}
		out := make([]item, 0, len(raw))
		for _, r := range raw {
			it, ok := r.(map[string]any)
			if !ok {
				continue
			}
			ext, _ := it["external_ids"].(map[string]any)
			out = append(out, item{
				hash:      str(it["id"]),
				title:     titleOf(it["title"]),
				imdb:      stringsOf(ext["imdb"]),
				tmdbTV:    stringsOf(ext["tmdb_tv"]),
				tmdbMovie: stringsOf(ext["tmdb_movie"]),
			})
		}
		return out
	}
	return nil
}

// media mengambil node halaman watch (node yang punya kunci media).
func (p *page) media() map[string]any {
	for _, n := range p.nodes {
		node, ok := n.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := node["media"]; ok {
			return node
		}
	}
	return nil
}

// sources mengambil tracks dari chunk sources (bentuknya bisa berubah posisi,
// jadi dicari berdasarkan kunci).
func (p *page) sources() []any {
	for _, v := range p.chunks {
		if m, ok := v.(map[string]any); ok {
			if tracks, ok := m["tracks"].([]any); ok {
				return tracks
			}
		}
	}
	for _, n := range p.nodes {
		if m, ok := n.(map[string]any); ok {
			if tracks, ok := m["tracks"].([]any); ok {
				return tracks
			}
		}
	}
	return nil
}

// seasonHash mencari hash season yang diminta dari seasonsData (promise pada
// node media).
func seasonHash(p *page, season int) string {
	media := p.media()
	if media == nil {
		return ""
	}
	rows, ok := p.promise(media["seasonsData"]).([]any)
	if !ok {
		return ""
	}
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		n, ok := m["seasonNumber"].(float64)
		if !ok || int(n) != season {
			continue
		}
		if id := str(m["id"]); id != "" {
			return id
		}
	}
	return ""
}

// promise menyelesaikan nilai ["Promise", id] ke chunk-nya.
func (p *page) promise(v any) any {
	arr, ok := v.([]any)
	if !ok || len(arr) != 2 || arr[0] != "Promise" {
		return v
	}
	id, ok := arr[1].(float64)
	if !ok {
		return nil
	}
	return p.chunks[int(id)]
}

// flatten meratakan tracks → providers → servers → streams jadi daftar server.
func flatten(tracks []any) []Server {
	seen := map[string]bool{}
	out := make([]Server, 0, 8)
	for _, t := range tracks {
		track, _ := t.(map[string]any)
		// Track "sub" = hardsub; hanya "ssub" (softsub) yang ditampilkan.
		// shortcut: label track milik miruro, sesuaikan bila mereka mengganti nama.
		if str(track["track"]) != "ssub" {
			continue
		}
		providers, _ := track["providers"].([]any)
		for _, pr := range providers {
			prov, _ := pr.(map[string]any)
			provider := str(prov["provider"])
			subs := subtitles(prov["subtitles"])
			servers, _ := prov["servers"].([]any)
			for _, sv := range servers {
				srv, _ := sv.(map[string]any)
				streams, _ := srv["streams"].([]any)
				for _, st := range streams {
					stream, _ := st.(map[string]any)
					rawURL := str(stream["url"])
					if rawURL == "" || seen[rawURL] {
						continue
					}
					seen[rawURL] = true
					kind := "mp4"
					if str(stream["format"]) == "hls" || strings.Contains(strings.ToLower(rawURL), ".m3u8") {
						kind = "hls"
					}
					name := provider
					if server := str(srv["server"]); server != "" {
						name = provider + " · " + server
					}
					out = append(out, Server{
						Provider: provider,
						Name:     name,
						Type:     kind,
						Quality:  str(stream["quality"]),
						URL:      rawURL,
						Headers:  stringsMap(srv["headers"]),
						Subs:     subs,
					})
				}
			}
		}
	}
	return out
}

// subtitles meratakan daftar subtitle provider ({language,label,file}).
func subtitles(v any) []Sub {
	rows, _ := v.([]any)
	out := make([]Sub, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
		m, _ := r.(map[string]any)
		file := str(m["file"])
		lang := str(m["language"])
		if file == "" || lang == "" || seen[file] {
			continue
		}
		seen[file] = true
		label := str(m["label"])
		if label == "" {
			label = lang
		}
		out = append(out, Sub{Label: label, Lang: lang, URL: file})
	}
	return out
}

// ---- devalue ----

// hydrate men-decode nilai format devalue (SvelteKit __data.json): array nilai
// dengan index 0 sebagai root; angka di dalam objek/array adalah referensi ke
// index, sedangkan nilai primitif di array adalah nilainya sendiri. Nilai
// negatif adalah konstanta devalue (undefined/hole/NaN/Infinity).
func hydrate(vals []any) any {
	memo := make(map[int]any, len(vals))
	var walk func(any) any
	at := func(i int) any {
		if i < 0 || i >= len(vals) {
			return nil
		}
		if v, ok := memo[i]; ok {
			return v
		}
		raw := vals[i]
		switch v := raw.(type) {
		case []any:
			out := make([]any, len(v))
			memo[i] = out
			for k, item := range v {
				out[k] = walk(item)
			}
			return out
		case map[string]any:
			out := make(map[string]any, len(v))
			memo[i] = out
			for k, item := range v {
				out[k] = walk(item)
			}
			return out
		default:
			memo[i] = raw
			return raw
		}
	}
	walk = func(v any) any {
		n, ok := v.(float64)
		if !ok {
			return v
		}
		return at(int(n))
	}
	return at(0)
}

// ---- util ----

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

func stringsOf(v any) []string {
	var out []string
	switch x := v.(type) {
	case string:
		out = append(out, x)
	case []any:
		for _, e := range x {
			if s := str(e); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func stringsMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, e := range m {
		if s := str(e); s != "" {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// titleOf mengambil judul yang paling cocok untuk query pencarian.
func titleOf(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return str(v)
	}
	for _, k := range []string{"english", "romaji", "native"} {
		if s := str(m[k]); s != "" {
			return s
		}
	}
	return ""
}

// slugOf mengubah judul jadi slug miruro ("violet-evergarden-movie").
func slugOf(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "x"
	}
	return slug
}
