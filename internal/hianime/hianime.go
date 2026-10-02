// Package hianime holds the shared upstream constants, the scraping HTTP
// client, and the API-facing data types every scraper produces.
package hianime

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

const (
	BaseAPI   = "https://hianime.at"
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

	// SubUserAgent = UA browser yang terekam di HAR untuk request .vtt
	// (Edge 154). Dipasangkan dengan profil Chrome_152: profil + UA yang sama
	// seperti browser terverifikasi lolos 200 di cfprobe.
	SubUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36 Edg/154.0.0.0"

	// The embed page ships its config as base64(json XOR "otaku-embed-v1").
	EmbedXORKey = "otaku-embed-v1"

	// DenseCues is the cue count a track has to reach to count as the episode
	// rather than its signs. Measured, not guessed (Sep 2026): signs tracks carry
	// 7-8 cues, dialogue tracks 553 — a threshold of 10 switches provider on the
	// sparse track without ever refusing a real one.
	DenseCues = 10
)

var (
	SearchAPI   = BaseAPI + "/search?keyword=%s"
	EpisodesAPI = BaseAPI + "/api/theme/episode/list/%s"
	ServersAPI  = BaseAPI + "/api/theme/episode/servers?episodeId=%s"

	ErrCloudflare = errors.New("blocked by cloudflare: retry from another egress IP")
)

// MaxIdleConnsPerHost defaults to 2, which queues the player's parallel
// segment fetches behind each other; the proxy is only as smooth as this pool.
var HTTPTransport = &http.Transport{
	MaxIdleConns:          64,
	MaxIdleConnsPerHost:   16,
	IdleConnTimeout:       90 * time.Second,
	ResponseHeaderTimeout: 10 * time.Second,
}

// HTTPClient is for scraping: one bounded response. StreamClient pipes media,
// where the body may legitimately take longer than any scrape timeout.
var (
	HTTPClient   = &http.Client{Timeout: PageTimeout, Transport: HTTPTransport}
	StreamClient = &http.Client{Transport: HTTPTransport}
)

// Batas diukur dari respons asli, bukan ditebak (Sep 2026): halaman
// search/detail ~0,3 MB, daftar episode One Piece 1,1 MB, servers 2 KB, embed
// player 4,5 KB, getSources ~0,5 KB, playlist master ~2 KB.
const (
	// Seri terpanjang masih muat dengan ruang lega, tapi tetap ada plafon supaya
	// satu halaman raksasa tidak menahan memori sampai timeout.
	MaxPageBytes = 4 << 20
	// Endpoint kecil: 64 KB jauh di atas kebutuhan, dan batas ini yang membuat
	// hop resolve gagal cepat lalu jatuh ke server berikutnya.
	MaxTinyBytes = 64 << 10

	// Waktu mengikuti ukuran, bukan satu angka untuk semua. Hop kecil rata-rata
	// < 0,5 s → 5 s sudah 10x; halaman besar butuh ruang saat koneksi lambat.
	PageTimeout = 10 * time.Second
	TinyTimeout = 5 * time.Second
)

// Get fetches rawURL with browser-ish headers. referer may be empty.
// Dipakai untuk halaman HTML dan daftar episode (bisa ratusan KB–beberapa MB).
func Get(ctx context.Context, rawURL, referer string) (string, error) {
	return getCapped(ctx, rawURL, referer, MaxPageBytes, PageTimeout)
}

// GetTiny is Get for the small hops: servers, embed page, playlist. Batasnya
// ketat supaya server mati ketahuan dalam detik, bukan menahan seluruh resolve.
func GetTiny(ctx context.Context, rawURL, referer string) (string, error) {
	return getCapped(ctx, rawURL, referer, MaxTinyBytes, TinyTimeout)
}

// GetPart reads at most max bytes of a body within timeout and drops the rest.
// It is for peeking at a subtitle track's shape without downloading it whole:
// the head decides whether a track is the dialogue or the sparse signs/songs
// one, and a subtitle host can be slower than the tiny hops.
func GetPart(ctx context.Context, rawURL, referer string, max int64, timeout time.Duration) (string, error) {
	return getCapped(ctx, rawURL, referer, max, timeout)
}

func getCapped(ctx context.Context, rawURL, referer string, max int64, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, max))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("upstream HTTP %d for %s", resp.StatusCode, rawURL)
	}
	page := string(body)
	if isChallenge(page) {
		return "", ErrCloudflare
	}
	return page, nil
}

// isChallenge membedakan halaman challenge CF dari konten asli. Frasa
// "just a moment" ada di dialog subtitle (terdeteksi di episode ini:
// "Wait just a moment!"), jadi teks saja tidak cukup — challenge selalu
// berupa HTML, sedangkan subtitle/playlist tidak pernah.
func isChallenge(page string) bool {
	lower := strings.ToLower(page)
	if !strings.Contains(lower, "<html") && !strings.Contains(lower, "<!doctype") {
		return false
	}
	return strings.Contains(lower, "just a moment") ||
		strings.Contains(lower, "cf-browser-verification") ||
		strings.Contains(lower, "challenge-platform") ||
		strings.Contains(lower, "cf-chl")
}

// browserOnce builds the impersonating client once: same Chrome handshake the
// HAR browser sends, so subtitle hosts that challenge Go's TLS fingerprint
// answer 200 (verified: dramahot.top and lunarfrontier.website challenge the
// stock client on /subs/*.vtt while the identical URL loads in a browser).
var (
	browserOnce   sync.Once
	browserClient tls_client.HttpClient
	browserErr    error
)

func browser() (tls_client.HttpClient, error) {
	browserOnce.Do(func() {
		browserClient, browserErr = newBrowserClient()
	})
	return browserClient, browserErr
}

func newBrowserClient() (tls_client.HttpClient, error) {
	// Tanpa cookie jar: browser di HAR lolos dengan nol cookie
	// (cookies: [] di tiap request .vtt), dan jar bersama berisiko menyimpan
	// cookie penanda bot dari respons challenge ke request berikutnya.
	return tls_client.NewHttpClient(tls_client.NewNoopLogger(),
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(profiles.Chrome_152),
	)
}

// GetSub is Get for subtitle files: same caps and challenge detection, but
// over the impersonating client. Only subtitle fetches use it — playlists and
// segments already pass with the stock client.
func GetSub(ctx context.Context, rawURL, referer string) (string, error) {
	// 20s, bukan PageTimeout 10s: HAR menunjukkan request .vtt pertama butuh
	// ~23 detik sebelum body penuh, dan timeout yang memotong di tengah
	// terbaca sebagai kegagalan bukan challenge.
	return getSubCapped(ctx, rawURL, referer, MaxPageBytes, 20*time.Second)
}

// GetSubPart is GetPart over the impersonating client.
func GetSubPart(ctx context.Context, rawURL, referer string, max int64, timeout time.Duration) (string, error) {
	return getSubCapped(ctx, rawURL, referer, max, timeout)
}

func getSubCapped(ctx context.Context, rawURL, referer string, max int64, timeout time.Duration) (string, error) {
	// Challenge kadang muncul sekali lalu lolos di handshake berikutnya
	// (teramati: URL sama gagal lalu 200 beberapa menit kemudian), jadi satu
	// percobaan ulang dengan handshake baru. Lebih dari itu hanya membakar
	// waktu: skor bot yang menetap butuh IP lain, bukan percobaan ke-N.
	for attempt := 0; attempt < 2; attempt++ {
		var client tls_client.HttpClient
		var err error
		if attempt == 0 {
			client, err = browser()
		} else {
			client, err = newBrowserClient()
		}
		if err != nil {
			return "", err
		}
		page, err := doSubFetch(ctx, client, rawURL, referer, max, timeout)
		if err == nil {
			return page, nil
		}
		if !errors.Is(err, ErrCloudflare) || attempt == 1 {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return "", ErrCloudflare
}

func doSubFetch(ctx context.Context, client tls_client.HttpClient, rawURL, referer string, max int64, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := fhttp.NewRequestWithContext(ctx, fhttp.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	// Header minimal saja; header + urutan khas Chrome datang dari profil
	// tls-client, jangan ditimpa manual agar tetap konsisten.
	req.Header.Set("User-Agent", SubUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, max))
	if err != nil {
		return "", err
	}
	// 403/503 dari host ini = challenge, bukan hilangnya file: diperlakukan
	// sama seperti body challenge supaya jalan cooldown, bukan error
	// menempel yang bikin player menyerah.
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusServiceUnavailable {
		return "", ErrCloudflare
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("upstream HTTP %d for %s", resp.StatusCode, rawURL)
	}
	page := string(body)
	if isChallenge(page) {
		return "", ErrCloudflare
	}
	return page, nil
}

var reTag = regexp.MustCompile(`<[^>]*>`)

// StripTags flattens an HTML fragment into one line of text.
func StripTags(fragment []string) string {
	if len(fragment) < 2 {
		return ""
	}
	flat := reTag.ReplaceAllString(fragment[1], " ")
	return strings.TrimSpace(html.UnescapeString(strings.Join(strings.Fields(flat), " ")))
}

// ------------------------------------------------------------ types

type Anime struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Poster       string   `json:"poster,omitempty"`
	Synopsis     string   `json:"synopsis,omitempty"`
	Japanese     string   `json:"japanese,omitempty"`
	Aired        string   `json:"aired,omitempty"`
	Premiered    string   `json:"premiered,omitempty"`
	Duration     string   `json:"duration,omitempty"`
	Status       string   `json:"status,omitempty"`
	Score        string   `json:"score,omitempty"`
	Genres       []string `json:"genres,omitempty"`
	Studios      string   `json:"studios,omitempty"`
	Producers    string   `json:"producers,omitempty"`
	SubCount     string   `json:"subCount,omitempty"`
	DubCount     string   `json:"dubCount,omitempty"`
	EpisodeCount string   `json:"episodeCount,omitempty"`
	Related      []Anime  `json:"related,omitempty"`
	Recommended  []Anime  `json:"recommended,omitempty"`
}

type Episode struct {
	ID     string `json:"id"`
	Number string `json:"number"`
}

type Quality struct {
	Height int    `json:"height"`
	Label  string `json:"label"`
	URL    string `json:"url"`
}

type Subtitle struct {
	Lang    string `json:"lang"`
	URL     string `json:"url"`
	Default bool   `json:"default"`
}

type Source struct {
	Master    string     `json:"master"`
	Referer   string     `json:"referer"`
	Qualities []Quality  `json:"qualities"`
	Subtitles []Subtitle `json:"subtitles"`

	// DialogueCues is how many cues the best subtitle track holds. It decides
	// whether a server is worth playing from: a track with a handful of cues is
	// the sparse signs/songs one, not the episode. Not part of the API answer.
	DialogueCues int `json:"-"`
}

// Langs maps the language this app works in onto the names a provider may label
// its tracks with. megaplay answers with full names ("Indonesian", "Chinese
// (Chinese - (Simplified))"), so comparing against a bare code is not enough.
var Langs = map[string][]string{
	id: {"id", "ind", "indonesian", "bahasa", "bahasa indonesia"},
	en: {"en", "eng", "english"},
}

const (
	id = "id"
	en = "en"
)

// MatchesLang reports whether a track's label is the wanted language. Unknown
// languages fall back to a prefix test, which covers codes a provider invents
// ("jpn-JP").
func MatchesLang(label, want string) bool {
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		return false
	}
	names, known := Langs[want]
	if !known {
		return strings.HasPrefix(label, want)
	}
	for _, n := range names {
		if label == n || strings.HasPrefix(label, n+" ") || strings.HasPrefix(label, n+"-") {
			return true
		}
	}
	return false
}

// LangRank scores a subtitle label for picking a default track: 0 Indonesian,
// 1 English, 2 anything else. Indonesian because that is what the app shows;
// English because it is the source the translator is tuned for. A Chinese or
// Japanese track is still offered to the player, but never becomes the default
// while one of those exists — translating from Han text is what once put Han
// text in the .vtt.
func LangRank(label string) int {
	switch {
	case MatchesLang(label, id):
		return 0
	case MatchesLang(label, en):
		return 1
	}
	return 2
}

// OriginOf returns scheme://host/ of raw.
func OriginOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("bad embed url %q", raw)
	}
	return u.Scheme + "://" + u.Host + "/", nil
}
