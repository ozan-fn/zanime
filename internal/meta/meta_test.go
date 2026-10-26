package meta

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestBuildMagnet(t *testing.T) {
	tcs := []struct {
		name string
		s    Stream
		want []string
		not  []string
	}{
		{
			name: "infoHash + tracker",
			s: Stream{
				InfoHash: "deadbeef",
				Sources:  []string{"tracker:udp://tr.opentrackr.org:1337/announce", "dnode:xyz"},
				BehaviorHints: struct {
					Filename string `json:"filename"`
				}{Filename: "Frieren S02E02.mkv"},
			},
			want: []string{"magnet:?xt=urn:btih:deadbeef", "dn=Frieren+S02E02.mkv",
				"tr=udp%3A%2F%2Ftr.opentrackr.org%3A1337%2Fannounce"},
			not: []string{"dnode"},
		},
		{
			name: "tanpa infoHash \u2192 pakai URL",
			s:    Stream{URL: "http://contoh/stream.m3u8"},
			want: []string{"http://contoh/stream.m3u8"},
			not:  []string{"magnet:"},
		},
	}
	for _, tc := range tcs {
		got := BuildMagnet(tc.s, "fallback.mkv")
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: magnet kurang %q\ndapat: %s", tc.name, w, got)
			}
		}
		for _, n := range tc.not {
			if strings.Contains(got, n) {
				t.Errorf("%s: magnet tidak boleh berisi %q\ndapat: %s", tc.name, n, got)
			}
		}
	}
}

// ---------- integration (hanya berjalan dengan INTEGRATION=1) ----------

func writeFakeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Jalankan: INTEGRATION=1 go test -run TestIntegrationFlow -v
func TestIntegrationFlow(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 untuk menjalankan")
	}
	cine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/catalog/series/"):
			writeFakeJSON(w, CatalogResp{Metas: []MetaPreview{{
				ID: "tt1", Type: "series", Name: "Test Anime", Year: "2021", IMDBRating: "8.5",
			}}})
		case strings.Contains(r.URL.Path, "/catalog/"):
			writeFakeJSON(w, CatalogResp{})
		case strings.Contains(r.URL.Path, "/meta/"):
			var resp MetaResp
			resp.Meta.ID, resp.Meta.Type, resp.Meta.Name = "tt1", "series", "Test Anime"
			resp.Meta.Videos = []Video{{ID: "tt1:1:2", Season: 1, Episode: 2, Title: "Ep 2"}}
			writeFakeJSON(w, resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer cine.Close()

	tor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFakeJSON(w, StreamResp{Streams: []Stream{{
			Name:     "Torrentio\n1080p",
			Title:    "Test.Anime.S01E02.mkv\n\U0001F465 100 \U0001F4BE 500 MB \u2699\uFE0F NyaaSi",
			InfoHash: "deadbeef",
			Sources:  []string{"tracker:udp://tracker.ccc.de:1337/announce"},
			BehaviorHints: struct {
				Filename string `json:"filename"`
			}{Filename: "Test.Anime.S01E02.mkv"},
		}}})
	}))
	defer tor.Close()

	oldCine, oldTor := CinemetaURL, TorrentioURL
	CinemetaURL, TorrentioURL = cine.URL, tor.URL
	defer func() { CinemetaURL, TorrentioURL = oldCine, oldTor }()

	// 1. search
	metas, err := SearchAll("test anime")
	if err != nil {
		t.Fatal("search:", err)
	}
	if len(metas) != 1 || metas[0].Name != "Test Anime" || metas[0].ID != "tt1" {
		t.Fatalf("hasil search salah: %+v", metas)
	}

	// 2. meta \u2192 episode
	m, err := GetMeta("series", "tt1")
	if err != nil {
		t.Fatal("meta:", err)
	}
	if vids := m.Meta.Videos; len(vids) != 1 || vids[0].ID != "tt1:1:2" {
		t.Fatalf("videos salah: %+v", vids)
	}

	// 3. streams \u2192 magnet
	streams, err := GetStreams("series", "tt1:1:2")
	if err != nil {
		t.Fatal("streams:", err)
	}
	if len(streams) != 1 {
		t.Fatalf("jumlah stream salah: %d", len(streams))
	}
	mag := BuildMagnet(streams[0], "Test Anime")
	if !strings.Contains(mag, "magnet:?xt=urn:btih:deadbeef") ||
		!strings.Contains(mag, "dn=Test.Anime.S01E02.mkv") ||
		!strings.Contains(mag, "tr=udp%3A%2F%2Ftracker.ccc.de%3A1337%2Fannounce") {
		t.Fatalf("magnet salah: %s", mag)
	}
}
