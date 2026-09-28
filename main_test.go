package main

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDeobfuscateBlobRoundTrip(t *testing.T) {
	json := `{"src":"https://cdn.example/h.m3u8","subtitles":[{"lang":"en","src":"https://cdn.example/en.vtt","default":true}]}`
	raw := make([]byte, len(json))
	key := []byte(embedXORKey)
	for i := 0; i < len(json); i++ {
		raw[i] = json[i] ^ key[i%len(key)]
	}
	blob := base64.StdEncoding.EncodeToString(raw)
	got, err := deobfuscateBlob(blob)
	if err != nil {
		t.Fatal(err)
	}
	if got != json {
		t.Fatalf("got %q want %q", got, json)
	}
}

func TestDeobfuscateBlobRejectsGarbage(t *testing.T) {
	if _, err := deobfuscateBlob("not base64 !!!"); err == nil {
		t.Fatal("expected error")
	}
}

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
	qs := parseMaster(sampleMaster, "https://cdn.example/hianime/vip/master.m3u8")
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

func TestPickQuality(t *testing.T) {
	src := &Source{Qualities: parseMaster(sampleMaster, "https://cdn.example/vip/master.m3u8")}
	cases := map[string]string{
		"":      "1080p",
		"best":  "1080p",
		"worst": "360p",
		"480p":  "480p",
		"480":   "480p",
		"nope":  "1080p", // unknown falls back to best
	}
	for in, want := range cases {
		got, err := pickQuality(src, in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if !strings.HasSuffix(got, "/"+strings.TrimSuffix(want, "p")+"/index.m3u8") {
			t.Errorf("quality %q: got %s want %s", in, got, want)
		}
	}
	if _, err := pickQuality(&Source{}, "best"); err == nil {
		t.Error("expected error with no variants")
	}
}

func TestZokoHashPicksMode(t *testing.T) {
	page := `<div class="ps__-list">
	<div class="item server-item" data-type="sub"
		data-server-name="ZokoAnime" data-hash="c3ViLWhhc2g=">
		<a href="javascript:;" class="btn">ZokoAnime</a></div>
	<div class="item server-item" data-type="sub"
		data-server-name="HD-1" data-hash="aGQx">
		<a href="javascript:;" class="btn">HD-1</a></div>
	<div class="item server-item" data-type="dub"
		data-server-name="ZokoAnime" data-hash="ZHViLWhhc2g=">
		<a href="javascript:;" class="btn">ZokoAnime</a></div>
	<div class="item server-item" data-type="dub"
		data-server-name="HD-1" data-hash="aGQy">
		<a href="javascript:;" class="btn">HD-1</a></div>`

	if got := zokoHash(page, "sub"); got != "c3ViLWhhc2g=" {
		t.Errorf("sub: got %q", got)
	}
	if got := zokoHash(page, "dub"); got != "ZHViLWhhc2g=" {
		t.Errorf("dub must not fall back to the sub embed, got %q", got)
	}
	if got := zokoHash(`<div class="item server-item" data-type="sub" data-server-name="HD-1" data-hash="eA==">`, "sub"); got != "" {
		t.Errorf("no ZokoAnime server should yield nothing, got %q", got)
	}
}

func TestStreamMasterRewritesVariants(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Referer"); got != "https://stream.example/" {
			t.Errorf("referer not forwarded: %q", got)
		}
		io.WriteString(w, sampleMaster)
	}))
	defer upstream.Close()

	master := upstream.URL + "/vip/master.m3u8"
	src := &Source{Master: master, Referer: "https://stream.example/",
		Qualities: parseMaster(sampleMaster, master)}

	req := httptest.NewRequest(http.MethodGet, "/hls/42/master.m3u8?mode=dub", nil)
	rec := httptest.NewRecorder()
	streamMaster(rec, req, "42", src, "dub")

	body := rec.Body.String()
	for _, want := range []string{"/hls/42/dub/360p/index.m3u8", "/hls/42/dub/1080p/index.m3u8", "/hls/42/dub/480p/index.m3u8"} {
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

func TestSearchParsing(t *testing.T) {
	page := `<div class="flw-item "><div class="film-poster">
		<img src="https://cdn.example/p1.jpg" class="film-poster-img" alt="a"></div>
		<div class="film-detail"><h3 class="film-name">
		<a href="/watch/naruto-shippuden-xyz" title="Naruto: Shipp&#039;uden &amp; more"></a></h3>
		<div class="description">Space ninja story.</div></div></div>
	<div class="flw-item "><div class="film-detail"><h3 class="film-name">
		<a href="/watch/one-piece" title="One Piece"></a></h3></div></div>
	<div id="main-sidebar"><h3 class="film-name">
		<a href="/watch/sidebar-noise" title="Sidebar Noise"></a></h3></div>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("keyword"); got != "naruto piece" {
			t.Errorf("keyword not escaped: %q", got)
		}
		io.WriteString(w, page)
	}))
	defer srv.Close()

	old := searchAPI
	searchAPI = srv.URL + "/search?keyword=%s"
	defer func() { searchAPI = old }()

	got, err := search(context.Background(), "naruto piece")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 results (sidebar cut off), got %d: %+v", len(got), got)
	}
	if got[0].ID != "naruto-shippuden-xyz" || got[0].Name != "Naruto: Shipp'uden & more" {
		t.Fatalf("bad first result: %+v", got[0])
	}
	if got[0].Poster != "https://cdn.example/p1.jpg" {
		t.Errorf("poster not scraped: %+v", got[0])
	}
	if got[0].Synopsis == "" {
		t.Errorf("synopsis not scraped: %+v", got[0])
	}
}

func TestEpisodesParsing(t *testing.T) {
	var gotPath string
	page := `{\"ep-item\" data-number=\"2\" data-id=\"20\" href=\"/watch/slug-xyz?ep=20\"},\
{\"ep-item\" data-number=\"1\" data-id=\"10\" href=\"/watch/slug-xyz?ep=10\"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.WriteString(w, page)
	}))
	defer srv.Close()

	old := episodesAPI
	episodesAPI = srv.URL + "/api/theme/episode/list/%s"
	defer func() { episodesAPI = old }()

	got, err := episodes(context.Background(), "slug-xyz")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/theme/episode/list/xyz" {
		t.Fatalf("list route wants the trailing id, not the slug: %s", gotPath)
	}
	if len(got) != 2 || got[0].Number != "1" || got[1].Number != "2" {
		t.Fatalf("episodes not sorted by number: %+v", got)
	}
}

func TestGetDetectsCloudflare(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<title>Just a moment...</title>")
	}))
	defer srv.Close()
	if _, err := get(context.Background(), srv.URL, ""); err != errCloudflare {
		t.Fatalf("want errCloudflare, got %v", err)
	}
}

func TestVTTRoundTripKeepsTiming(t *testing.T) {
	vtt := "WEBVTT\nKind: captions\nLanguage: en\n\n1\n00:00:01.000 --> 00:00:03.000\nHello there\n\n2\n00:00:04.000 --> 00:00:06.000\nGeneral Kenobi\n"
	header, blocks := parseVTT(vtt)
	if len(blocks) != 2 {
		t.Fatalf("want 2 cue blocks, got %d", len(blocks))
	}
	if !strings.Contains(header, "Language: en") {
		t.Errorf("header lost metadata: %q", header)
	}
	blocks[0] = setCueText(blocks[0], "Halo")
	if got := cueText(blocks[0]); got != "Halo" {
		t.Errorf("setCueText/cueText mismatch: %q", got)
	}
	out := rebuildVTT(header, blocks)
	for _, want := range []string{"00:00:01.000 --> 00:00:03.000", "Halo", "00:00:04.000 --> 00:00:06.000", "General Kenobi"} {
		if !strings.Contains(out, want) {
			t.Errorf("rebuild lost %q:\n%s", want, out)
		}
	}

	// Cue blocks with no id line put the timing on line 0; the same code must
	// not mistake the timestamp for subtitle text.
	bare := []string{"00:00:01.000 --> 00:00:03.000", "Hi"}
	if got := cueText(bare); got != "Hi" {
		t.Errorf("bare cue: got %q want %q", got, "Hi")
	}
	out2 := strings.Join(setCueText(bare, "Halo"), "\n")
	if out2 != "00:00:01.000 --> 00:00:03.000\nHalo" {
		t.Errorf("bare cue rewrite: %q", out2)
	}

	// Multi-line payloads must survive as one translated line.
	multi := []string{"00:00:01.000 --> 00:00:03.000", "line one", "line two"}
	if got := cueText(multi); got != "line one line two" {
		t.Errorf("multi-line cue: %q", got)
	}
}

func TestSubJobStates(t *testing.T) {
	job := &subJob{running: true}
	if state, _, _, _ := job.snapshot(); state != "converting" {
		t.Errorf("want converting, got %q", state)
	}
	// Each report carries the run's running total, not a delta: a split batch
	// added on top of the previous one drove the bar past its end.
	job.setProgress(4, 10)
	job.setProgress(6, 10)
	if state, done, total, _ := job.snapshot(); state != "converting" || done != 6 || total != 10 {
		t.Errorf("progress not reported: %q %d/%d", state, done, total)
	}
	// 4 cues in 8s projects the remaining 6 at another 12s.
	if got := etaSeconds(4, 10, 8*time.Second); got != 12 {
		t.Errorf("want 12s by the observed rate, got %d", got)
	}
	if got := etaSeconds(10, 10, time.Second); got != 0 {
		t.Errorf("eta should be zero when finished, got %d", got)
	}
	// Nothing has come back yet, so there is no rate to project from.
	if got := etaSeconds(0, 10, time.Minute); got != 0 {
		t.Errorf("eta should wait for the first batch, got %d", got)
	}
	job.finish(nil)
	if state, _, _, _ := job.snapshot(); state != "ready" {
		t.Errorf("want ready after finish(nil), got %q", state)
	}

	broken := &subJob{running: true}
	broken.finish(errors.New("boom"))
	if state, _, _, err := broken.snapshot(); state != "error" || err == nil {
		t.Errorf("want error state carrying its cause, got %q err=%v", state, err)
	}
}

func TestTranslateVTTReportsProgressAndRetriesDroppedCue(t *testing.T) {
	vtt := "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\none\n\n00:00:03.000 --> 00:00:04.000\ntwo\n\n00:00:05.000 --> 00:00:06.000\nthree\n"

	dropped := false
	old := batchFn
	defer func() { batchFn = old }()
	batchFn = func(_ context.Context, lines []string) (map[int]string, error) {
		out := map[int]string{}
		for _, line := range lines {
			bar := strings.Index(line, "|")
			if bar < 1 {
				continue
			}
			// Play a flaky model: it forgets the first line exactly once.
			if line[:bar] == "1" && !dropped {
				dropped = true
				continue
			}
			idx, err := strconv.Atoi(line[:bar])
			if err != nil {
				continue
			}
			out[idx] = "ID:" + line[bar+1:]
		}
		return out, nil
	}

	var progress, totals []int
	got, err := translateVTT(context.Background(), vtt, func(done, total int) {
		progress = append(progress, done)
		totals = append(totals, total)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !dropped {
		t.Fatal("the stub never dropped a cue, so the retry path went untested")
	}
	for _, want := range []string{"ID:one", "ID:two", "ID:three"} {
		if !strings.Contains(got, want) {
			t.Errorf("cue %q missing after the retry pass:\n%s", want, got)
		}
	}
	if len(progress) == 0 || progress[len(progress)-1] != 3 {
		t.Errorf("progress should end at 3 cues, got %v", progress)
	}
	if len(totals) == 0 || totals[0] != 3 || totals[len(totals)-1] != 3 {
		t.Errorf("total should be known up front and stay at 3, got %v", totals)
	}
}

func TestTranslateVTTDeduplicatesIdenticalCues(t *testing.T) {
	vtt := "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nYes\n\n00:00:03.000 --> 00:00:04.000\nWhat?!\n\n00:00:05.000 --> 00:00:06.000\nYes\n"

	var sent []string
	old := batchFn
	defer func() { batchFn = old }()
	batchFn = func(_ context.Context, lines []string) (map[int]string, error) {
		out := map[int]string{}
		for _, line := range lines {
			bar := strings.Index(line, "|")
			sent = append(sent, line[bar+1:])
			idx, _ := strconv.Atoi(line[:bar])
			out[idx] = "ID:" + line[bar+1:]
		}
		return out, nil
	}

	var done, total int
	got, err := translateVTT(context.Background(), vtt, func(d, t int) { done, total = d, t })
	if err != nil {
		t.Fatal(err)
	}
	// "Yes" appears twice but must reach the model once.
	if len(sent) != 2 {
		t.Errorf("want 2 unique lines sent, got %v", sent)
	}
	// Progress is counted in the lines the model was asked for, so the duplicate
	// cannot leave the bar short of the total it was given.
	if done != 2 || total != 2 {
		t.Errorf("progress should end at 2/2 distinct lines, got %d/%d", done, total)
	}
	if !strings.Contains(got, "ID:Yes") || strings.Count(got, "ID:Yes") != 2 {
		t.Errorf("both Yes cues should carry the translation:\n%s", got)
	}
	if !strings.Contains(got, "ID:What?!") {
		t.Errorf("unique cue lost:\n%s", got)
	}
}

func TestPickSubtitle(t *testing.T) {
	subs := []Subtitle{{Lang: "en", Default: false}, {Lang: "es", Default: true}}
	if got := pickSubtitle(subs, "en"); got.Lang != "en" {
		t.Errorf("explicit lang ignored: %+v", got)
	}
	if got := pickSubtitle(subs, "de"); got.Lang != "es" {
		t.Errorf("want default fallback, got %+v", got)
	}
	if pickSubtitle(nil, "en") != nil {
		t.Error("want nil for no subtitles")
	}
}

func TestLimiterEnforcesPerMinuteCap(t *testing.T) {
	l := newLimiter()
	for i := 0; i < limitRequestsPerMinute; i++ {
		if _, ok := l.take(); !ok {
			t.Fatalf("call %d rejected early", i+1)
		}
	}
	if wait, ok := l.take(); ok || wait <= 0 {
		t.Errorf("per-minute cap not enforced (ok=%v wait=%v)", ok, wait)
	}

	// A fresh window resets the count.
	l.minStart = time.Now().Add(-time.Minute - time.Second)
	if _, ok := l.take(); !ok {
		t.Error("budget did not reset after the minute window")
	}
}

func TestRateLimitsSplitButRejectionsDoNot(t *testing.T) {
	if err := (apiError{Code: "free_quota_rpm", Message: "slow down"}); !errors.Is(err, errRetryable) {
		t.Errorf("free_quota_rpm should be retryable, got %v", err)
	}
	if err := (apiError{Code: "all_providers_failed", Message: "upstream unavailable"}); !errors.Is(err, errRetryable) {
		t.Errorf("a 503 across the whole route is worth a retry, got %v", err)
	}
	// kenari answers 429 for upstream rejections too; retrying one would burn
	// the whole budget splitting a request that can never pass.
	if err := (apiError{Code: "upstream_rejected", Message: "bad field"}); errors.Is(err, errRetryable) {
		t.Errorf("upstream rejection must not be treated as retryable, got %v", err)
	}

	// A daily window is hours away, so one call has to be enough to surface it
	// instead of recursing the batch down to single lines.
	calls := 0
	old := batchFn
	defer func() { batchFn = old }()
	batchFn = func(context.Context, []string) (map[int]string, error) {
		calls++
		return nil, apiError{Code: "free_quota_daily", Message: "resets tomorrow", retry: 6 * time.Hour}
	}
	if _, err := translateSplit(context.Background(), []string{"1|a", "2|b"}); !errors.Is(err, errRetryable) {
		t.Fatalf("want the rate limit surfaced, got %v", err)
	}
	if calls != 1 {
		t.Errorf("want the daily limit reported without splitting, got %d calls", calls)
	}
}

func TestTruncatedAnswerSplitsUntilItFits(t *testing.T) {
	// Play a reasoning model that answers only the first two lines of whatever
	// it is handed, the way a thinking model that burns the reply budget does.
	old := batchFn
	defer func() { batchFn = old }()
	batchFn = func(_ context.Context, lines []string) (map[int]string, error) {
		if len(lines) > 2 {
			return nil, errTruncated
		}
		out := map[int]string{}
		for _, line := range lines {
			bar := strings.Index(line, "|")
			if bar < 1 {
				continue
			}
			idx, err := strconv.Atoi(line[:bar])
			if err != nil {
				continue
			}
			out[idx] = "ID:" + line[bar+1:]
		}
		return out, nil
	}

	lines := []string{"1|one", "2|two", "3|three", "4|four"}
	got, err := translateSplit(context.Background(), lines)
	if err != nil {
		t.Fatalf("truncation should split, not fail: %v", err)
	}
	for i := 1; i <= len(lines); i++ {
		if got[i] == "" {
			t.Errorf("cue %d lost to the split", i)
		}
	}
}

func TestBatchEndPacksByTokenBudget(t *testing.T) {
	cue := func(text string) []string {
		return []string{"00:00:01.000 --> 00:00:02.000", text}
	}
	blocks := [][]string{cue("short"), cue(strings.Repeat("x", 4*batchTokens)), cue("tail")}
	idx := []int{0, 1, 2}

	if end := batchEnd(blocks, idx, 0); end != 1 {
		t.Errorf("the budget should stop the batch before the oversized cue, got end=%d", end)
	}
	if end := batchEnd(blocks, idx, 1); end != 2 {
		t.Errorf("the oversized cue should not drag the next one in, got end=%d", end)
	}
	if end := batchEnd(blocks, idx, 2); end != 3 {
		t.Errorf("the last cue should be taken, got end=%d", end)
	}
}

func TestSubtitleCache(t *testing.T) {
	c := newSubtitleCache(t.TempDir())
	if _, ok := c.get("k"); ok {
		t.Error("cold cache hit")
	}
	c.put("k", "WEBVTT\n\n1\n00:00:00.000 --> 00:00:01.000\nHalo\n")
	if got, ok := c.get("k"); !ok || !strings.Contains(got, "Halo") {
		t.Fatalf("memory cache miss: %q", got)
	}
	fresh := newSubtitleCache(c.dir)
	if got, ok := fresh.get("k"); !ok || !strings.Contains(got, "Halo") {
		t.Fatalf("disk cache miss: %q", got)
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
