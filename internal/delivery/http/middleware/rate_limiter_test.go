package middleware

import (
	"testing"
	"time"
)

func TestRateLimiterLimitsEachKey(t *testing.T) {
	limiter := NewRateLimiter(2, time.Minute)
	if !limiter.Allow("ip:1", "user:a") || !limiter.Allow("ip:1", "user:a") {
		t.Fatal("requests under the limit should be allowed")
	}
	if limiter.Allow("ip:1", "user:a") {
		t.Fatal("request over the limit should be denied")
	}
	if limiter.Allow("ip:2", "user:a") {
		t.Fatal("the per-user allowance should apply across IPs")
	}
	if !limiter.Allow("ip:2", "user:b") {
		t.Fatal("a different user should have a separate user allowance")
	}
	if !limiter.Allow("ip:2", "user:c") {
		t.Fatal("second request under the IP limit should be allowed")
	}
	if limiter.Allow("ip:2", "user:d") {
		t.Fatal("the per-IP allowance should apply across users")
	}
}
