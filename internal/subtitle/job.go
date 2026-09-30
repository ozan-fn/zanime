// Background conversion jobs with progress tracking.
package subtitle

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"zanime/internal/hianime"
	"zanime/internal/stream"
)

// MaxConcurrentJobs bounds how many conversions translate at once. Each job
// holds a whole subtitle in memory and competes for the same five-calls-a-minute
// window, so an unbounded pile of viewers would queue without limit and none of
// them would finish. The extras wait for a slot; the player is polling status,
// so waiting is visible rather than an error.
const MaxConcurrentJobs = 4

// JobSlots is the conversion semaphore. Buffered rather than a mutex so the
// wait below stays selectable against the job's context.
var JobSlots = make(chan struct{}, MaxConcurrentJobs)

// SubJob tracks one episode's conversion. Translation runs in the background
// because a whole episode takes minutes — far past any sane request timeout —
// and the client disconnects long before it ends. The player
// polls this progress instead of waiting on a track that never loads.
//
// done and total are written together by SetProgress and always describe the
// same unit: the distinct lines the model is asked to translate.
type SubJob struct {
	mu      sync.Mutex
	start   time.Time
	total   int
	done    int
	err     error
	ready   bool
	running bool
}

// Elapsed is how long this job has been converting, which is the denominator of
// the rate the ETA is projected from.
func (j *SubJob) Elapsed() time.Duration {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.start.IsZero() {
		return 0
	}
	return time.Since(j.start)
}

func (j *SubJob) Snapshot() (state string, done, total int, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	switch {
	case j.ready:
		state = "ready"
	case j.err != nil:
		state = "error"
	case j.running:
		state = "converting"
	default:
		state = "pending"
	}
	return state, j.done, j.total, j.err
}

// SetProgress records where the run is. Totalling by addition would have to be
// fed increments, but the translator reports a running count: a split batch used
// to be added to the running total and drove the bar past its end, and the lines
// it deduplicates were never counted at all, so an unbroken run stopped short.
// One setter for both numbers is what keeps them commensurable.
func (j *SubJob) SetProgress(done, total int) {
	j.mu.Lock()
	j.done, j.total = done, total
	j.mu.Unlock()
}

func (j *SubJob) Finish(err error) {
	j.mu.Lock()
	j.running, j.err, j.ready = false, err, err == nil
	j.mu.Unlock()
}

// SubKey names one finished conversion: an episode and a target language. It is
// both the cache key and the job key. Only the dub stream is played, so the
// audio type is not part of the key.
func SubKey(episodeID, lang string) string {
	return episodeID + "|" + lang
}

var subJobs sync.Map // SubKey -> *SubJob

// EtaSeconds projects the cues still to come from how fast the job has actually
// been going. A fixed rate would be a guess now that one call carries a whole
// episode's worth of cues and the first answer is what tells us the speed.
func EtaSeconds(done, total int, elapsed time.Duration) int {
	if total <= done || done == 0 || elapsed <= 0 {
		return 0
	}
	remaining := elapsed / time.Duration(done) * time.Duration(total-done)
	return int(remaining / time.Second)
}

// Status returns this request's conversion, starting it on first ask. A source
// already in the target language is cached verbatim: no job, no quota.
func Status(req *http.Request, cache *SubtitleCache, episodeID, lang string) (*SubJob, error) {
	key := SubKey(episodeID, lang)
	if _, ok := cache.Get(key); ok {
		return &SubJob{ready: true}, nil
	}
	// A conversion already under way is answered from memory, before upstream is
	// touched. Polling used to re-resolve the episode on every call, so a scrape
	// that failed mid-conversion turned each /status into an error — the player
	// counted misses, gave up, and left a finished translation sitting in the
	// cache until the page was reloaded.
	if v, ok := subJobs.Load(key); ok {
		return v.(*SubJob), nil
	}
	src, err := stream.CachedResolve(req.Context(), episodeID)
	if err != nil {
		return nil, err
	}
	pick := PickSubtitle(src.Subtitles, lang)
	if pick == nil {
		return nil, errors.New("episode has no subtitles")
	}
	if hianime.MatchesLang(pick.Lang, lang) {
		raw, err := hianime.Get(req.Context(), pick.URL, src.Referer)
		if err != nil {
			return nil, err
		}
		cache.Put(key, raw)
		return &SubJob{ready: true}, nil
	}
	job := &SubJob{running: true, start: time.Now()}
	actual, loaded := subJobs.LoadOrStore(key, job)
	if loaded {
		return actual.(*SubJob), nil
	}
	go RunSubtitleJob(cache, key, episodeID, lang, job)
	return job, nil
}

func RunSubtitleJob(cache *SubtitleCache, key, episodeID, lang string, job *SubJob) {
	// Deliberately not the request context: this job outlives the request that
	// started it, which is the whole reason it runs in a goroutine.
	ctx := context.Background()
	fail := func(err error) {
		log.Printf("subtitle %s: %v", key, err)
		// Sticky on purpose: what TranslateSplit gives up on is structural — a
		// rejected request, a spent quota — so re-running it on each poll would
		// only burn calls to hear the same answer.
		job.Finish(err)
	}
	src, err := stream.Resolve(ctx, episodeID)
	if err != nil {
		fail(err)
		return
	}
	pick := PickSubtitle(src.Subtitles, lang)
	if pick == nil {
		fail(errors.New("episode has no subtitles"))
		return
	}
	raw, err := hianime.Get(ctx, pick.URL, src.Referer)
	if err != nil {
		fail(err)
		return
	}
	// The fetches above run without holding a slot; only translating competes
	// for the model window.
	select {
	case JobSlots <- struct{}{}:
		defer func() { <-JobSlots }()
	case <-ctx.Done():
		fail(ctx.Err())
		return
	}
	done, err := TranslateFn(ctx, raw, job.SetProgress)
	if err != nil {
		fail(err)
		return
	}
	// Validated before the job is called ready: the cache refuses a file whose
	// script is not the target's, so a job marked ready with such a file left
	// /subtitle/{id} answering "finished subtitle is missing from the cache"
	// forever. Failing here reports the real cause instead.
	if _, ok := usable(done); !ok {
		fail(errors.New("model returned the subtitle in a non-Latin script"))
		return
	}
	cache.Put(key, done)
	job.Finish(nil)
}

// WriteJob turns a job into the progress body the player polls.
func WriteJob(w http.ResponseWriter, writeJSON func(w http.ResponseWriter, status int, v any), code int, job *SubJob) {
	state, done, total, err := job.Snapshot()
	body := map[string]any{
		"state":       state,
		"done":        done,
		"total":       total,
		"eta_seconds": EtaSeconds(done, total, job.Elapsed()),
	}
	if err != nil {
		body["error"] = err.Error()
	}
	if code == http.StatusAccepted {
		w.Header().Set("Retry-After", "3")
	}
	writeJSON(w, code, body)
}

// PickSubtitle chooses the track to convert: the wanted language when the
// provider already has it (that file is served verbatim), otherwise the track
// the resolver marked as the episode's dialogue.
func PickSubtitle(subs []hianime.Subtitle, lang string) *hianime.Subtitle {
	if len(subs) == 0 {
		return nil
	}
	for i := range subs {
		if hianime.MatchesLang(subs[i].Lang, lang) {
			return &subs[i]
		}
	}
	for i := range subs {
		if subs[i].Default {
			return &subs[i]
		}
	}
	return &subs[0]
}

// WantedLang reads the lang query param, defaulting to Indonesian.
func WantedLang(req *http.Request) string {
	if lang := req.URL.Query().Get("lang"); lang != "" {
		return lang
	}
	return "id"
}
