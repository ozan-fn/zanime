// Rate limiting for kenari calls: one limiter shared by all translation jobs.
package subtitle

import (
	"sync"
	"time"
)

// Limiter paces calls per minute. The number is ours, not kenari's: it exists to
// keep a job from arriving in a burst, since a burst is what a 429 punishes.
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
	if l.minCount >= LimitRequestsPerMinute {
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
		"requests_per_minute":  LimitRequestsPerMinute,
	}
}

var LlmLimit = NewLimiter()
