package verification

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

func TestRandomCodeRejectsThreeOccurrences(t *testing.T) {
	cases := []struct {
		name   string
		digits []byte
	}{
		{name: "33626000", digits: []byte{3, 3, 6, 2, 6, 0, 0, 0}},
		{name: "separated", digits: []byte{1, 2, 1, 3, 1, 4, 5, 6}},
		{name: "identical", digits: []byte{7, 7, 7, 7, 7, 7, 7, 7}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := make([]byte, DefaultCodeLength*4)
			copy(raw, tc.digits)
			copy(raw[DefaultCodeLength*2:], []byte{0, 0, 1, 1, 2, 2, 3, 3})
			code, err := randomCodeFromReader(DefaultCodeLength, bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if code != "00112233" {
				t.Fatalf("code = %q, want 00112233", code)
			}
		})
	}
}

func TestRandomCodeSupportedLengths(t *testing.T) {
	for length := 1; length <= 12; length++ {
		raw := bytes.Repeat([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, 3)
		code, err := randomCodeFromReader(length, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != length {
			t.Fatalf("code length = %d, want %d", len(code), length)
		}
		var counts [10]int
		for _, digit := range []byte(code) {
			if digit < '0' || digit > '9' {
				t.Fatal("non-digit code")
			}
			counts[digit-'0']++
			if counts[digit-'0'] > 2 {
				t.Fatal("digit occurred more than twice")
			}
		}
	}
}

func TestRandomCodeRejectsBiasedBytes(t *testing.T) {
	raw := []byte{250, 251, 252, 253, 254, 255, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4}
	code, err := randomCodeFromReader(DefaultCodeLength, bytes.NewReader(raw))
	if err != nil || code != "00112233" {
		t.Fatalf("code = %q, error = %v", code, err)
	}
}

func TestRandomCodeReaderFailure(t *testing.T) {
	for _, raw := range [][]byte{nil, {1}, make([]byte, DefaultCodeLength*2)} {
		code, err := randomCodeFromReader(DefaultCodeLength, bytes.NewReader(raw))
		if code != "" || (!errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF)) {
			t.Fatalf("code = %q, error = %v", code, err)
		}
	}
}

func TestRandomCodeRejectsInvalidLength(t *testing.T) {
	for _, length := range []int{-1, 0, 13} {
		code, err := randomCodeFromReader(length, bytes.NewReader(nil))
		if code != "" || !errors.Is(err, ErrInvalidChallenge) {
			t.Fatalf("length = %d, code = %q, error = %v", length, code, err)
		}
	}
}

func TestStoreIssuesAndConsumesCodeOnlyOnce(t *testing.T) {
	store, err := NewStore(4)
	if err != nil {
		t.Fatal(err)
	}
	var issuedCode string
	challenge, err := store.Issue(IssueRequest{Purpose: PurposeLogin, Account: "smail", Email: "User@Example.com"}, func(challenge Challenge, code string) error {
		issuedCode = code
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if issuedCode == "" || store.Size() != 1 {
		t.Fatal("challenge was not retained in memory")
	}
	if err := store.Verify(challenge.ID, "smail", "user@example.com", issuedCode, PurposeLogin); err != nil {
		t.Fatal(err)
	}
	if store.Size() != 0 {
		t.Fatal("verified challenge was not consumed")
	}
	if err := store.Verify(challenge.ID, "smail", "user@example.com", issuedCode, PurposeLogin); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("replayed challenge was accepted: %v", err)
	}
}

func TestStoreLimitsAttemptsAndExpires(t *testing.T) {
	store, err := NewStore(4)
	if err != nil {
		t.Fatal(err)
	}
	var code string
	challenge, err := store.Issue(IssueRequest{Purpose: PurposePasswordRecover, Account: "smail", Email: "user@example.com", TTL: time.Second, MaxAttempts: 2}, func(_ Challenge, issued string) error {
		code = issued
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongCode := "000000"
	if code == wrongCode {
		wrongCode = "111111"
	}
	if err := store.Verify(challenge.ID, "smail", "user@example.com", wrongCode, PurposePasswordRecover); err == nil {
		t.Fatal("wrong code was accepted")
	}
	if err := store.Verify(challenge.ID, "smail", "user@example.com", wrongCode, PurposePasswordRecover); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("wrong code result = %v", err)
	}
	if err := store.Verify(challenge.ID, "smail", "user@example.com", code, PurposePasswordRecover); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("replayed exhausted challenge result = %v", err)
	}
	challenge, err = store.Issue(IssueRequest{Purpose: PurposeLogin, Account: "smail", Email: "user@example.com", TTL: time.Nanosecond}, func(_ Challenge, _ string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if err := store.Verify(challenge.ID, "smail", "user@example.com", code, PurposeLogin); !errors.Is(err, ErrInvalidChallenge) && !errors.Is(err, ErrExpiredChallenge) {
		t.Fatalf("expired challenge result = %v", err)
	}
}

func TestStoreRestartInvalidatesAllChallenges(t *testing.T) {
	store, err := NewStore(4)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := store.Issue(IssueRequest{Purpose: PurposeLogin, Account: "smail", Email: "user@example.com"}, func(_ Challenge, _ string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	store.ResetForProcessRestart()
	if store.Size() != 0 {
		t.Fatal("restart did not clear in-memory challenges")
	}
	if err := store.Verify(challenge.ID, "smail", "user@example.com", "000000", PurposeLogin); err == nil {
		t.Fatal("challenge survived process restart")
	}
}

func TestInvalidateAccountOnlyRemovesTargetChallenges(t *testing.T) {
	store, err := NewStore(4)
	if err != nil {
		t.Fatal(err)
	}
	codes := make(map[string]string)
	delivery := func(challenge Challenge, code string) error {
		codes[challenge.Account] = code
		return nil
	}
	target, err := store.Issue(IssueRequest{Purpose: PurposeLogin, Account: "root", Email: "root@example.com"}, delivery)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.Issue(IssueRequest{Purpose: PurposeLogin, Account: "alice", Email: "alice@example.com"}, delivery)
	if err != nil {
		t.Fatal(err)
	}
	if removed := store.InvalidateAccount("root"); removed != 1 {
		t.Fatalf("removed challenges = %d", removed)
	}
	if err := store.Verify(target.ID, "root", "root@example.com", codes["root"], PurposeLogin); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("target challenge survived invalidation: %v", err)
	}
	if err := store.Verify(other.ID, "alice", "alice@example.com", codes["alice"], PurposeLogin); err != nil {
		t.Fatalf("other challenge was invalidated: %v", err)
	}
}

func TestStoreUsesEightDigitCodesAndReissueInvalidatesPrevious(t *testing.T) {
	store, err := NewStore(8)
	if err != nil {
		t.Fatal(err)
	}
	var firstCode string
	first, err := store.Issue(IssueRequest{Purpose: PurposeLogin, Account: "smail", Email: "user@example.com"}, func(_ Challenge, code string) error {
		firstCode = code
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstCode) != DefaultCodeLength {
		t.Fatalf("code length = %d, want %d", len(firstCode), DefaultCodeLength)
	}
	var secondCode string
	second, err := store.Issue(IssueRequest{Purpose: PurposeLogin, Account: "smail", Email: "user@example.com"}, func(_ Challenge, code string) error {
		secondCode = code
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || store.Size() != 1 {
		t.Fatal("reissue did not replace the previous challenge")
	}
	if err := store.Verify(first.ID, "smail", "user@example.com", firstCode, PurposeLogin); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("old challenge result = %v", err)
	}
	if err := store.Verify(second.ID, "smail", "user@example.com", secondCode, PurposeLogin); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsDifferentBrowserContextWithoutConsumingChallenge(t *testing.T) {
	store, err := NewStore(4)
	if err != nil {
		t.Fatal(err)
	}
	var code string
	challenge, err := store.Issue(IssueRequest{
		Purpose:            PurposePasswordRecover,
		Account:            "smail",
		Email:              "user@example.com",
		BrowserTransaction: "browser-a",
	}, func(_ Challenge, issued string) error {
		code = issued
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyBoundWithContext(challenge.ID, "smail", "user@example.com", code, PurposePasswordRecover, 0, "browser-b"); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("different browser context result = %v", err)
	}
	if store.Size() != 1 {
		t.Fatal("context mismatch consumed the challenge")
	}
	if err := store.VerifyBoundWithContext(challenge.ID, "smail", "user@example.com", code, PurposePasswordRecover, 0, "browser-a"); err != nil {
		t.Fatalf("matching browser context result = %v", err)
	}
}
