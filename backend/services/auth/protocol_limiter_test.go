package auth

import (
	"strings"
	"testing"
	"time"
)

func TestProtocolLoginLimiterBlocksAndExpires(t *testing.T) {
	limiter := newProtocolLoginLimiter(3)
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	key := hashedProtocolLoginKey("pair", "imap", "192.0.2.10", "user")
	if strings.Contains(key, "192.0.2.10") || strings.Contains(key, "user") {
		t.Fatal("protocol limiter key contains identifying input")
	}
	for index := 0; index < 3; index++ {
		limiter.recordFailure(key)
	}
	if !limiter.blocked(key) {
		t.Fatal("protocol login was not blocked after the failure limit")
	}
	now = now.Add(protocolLoginBlockTime + time.Second)
	if limiter.blocked(key) {
		t.Fatal("protocol login block did not expire")
	}
}

func TestProtocolRemoteHostRemovesPort(t *testing.T) {
	if host := protocolRemoteHost("192.0.2.10:12345"); host != "192.0.2.10" {
		t.Fatalf("remote host = %q", host)
	}
	if host := protocolRemoteHost("[2001:db8::1]:993"); host != "2001:db8::1" {
		t.Fatalf("IPv6 remote host = %q", host)
	}
}
