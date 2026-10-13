package middleware

import (
	"sync"
	"time"
)

type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]rateBucket
}

type rateBucket struct {
	start time.Time
	count int
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, buckets: make(map[string]rateBucket)}
}

func (l *RateLimiter) Allow(keys ...string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	allowed := true
	for _, key := range keys {
		bucket := l.buckets[key]
		if now.Sub(bucket.start) >= l.window {
			bucket = rateBucket{start: now}
		}
		if bucket.count >= l.limit {
			allowed = false
		}
		l.buckets[key] = bucket
	}
	if allowed {
		for _, key := range keys {
			bucket := l.buckets[key]
			bucket.count++
			l.buckets[key] = bucket
		}
	}
	if len(l.buckets) > 4096 {
		for key, bucket := range l.buckets {
			if now.Sub(bucket.start) >= l.window {
				delete(l.buckets, key)
			}
		}
	}
	return allowed
}
