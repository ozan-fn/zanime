package miruro

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// searchBody: satu hasil "Nisekoi" dengan external_ids imdb/tmdb.
const searchBody = `{"type":"data","nodes":[{"type":"data","data":[` +
	`{"items":2},"anime",[3],{"id":4,"external_ids":5,"title":6},` +
	`"HASH1",{"imdb":7,"tmdb_tv":8},"Nisekoi: False Love","tt3115338","62640"]}]}`

// watchBody: payload watch tanpa ep + chunk sources. Slug kanonik "nisekoi",
// jadi slug hasil judul ("nisekoi-false-love") harus lewat redirect dulu.
const watchBody = `{"type":"data","nodes":[{"type":"data","data":[` +
	`{"media":1,"sourcesData":2},{"id":3,"title":4,"format":5},"HASH1","Nisekoi","MOVIE"]}]}` + "\n" +
	`{"type":"chunk","id":1,"data":[` +
	`{"tracks":1},[2],{"track":3,"providers":4},"ssub",[5],` +
	`{"provider":6,"servers":7,"subtitles":17},"bee",[8],` +
	`{"server":9,"headers":10,"embed":11,"streams":12},"HD-1",{"Referer":13},null,[14],` +
	`"https://megaplay.buzz/",{"url":15,"format":16},"https://h/v.m3u8","hls",` +
	`[18],{"language":19,"label":20,"file":21},"en","English","https://h/sub.vtt"]}`

func TestStreamsMatchesIMDBAndFlattensSources(t *testing.T) {
	var watched []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search/__data.json":
			if got := r.URL.Query().Get("q"); got != "Nisekoi: False Love" {
				t.Errorf("query pencarian = %q", got)
			}
			_, _ = w.Write([]byte(searchBody))
		case strings.HasPrefix(r.URL.Path, "/watch/"):
			watched = append(watched, r.URL.Path+r.URL.RawQuery)
			if r.URL.Path != "/watch/HASH1/nisekoi/__data.json" {
				_, _ = w.Write([]byte(`{"type":"redirect","location":"/watch/HASH1/nisekoi"}`))
				return
			}
			_, _ = w.Write([]byte(watchBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer up.Close()

	c := New()
	c.SetBase(up.URL)
	servers, err := c.Streams(context.Background(), "movie", "tt3115338", "62640", "Nisekoi: False Love", 0, 0)
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("server = %d, want 1", len(servers))
	}
	got := servers[0]
	if got.Name != "bee · HD-1" || got.Type != "hls" || got.URL != "https://h/v.m3u8" {
		t.Fatalf("server = %+v", got)
	}
	if got.Headers["Referer"] != "https://megaplay.buzz/" {
		t.Fatalf("headers = %v", got.Headers)
	}
	if len(got.Subs) != 1 || got.Subs[0].Lang != "en" || got.Subs[0].URL != "https://h/sub.vtt" {
		t.Fatalf("subs = %+v, want subtitle provider", got.Subs)
	}
	if len(watched) != 2 || watched[1] != "/watch/HASH1/nisekoi/__data.json" {
		t.Fatalf("watch dipanggil %v, redirect tidak diikuti", watched)
	}
}

func TestStreamsNoMatch(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(searchBody))
	}))
	defer up.Close()

	c := New()
	c.SetBase(up.URL)
	if _, err := c.Streams(context.Background(), "movie", "tt999", "999", "Nisekoi", 0, 0); err == nil {
		t.Fatal("judul yang tidak cocok harus error")
	}
}

// TestFlattenSkipsHardsub: track "sub" (hardsub) diabaikan, hanya softsub.
func TestFlattenSkipsHardsub(t *testing.T) {
	track := func(name string, url string) map[string]any {
		return map[string]any{
			"track": name,
			"providers": []any{map[string]any{
				"provider": "sun",
				"servers": []any{map[string]any{
					"server":  name + "-srv",
					"streams": []any{map[string]any{"url": url, "format": "hls"}},
				}},
			}},
		}
	}
	got := flatten([]any{track("sub", "https://h/hard.m3u8"), track("ssub", "https://h/soft.m3u8")})
	if len(got) != 1 || got[0].URL != "https://h/soft.m3u8" {
		t.Fatalf("server = %+v, want hanya softsub", got)
	}
}

func TestSlugOf(t *testing.T) {
	if got := slugOf("Violet Evergarden: The Movie"); got != "violet-evergarden-the-movie" {
		t.Fatalf("slugOf = %q", got)
	}
}
