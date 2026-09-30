// Translation of English WebVTT into Indonesian via kenari.id, with batching,
// split-on-retry and dedup so one episode is normally one model call.
package subtitle

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"zanime/internal/hianime"
)

const (
	LLMURL   = "https://kenari.id/v1/chat/completions"
	LLMModel = "deepseek-v4-1-flash"
)

// LLMKey comes from the environment instead of the source, so the secret never
// ends up in version control or in a pasted snippet. Set KENARI_API_KEY before
// the server starts; TranslateBatch refuses to run without it.
var LLMKey = os.Getenv("KENARI_API_KEY")

// LimitRequestsPerMinute is our own pacing, not a kenari number. The 5/min it
// used to hold was the free route's window, advertised in x-ratelimit-limit;
// this account on a paid model answers 60 concurrent calls without complaint and
// sends no such header. Pacing still buys something, because a burst is what a
// 429 punishes, so a generous ceiling stays — far under what the account takes.
const LimitRequestsPerMinute = 30

// ContextTokens is the ceiling a request must stay inside. deepseek-v4-1-flash
// models 1M, so this is under a tenth of what it holds: a bound that has to be
// reasoned about rather than one that is ever reached.
const ContextTokens = 120_000

// MaxReplyTokens is the reply allowance sent as max_completion_tokens. It has to
// cover the translation (about as long as its cues) plus room for a model that
// thinks before answering. An answer that overruns it comes back
// finish_reason=length and the batch is split.
const MaxReplyTokens = 12_000

// BatchTokens is the slice of the window one call spends on its cues: half the
// reply allowance, because a translation runs about as long as its source. It is
// sized so that a whole episode is one call. That is about more than speed: the
// same model asked twice will word a recurrent line two ways unless it can see
// how it worded the last one, so every call an episode does not need is one more
// chance for the file to read as two translations. A real episode is 285-394
// cues, about 3-4k source tokens, so this covers one comfortably. Prompt plus
// reply stay far inside ContextTokens throughout.
const BatchTokens = MaxReplyTokens / 2

// TokensFor estimates a text's token count from its byte length. It only has to
// be conservative: an overestimate costs a slightly smaller batch, an
// underestimate costs a rejected or truncated call.
func TokensFor(s string) int { return (len(s) + 3) / 4 }

// TranslateSystem carries the consistency rules. An episode is normally one
// call, but a long file, or a second pass repairing lines the first one dropped,
// still means a second call — and a model asked twice will word a recurring line
// two ways unless it is told not to. These rules are what keeps the finished file
// reading as one translation.
//
// It must describe exactly what TranslateBatch sends and parses: "<n>|<text>"
// lines in, "<n>|<translation>" lines out. The .vtt scaffolding (header, cue
// ids, timestamps) is handled by ParseVTT and RebuildVTT and never reaches the
// model, so the prompt must not talk about it — a model told to return a .vtt
// file answers in a shape the parser cannot read.
const TranslateSystem = `You are a professional anime subtitle localizer translating English into Indonesian.

The user message is a list of numbered subtitle lines from one episode, one per line, in the form "<number>|<English text>". Translate the text of every line into natural, fluent Indonesian, following these rules:
1. Translate every English word into its standard Indonesian (KBBI) equivalent, including common nouns, occupations, ranks, titles, and roles. Keep only proper names (characters, places, organizations) and Japanese honorifics unchanged.
2. Before translating, read all the lines and settle on one Indonesian equivalent for each recurring term or phrase, then use it identically everywhere.
3. Match each character's tone and speech style, using natural spoken Indonesian rather than literal word-for-word translation.
4. Keep each line concise enough to be read at a glance, without adding information that is not in the source.
5. Return exactly one line per input line, in the same order, in the form "<number>|<Indonesian text>", copying each number unchanged. Never merge, split, skip, add, or renumber lines, and if a line has nothing to translate (for example only symbols), return it unchanged. Example: "12|Wait for me!" becomes "12|Tunggu aku!".
6. Keep formatting tags such as <i></i>, music symbols, and punctuation such as "..." in the same position as in the source, changing only the words.
7. Output only the translated lines, with no explanations, notes, headers, or code fences.`

// APIError is kenari's error envelope. type and code carry the same code, so
// reading code is enough to tell a rate limit from a request that is simply
// wrong — kenari also answers 429 for upstream rejections.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Retry   time.Duration
}

func (e APIError) Error() string { return fmt.Sprintf("kenari %s: %s", e.Code, e.Message) }

func (e APIError) Is(target error) bool { return target == ErrRetryable && retryableCodes[e.Code] }

// ParseRetryAfter reads the seconds form kenari sends. Anything else (an HTTP
// date, an empty header) becomes 0, which callers read as "no hint".
func ParseRetryAfter(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// TranslateBatch asks the model to translate "<index>|<text>" lines and returns
// the translations keyed by index.
func TranslateBatch(ctx context.Context, lines []string) (map[int]string, error) {
	// Checked before the limiter so a missing key fails at once instead of after
	// a wait for budget it will never use.
	if LLMKey == "" {
		return nil, errors.New("KENARI_API_KEY is not set")
	}
	// Charge the limiter for every real call. TranslateSplit fans one job out
	// into many calls, so charging once per HTTP request undercounted the quota
	// by an order of magnitude and let a burst walk straight into a 429.
	if err := WaitForBudget(ctx); err != nil {
		return nil, err
	}
	// The wait above may be long on purpose, so the deadline starts here. The
	// client cannot carry this one: its Timeout covers reading the streamed
	// body, and an answer legitimately runs past any scrape timeout — that is
	// what produced "context deadline exceeded (Client.Timeout ... while reading
	// body)" on the first batch of an episode.
	ctx, cancel := context.WithTimeout(ctx, TranslateCallTimeout)
	defer cancel()
	payload := map[string]any{
		"model": LLMModel,
		// Low, not zero: sampling is what makes the same line come back worded
		// differently each time it appears, and a subtitle file that renders
		// "Ugh, my head..." two ways reads as machine output.
		"temperature":           0.2,
		"max_completion_tokens": MaxReplyTokens,
		"top_p":                 0.95,
		"stream":                true,
		"messages": []map[string]string{
			{"role": "system", "content": TranslateSystem},
			{"role": "user", "content": strings.Join(lines, "\n")},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, LLMURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+LLMKey)
	resp, err := hianime.StreamClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		// 8 KB, not 512: an all_providers_failed envelope lists every backend's
		// failure and can run past 512 bytes, and a cut-off envelope no longer
		// parses, so a retryable error would be reported as a plain failure.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		var env struct {
			Error APIError `json:"error"`
		}
		if json.Unmarshal(b, &env) == nil && env.Error.Code != "" {
			env.Error.Retry = ParseRetryAfter(resp.Header.Get("Retry-After"))
			return nil, env.Error
		}
		msg := strings.TrimSpace(string(b))
		if r := []rune(msg); len(r) > 300 {
			msg = string(r[:300]) + "..."
		}
		return nil, fmt.Errorf("kenari HTTP %d: %s", resp.StatusCode, msg)
	}

	var answer strings.Builder
	finish := ""
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Error *APIError `json:"error"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			continue
		}
		// A mid-stream failure rides in a 200 chunk, not in the status code.
		if chunk.Error != nil {
			return nil, *chunk.Error
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		if r := chunk.Choices[0].FinishReason; r != "" {
			finish = r
		}
		// A model that thinks before answering puts that in a sibling field;
		// only delta.content is the translation, so reasoning is ignored.
		answer.WriteString(chunk.Choices[0].Delta.Content)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	out := map[int]string{}
	for _, line := range strings.Split(answer.String(), "\n") {
		line = strings.TrimSpace(line)
		bar := strings.Index(line, "|")
		if bar < 1 {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(line[:bar]))
		if err != nil {
			continue
		}
		if text := strings.TrimSpace(line[bar+1:]); text != "" {
			out[idx] = text
		}
	}
	// finish_reason=length with lines still unanswered means the answer was cut
	// off, which is what a reasoning model does when it spends the whole reply
	// budget thinking: it returns finish_reason=length and no lines at all. The
	// batch is simply more than one reply can carry, so it gets split.
	if finish == "length" && len(out) < len(lines) {
		return nil, ErrTruncated
	}
	if len(out) == 0 {
		return nil, errors.New("model returned no parsable translations")
	}
	return out, nil
}

// ErrRetryable marks a failure a retry can clear, so the batch is split and
// retried instead of failing outright.
var ErrRetryable = errors.New("kenari retryable failure")

// ErrTruncated marks an answer the model cut off mid-list. Nothing is
// throttling us, so the fix is a smaller batch rather than a wait.
var ErrTruncated = errors.New("kenari answer truncated")

// TranslateCallTimeout bounds one streaming call. It is generous because the
// batch is deliberately large and a reasoning model can think for a while.
const TranslateCallTimeout = 5 * time.Minute

// retryableCodes are the failures a retry can clear: kenari's rate limits, and
// the 503 it answers when every backend for the route failed at once. Any other
// code is an upstream rejection of the request itself, where extra patience
// only delays the report.
var retryableCodes = map[string]bool{
	"rate_limit_exceeded":  true,
	"free_quota_rpm":       true,
	"free_quota_daily":     true,
	"plan_limit_reached":   true,
	"upstream_error":       true,
	"all_providers_failed": true,
}

// BatchFn is indirect so tests can exercise the batching without kenari.
var BatchFn = TranslateBatch

// WaitForBudget blocks until the request window allows one more call.
// Conversion runs in a background job, so waiting only costs time — which is
// exactly the point: pacing beats a 429 storm.
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

// rateLimitPause is the fallback wait when a 429 arrives without a usable
// Retry-After.
const rateLimitPause = 15 * time.Second

// TranslateSplit runs TranslateBatch and, on a retryable failure, halves the
// batch. Whatever the limit was, it counts calls rather than lines, so a batch
// that loses the race is better off split than retried at the same size — and a
// truncated answer can only be fixed by asking for less.
func TranslateSplit(ctx context.Context, lines []string) (map[int]string, error) {
	for paused := false; ; {
		got, err := BatchFn(ctx, lines)
		if err == nil {
			return got, nil
		}
		if !errors.Is(err, ErrRetryable) && !errors.Is(err, ErrTruncated) {
			return nil, err
		}
		retry := rateLimitPause
		var ae APIError
		if errors.As(err, &ae) && ae.Retry > 0 {
			retry = ae.Retry
		}
		// A window this far out is the daily allowance or a plan quota, not a
		// burst: recursion would stall the job for hours before reporting it.
		if retry > time.Minute {
			return nil, err
		}
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
		if errors.Is(err, ErrTruncated) {
			// One cue is the smallest ask there is; a model that cannot answer
			// it will not answer a retry of it either.
			return nil, err
		}
		if paused {
			return nil, err
		}
		paused = true
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retry):
		}
	}
}

// TranslateRange translates the listed cue blocks in batches, writing results
// back into blocks and returning the blocks the model left unanswered. onBatch
// gets the number of cues actually answered, so a dropped line does not
// overstate progress before the retry pass catches it.
func TranslateRange(ctx context.Context, blocks [][]string, idx []int, onBatch func(int)) ([]int, error) {
	var missing []int
	for start := 0; start < len(idx); {
		end := batchEnd(blocks, idx, start)
		batch := idx[start:end]
		lines := make([]string, len(batch))
		for i, b := range batch {
			lines[i] = fmt.Sprintf("%d|%s", i+1, CueText(blocks[b]))
		}
		got, err := TranslateSplit(ctx, lines)
		if err != nil {
			return nil, err
		}
		answered := 0
		for i, b := range batch {
			text, ok := got[i+1]
			if !ok {
				missing = append(missing, b)
				continue
			}
			blocks[b] = SetCueText(blocks[b], text)
			answered++
		}
		if onBatch != nil && answered > 0 {
			onBatch(answered)
		}
		start = end
	}
	return missing, nil
}

// batchEnd returns the slice of idx that fits one call: cues are added while the
// estimate stays inside BatchTokens. The first cue is taken whatever it costs,
// so a single long line still has somewhere to go.
func batchEnd(blocks [][]string, idx []int, start int) int {
	used, end := 0, start
	for end < len(idx) {
		t := TokensFor(CueText(blocks[idx[end]])) + 4 // the "<n>|" prefix and newline
		// The cues, the system prompt and the whole reply allowance still have to
		// fit the window every model on the route is guaranteed.
		if end > start && (used+t > BatchTokens || used+t+MaxReplyTokens > ContextTokens) {
			break
		}
		used += t
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
	// Identical lines are translated once and copied. Dialogue repeats heavily
	// ("Yes", "What?!", catchphrases), so this shrinks the prompt — keeping the
	// whole episode in one call — and makes consistency free: one source line,
	// one wording, everywhere. It also fixes what progress counts: the model
	// answers one line per distinct text, so that is the total the player is
	// told, and the bar can reach its end.
	seen := map[string]int{}
	dupOf := map[int]int{} // cue index -> index of its first occurrence in uniq
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
	done := 0
	bump := func(n int) {
		done += n
		if progress != nil {
			progress(done, len(uniq))
		}
	}
	// Reported before the first call so the player can show the size of the job
	// while the model is still thinking about it.
	if progress != nil {
		progress(0, len(uniq))
	}
	missing, err := TranslateRange(ctx, blocks, uniq, bump)
	if err != nil {
		return "", err
	}
	// The model occasionally drops a line. Without this pass those cues stay
	// English in an otherwise Indonesian file, which reads as "the subtitle was
	// not converted" even though most of it was.
	if len(missing) > 0 {
		// TranslateRange now reports its own progress, so cues the retry pass
		// rescues are counted too.
		if _, err := TranslateRange(ctx, blocks, missing, bump); err != nil {
			return "", err
		}
	}
	// Copy each unique line's (now Indonesian) text to its duplicates.
	for _, i := range queue {
		if first := uniq[dupOf[i]]; first != i {
			blocks[i] = SetCueText(blocks[i], CueText(blocks[first]))
		}
	}
	return RebuildVTT(header, blocks), nil
}

// TranslateFn is indirect so the job's progress reporting is testable offline.
var TranslateFn = TranslateVTT
