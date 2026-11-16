package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"miruro-proxy/internal/anilist"
	"miruro-proxy/internal/miruro"
	"miruro-proxy/internal/proxy"
)

type stubBanners struct {
	items []anilist.Banner
}

func (s stubBanners) Banners(context.Context, int) ([]anilist.Banner, error) {
	return s.items, nil
}

type stubMiruro struct {
	latest     []miruro.LatestEpisode
	search     []miruro.SearchResult
	watch      *miruro.WatchData
	lastQuery  string
	lastAnime  string
	lastEp     int
	watchError error
}

func (s *stubMiruro) Latest(context.Context) ([]miruro.LatestEpisode, error) {
	return s.latest, nil
}

func (s *stubMiruro) Search(_ context.Context, query string) ([]miruro.SearchResult, error) {
	s.lastQuery = query
	return s.search, nil
}

func (s *stubMiruro) Watch(_ context.Context, animeID string, episode int) (*miruro.WatchData, error) {
	s.lastAnime = animeID
	s.lastEp = episode
	return s.watch, s.watchError
}

type stubStreamer struct {
	upstream *proxy.Upstream
	err      error
}

func (s stubStreamer) Fetch(context.Context, string, string, string, string) (*proxy.Upstream, error) {
	return s.upstream, s.err
}

func newTestApp(t *testing.T, source *stubMiruro, streams Streamer) *fiber.App {
	t.Helper()
	return New(
		stubBanners{items: []anilist.Banner{{
			Title: "Banner", ImageURL: "https://img/banner.jpg",
			Genres: []string{"Fantasy"}, Year: intPtr(2026), Episodes: intPtr(12), Aired: intPtr(7),
		}}},
		source,
		streams,
	)
}

func get(t *testing.T, app *fiber.App, target string) *http.Response {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, target, nil))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var value T
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func intPtr(value int) *int {
	return &value
}

func TestBannersAndLatestEndpoints(t *testing.T) {
	source := &stubMiruro{latest: []miruro.LatestEpisode{{
		AnimeID: "id1", Title: "Frieren", Romaji: "Sousou", ImageURL: "https://img/1.jpg",
		Episode: 5, Format: "TV", Year: 2024, SubCount: intPtr(12), DubCount: intPtr(3),
	}}}
	app := newTestApp(t, source, stubStreamer{})

	banners := decode[[]anilist.Banner](t, get(t, app, "/api/banners"))
	if len(banners) != 1 || banners[0].Title != "Banner" {
		t.Fatalf("banners = %+v", banners)
	}

	latest := decode[[]miruro.LatestEpisode](t, get(t, app, "/api/latest"))
	if len(latest) != 1 || latest[0].AnimeID != "id1" || latest[0].Episode != 5 {
		t.Fatalf("latest = %+v", latest)
	}
}

func TestSearchAndWatchEndpoints(t *testing.T) {
	source := &stubMiruro{
		search: []miruro.SearchResult{{
			AnimeID: "id1", Title: "Frieren", ImageURL: "https://img/1.jpg",
			Format: "TV", Year: intPtr(2023), Score: intPtr(91),
		}},
		watch: &miruro.WatchData{
			Anime: miruro.WatchAnime{
				Title: "Dungeon Meshi", ImageURL: "https://img/cover.jpg",
				Genres: []string{"Adventure"}, Studios: []string{"TRIGGER"},
				Year: intPtr(2024), Episodes: intPtr(24),
			},
			Episodes: []miruro.WatchEpisode{
				{Number: 1, Title: "Hot Pot", AirDate: strPtr("2024-01-04")},
				{Number: 2, Title: "Tart"},
			},
			Servers:        []miruro.WatchServer{{Provider: "sun", URL: "https://cdn/1.m3u8"}},
			Subtitles:      []miruro.WatchSubtitle{{Label: "English", URL: "https://sub/en.vtt"}},
			CurrentEpisode: 2,
		},
	}
	app := newTestApp(t, source, stubStreamer{})

	results := decode[[]miruro.SearchResult](t, get(t, app, "/api/search?q=frieren"))
	if len(results) != 1 || results[0].Title != "Frieren" || source.lastQuery != "frieren" {
		t.Fatalf("results = %+v query = %q", results, source.lastQuery)
	}

	watch := decode[miruro.WatchData](t, get(t, app, "/api/watch/abc?ep=3"))
	if source.lastAnime != "abc" || source.lastEp != 3 {
		t.Fatalf("watched %q ep %d", source.lastAnime, source.lastEp)
	}
	if watch.Anime.Title != "Dungeon Meshi" || len(watch.Episodes) != 2 ||
		watch.CurrentEpisode != 2 || len(watch.Servers) != 1 {
		t.Fatalf("watch = %+v", watch)
	}

	if resp := get(t, app, "/api/watch/abc?ep=0"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("ep 0 status = %d", resp.StatusCode)
	}
}

func TestStreamRewritesPlaylistAndMapsErrors(t *testing.T) {
	base, _ := url.Parse("https://cdn.example.com/hls/index.m3u8")
	body := io.NopCloser(strings.NewReader("#EXTM3U\nseg1.ts"))
	app := newTestApp(t, &stubMiruro{}, stubStreamer{upstream: &proxy.Upstream{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/vnd.apple.mpegurl"}},
		Body:       body,
		FinalURL:   base,
	}})

	resp := get(t, app, "/stream?url=https%3A%2F%2Fcdn.example.com%2Fhls%2Findex.m3u8&referer=https%3A%2F%2Fmiruro.cx%2F")
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(raw), `/stream?referer=https%3A%2F%2Fmiruro.cx%2F&url=https%3A%2F%2Fcdn.example.com%2Fhls%2Fseg1.ts`) {
		t.Fatalf("playlist = %s", raw)
	}

	for _, test := range []struct {
		err    error
		status int
	}{
		{proxy.ErrInvalidURL, http.StatusBadRequest},
		{proxy.ErrPrivateHost, http.StatusForbidden},
		{errors.New("boom"), http.StatusBadGateway},
	} {
		app := newTestApp(t, &stubMiruro{}, stubStreamer{err: test.err})
		if resp := get(t, app, "/stream?url=https%3A%2F%2Fcdn.example.com%2Fa.m3u8"); resp.StatusCode != test.status {
			t.Fatalf("%v status = %d", test.err, resp.StatusCode)
		}
	}
}

func strPtr(value string) *string {
	return &value
}
