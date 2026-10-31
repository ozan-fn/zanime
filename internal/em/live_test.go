package em

import (
	"testing"
	"time"
)

// TestLiveEmbedmaster: pipeline Go sungguhan ke embedmaster.link memakai
// "Spider-Man: Brand New Day" (tmdb 969681), lalu memastikan SEMUA server yang
// dikembalikan bisa diputar (URL ada, tanpa Err) — server yang cuma halaman
// embed / tidak lolos verifikasi 128 KiB sudah disaring di dalam scraper.
// Butuh jaringan; lewati dengan -short karena lambat.
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
		servers, err = c.Streams(t.Context(), "movie", "969681", nil)
	}()
	select {
	case <-done:
	case <-time.After(120 * time.Second):
		t.Fatal("live scrape melebihi 120 s")
	}
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if len(servers) == 0 {
		t.Fatal("tidak ada server yang dikembalikan")
	}
	playable := 0
	for _, s := range servers {
		t.Logf("%s | %s | %s | qualities=%d subs=%d err=%q", s.Name, s.Type, s.URL, len(s.Qualities), len(s.Subs), s.Err)
		if s.URL == "" {
			continue
		}
		if s.Err != "" {
			t.Errorf("server dengan URL tapi ber-Err: %+v", s)
		}
		playable++
	}
	if playable == 0 {
		t.Fatal("tidak ada server playable (semua tersaring/ter-throttle)")
	}
}
