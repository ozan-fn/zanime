package proxy

import (
	"net/url"
	"strings"
	"testing"
)

func TestRewritePlaylistRoutesRelativeAndKeyURIs(t *testing.T) {
	base, err := url.Parse("https://cdn.example.com/hls/index.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"#EXTM3U",
		`#EXT-X-KEY:METHOD=AES-128,URI="key.bin"`,
		`#EXT-X-MAP:URI="https://cdn.example.com/hls/init.mp4"`,
		"seg1.ts",
		"https://other.example.com/seg2.ts",
	}, "\n")

	got := RewritePlaylist(body, base, "https://miruro.cx/", "https://ultracloud.cc")
	want := []string{
		"#EXTM3U",
		`#EXT-X-KEY:METHOD=AES-128,URI="/stream?origin=https%3A%2F%2Fultracloud.cc` +
			`&referer=https%3A%2F%2Fmiruro.cx%2F` +
			`&url=https%3A%2F%2Fcdn.example.com%2Fhls%2Fkey.bin"`,
		`#EXT-X-MAP:URI="/stream?origin=https%3A%2F%2Fultracloud.cc` +
			`&referer=https%3A%2F%2Fmiruro.cx%2F` +
			`&url=https%3A%2F%2Fcdn.example.com%2Fhls%2Finit.mp4"`,
		"/stream?origin=https%3A%2F%2Fultracloud.cc" +
			"&referer=https%3A%2F%2Fmiruro.cx%2F" +
			"&url=https%3A%2F%2Fcdn.example.com%2Fhls%2Fseg1.ts",
		"/stream?origin=https%3A%2F%2Fultracloud.cc" +
			"&referer=https%3A%2F%2Fmiruro.cx%2F" +
			"&url=https%3A%2F%2Fother.example.com%2Fseg2.ts",
	}
	if got != strings.Join(want, "\n") {
		t.Fatalf("unexpected playlist:\n%s", got)
	}
}

func TestIsPlaylist(t *testing.T) {
	if !IsPlaylist("application/vnd.apple.mpegurl", "/x") {
		t.Fatal("expected mpegurl playlist")
	}
	if !IsPlaylist("", "/path/index.M3U8") {
		t.Fatal("expected m3u8 suffix playlist")
	}
	if IsPlaylist("video/mp4", "/movie.mp4") {
		t.Fatal("mp4 is not a playlist")
	}
}
