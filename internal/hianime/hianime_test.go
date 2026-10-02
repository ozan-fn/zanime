package hianime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetDetectsCloudflare(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<!DOCTYPE html><html><head><title>Just a moment...</title></head><body></body></html>`)
	}))
	defer srv.Close()
	if _, err := Get(context.Background(), srv.URL, ""); err != ErrCloudflare {
		t.Fatalf("want ErrCloudflare, got %v", err)
	}
}

// Regresi: dialog subtitle "Wait just a moment!" pernah terdeteksi sebagai
// challenge sehingga fetch yang sebenarnya 200 dilaporkan blocked.
func TestGetIgnoresChallengePhraseOutsideHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "WEBVTT\n\n00:02:13.540 --> 00:02:14.280\nWait just a moment!\n")
	}))
	defer srv.Close()
	if _, err := Get(context.Background(), srv.URL, ""); err != nil {
		t.Fatalf("subtitle body misread as challenge: %v", err)
	}
}

func TestSearchParsing(t *testing.T) {
	page := `<div class="flw-item "><div class="film-poster">
		<img src="https://cdn.example/p1.jpg" class="film-poster-img" alt="a"></div>
		<div class="film-detail"><h3 class="film-name">
		<a href="/watch/naruto-shippuden-xyz" title="Naruto: Shipp&#039;uden &amp; more"></a></h3>
		<div class="description">Space ninja story.</div></div></div>
	<div class="flw-item "><div class="film-detail"><h3 class="film-name">
		<a href="/watch/one-piece" title="One Piece"></a></h3></div></div>
	<div id="main-sidebar"><h3 class="film-name">
		<a href="/watch/sidebar-noise" title="Sidebar Noise"></a></h3></div>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("keyword"); got != "naruto piece" {
			t.Errorf("keyword not escaped: %q", got)
		}
		io.WriteString(w, page)
	}))
	defer srv.Close()

	old := SearchAPI
	SearchAPI = srv.URL + "/search?keyword=%s"
	defer func() { SearchAPI = old }()

	got, err := Search(context.Background(), "naruto piece")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 results (sidebar cut off), got %d: %+v", len(got), got)
	}
	if got[0].ID != "naruto-shippuden-xyz" || got[0].Name != "Naruto: Shipp'uden & more" {
		t.Fatalf("bad first result: %+v", got[0])
	}
	if got[0].Synopsis == "" {
		t.Errorf("synopsis not scraped: %+v", got[0])
	}
}

func TestEpisodesParsing(t *testing.T) {
	var gotPath string
	page := `{\"ep-item\" data-number=\"2\" data-id=\"20\" href=\"/watch/slug-xyz?ep=20\"},\
{\"ep-item\" data-number=\"1\" data-id=\"10\" href=\"/watch/slug-xyz?ep=10\"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.WriteString(w, page)
	}))
	defer srv.Close()

	old := EpisodesAPI
	EpisodesAPI = srv.URL + "/api/theme/episode/list/%s"
	defer func() { EpisodesAPI = old }()

	got, err := Episodes(context.Background(), "slug-xyz")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/theme/episode/list/xyz" {
		t.Fatalf("list route wants the trailing id, not the slug: %s", gotPath)
	}
	if len(got) != 2 || got[0].Number != "1" || got[1].Number != "2" {
		t.Fatalf("episodes not sorted by number: %+v", got)
	}
}
