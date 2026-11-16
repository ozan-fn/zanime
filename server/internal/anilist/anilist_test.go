package anilist

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBannersMapsGraphQLResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("body: %v", err)
		}
		if body["query"] == "" {
			t.Error("missing query")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"Page":{"media":[
			{"title":{"romaji":"Romaji A","english":"Title A"},"bannerImage":"https://img/banner.jpg","coverImage":{"extraLarge":"https://img/cover.jpg"},"genres":["Fantasy","Romance"],"seasonYear":2026,"episodes":12,"nextAiringEpisode":{"episode":8}},
			{"title":{"romaji":"Title B","english":""},"bannerImage":"","coverImage":{"extraLarge":"https://img/cover-b.jpg"},"genres":[],"seasonYear":2025,"episodes":24,"nextAiringEpisode":null}
		]}}}`))
	}))
	defer server.Close()

	banners, err := NewWithEndpoint(server.URL).Banners(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(banners) != 2 {
		t.Fatalf("got %d banners", len(banners))
	}
	first := banners[0]
	if first.Title != "Title A" || first.ImageURL != "https://img/banner.jpg" {
		t.Fatalf("first = %+v", first)
	}
	if first.Aired == nil || *first.Aired != 7 {
		t.Fatalf("aired = %v", first.Aired)
	}
	second := banners[1]
	if second.Title != "Title B" || second.ImageURL != "https://img/cover-b.jpg" {
		t.Fatalf("second = %+v", second)
	}
	if second.Aired == nil || *second.Aired != 24 {
		t.Fatalf("second aired = %v", second.Aired)
	}
}
