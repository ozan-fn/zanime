// Alur scrape embedmaster, semuanya di Go (tanpa bun/node):
//
//	GET embed → (bila gerbang) POST tr= → halaman player
//	halaman player → skrip kredensial obfuscated → ticket/pk/scrapeToken
//	feed NDJSON /api/sources → daftar server
//	resolve.php per server → blob terenkripsi → decrypt() → URL stream
package em

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

const (
	// UA dipakai ke semua permintaan; host video menolak tanpa ini.
	userAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0"
	// referer embed: halaman penyemat yang diharapkan pemain.
	embedReferer = "https://embedmaster.com/"
)

// feedRow adalah satu baris NDJSON feed /api/sources.
type feedRow struct {
	Type       string `json:"type"`
	SourceName string `json:"source_name"`
	SourceURL  string `json:"source_url"`
}

// resolved adalah isi plaintext resolve.php.
type resolved struct {
	OK        bool      `json:"ok"`
	Kind      string    `json:"kind"`
	URL       string    `json:"url"`
	Type      string    `json:"type"`
	Quality   string    `json:"quality"`
	Qualities []Quality `json:"qualities"`
	Subtitles []Sub     `json:"subtitles"`
}

type episodes struct {
	ShowTitle string `json:"showTitle"`
	Seasons   []struct {
		Season   int `json:"season"`
		Episodes []struct {
			Episode int    `json:"episode"`
			Title   string `json:"title"`
			URL     string `json:"url"`
		} `json:"episodes"`
	} `json:"seasons"`
}

var (
	credScriptRe = regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)
	episodesRe   = regexp.MustCompile(`(?s)window\.__EM_EPISODES__\s*=\s*(\{.*?\});`)
)

// embedURL menyusun halaman embed untuk movie atau satu episode tv.
func (c *Client) embedURL(tab, id string, tv *TVEpisode) string {
	path := fmt.Sprintf("/movie/%s", url.PathEscape(id))
	if tab == "tv" && tv != nil {
		path = fmt.Sprintf("/tv/%s/%d/%d", url.PathEscape(id), tv.Season, tv.Episode)
	}
	return c.Base + path + "?skin=onyx&welcome_page=on&autoplay=off"
}

// playerPage menembus welcome gate Cloudflare dan mengembalikan URL akhir
// halaman player beserta HTML-nya.
func (c *Client) playerPage(ctx context.Context, embed string) (string, string, error) {
	c.logf("page", LogInfo, "GET halaman embed %s", embed)
	res, err := c.get(ctx, embed, map[string]string{"Referer": embedReferer})
	if err != nil {
		c.logf("page", LogErr, "halaman embed gagal: %v", err)
		return "", "", err
	}
	html, err := readHTML(res)
	if err != nil {
		return "", "", err
	}
	playerURL := res.Request.URL.String()
	// Gerbang welcome: halaman belum memuat config obfuscated; POST tr= untuk
	// menukar cookie menjadi halaman player sesungguhnya.
	if !strings.Contains(html, "atob(") || !strings.Contains(html, "v2/assets") {
		c.logf("page", LogInfo, "gerbang welcome · POST tr=")
		form := url.Values{"tr": {embedReferer}}
		res, err = c.postForm(ctx, playerURL, form, map[string]string{
			"Origin":  originOf(embed),
			"Referer": embedReferer,
		})
		if err != nil {
			c.logf("page", LogErr, "gerbang welcome gagal: %v", err)
			return "", "", err
		}
		if html, err = readHTML(res); err != nil {
			return "", "", err
		}
		playerURL = res.Request.URL.String()
	}
	c.logf("page", LogOK, "halaman player siap (%s)", playerURL)
	return playerURL, html, nil
}

// creds mengekstrak skrip kredensial dari HTML player, lalu mendekodenya.
func creds(html string) (credCfg, error) {
	for _, m := range credScriptRe.FindAllStringSubmatch(html, -1) {
		body := m[1]
		if !strings.Contains(body, "atob(") || !strings.Contains(body, "(0,eval)(decodeURIComponent(escape(") {
			continue
		}
		return decodeCred(body)
	}
	return credCfg{}, fmt.Errorf("halaman player tanpa skrip kredensial")
}

// episodeURL memilih URL episode dari window.__EM_EPISODES__ halaman tv.
func episodeURL(html string, season, episode int) (string, error) {
	m := episodesRe.FindStringSubmatch(html)
	if m == nil {
		return "", fmt.Errorf("halaman tv tanpa __EM_EPISODES__")
	}
	var list episodes
	if err := json.Unmarshal([]byte(m[1]), &list); err != nil {
		return "", fmt.Errorf("daftar episode: %w", err)
	}
	for _, s := range list.Seasons {
		if s.Season != season {
			continue
		}
		for _, e := range s.Episodes {
			if e.Episode == episode && e.URL != "" {
				return e.URL, nil
			}
		}
	}
	return "", fmt.Errorf("episode S%dE%d tidak ada di daftar", season, episode)
}

// feed mengambil daftar server dari feed NDJSON memakai scrapeToken segar.
func (c *Client) feed(ctx context.Context, cfg credCfg, playerURL string) ([]feedRow, error) {
	endpoint, err := resolveRef(playerURL, cfg.FeedURL+cfg.ScrapeToken)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, endpoint, map[string]string{
		"Accept":  "application/x-ndjson",
		"Origin":  "https://embdmstrplayer.com",
		"Referer": playerURL,
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var out []feedRow
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row feedRow
		if json.Unmarshal([]byte(line), &row) == nil && row.Type == "server" && row.SourceURL != "" {
			out = append(out, row)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("feed embedmaster kosong")
	}
	return out, nil
}

// resolve menanyakan satu server ke resolve.php lalu mendekripsinya.
func (c *Client) resolve(ctx context.Context, cfg credCfg, playerURL string, row feedRow) resolved {
	endpoint, err := resolveRef(playerURL, cfg.ResolveURL)
	if err != nil {
		return resolved{}
	}
	// source_url dibiarkan apa adanya (pemain juga begitu): RawQuery agar
	// query mentah server tidak di-encode ulang.
	target := endpoint + "?src=" + row.SourceURL
	headers := map[string]string{
		"Referer":     playerURL,
		"X-EM-Ticket": cfg.Ticket,
		"X-Em-Player": "1",
	}
	var out resolved
	for attempt := 0; attempt < 2; attempt++ {
		res, err := c.get(ctx, target, headers)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			continue
		}
		plain, err := decrypt(string(body), cfg.PK)
		if err != nil {
			continue
		}
		if json.Unmarshal([]byte(plain), &out) == nil && out.OK {
			return out
		}
	}
	return resolved{}
}

// get mengirim GET dengan User-Agent + header tambahan.
func (c *Client) get(ctx context.Context, endpoint string, headers map[string]string) (*http.Response, error) {
	return c.do(ctx, http.MethodGet, endpoint, nil, headers)
}

// postForm mengirim POST x-www-form-urlencoded (dipakai gerbang welcome).
func (c *Client) postForm(ctx context.Context, endpoint string, form url.Values, headers map[string]string) (*http.Response, error) {
	h := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	for k, v := range headers {
		h[k] = v
	}
	return c.do(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()), h)
}

func (c *Client) do(ctx context.Context, method, endpoint string, body io.Reader, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("permintaan %s: %w", method, err)
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 400 {
		_ = res.Body.Close()
		return nil, fmt.Errorf("embedmaster HTTP %s", res.Status)
	}
	return res, nil
}

func readHTML(res *http.Response) (string, error) {
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// resolveRef menyelesaikan URL relatif terhadap halaman player.
func resolveRef(page, ref string) (string, error) {
	base, err := url.Parse(page)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(u).String(), nil
}

func originOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// newHTTP mengembalikan klien HTTP dengan cookie jar sendiri (sesi per judul).
func newHTTP() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}
