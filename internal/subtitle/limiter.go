// Rate limiting for translation requests so the free endpoint is not hammered in
// a burst. The ceiling is ours, not the provider's: it exists so one job cannot
// walk straight into a soft-block.
package subtitle

import (
	"sync"
	"time"
)

// Limiter paces calls per sliding minute window.
type Limiter struct {
	mu       sync.Mutex
	minStart time.Time
	minCount int
}

func NewLimiter() *Limiter {
	return &Limiter{minStart: time.Now()}
}

// Take reserves budget for one call, returning how long to wait when full.
func (l *Limiter) Take() (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.minStart) >= time.Minute {
		l.minStart, l.minCount = now, 0
	}
	if l.minCount >= requestsPerMinute {
		return time.Until(l.minStart.Add(time.Minute)), false
	}
	l.minCount++
	return 0, true
}

func (l *Limiter) Usage() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return map[string]int{
		"requests_this_minute": l.minCount,
		"requests_per_minute":  requestsPerMinute,
	}
}

var LlmLimit = NewLimiter()

// requestsPerMinute is our own pacing for the free translator endpoint. It is
// not a provider limit; it is a safety belt so a busy episode does not turn into
// a burst that gets the source soft-blocked.
const requestsPerMinute = 120
