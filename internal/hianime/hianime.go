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
	"time"
)

const (
	BaseAPI   = "https://hianime.at"
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

	// The embed page ships its config as base64(json XOR "otaku-embed-v1").
	EmbedXORKey = "otaku-embed-v1"
)

var (
	SearchAPI   = BaseAPI + "/search?keyword=%s"
	EpisodesAPI = BaseAPI + "/api/theme/episode/list/%s"
	ServersAPI  = BaseAPI + "/api/theme/episode/servers?episodeId=%s"

	ErrCloudflare = errors.New("blocked by cloudflare: install curl-impersonate or retry from another egress IP")
)

// MaxIdleConnsPerHost defaults to 2, which queues the player's parallel
// segment fetches behind each other; the proxy is only as smooth as this pool.
var HTTPTransport = &http.Transport{
	MaxIdleConns:          64,
	MaxIdleConnsPerHost:   16,
	IdleConnTimeout:       90 * time.Second,
	ResponseHeaderTimeout: 20 * time.Second,
}

// HTTPClient is for scraping: one bounded response. StreamClient pipes media,
// where the body may legitimately take longer than any scrape timeout.
var (
	HTTPClient   = &http.Client{Timeout: 20 * time.Second, Transport: HTTPTransport}
	StreamClient = &http.Client{Transport: HTTPTransport}
)

// Get fetches rawURL with browser-ish headers. referer may be empty.
func Get(ctx context.Context, rawURL, referer string) (string, error) {
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("upstream HTTP %d for %s", resp.StatusCode, rawURL)
	}
	page := string(body)
	if strings.Contains(strings.ToLower(page), "just a moment") ||
		strings.Contains(page, "cf-browser-verification") {
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
}

// OriginOf returns scheme://host/ of raw.
func OriginOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("bad embed url %q", raw)
	}
	return u.Scheme + "://" + u.Host + "/", nil
}
