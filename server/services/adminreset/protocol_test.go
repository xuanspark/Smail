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

func TestProtocolRejectsOversizedFrame(t *testing.T) {
	input := bytes.Repeat([]byte{'x'}, maxControlMessageSize+1)
	if _, err := DecodeRequest(bytes.NewReader(input)); err == nil {
		t.Fatal("oversized request was accepted")
	}
}
