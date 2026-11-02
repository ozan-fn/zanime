package translate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestTranslateVTTPreservesTimingAndCaches: tiap cue diterjemahkan, timestamp
// dan struktur VTT dipertahankan, dan permintaan kedua dilayani cache.
func TestTranslateVTTPreservesTimingAndCaches(t *testing.T) {
	t.Chdir(t.TempDir())
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		q := r.URL.Query().Get("q")
		_ = json.NewEncoder(w).Encode([]any{[]any{[]any{"[id] " + q}}})
	}))
	defer up.Close()

	c := New()
	c.Base = up.URL
	vtt := "WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.000\nHello\nworld\n\n2\n00:00:03.000 --> 00:00:04.000\nBye\n"

	res, err := c.TranslateVTT(context.Background(), vtt, "en", "id")
	if err != nil {
		t.Fatalf("TranslateVTT: %v", err)
	}
	if res.Cached || res.Cues != 2 {
		t.Fatalf("hasil = %+v, want 2 cue bukan cache", res)
	}
	for _, want := range []string{
		"WEBVTT",
		"00:00:01.000 --> 00:00:02.000",
		"[id] Hello\nworld",
		"00:00:03.000 --> 00:00:04.000",
		"[id] Bye",
	} {
		if !strings.Contains(res.VTT, want) {
			t.Fatalf("VTT tanpa %q:\n%s", want, res.VTT)
		}
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("permintaan terjemahan = %d, want 2", got)
	}

	again, err := c.TranslateVTT(context.Background(), vtt, "en", "id")
	if err != nil {
		t.Fatalf("TranslateVTT (cache): %v", err)
	}
	if !again.Cached || again.VTT != res.VTT {
		t.Fatalf("cache tidak dipakai: %+v", again)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("cache tetap menghubungi upstream: %d permintaan", got)
	}
	if cached, ok := Cached(res.Key); !ok || cached != res.VTT {
		t.Fatal("Cached tidak mengembalikan hasil tersimpan")
	}
}

// TestCachedRejectsBadKey: kunci yang bukan hex 40 karakter tidak dibaca dari
// disk (mencegah path traversal).
func TestCachedRejectsBadKey(t *testing.T) {
	for _, key := range []string{"", "../rahasia", "zzzz", strings.Repeat("a", 41)} {
		if _, ok := Cached(key); ok {
			t.Fatalf("kunci %q seharusnya ditolak", key)
		}
	}
}

// TestParseSingleJoinsSegments: jawaban Google bisa berisi beberapa segmen;
// semuanya digabung.
func TestParseSingleJoinsSegments(t *testing.T) {
	body := []byte(`[[["Halo ","Hello ","",null],["dunia","world",null,null]],null,"en"]`)
	got, err := parseSingle(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Halo dunia" {
		t.Fatalf("parseSingle = %q", got)
	}
	if _, err := parseSingle([]byte(`[]`)); err == nil {
		t.Fatal("jawaban kosong harus error")
	}
}
