package subtitle

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"zanime/internal/hianime"
)

func TestVTTRoundTripKeepsTiming(t *testing.T) {
	vtt := "WEBVTT\nKind: captions\nLanguage: en\n\n1\n00:00:01.000 --> 00:00:03.000\nHello there\n\n2\n00:00:04.000 --> 00:00:06.000\nGeneral Kenobi\n"
	header, blocks := ParseVTT(vtt)
	if len(blocks) != 2 {
		t.Fatalf("want 2 cue blocks, got %d", len(blocks))
	}
	if !strings.Contains(header, "Language: en") {
		t.Errorf("header lost metadata: %q", header)
	}
	blocks[0] = SetCueText(blocks[0], "Halo")
	if got := CueText(blocks[0]); got != "Halo" {
		t.Errorf("SetCueText/CueText mismatch: %q", got)
	}
	out := RebuildVTT(header, blocks)
	for _, want := range []string{"00:00:01.000 --> 00:00:03.000", "Halo", "00:00:04.000 --> 00:00:06.000", "General Kenobi"} {
		if !strings.Contains(out, want) {
			t.Errorf("rebuild lost %q:\n%s", want, out)
		}
	}

	// Cue blocks with no id line put the timing on line 0; the same code must
	// not mistake the timestamp for subtitle text.
	bare := []string{"00:00:01.000 --> 00:00:03.000", "Hi"}
	if got := CueText(bare); got != "Hi" {
		t.Errorf("bare cue: got %q want %q", got, "Hi")
	}
	out2 := strings.Join(SetCueText(bare, "Halo"), "\n")
	if out2 != "00:00:01.000 --> 00:00:03.000\nHalo" {
		t.Errorf("bare cue rewrite: %q", out2)
	}

	// Multi-line payloads must survive as one translated line.
	multi := []string{"00:00:01.000 --> 00:00:03.000", "line one", "line two"}
	if got := CueText(multi); got != "line one line two" {
		t.Errorf("multi-line cue: %q", got)
	}
}

func TestSubJobStates(t *testing.T) {
	job := &SubJob{running: true}
	if state, _, _, _ := job.Snapshot(); state != "converting" {
		t.Errorf("want converting, got %q", state)
	}
	// Each report carries the run's running total, not a delta: a split batch
	// added on top of the previous one drove the bar past its end.
	job.SetProgress(4, 10)
	job.SetProgress(6, 10)
	if state, done, total, _ := job.Snapshot(); state != "converting" || done != 6 || total != 10 {
		t.Errorf("progress not reported: %q %d/%d", state, done, total)
	}
	// 4 cues in 8s projects the remaining 6 at another 12s.
	if got := EtaSeconds(4, 10, 8*time.Second); got != 12 {
		t.Errorf("want 12s by the observed rate, got %d", got)
	}
	if got := EtaSeconds(10, 10, time.Second); got != 0 {
		t.Errorf("eta should be zero when finished, got %d", got)
	}
	// Nothing has come back yet, so there is no rate to project from.
	if got := EtaSeconds(0, 10, time.Minute); got != 0 {
		t.Errorf("eta should wait for the first batch, got %d", got)
	}
	job.Finish(nil)
	if state, _, _, _ := job.Snapshot(); state != "ready" {
		t.Errorf("want ready after Finish(nil), got %q", state)
	}

	broken := &SubJob{running: true}
	broken.Finish(errors.New("boom"))
	if state, _, _, err := broken.Snapshot(); state != "error" || err == nil {
		t.Errorf("want error state carrying its cause, got %q err=%v", state, err)
	}
}

func stubBatch(t *testing.T, fn func(ctx context.Context, lines []string) (map[int]string, error)) {
	t.Helper()
	old := BatchFn
	BatchFn = fn
	t.Cleanup(func() { BatchFn = old })
}

func TestTranslateVTTReportsProgressAndRetriesDroppedCue(t *testing.T) {
	vtt := "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\none\n\n00:00:03.000 --> 00:00:04.000\ntwo\n\n00:00:05.000 --> 00:00:06.000\nthree\n"

	dropped := false
	stubBatch(t, func(_ context.Context, lines []string) (map[int]string, error) {
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
	})

	var progress, totals []int
	got, err := TranslateVTT(context.Background(), vtt, func(done, total int) {
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
	stubBatch(t, func(_ context.Context, lines []string) (map[int]string, error) {
		out := map[int]string{}
		for _, line := range lines {
			bar := strings.Index(line, "|")
			sent = append(sent, line[bar+1:])
			idx, _ := strconv.Atoi(line[:bar])
			out[idx] = "ID:" + line[bar+1:]
		}
		return out, nil
	})

	var done, total int
	got, err := TranslateVTT(context.Background(), vtt, func(d, t int) { done, total = d, t })
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
	subs := []hianime.Subtitle{{Lang: "en", Default: false}, {Lang: "es", Default: true}}
	if got := PickSubtitle(subs, "en"); got.Lang != "en" {
		t.Errorf("explicit lang ignored: %+v", got)
	}
	if got := PickSubtitle(subs, "de"); got.Lang != "es" {
		t.Errorf("want default fallback, got %+v", got)
	}
	if PickSubtitle(nil, "en") != nil {
		t.Error("want nil for no subtitles")
	}
}

func TestLimiterEnforcesPerMinuteCap(t *testing.T) {
	l := NewLimiter()
	for i := 0; i < LimitRequestsPerMinute; i++ {
		if _, ok := l.Take(); !ok {
			t.Fatalf("call %d rejected early", i+1)
		}
	}
	if wait, ok := l.Take(); ok || wait <= 0 {
		t.Errorf("per-minute cap not enforced (ok=%v wait=%v)", ok, wait)
	}

	// A fresh window resets the count.
	l.minStart = time.Now().Add(-time.Minute - time.Second)
	if _, ok := l.Take(); !ok {
		t.Error("budget did not reset after the minute window")
	}
}

func TestRateLimitsSplitButRejectionsDoNot(t *testing.T) {
	if err := (APIError{Code: "free_quota_rpm", Message: "slow down"}); !errors.Is(err, ErrRetryable) {
		t.Errorf("free_quota_rpm should be retryable, got %v", err)
	}
	if err := (APIError{Code: "all_providers_failed", Message: "upstream unavailable"}); !errors.Is(err, ErrRetryable) {
		t.Errorf("a 503 across the whole route is worth a retry, got %v", err)
	}
	// kenari answers 429 for upstream rejections too; retrying one would burn
	// the whole budget splitting a request that can never pass.
	if err := (APIError{Code: "upstream_rejected", Message: "bad field"}); errors.Is(err, ErrRetryable) {
		t.Errorf("upstream rejection must not be treated as retryable, got %v", err)
	}

	// A daily window is hours away, so one call has to be enough to surface it
	// instead of recursing the batch down to single lines.
	calls := 0
	stubBatch(t, func(context.Context, []string) (map[int]string, error) {
		calls++
		return nil, APIError{Code: "free_quota_daily", Message: "resets tomorrow", Retry: 6 * time.Hour}
	})
	if _, err := TranslateSplit(context.Background(), []string{"1|a", "2|b"}); !errors.Is(err, ErrRetryable) {
		t.Fatalf("want the rate limit surfaced, got %v", err)
	}
	if calls != 1 {
		t.Errorf("want the daily limit reported without splitting, got %d calls", calls)
	}
}

func TestTruncatedAnswerSplitsUntilItFits(t *testing.T) {
	// Play a reasoning model that answers only the first two lines of whatever
	// it is handed, the way a thinking model that burns the reply budget does.
	stubBatch(t, func(_ context.Context, lines []string) (map[int]string, error) {
		if len(lines) > 2 {
			return nil, ErrTruncated
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
	})

	lines := []string{"1|one", "2|two", "3|three", "4|four"}
	got, err := TranslateSplit(context.Background(), lines)
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
	blocks := [][]string{cue("short"), cue(strings.Repeat("x", 4*BatchTokens)), cue("tail")}
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
	c := NewSubtitleCache(t.TempDir())
	if _, ok := c.Get("k"); ok {
		t.Error("cold cache hit")
	}
	c.Put("k", "WEBVTT\n\n1\n00:00:00.000 --> 00:00:01.000\nHalo\n")
	if got, ok := c.Get("k"); !ok || !strings.Contains(got, "Halo") {
		t.Fatalf("memory cache miss: %q", got)
	}
	fresh := NewSubtitleCache(c.dir)
	if got, ok := fresh.Get("k"); !ok || !strings.Contains(got, "Halo") {
		t.Fatalf("disk cache miss: %q", got)
	}
}
