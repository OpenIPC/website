package firmware

import (
	"sync"
	"time"
)

// Limiter caps how many images one address may have BUILT in a minute. A
// cached image is never refused: nginx already limits requests, and a 32 MB
// image is legitimately fetched as thirty range requests. Only a cache miss
// costs anything, so only a miss is counted here.
//
// In memory, in the one firmware process: a restart forgets a minute of
// history, which is strictly better than the per-worker counters it replaces.
type Limiter struct {
	Limit  int
	Window time.Duration

	mu   sync.Mutex
	seen map[string][]time.Time
}

// Allow says whether ip may start another build now, and records it if so.
func (l *Limiter) Allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen == nil {
		l.seen = map[string][]time.Time{}
	}
	cutoff := now.Add(-l.Window)
	recent := l.seen[ip][:0]
	for _, t := range l.seen[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.Limit {
		l.seen[ip] = recent
		return false
	}
	l.seen[ip] = append(recent, now)
	// Forget addresses that have gone quiet, so the map does not grow.
	if len(l.seen) > 4096 {
		for k, v := range l.seen {
			if len(v) == 0 || !v[len(v)-1].After(cutoff) {
				delete(l.seen, k)
			}
		}
	}
	return true
}
