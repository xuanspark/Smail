package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublicSecurityLimitersIsolateIPAndExpire(t *testing.T) {
	limiter := newLoginLimiter(2)
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	first := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/security", nil)
	first.RemoteAddr = "192.0.2.10:1234"
	second := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/security", nil)
	second.RemoteAddr = "192.0.2.11:1234"
	firstKey := limiter.ipKey(first)
	secondKey := limiter.ipKey(second)
	limiter.recordFailure(firstKey)
	if limiter.blocked(firstKey) {
		t.Fatal("public security limiter blocked the first attempt too early")
	}
	limiter.recordFailure(firstKey)
	if !limiter.blocked(firstKey) {
		t.Fatal("public security limiter did not block the repeated IP")
	}
	if limiter.blocked(secondKey) {
		t.Fatal("public security limiter shared state across IPs")
	}
	now = now.Add(loginBlockTime + time.Second)
	if limiter.blocked(firstKey) {
		t.Fatal("public security limiter block did not expire")
	}
}

func TestPublicSecurityLimiterUsesBoundedEntryStore(t *testing.T) {
	limiter := newLoginLimiter(1)
	for index := 0; index < loginMaxEntries+128; index++ {
		limiter.recordFailure("public-ip-key-" + string(rune(index)))
	}
	if len(limiter.attempts) > loginMaxEntries {
		t.Fatalf("public security limiter entries = %d, want <= %d", len(limiter.attempts), loginMaxEntries)
	}
}
