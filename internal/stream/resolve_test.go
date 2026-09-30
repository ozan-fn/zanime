package stream

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// The servers API is the regex from index.js, on the escaped JSON blob it
// answers: base64 hash decoded, duplicates dropped, both audio types kept.
func TestServersParsesEmbeds(t *testing.T) {
	sub := base64.StdEncoding.EncodeToString([]byte("https://zokoanime.video/stream/mal/1/1/sub"))
	dub := base64.StdEncoding.EncodeToString([]byte("https://megaplay.buzz/stream/s-2/2/dub"))
	zoko := base64.StdEncoding.EncodeToString([]byte("https://zokoanime.video/stream/mal/1/1/dub"))

	page := `{"status":200,"html":"<div class=\"item server-item\" data-type=\"sub\" data-server-name=\"ZokoAnime\" data-hash=\"` + sub + `\"></div>
	<div class=\"item server-item\" data-type=\"dub\" data-server-name=\"ZokoAnime\" data-hash=\"` + zoko + `\"></div>
	<div class=\"item server-item\" data-type=\"dub\" data-server-name=\"Vidstream-2\" data-hash=\"` + dub + `\"></div>
	<div class=\"item server-item\" data-type=\"dub\" data-server-name=\"Vidstream-2\" data-hash=\"` + dub + `\"></div>
	<div class=\"item server-item\" data-type=\"dub\" data-server-name=\"Bogus\" data-hash=\"not base64!\"></div>"}`

	// Re-wrap it the way the API answers: json-escaped html inside an envelope.
	envelope, err := json.Marshal(map[string]any{
		"status": true,
		"html":   strings.NewReplacer(`\"`, `"`, `\/`, "/").Replace(page),
	})
	if err != nil {
		t.Fatal(err)
	}

	got := Servers(string(envelope))
	if len(got) != 3 {
		t.Fatalf("want 3 embeds (dedup + bad hash dropped), got %d: %+v", len(got), got)
	}
	if got[0].URL != "https://zokoanime.video/stream/mal/1/1/sub" || got[0].Type != typeSub || !IsZoko(got[0].URL) {
		t.Errorf("first embed should be the zoko sub one: %+v", got[0])
	}
	if got[2].Name != "Vidstream-2 [dub]" || got[2].Type != "dub" {
		t.Errorf("unexpected row: %+v", got[2])
	}
}

// Try order: sub before dub, ZokoAnime before megaplay, upstream order kept.
func TestOrderedRanksSubThenZoko(t *testing.T) {
	in := []Embed{
		{Type: typeDub, URL: "https://megaplay.buzz/stream/s-2/2/dub"},
		{Type: typeDub, URL: "https://zokoanime.video/stream/mal/1/1/dub"},
		{Type: typeSub, URL: "https://megaplay.buzz/stream/s-2/2/sub"},
		{Type: typeSub, URL: "https://zokoanime.video/stream/mal/1/1/sub"},
	}
	got := ordered(in)
	for i, want := range []string{
		"https://zokoanime.video/stream/mal/1/1/sub",
		"https://megaplay.buzz/stream/s-2/2/sub",
		"https://zokoanime.video/stream/mal/1/1/dub",
		"https://megaplay.buzz/stream/s-2/2/dub",
	} {
		if got[i].URL != want {
			t.Fatalf("position %d: got %s want %s (%+v)", i, got[i].URL, want, got)
		}
	}
}

// ResolveOne on a ZokoAnime embed: blob -> config -> master + subtitles.
func TestResolveOneZoko(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stream/mal/1/1/dub":
			if got := r.Header.Get("Referer"); got != hianime.BaseAPI+"/" {
				t.Errorf("embed referer: %q", got)
			}
			cfg := `{"src":"` + srv.URL + `/master.m3u8","subtitles":[{"lang":"en","src":"` + srv.URL + `/en.vtt"}]}`
			io.WriteString(w, `<html><script>window.__P="`+obfuscate(cfg)+`"</script></html>`)
		case "/master.m3u8":
			if got := r.Header.Get("Referer"); got != srv.URL+"/" {
				t.Errorf("master referer: %q", got)
			}
			io.WriteString(w, sampleMaster)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	src, err := ResolveOne(context.Background(), srv.URL+"/stream/mal/1/1/dub")
	if err != nil {
		t.Fatal(err)
	}
	if src.Master != srv.URL+"/master.m3u8" || len(src.Qualities) != 3 {
		t.Fatalf("master/qualities: %+v", src)
	}
	if len(src.Subtitles) != 1 || src.Subtitles[0].URL != srv.URL+"/en.vtt" || !src.Subtitles[0].Default {
		t.Fatalf("subtitles: %+v", src.Subtitles)
	}
}

// ResolveOne on a megaplay embed: data-id -> getSources -> enc token.
func TestResolveOneMegaplay(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stream/s-2/2/dub":
			io.WriteString(w, `<div id="megaplay-player" data-id="12345"></div>`)
		case "/stream/getSources":
			if r.URL.Query().Get("id") != "12345" || r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				t.Errorf("bad getSources request: %s %s", r.URL.RawQuery, r.Header.Get("X-Requested-With"))
			}
			file := `{"file":"` + srv.URL + `/master.m3u8"}`
			json.NewEncoder(w).Encode(map[string]any{
				"enc": encrypt(file),
				"tracks": []map[string]string{
					{"file": srv.URL + "/en.vtt", "label": "English", "kind": "captions"},
				},
			})
		case "/master.m3u8":
			io.WriteString(w, sampleMaster)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	src, err := ResolveOne(context.Background(), srv.URL+"/stream/s-2/2/dub")
	if err != nil {
		t.Fatal(err)
	}
	if src.Master != srv.URL+"/master.m3u8" {
		t.Fatalf("master: %s", src.Master)
	}
	if len(src.Subtitles) != 1 || !src.Subtitles[0].Default || src.Subtitles[0].Lang != "English" {
		t.Fatalf("subtitles: %+v", src.Subtitles)
	}
}

// A megaplay embed whose getSources answers plain sources instead of a token is
// still resolved (index.js falls back the same way).
func TestMasterFromPlainSources(t *testing.T) {
	got, err := masterFrom(&megaSources{Sources: json.RawMessage(`[{"file":"https://cdn.example/a.m3u8"}]`)})
	if err != nil || got != "https://cdn.example/a.m3u8" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := masterFrom(&megaSources{}); err == nil {
		t.Fatal("expected error without any playlist")
	}
}

// The default track is chosen in two steps: the best language available
// (Indonesian, else English), and among those the densest one — so neither the
// sparse signs/songs track nor a Chinese one that merely has more cues can take it.
func TestPickDialoguePrefersLanguageThenDensity(t *testing.T) {
	cues := func(n int) string {
		var b strings.Builder
		b.WriteString("WEBVTT\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "\n00:00:%02d.000 --> 00:00:%02d.500\nBaris %d\n", i%60, i%60, i)
		}
		return b.String()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/signs.vtt":
			io.WriteString(w, cues(8))
		case "/chi.vtt":
			io.WriteString(w, cues(600))
		case "/thinner.vtt":
			io.WriteString(w, cues(250))
		default:
			io.WriteString(w, cues(300))
		}
	}))
	defer srv.Close()

	src := &hianime.Source{Subtitles: []hianime.Subtitle{
		{Lang: "Chinese (Chinese - (Simplified))", URL: srv.URL + "/chi.vtt"},
		{Lang: "English", URL: srv.URL + "/signs.vtt"},
		{Lang: "English 2", URL: srv.URL + "/dialogue.vtt"},
	}}
	pickDialogue(context.Background(), src, srv.URL+"/")
	if src.Subtitles[0].Default || src.Subtitles[1].Default || !src.Subtitles[2].Default {
		t.Fatalf("English dialogue should win over a denser Chinese track: %+v", src.Subtitles)
	}
	if src.DialogueCues != 300 {
		t.Fatalf("cue count not recorded: %d", src.DialogueCues)
	}

	// Indonesian beats English; both are Indonesian, so the denser cue count
	// decides.
	src = &hianime.Source{Subtitles: []hianime.Subtitle{
		{Lang: "English", URL: srv.URL + "/dialogue.vtt"},
		{Lang: "Indonesian", URL: srv.URL + "/thinner.vtt"},
		{Lang: "Indonesian 2", URL: srv.URL + "/id.vtt"},
	}}
	// 250 vs 300 cues: the second Indonesian track is the denser one.
	pickDialogue(context.Background(), src, srv.URL+"/")
	if src.Subtitles[0].Default || src.Subtitles[1].Default || !src.Subtitles[2].Default {
		t.Fatalf("Indonesian should win over English: %+v", src.Subtitles)
	}

	// Nothing fetchable: the first track keeps the default, as index.js would.
	broken := &hianime.Source{Subtitles: []hianime.Subtitle{{URL: srv.URL + "/missing.vtt"}, {URL: srv.URL + "/gone.vtt"}}}
	pickDialogue(context.Background(), broken, srv.URL+"/")
	if !broken.Subtitles[0].Default || broken.Subtitles[1].Default {
		t.Fatalf("fallback default: %+v", broken.Subtitles)
	}
}

// Invalidate must drop a cached episode, so a rejected token is re-resolved
// instead of being served until the TTL runs out.
func TestInvalidateDropsEntry(t *testing.T) {
	resolvedCache.Store("7", resolvedEntry{src: &hianime.Source{}, expires: time.Now().Add(time.Hour)})
	if _, ok := resolvedCache.Load("7"); !ok {
		t.Fatal("setup failed")
	}
	Invalidate("7")
	if _, ok := resolvedCache.Load("7"); ok {
		t.Fatal("entry survived Invalidate")
	}
}

// obfuscate is the inverse of DeobfuscateBlob, as the zoko embed writes it.
func obfuscate(s string) string {
	key := []byte(hianime.EmbedXORKey)
	raw := make([]byte, len(s))
	for i := range s {
		raw[i] = s[i] ^ key[i%len(key)]
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// encrypt is the inverse of DecryptEnc.
func encrypt(s string) string {
	block, _ := aes.NewCipher(megaKey)
	plain := pkcs7([]byte(s))
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, megaIV).CryptBlocks(out, plain)
	return base64.RawURLEncoding.EncodeToString(out)
}

func pkcs7(b []byte) []byte {
	n := aes.BlockSize - len(b)%aes.BlockSize
	return append(b, bytes.Repeat([]byte{byte(n)}, n)...)
}
