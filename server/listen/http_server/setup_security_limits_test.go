package http_server

import (
	"fmt"
	"testing"
	"time"
)

func TestSetupSecurityCleansExpiredAttempts(t *testing.T) {
	security := withSetupSecurity(t, "0123456789abcdefghijklmnop")
	security.attempts["expired"] = setupAttempt{
		failures:     5,
		started:      time.Now().Add(-30 * time.Minute),
		blockedUntil: time.Now().Add(-time.Minute),
	}
	security.attempts["active"] = setupAttempt{
		failures:     5,
		started:      time.Now(),
		blockedUntil: time.Now().Add(time.Minute),
	}

	if !security.isBlocked("active") {
		t.Fatal("active setup block was removed")
	}
	if _, exists := security.attempts["expired"]; exists {
		t.Fatal("expired setup attempt was not removed")
	}
}

func TestSetupSecurityAttemptCapacity(t *testing.T) {
	security := withSetupSecurity(t, "0123456789abcdefghijklmnop")
	now := time.Now()
	for i := 0; i < setupMaxAttempts; i++ {
		security.attempts[fmt.Sprintf("client-%d", i)] = setupAttempt{
			failures: 1,
			started:  now.Add(time.Duration(i) * time.Nanosecond),
		}
	}

	security.recordFailure("new-client")
	if len(security.attempts) != setupMaxAttempts {
		t.Fatalf("unexpected setup attempt count: %d", len(security.attempts))
	}
	if _, exists := security.attempts["new-client"]; !exists {
		t.Fatal("new setup attempt was not recorded")
	}
	if _, exists := security.attempts["client-0"]; exists {
		t.Fatal("oldest setup attempt was not evicted")
	}
}
