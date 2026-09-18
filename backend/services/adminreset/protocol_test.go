package adminreset

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSessionGuardRequiresIssuedNonceAndConsumesOnce(t *testing.T) {
	guard := NewSessionGuard(time.Minute)
	if err := guard.Consume("not-issued"); !errors.Is(err, ErrReplay) {
		t.Fatalf("unissued nonce error = %v", err)
	}
	nonce, err := guard.Issue()
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Consume(nonce); err != nil {
		t.Fatal(err)
	}
	if err := guard.Consume(nonce); !errors.Is(err, ErrReplay) {
		t.Fatalf("replayed nonce error = %v", err)
	}
}

func TestSessionGuardRejectsExpiredNonce(t *testing.T) {
	guard := NewSessionGuard(time.Second)
	now := time.Unix(100, 0)
	guard.now = func() time.Time { return now }
	nonce, err := guard.Issue()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err := guard.Consume(nonce); !errors.Is(err, ErrExpiredSession) {
		t.Fatalf("expired nonce error = %v", err)
	}
}

func TestSessionGuardRejectsNewSessionsWhenCapacityIsFull(t *testing.T) {
	guard := NewSessionGuardWithCapacity(time.Minute, 2)
	if _, err := guard.Issue(); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Issue(); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Issue(); !errors.Is(err, ErrSessionCapacity) {
		t.Fatalf("capacity error = %v", err)
	}
}

func TestSessionGuardPrunesExpiredSessionsBeforeCapacityCheck(t *testing.T) {
	guard := NewSessionGuardWithCapacity(time.Second, 1)
	now := time.Unix(100, 0)
	guard.now = func() time.Time { return now }
	if _, err := guard.Issue(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if _, err := guard.Issue(); err != nil {
		t.Fatalf("expired session blocked a new session: %v", err)
	}
}

func TestProtocolRejectsUnknownFieldsInvalidVersionAndTrailingData(t *testing.T) {
	cases := []string{
		`{"version":1,"nonce":"n","new_password":"Password123","extra":true}`,
		`{"version":2,"nonce":"n","new_password":"Password123"}`,
		`{"version":1,"nonce":"n","new_password":"Password123"} {}`,
	}
	for _, input := range cases {
		if _, err := DecodeRequest(strings.NewReader(input)); err == nil {
			t.Fatalf("invalid request was accepted: %s", input)
		}
	}
}

func TestProtocolEnforcesUnifiedPasswordLength(t *testing.T) {
	valid := []string{
		`{"version":2,"action":"reset","nonce":"n","new_password":"12345678"}`,
		`{"version":2,"action":"reset","nonce":"n","new_password":"123456789012"}`,
		`{"version":2,"action":"reset","nonce":"n","new_password":"密码aa"}`,
	}
	for _, input := range valid {
		if _, err := DecodeRequest(strings.NewReader(input)); err != nil {
			t.Fatalf("valid password request was rejected: %v", err)
		}
	}

	invalid := []string{
		`{"version":2,"action":"reset","nonce":"n","new_password":"1234567"}`,
		`{"version":2,"action":"reset","nonce":"n","new_password":"1234567890123"}`,
		`{"version":2,"action":"reset","nonce":"n","new_password":"密码密码a"}`,
	}
	for _, input := range invalid {
		if _, err := DecodeRequest(strings.NewReader(input)); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid password request returned %v", err)
		}
	}
}

func TestProtocolRejectsOversizedFrame(t *testing.T) {
	input := bytes.Repeat([]byte{'x'}, maxControlMessageSize+1)
	if _, err := DecodeRequest(bytes.NewReader(input)); err == nil {
		t.Fatal("oversized request was accepted")
	}
}
