package miruro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const defaultUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

type LatestEpisode struct {
	AnimeID  string    `json:"animeId"`
	Title    string    `json:"title"`
	Romaji   string    `json:"romaji"`
	ImageURL string    `json:"imageUrl"`
	Episode  int       `json:"episode"`
	AiredAt  time.Time `json:"airedAt"`
	Format   string    `json:"format"`
	Year     int       `json:"year"`
	SubCount *int      `json:"subCount"`
	DubCount *int      `json:"dubCount"`
}

type SearchResult struct {
	AnimeID  string `json:"animeId"`
	Title    string `json:"title"`
	ImageURL string `json:"imageUrl"`
	Format   string `json:"format"`
	Year     *int   `json:"year"`
	Episodes *int   `json:"episodes"`
	Score    *int   `json:"score"`
}

type WatchAnime struct {
	Title       string   `json:"title"`
	ImageURL    string   `json:"imageUrl"`
	Description string   `json:"description"`
	Format      string   `json:"format"`
	Status      string   `json:"status"`
	Season      string   `json:"season"`
	Genres      []string `json:"genres"`
	Studios     []string `json:"studios"`
	Year        *int     `json:"year"`
	Episodes    *int     `json:"episodes"`
	Duration    *int     `json:"duration"`
	Score       *int     `json:"score"`
}

type WatchEpisode struct {
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	ImageURL    *string `json:"imageUrl"`
	AirDate     *string `json:"airDate"`
	Description *string `json:"description"`
	Duration    *int    `json:"duration"`
}

type WatchSubtitle struct {
	Label     string `json:"label"`
	URL       string `json:"url"`
	IsDefault bool   `json:"isDefault"`
}

type WatchServer struct {
	Provider string            `json:"provider"`
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
}

type WatchData struct {
	Anime          WatchAnime      `json:"anime"`
	Episodes       []WatchEpisode  `json:"episodes"`
	Servers        []WatchServer   `json:"servers"`
	Subtitles      []WatchSubtitle `json:"subtitles"`
	CurrentEpisode int             `json:"currentEpisode"`
}

type Client struct {
	http *resty.Client
	base string
}

func New() *Client {
	return NewWithBase("https://miruro.cx")
}

func NewWithBase(base string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		http: resty.New().
			SetTimeout(30*time.Second).
			SetHeader("referer", "https://miruro.cx/").
			SetHeader("user-agent", defaultUA),
	}
}

func (c *Client) get(ctx context.Context, target, accept string) (string, error) {
	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("accept", accept).
		Get(target)
	if err != nil {
		return "", fmt.Errorf("miruro request: %w", err)
	}
	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("miruro HTTP %d", resp.StatusCode())
	}
	return resp.String(), nil
}

func (c *Client) getData(
	ctx context.Context,
	path string,
	query map[string]string,
) (map[string]any, error) {
	values := url.Values{}
	for key, value := range query {
		values.Set(key, value)
	}
	target := c.base + path
	if encoded := values.Encode(); encoded != "" {
		target += "?" + encoded
	}
	body, err := c.get(ctx, target, "*/*")
	if err != nil {
		return nil, err
	}

	// SvelteKit signals slug redirects in the JSON body, not with HTTP 3xx.
	first := strings.SplitN(body, "\n", 2)[0]
	var header map[string]any
	if err := json.Unmarshal([]byte(first), &header); err == nil {
		if header["type"] == "redirect" {
			location, err := url.Parse(asString(header["location"]))
			if err != nil {
				return nil, fmt.Errorf("miruro redirect: %w", err)
			}
			next := c.base + location.Path + "/__data.json"
			if location.RawQuery != "" {
				next += "?" + location.RawQuery
			}
			body, err = c.get(ctx, next, "*/*")
			if err != nil {
				return nil, err
			}
		}
	}

	data, err := parseDevalue(body)
	if err != nil {
		return nil, err
	}
	root, err := data.root(1)
	if err != nil {
		return nil, err
	}
	page, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected sveltekit root")
	}
	return page, nil
}

func asMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func asList(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}
	return nil
}

func asString(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func asIntPtr(value any) *int {
	if parsed, ok := asInt(value); ok {
		return &parsed
	}
	return nil
}

func asStringSlice(value any) []string {
	items := asList(value)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, asString(item))
	}
	return out
}
