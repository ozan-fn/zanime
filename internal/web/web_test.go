package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"zanime/internal/meta"
)

func TestStreamsTrackersNotNull(t *testing.T) {
	tor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resp meta.StreamResp
		resp.Streams = []meta.Stream{
			{
				Name:     "Torrentio\n1080p",
				Title:    "Test.S01E01.mkv\n👤 10 💾 100 MB ⚙️ NyaaSi",
				InfoHash: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
				Sources:  []string{"tracker:udp://tracker.opentrackr.org:1337/announce"},
			},
			{
				Name:  "Torrentio\n720p",
				Title: "Test2.mkv\n👤 1 💾 50 MB ⚙️ Other",
				// tanpa sources → Trackers harus [] (bukan null)
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer tor.Close()

	old := meta.TorrentioURL
	meta.TorrentioURL = tor.URL
	defer func() { meta.TorrentioURL = old }()

	srv := httptest.NewServer(New(nil).Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/streams?type=series&id=tt1:1:1&name=Test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)

	// regresi: slice kosong tidak boleh ter-serialisasi jadi null
	if strings.Contains(string(body), `"trackers":null`) {
		t.Fatalf("trackers null — frontend gagal mengiterasi: %s", body)
	}
	if !strings.Contains(string(body), `"trackers":[]`) {
		t.Fatalf("trackers [] tidak ditemukan: %s", body)
	}
	if !strings.Contains(string(body), "magnet:?xt=urn:btih:deadbeef") {
		t.Fatalf("magnet tidak terbentuk: %s", body)
	}
}

// Frontend ter-embed: / dan route SPA sama-sama dilayani index.html,
// aset statis dari folder yang sama, path /api tak dikenal tetap 404.
func TestEmbeddedFrontend(t *testing.T) {
	if !HasFrontend() {
		t.Skip("frontend belum dibangun (internal/web/dist/index.html tidak ada)")
	}
	srv := httptest.NewServer(New(nil).Routes())
	defer srv.Close()

	get := func(p string) (int, string) {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	for _, p := range []string{"/", "/genre/action"} {
		code, body := get(p)
		if code != 200 || !strings.Contains(body, "<div id=\"root\">") {
			t.Fatalf("%s: status %d, bukan index.html: %.80s", p, code, body)
		}
	}
	if code, _ := get("/api/tidak-ada"); code != http.StatusNotFound {
		t.Fatalf("/api tak dikenal harus 404, dapat %d", code)
	}
}

func TestStreamsRequiresParams(t *testing.T) {
	srv := httptest.NewServer(New(nil).Routes())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/streams")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("tanpa param harus 400, dapat %d", resp.StatusCode)
	}
}
