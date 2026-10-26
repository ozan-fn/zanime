package torrent_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	ztorrent "zanime/internal/torrent"
	"zanime/internal/web"
)

// TestStatusWSRealtime: status dikirim lewat WebSocket ± tiap 100 ms (tanpa polling).
// Sesi sengaja tanpa peer — yang diukur hanya kanal push-nya.
func TestStatusWSRealtime(t *testing.T) {
	t.Setenv("ZANIME_TORRENT_NO_DHT", "1")
	t.Setenv("ZANIME_TORRENT_DATA_DIR", t.TempDir())
	t.Setenv("ZANIME_CACHE_DIR", t.TempDir())

	mgr, err := ztorrent.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	srv := httptest.NewServer(web.New(mgr).Routes())
	defer srv.Close()

	hash := strings.Repeat("ab", 20)
	if resp, gerr := srv.Client().Get(srv.URL + "/api/torrent/start?hash=" + hash); gerr == nil {
		_ = resp.Body.Close()
	}

	ws := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/torrent/ws/" + hash
	conn, _, err := websocket.DefaultDialer.Dial(ws, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var first, last time.Time
	for i := 0; i < 6; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("pesan ke-%d: %v", i+1, err)
		}
		if i == 0 {
			first = time.Now()
		}
		last = time.Now()
		var payload struct {
			Sid string `json:"sid"`
		}
		if jerr := json.Unmarshal(data, &payload); jerr != nil || payload.Sid != hash {
			t.Fatalf("payload tidak sesi ini: %s", data)
		}
	}
	d := last.Sub(first)
	if d < 350*time.Millisecond || d > 2*time.Second {
		t.Fatalf("6 pesan (5 jeda) butuh %s, seharusnya ±500 ms (tiap 100 ms)", d)
	}
	t.Logf("5 jeda antar pesan: %d ms", d.Milliseconds())
}
