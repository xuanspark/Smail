package verification

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func newRateLimitEngine(t *testing.T) *xorm.Engine {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "rate-limit.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Sync2(&models.VerificationRateLimit{}); err != nil {
		_ = engine.Close()
		t.Fatal(err)
	}
	return engine
}

func TestPersistentRateLimiterSurvivesServiceRestart(t *testing.T) {
	engine := newRateLimitEngine(t)
	defer engine.Close()
	limiter, err := NewPersistentRateLimiter(engine)
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewStore(32)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(32)
	if err != nil {
		t.Fatal(err)
	}
	service1, err := NewService(first, func(_ Challenge, _ string) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	service2, err := NewService(second, func(_ Challenge, _ string) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	service1.SetRateLimitStore(limiter)
	service2.SetRateLimitStore(limiter)
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	service1.now = func() time.Time { return now }
	service2.now = func() time.Time { return now }
	for i := 0; i < maxPerHour; i++ {
		if _, err := service1.Issue(IssueRequest{Purpose: PurposeLogin, Account: "smail", Email: "owner@example.com", TTL: time.Second}, "[2001:0db8::1]:443"); err != nil {
			t.Fatalf("issue %d: %v", i, err)
		}
		now = now.Add(issueInterval)
	}
	if _, err := service2.Issue(IssueRequest{Purpose: PurposeLogin, Account: "smail", Email: "owner@example.com"}, "2001:db8::1"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("persistent account limit was bypassed after restart: %v", err)
	}
	if _, err := limiter.Cleanup(context.Background(), now); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentRateLimiterAppliesIPLimitAcrossIdentities(t *testing.T) {
	engine := newRateLimitEngine(t)
	defer engine.Close()
	limiter, err := NewPersistentRateLimiter(engine)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	for i := 0; i < maxPerIPHour; i++ {
		allowed, err := limiter.Allow(context.Background(), RateLimitRequest{
			Account:  "user-" + string(rune('a'+i)),
			Email:    "user-" + string(rune('a'+i)) + "@example.com",
			RemoteIP: "192.0.2.10",
			Purpose:  PurposeLogin,
		}, now)
		if err != nil || !allowed {
			t.Fatalf("allow %d = %v, %v", i, allowed, err)
		}
		now = now.Add(issueInterval)
	}
	allowed, err := limiter.Allow(context.Background(), RateLimitRequest{Account: "other", Email: "other@example.com", RemoteIP: "192.0.2.10", Purpose: PurposePasswordRecover}, now)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("IP limit was bypassed by changing account, email, and purpose")
	}
}

func TestPersistentRateLimiterDoesNotPersistSensitiveValues(t *testing.T) {
	engine := newRateLimitEngine(t)
	defer engine.Close()
	limiter, err := NewPersistentRateLimiter(engine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limiter.Allow(context.Background(), RateLimitRequest{Account: "smail", Email: "owner@example.com", RemoteIP: "127.0.0.1", Purpose: PurposePasswordRecover}, time.Now()); err != nil {
		t.Fatal(err)
	}
	var raw string
	if _, err := engine.SQL("SELECT dimension_key || '|' || dimension_type FROM verification_rate_limit").Get(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "" || containsAny(raw, "smail", "owner@example.com", "127.0.0.1") {
		t.Fatalf("sensitive value persisted: %q", raw)
	}
}

func containsAny(value string, values ...string) bool {
	for _, item := range values {
		if strings.Contains(value, item) {
			return true
		}
	}
	return false
}
