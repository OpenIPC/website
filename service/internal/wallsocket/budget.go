package wallsocket

import (
	"math"
	"sync"
	"time"
)

// Budget counts frames per address per hour the way WallChannel did, so the
// numbers stay comparable: fixed clock-hour buckets, with the previous bucket
// counted in proportion to how much of it still lies inside the trailing hour
// (the standard sliding-window approximation, which over-counts a burst at
// the very start of the previous bucket -- the safe direction for a ceiling).
// Reserve first, then decide, then refund a frame that was never sent.
//
// In Rails this lived in each Puma worker's FileStore and reset on every
// deploy, so the real ceiling was workers x Limit. One Go process makes Limit
// true for the first time, which is a tightening; until the per-address
// distribution has been measured, Charge only reports what would be refused.
type Budget struct {
	Limit  int
	Window time.Duration

	mu      sync.Mutex
	buckets map[string]*addressBuckets
}

type addressBuckets struct {
	bucket   int64
	current  int
	previous int
}

func (b *Budget) window() time.Duration {
	if b.Window <= 0 {
		return time.Hour
	}
	return b.Window
}

func (b *Budget) at(ip string, now time.Time) *addressBuckets {
	if b.buckets == nil {
		b.buckets = map[string]*addressBuckets{}
	}
	bucket := now.Unix() / int64(b.window().Seconds())
	a := b.buckets[ip]
	if a == nil {
		a = &addressBuckets{bucket: bucket}
		b.buckets[ip] = a
	}
	switch {
	case bucket == a.bucket+1:
		a.previous, a.current, a.bucket = a.current, 0, bucket
	case bucket > a.bucket+1:
		a.previous, a.current, a.bucket = 0, 0, bucket
	}
	if len(b.buckets) > 20000 { // forget addresses that have gone quiet
		for k, v := range b.buckets {
			if v.bucket < bucket-1 {
				delete(b.buckets, k)
			}
		}
	}
	return a
}

// Charge takes one frame for ip and reports whether the address is now over
// the limit, and what it has spent across the window.
func (b *Budget) Charge(ip string, now time.Time) (bool, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := b.at(ip, now)
	a.current++
	secs := int64(b.window().Seconds())
	elapsed := float64(now.Unix()%secs) / float64(secs)
	spent := a.current + int(math.Round(float64(a.previous)*(1.0-elapsed)))
	return spent > b.Limit, spent
}

// Refund gives back a frame that turned out to have nothing behind it.
func (b *Budget) Refund(ip string, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if a := b.at(ip, now); a.current > 0 {
		a.current--
	}
}
