package server

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"zanime/internal/hianime"
	"zanime/internal/stream"
	"zanime/internal/subtitle"
)

const sampleMaster = `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360
360/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=4000000,RESOLUTION=1920x1080
1080/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1200000,RESOLUTION=854x480
480/index.m3u8
#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=90000,RESOLUTION=1920x1080,URI="1080/iframe.m3u8"
`

func TestStreamMasterRewritesVariants(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Referer"); got != "https://stream.example/" {
			t.Errorf("referer not forwarded: %q", got)
		}
		io.WriteString(w, sampleMaster)
	}))
	defer upstream.Close()

	master := upstream.URL + "/vip/master.m3u8"
	src := &hianime.Source{Master: master, Referer: "https://stream.example/",
		Qualities: stream.ParseMaster(sampleMaster, master)}

	req := httptest.NewRequest(http.MethodGet, "/api/hls/42/master.m3u8?mode=dub", nil)
	rec := httptest.NewRecorder()
	stream.StreamMaster(rec, req, "42", src, "dub")

	body := rec.Body.String()
	for _, want := range []string{"/api/hls/42/dub/360p/index.m3u8", "/api/hls/42/dub/1080p/index.m3u8", "/api/hls/42/dub/480p/index.m3u8"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in:\n%s", want, body)
		}
	}
	if strings.Contains(body, upstream.URL) {
		t.Errorf("upstream URL leaked to the player:\n%s", body)
	}
	if strings.Contains(body, "I-FRAME") {
		t.Errorf("I-FRAME variant should be dropped:\n%s", body)
	}
	if strings.Count(body, "#EXT-X-STREAM-INF") != 3 {
		t.Errorf("want 3 variants, got:\n%s", body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/vnd.apple.mpegurl" {
		t.Errorf("content type: %q", got)
	}
}

func TestSegmentNameGuard(t *testing.T) {
	for _, ok := range []string{"seg_00000.ts", "seg1.m4s", "a.mp4"} {
		if !reSegment.MatchString(ok) {
			t.Errorf("%q should be allowed", ok)
		}
	}
	// Anything that could walk out of the variant directory or name a host.
	for _, bad := range []string{
		"../../etc/passwd", "..%2f..%2fx.ts", "seg/00000.ts",
		"http://evil.test/x.ts", "x.ts?a=b", "seg_0.ts.m3u8", "",
	} {
		if reSegment.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestDeobfuscateBlobRoundTrip(t *testing.T) {
	json := `{"src":"https://cdn.example/h.m3u8","subtitles":[{"lang":"en","src":"https://cdn.example/en.vtt","default":true}]}`
	raw := make([]byte, len(json))
	key := []byte(hianime.EmbedXORKey)
	for i := 0; i < len(json); i++ {
		raw[i] = json[i] ^ key[i%len(key)]
	}
	blob := base64.StdEncoding.EncodeToString(raw)
	got, err := stream.DeobfuscateBlob(blob)
	if err != nil {
		t.Fatal(err)
	}
	if got != json {
		t.Fatalf("got %q want %q", got, json)
	}
	if _, err := stream.DeobfuscateBlob("not base64 !!!"); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewServerRoutes(t *testing.T) {
	h := New(subtitle.NewSubtitleCache(t.TempDir()), nil)
	if h == nil {
		t.Fatal("nil handler")
	}
}
