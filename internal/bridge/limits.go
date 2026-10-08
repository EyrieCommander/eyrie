package bridge

import (
	"sync"
	"time"
)

// limiter holds a token bucket per bearer-token hash and a failed-auth
// counter per client address.
type limiter struct {
	mu      sync.Mutex
	now     func() time.Time
	buckets map[string]*bucket
	fails   map[string]*window
}

type bucket struct {
	tokens float64
	last   time.Time
}

type window struct {
	start time.Time
	n     int
}

func newLimiter() *limiter {
	return &limiter{now: time.Now, buckets: map[string]*bucket{}, fails: map[string]*window{}}
}

// allow takes one token from key's bucket (60/min refill, burst 20). When
// empty it returns how long until the next token.
func (l *limiter) allow(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: rateBurst, last: now}
		l.buckets[key] = b
	}
	rate := float64(ratePerMinute) / 60.0 // per second
	b.tokens += now.Sub(b.last).Seconds() * rate
	if b.tokens > rateBurst {
		b.tokens = rateBurst
	}
	b.last = now
	if b.tokens < 1 {
		need := (1 - b.tokens) / rate
		return time.Duration(need * float64(time.Second)), false
	}
	b.tokens--
	return 0, true
}

// authFail records a failed auth from ip.
func (l *limiter) authFail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w, ok := l.fails[ip]
	if !ok || now.Sub(w.start) >= time.Minute {
		l.fails[ip] = &window{start: now, n: 1}
		l.gc(now)
		return
	}
	w.n++
}

// authBlocked: more than authFailPerMin failures from ip in the current minute.
func (l *limiter) authBlocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.fails[ip]
	if !ok {
		return false
	}
	if l.now().Sub(w.start) >= time.Minute {
		delete(l.fails, ip)
		return false
	}
	return w.n > authFailPerMin
}

// gc drops expired fail windows so a scan from many addresses can't grow
// the map without bound. Called with mu held.
func (l *limiter) gc(now time.Time) {
	if len(l.fails) < 1024 {
		return
	}
	for ip, w := range l.fails {
		if now.Sub(w.start) >= time.Minute {
			delete(l.fails, ip)
		}
	}
}
