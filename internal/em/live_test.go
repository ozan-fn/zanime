package em

import (
	"testing"
	"time"
)

// TestLiveEmbedmaster: pipeline Go sungguhan ke embedmaster.link. Butuh
// jaringan; lewati dengan -short karena lambat (~beberapa detik per server).
func TestLiveEmbedmaster(t *testing.T) {
	if testing.Short() {
		t.Skip("live scrape dilewati di -short")
	}
	t.Chdir(t.TempDir())

	c := New()
	done := make(chan struct{})
	var (
		servers []Server
		err     error
	)
	go func() {
		defer close(done)
		servers, err = c.Streams(t.Context(), "movie", "557", nil)
	}()
	select {
	case <-done:
	case <-time.After(90 * time.Second):
		t.Fatal("live scrape melebihi 90 s")
	}
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	playable := 0
	for _, s := range servers {
		if s.URL != "" {
			playable++
		}
		t.Logf("%s | %s | %s | qualities=%d subs=%d err=%q", s.Name, s.Type, s.URL, len(s.Qualities), len(s.Subs), s.Err)
	}
	if playable == 0 {
		t.Fatalf("tidak ada server playable dari %d server", len(servers))
	}
}
