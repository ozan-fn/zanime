// Translation of English WebVTT into Indonesian via the Google Translate web
// RPC (batchexecute MkEWBc, shape captured from browser traffic), no API key.
// The RPC answers one text per call, so a batch is one request per line.
package subtitle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"zanime/internal/hianime"
)

const (
	googleBatchURL = "https://translate.google.com/_/TranslateWebserverUi/data/batchexecute"
	googleBl       = "boq_translate-webserver_20260928.05_p0"
	// Captured from browser traffic; override via env when Google rotates them:
	// GOOGLE_FSID / GOOGLE_AT (fresh values: record translate.google.com in
	// devtools, copy f.sid + at from a batchexecute request).
	googleFSID = "6012021807798436984"
	// Chars per batch: one batch is one call. Numbered "N|text" lines survive
	// the web round trip (proven 60/60), so rows map back by prefix; a count
	// mismatch splits back down instead of misaligning.
	maxBatchChars = 4500
	// Pause between batches so a burst does not get us soft-blocked.
	requestPause = 500 * time.Millisecond
)

// googleReqID gives each RPC a fresh _reqid like the browser does.
var googleReqID atomic.Int64

func init() { googleReqID.Store(100000) }

// Circuit breaker: consecutive 429s mean the IP is sorry-blocked; hammering
// longer only extends it. Trip after a few, cool down, fail fast with a clear
// message instead of retrying for minutes.
var (
	google429s    atomic.Int64
	googleCoolMu  sync.Mutex
	googleCooling time.Time
)

func googleCoolLeft() time.Duration {
	googleCoolMu.Lock()
	defer googleCoolMu.Unlock()
	return time.Until(googleCooling)
}

func googleTrip() error {
	n := google429s.Add(1)
	if n < 5 {
		return nil
	}
	googleCoolMu.Lock()
	googleCooling = time.Now().Add(10 * time.Minute)
	googleCoolMu.Unlock()
	return fmt.Errorf("%w: cooling down 10m", errCooling)
}

// errCooling fails a batch fast without hammering; TranslateBatch still tries
// the proxy fallback for it.
var errCooling = errors.New("google web rate-limited this IP")

// googleTok holds the web session (f.sid + build label), auto-refreshed from
// the Translate page whenever the RPC stops answering.
var (
	googleTokMu sync.RWMutex
	googleTok   = struct{ fsid, bl string }{googleFSID, googleBl}
)

func googleSession() (fsid, bl string) {
	if v := os.Getenv("GOOGLE_FSID"); v != "" {
		fsid = v
	} else {
		googleTokMu.RLock()
		fsid = googleTok.fsid
		googleTokMu.RUnlock()
	}
	if v := os.Getenv("GOOGLE_BL"); v != "" {
		return fsid, v
	}
	googleTokMu.RLock()
	defer googleTokMu.RUnlock()
	return fsid, googleTok.bl
}

// refreshGoogleSession scrapes a fresh f.sid + build label from the Translate
// page. No login needed; the page embeds both.
func refreshGoogleSession(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://translate.google.com/?sl=en&tl=id&op=translate", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36")
	resp, err := hianime.StreamClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	page := string(raw)
	fsid, bl := "", ""
	if m := googleFSIDRe.FindStringSubmatch(page); m != nil {
		fsid = m[1]
	}
	if m := googleBlRe.FindStringSubmatch(page); m != nil {
		bl = m[1]
	}
	if fsid == "" || bl == "" {
		return errors.New("google web session markers not found")
	}
	googleTokMu.Lock()
	googleTok.fsid, googleTok.bl = fsid, bl
	googleTokMu.Unlock()
	log.Printf("google web session refreshed (bl=%s)", bl)
	return nil
}

var (
	googleFSIDRe = regexp.MustCompile(`FdrFJe":"(-?\d+)"`)
	googleBlRe   = regexp.MustCompile(`cfb2h":"(boq_translate-webserver[^"]+)"`)
)

// TokensFor is kept as a rough text-size estimate for batching cues into one
// translate request. It is not a token count for a model anymore; it is only
// used to decide when a batch is getting too long for one free-request payload.
func TokensFor(s string) int { return len(s) }

// TranslateBatch sends "<index>|<text>" lines and returns translations keyed by
// index. One batch is one RPC: the numbered lines go in verbatim, rows map
// back by their numeric prefix (order-independent). A count mismatch is
// retryable, so TranslateSplit halves down instead of misaligning cues.
func TranslateBatch(ctx context.Context, lines []string) (map[int]string, error) {
	if len(lines) == 0 {
		return map[int]string{}, nil
	}
	var idxs []int
	var texts []string
	for _, line := range lines {
		bar := strings.Index(line, "|")
		if bar < 1 {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(line[:bar]))
		if err != nil {
			continue
		}
		if t := strings.TrimSpace(line[bar+1:]); t != "" {
			idxs = append(idxs, idx)
			texts = append(texts, t)
		}
	}
	if len(idxs) == 0 {
		return nil, errors.New("google web returned no parsable translations")
	}
	send := make([]string, len(texts))
	for i, t := range texts {
		send[i] = fmt.Sprintf("%d|%s", i+1, t)
	}
	if rows, err := googleCall(ctx, send); err == nil {
		out := map[int]string{}
		for _, row := range rows {
			bar := strings.Index(row, "|")
			if bar < 1 {
				continue
			}
			pos, err := strconv.Atoi(strings.TrimSpace(row[:bar]))
			if err != nil || pos < 1 || pos > len(idxs) {
				continue
			}
			if t := strings.TrimSpace(row[bar+1:]); t != "" {
				out[idxs[pos-1]] = t
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	} else if !errors.Is(err, ErrRetryable) && !errors.Is(err, errGoogleToken) && !errors.Is(err, errCooling) {
		return nil, err
	}
	// Web failed → gtx via third-party fetch proxy (our egress IP is
	// sorry-blocked on googleapis, the proxy's is not). Plain texts,
	// positional mapping, same mismatch guard.
	trans, err := gtxProxyCall(ctx, texts)
	if err != nil {
		return nil, err
	}
	out := map[int]string{}
	for i, t := range trans {
		if t != "" {
			out[idxs[i]] = t
		}
	}
	if len(out) == 0 {
		return nil, errors.New("gtx proxy returned no parsable translations")
	}
	return out, nil
}

// gtxProxyBase routes a gtx call through a fetch proxy (pattern from the
// anistream userscript): ?url=<gtx>&origin=...
const gtxProxyBase = "https://minky.anistream.one/fetch?url="

func gtxProxyCall(ctx context.Context, texts []string) ([]string, error) {
	var all []string
	for start := 0; start < len(texts); {
		end, size := start, 0
		for end < len(texts) {
			// chisle: proxy answers 431 past a few KB of URL — keep each
			// GET small, concat in order.
			if size+len(texts[end]) > 1200 && end > start {
				break
			}
			size += len(texts[end])
			end++
		}
		part, err := gtxProxyGet(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		all = append(all, part...)
		start = end
	}
	return all, nil
}

func gtxProxyGet(ctx context.Context, texts []string) ([]string, error) {
	gtx := "https://translate.googleapis.com/translate_a/single?client=gtx&sl=en&tl=id&dt=t&q=" + url.QueryEscape(strings.Join(texts, "\n"))
	u := gtxProxyBase + url.QueryEscape(gtx) + "&origin=" + url.QueryEscape("https://megaplay.buzz")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36")
	resp, err := hianime.StreamClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, APIError{Code: strconv.Itoa(resp.StatusCode), Message: fmt.Sprintf("gtx proxy HTTP %d", resp.StatusCode), Retry: 15 * time.Second}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gtx proxy HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("gtx proxy gave no translation: %.120q", string(raw))
	}
	top, ok := payload.([]any)
	if !ok || len(top) == 0 {
		return nil, errors.New("gtx proxy gave no translation")
	}
	segs, ok := top[0].([]any)
	if !ok {
		return nil, errors.New("gtx proxy gave no translation")
	}
	var sb strings.Builder
	for _, s := range segs {
		if pair, ok := s.([]any); ok && len(pair) > 0 {
			if t, _ := pair[0].(string); t != "" {
				sb.WriteString(t)
			}
		}
	}
	rows := strings.Split(sb.String(), "\n")
	// chisle: mismatch is retryable — split halves it.
	if len(rows) != len(texts) {
		return nil, APIError{Message: fmt.Sprintf("gtx proxy line mismatch: %d cues, %d rows", len(texts), len(rows)), Retry: 5 * time.Second}
	}
	return rows, nil
}

// googleCall translates many lines in one TranslateWebserverUi batchexecute
// RPC, joined by newline. The answer splits back by newline; a count mismatch
// is retryable (split halves it). A stale session triggers one auto-refresh +
// retry first.
func googleCall(ctx context.Context, texts []string) ([]string, error) {
	if left := googleCoolLeft(); left > 0 {
		return nil, fmt.Errorf("%w: %s left", errCooling, left.Round(time.Second))
	}
	if err := WaitForBudget(ctx); err != nil {
		return nil, err
	}
	trans, err := googlePost(ctx, texts)
	if !errors.Is(err, errGoogleToken) {
		if err == nil {
			google429s.Store(0)
		}
		return trans, err
	}
	if rerr := refreshGoogleSession(ctx); rerr != nil {
		return nil, err
	}
	trans, err = googlePost(ctx, texts)
	if err == nil {
		google429s.Store(0)
	}
	return trans, err
}

func googlePost(ctx context.Context, texts []string) ([]string, error) {
	joined := strings.Join(texts, "\n")
	log.Printf("google web: %d lines %d chars", len(texts), len(joined))
	payload, _ := json.Marshal([]any{[]any{joined, "en", "id", 1, nil, 1}, []any{}})
	freq, _ := json.Marshal([]any{[]any{[]any{"MkEWBc", string(payload), nil, "generic"}}})
	form := url.Values{}
	form.Set("f.req", string(freq))
	// chisle: no `at` by default — a stale one fails, omitting works. Send only
	// an explicit GOOGLE_AT override.
	if at := os.Getenv("GOOGLE_AT"); at != "" {
		form.Set("at", at)
	}
	fsid, bl := googleSession()
	q := url.Values{}
	q.Set("rpcids", "MkEWBc")
	q.Set("source-path", "/")
	q.Set("f.sid", fsid)
	q.Set("bl", bl)
	q.Set("hl", "en")
	q.Set("soc-app", "1")
	q.Set("soc-platform", "1")
	q.Set("soc-device", "1")
	q.Set("_reqid", strconv.FormatInt(googleReqID.Add(1), 10))
	q.Set("rt", "c")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleBatchURL+"?"+q.Encode(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	req.Header.Set("Origin", "https://translate.google.com")
	req.Header.Set("Referer", "https://translate.google.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36")
	resp, err := hianime.StreamClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// chisle: 429/5xx retryable (split+backoff handles it), else fail fast.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden || resp.StatusCode >= 500 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		msg := strings.TrimSpace(string(b))
		if r := []rune(msg); len(r) > 120 {
			msg = string(r[:120]) + "..."
		}
		retry := 15 * time.Second
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if n, convErr := strconv.Atoi(ra); convErr == nil {
				retry = time.Duration(n) * time.Second
			}
		}
		if trip := googleTrip(); trip != nil {
			log.Printf("google web: %v", trip)
			return nil, trip
		}
		return nil, APIError{Code: strconv.Itoa(resp.StatusCode), Message: fmt.Sprintf("google web HTTP %d: %s", resp.StatusCode, msg), Retry: retry}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google web HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	// Sorry-block arrives as 200 after redirect; treat like a 429.
	if strings.Contains(string(raw), "sorry/index") {
		if trip := googleTrip(); trip != nil {
			log.Printf("google web: %v", trip)
			return nil, trip
		}
		return nil, APIError{Code: "429", Message: "google web sorry-block", Retry: 15 * time.Second}
	}
	trans, ok := parseGoogleWeb(raw)
	if !ok {
		return nil, fmt.Errorf("%w: %.200q", errGoogleToken, string(raw))
	}
	rows := strings.Split(trans, "\n")
	for i := range rows {
		rows[i] = strings.TrimSpace(rows[i])
	}
	// Single line: take as-is (its own translation may hold a newline).
	if len(texts) == 1 {
		if rows[0] == "" {
			return nil, errGoogleToken
		}
		return []string{strings.Join(rows, " ")}, nil
	}
	// chisle: count mismatch is retryable — split halves it instead of
	// misaligning cues.
	if len(rows) != len(texts) {
		return nil, APIError{Message: fmt.Sprintf("google web line mismatch: %d cues, %d rows", len(texts), len(rows)), Retry: 5 * time.Second}
	}
	return rows, nil
}

// errGoogleToken means the web session went stale; googleCall refreshes once
// and retries before surfacing this.
var errGoogleToken = errors.New("google web gave no translation (token expired?)")

// parseGoogleWeb pulls the translation out of a batchexecute body:
// length-prefixed JSON chunks, the MkEWBc one carrying
// [translation, null×5, source-part, 1] rows. Multi-sentence answers split per
// sentence, so matches join in order.
func parseGoogleWeb(body []byte) (string, bool) {
	var parts []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[[") {
			continue
		}
		var arr []any
		if err := json.Unmarshal([]byte(line), &arr); err != nil {
			continue
		}
		var inner string
		for _, sub := range arr {
			s, ok := sub.([]any)
			if ok && len(s) >= 3 && s[0] == "wrb.fr" && s[1] == "MkEWBc" {
				inner, _ = s[2].(string)
				break
			}
		}
		if inner == "" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(inner), &v); err != nil {
			continue
		}
		var out []string
		var walk func(o any)
		walk = func(o any) {
			a, ok := o.([]any)
			if !ok {
				return
			}
			if len(a) == 8 {
				if dst, _ := a[0].(string); dst != "" {
					if src, _ := a[6].(string); src != "" {
						if n, _ := a[7].(float64); n == 1 {
							out = append(out, dst)
						}
					}
				}
			}
			for _, x := range a {
				walk(x)
			}
		}
		walk(v)
		if len(out) > 0 {
			parts = append(parts, strings.Join(out, " "))
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, " "), true
}

// APIError is the failure envelope the translator path exposes to the retry
// splitter. For the free endpoint there is usually no structured code, so a
// rate-limited or unavailable answer is still just an HTTP failure we decide how
// to handle in TranslateSplit.
type APIError struct {
	Code    string `json:"-"`
	Message string
	Retry   time.Duration
}

func (e APIError) Error() string { return e.Message }

func (e APIError) Is(target error) bool {
	return target == ErrRetryable
}

// ErrRetryable marks a failure a retry can clear, so the batch is split and
// retried instead of failing outright.
var ErrRetryable = errors.New("google web retryable failure")

// ErrTruncated is unused by the free endpoint path; kept so existing split
// helpers still compile, but the free translator either returns translations or
// fails, it does not cut an answer mid-list.
var ErrTruncated = errors.New("google web answer truncated")

// TranslateCallTimeout bounds one translate request.
const TranslateCallTimeout = 30 * time.Second

// BatchFn is indirect so tests can exercise the batching without network.
var BatchFn = TranslateBatch

// WaitForBudget blocks until one more translator request is allowed. It is
// backed by LlmLimit so the existing job pacing is reused.
func WaitForBudget(ctx context.Context) error {
	for {
		wait, ok := LlmLimit.Take()
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// TranslateSplit runs TranslateBatch and, on a retryable failure, splits the
// batch so the request shrinks instead of being retried at the same size.
func TranslateSplit(ctx context.Context, lines []string) (map[int]string, error) {
	var apiErr APIError
	for attempt := 0; ; attempt++ {
		if err := WaitForBudget(ctx); err != nil {
			return nil, err
		}
		got, err := BatchFn(ctx, lines)
		if err == nil {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(requestPause):
			}
			return got, nil
		}
		if !errors.Is(err, ErrRetryable) {
			return nil, err
		}
		errors.As(err, &apiErr)
		// The free endpoint has no structured Retry-After we can trust, so fall
		// back to a short pause and a smaller batch.
		if len(lines) > 1 {
			half := len(lines) / 2
			a, err := TranslateSplit(ctx, lines[:half])
			if err != nil {
				return nil, err
			}
			b, err := TranslateSplit(ctx, lines[half:])
			if err != nil {
				return nil, err
			}
			for k, v := range b {
				a[k] = v
			}
			return a, nil
		}
		if attempt >= 4 {
			return nil, err
		}
		wait := requestPause * time.Duration(1<<attempt) * 5
		if apiErr.Retry > wait {
			wait = apiErr.Retry
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// mostlyNonLatin reports whether more than half of the letters in s are outside
// the Latin script. Symbols, digits and punctuation are not letters, so "♪" or
// "..." are never flagged.
func mostlyNonLatin(s string) bool {
	letters, other := 0, 0
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if !unicode.Is(unicode.Latin, r) {
			other++
		}
	}
	return letters > 0 && other*2 > letters
}

// MaxTranslateWorkers caps concurrent batch requests: pacing per request
// (limiter + pause inside TranslateSplit) still applies.
const MaxTranslateWorkers = 4

// TranslateRange translates the listed cue blocks in batches, writing results
// back into blocks and returning the blocks the endpoint left unanswered, or
// answered in the wrong script. onBatch gets the number of cues actually
// answered, so a dropped line does not overstate progress before any retry pass
// catches it. Batches run concurrently; each touches disjoint blocks.
func TranslateRange(ctx context.Context, blocks [][]string, idx []int, onBatch func(int)) ([]int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type work struct {
		lines []string
		cues  []int
	}
	var batches []work
	for start := 0; start < len(idx); {
		end := batchEnd(blocks, idx, start)
		cues := idx[start:end]
		lines := make([]string, len(cues))
		for i, b := range cues {
			lines[i] = fmt.Sprintf("%d|%s", i+1, CueText(blocks[b]))
		}
		batches = append(batches, work{lines, cues})
		start = end
	}
	var mu sync.Mutex
	var missing []int
	var firstErr error
	sem := make(chan struct{}, MaxTranslateWorkers)
	var wg sync.WaitGroup
loop:
	for _, w := range batches {
		select {
		case <-ctx.Done():
			break loop
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(w work) {
			defer wg.Done()
			defer func() { <-sem }()
			got, err := TranslateSplit(ctx, w.lines)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
				return
			}
			answered := 0
			var miss []int
			for i, b := range w.cues {
				text, ok := got[i+1]
				if !ok || (mostlyNonLatin(text) && !mostlyNonLatin(CueText(blocks[b]))) {
					miss = append(miss, b)
					continue
				}
				blocks[b] = SetCueText(blocks[b], text)
				log.Printf("cue %d: %s", b, text)
				answered++
			}
			mu.Lock()
			missing = append(missing, miss...)
			mu.Unlock()
			if onBatch != nil && answered > 0 {
				onBatch(answered)
			}
		}(w)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return missing, nil
}

// batchEnd returns the slice of idx that fits one call: up to maxBatchChars.
func batchEnd(blocks [][]string, idx []int, start int) int {
	end, size := start, 0
	for end < len(idx) {
		if n := len(CueText(blocks[idx[end]])); size+n > maxBatchChars && end > start {
			break
		} else {
			size += n
		}
		end++
	}
	return end
}

// TranslateVTT walks the cue blocks in batches, translating each into the
// target language and reporting progress as distinct lines answered out of the
// distinct lines the file holds.
func TranslateVTT(ctx context.Context, vtt string, progress func(done, total int)) (string, error) {
	header, blocks := ParseVTT(vtt)
	var queue []int
	for i, blk := range blocks {
		if CueText(blk) != "" {
			queue = append(queue, i)
		}
	}
	if len(queue) == 0 {
		return vtt, nil
	}
	seen := map[string]int{}
	dupOf := map[int]int{}
	var uniq []int
	for _, i := range queue {
		t := CueText(blocks[i])
		first, ok := seen[t]
		if !ok {
			first = len(uniq)
			seen[t] = first
			uniq = append(uniq, i)
		}
		dupOf[i] = first
	}
	var progMu sync.Mutex
	done := 0
	bump := func(n int) {
		progMu.Lock()
		done += n
		d := done
		progMu.Unlock()
		if progress != nil {
			progress(d, len(uniq))
		}
	}
	if progress != nil {
		progress(0, len(uniq))
	}
	missing, err := TranslateRange(ctx, blocks, uniq, bump)
	if err != nil {
		return "", err
	}
	if len(missing) > 0 {
		if _, err := TranslateRange(ctx, blocks, missing, bump); err != nil {
			return "", err
		}
	}
	for _, i := range queue {
		if first := uniq[dupOf[i]]; first != i {
			blocks[i] = SetCueText(blocks[i], CueText(blocks[first]))
		}
	}
	return RebuildVTT(header, blocks), nil
}

// TranslateFn is indirect so the job's progress reporting is testable offline.
var TranslateFn = TranslateVTT
