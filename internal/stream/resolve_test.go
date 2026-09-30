package stream

import (
	"encoding/base64"
	"strings"
	"testing"

	"zanime/internal/hianime"
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

func TestParseMasterSortsAndResolves(t *testing.T) {
	qs := ParseMaster(sampleMaster, "https://cdn.example/hianime/vip/master.m3u8")
	if len(qs) != 3 {
		t.Fatalf("want 3 variants (I-FRAME excluded), got %d: %+v", len(qs), qs)
	}
	if qs[0].Label != "1080p" || qs[2].Label != "360p" {
		t.Fatalf("not sorted desc by height: %+v", qs)
	}
	if qs[0].URL != "https://cdn.example/hianime/vip/1080/index.m3u8" {
		t.Fatalf("relative URL not resolved against master dir: %s", qs[0].URL)
	}
}

func masterSrc() *hianime.Source {
	return &hianime.Source{Qualities: ParseMaster(sampleMaster, "https://cdn.example/vip/master.m3u8")}
}

func emptySrc() *hianime.Source { return &hianime.Source{} }

func TestPickQuality(t *testing.T) {
	src := masterSrc()
	cases := map[string]string{
		"":      "1080p",
		"best":  "1080p",
		"worst": "360p",
		"480p":  "480p",
		"480":   "480p",
		"nope":  "1080p", // unknown falls back to best
	}
	for in, want := range cases {
		got, err := PickQuality(src, in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if !strings.HasSuffix(got, "/"+strings.TrimSuffix(want, "p")+"/index.m3u8") {
			t.Errorf("quality %q: got %s want %s", in, got, want)
		}
	}
	if _, err := PickQuality(emptySrc(), "best"); err == nil {
		t.Error("expected error with no variants")
	}
}

func TestServerHashPicksMode(t *testing.T) {
	subHash := base64.StdEncoding.EncodeToString([]byte("https://megaplay.buzz/stream/s-2/1/sub"))
	dubHash := base64.StdEncoding.EncodeToString([]byte("https://megaplay.buzz/stream/s-2/2/dub"))
	zokoHash := base64.StdEncoding.EncodeToString([]byte("https://zokoanime.video/stream/mal/1/1/sub"))
	page := `<div class="item server-item" data-type="sub" data-server-name="ZokoAnime" data-hash="` + zokoHash + `">
	<a class="btn">ZokoAnime</a></div>
	<div class="item server-item" data-type="sub" data-server-name="HD-2" data-hash="` + subHash + `">
	<a class="btn">HD-2</a></div>
	<div class="item server-item" data-type="dub" data-server-name="Vidstream-2" data-hash="` + dubHash + `">
	<a class="btn">Vidstream-2</a></div>`

	if got := ServerHashes(page, "sub"); len(got) != 1 || got[0] != subHash {
		t.Errorf("sub: want only the megaplay hash, got %q", got)
	}
	if got := ServerHashes(page, "dub"); len(got) != 1 || got[0] != dubHash {
		t.Errorf("dub must not fall back to the sub embed, got %q", got)
	}
	if got := ServerHashes(`<div class="item server-item" data-type="dub" data-server-name="HD-1" data-hash="eA==">`, "sub"); len(got) != 0 {
		t.Errorf("no sub server should yield nothing, got %q", got)
	}
}
