package verification

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestServiceIssuesVerifiesAndAppliesMultiDimensionCooldown(t *testing.T) {
	store, err := NewStore(8)
	if err != nil {
		t.Fatal(err)
	}
	var code string
	service, err := NewService(store, func(_ Challenge, issued string) error {
		code = issued
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixed }
	challenge, err := service.Issue(IssueRequest{Purpose: PurposeLogin, Account: "smail", Email: "user@example.com"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Verify(challenge.ID, "smail", "user@example.com", code, PurposeLogin, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Issue(IssueRequest{Purpose: PurposeLogin, Account: "smail", Email: "user@example.com"}, "127.0.0.1"); err != nil {
		t.Fatalf("second issue error = %v", err)
	}
}

func TestServiceRequiresDelivery(t *testing.T) {
	store, err := NewStore(4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(store, nil, nil); !errors.Is(err, ErrServiceDown) {
		t.Fatalf("error = %v", err)
	}
}

func TestServiceAppliesIndependentAccountEmailAndIPLimits(t *testing.T) {
	store, err := NewStore(64)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, func(_ Challenge, _ string) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	for i := 0; i < maxPerIPHour; i++ {
		if _, err := service.Issue(IssueRequest{Purpose: PurposeLogin, Account: "user", Email: "one@example.com", TTL: time.Second}, "192.0.2.1"); err != nil {
			if i == 0 || !errors.Is(err, ErrRateLimited) {
				t.Fatalf("issue %d error = %v", i, err)
			}
		}
		now = now.Add(issueInterval)
	}
	if _, err := service.Issue(IssueRequest{Purpose: PurposeLogin, Account: "user", Email: "two@example.com"}, "192.0.2.2"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("account limit was bypassed by changing email and IP: %v", err)
	}
}

func TestServiceAppliesIPLimitAcrossAccounts(t *testing.T) {
	store, err := NewStore(128)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, func(_ Challenge, _ string) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	for i := 0; i < maxPerIPHour; i++ {
		if _, err := service.Issue(IssueRequest{Purpose: PurposeLogin, Account: fmt.Sprintf("user-%d", i), Email: fmt.Sprintf("user-%d@example.com", i), TTL: time.Second}, "192.0.2.10"); err != nil {
			t.Fatal(err)
		}
		now = now.Add(issueInterval)
	}
	if _, err := service.Issue(IssueRequest{Purpose: PurposeLogin, Account: "another", Email: "another@example.com"}, "192.0.2.10"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("IP limit was bypassed by changing account and email: %v", err)
	}
}

func TestServiceBindsVerificationToResource(t *testing.T) {
	store, err := NewStore(8)
	if err != nil {
		t.Fatal(err)
	}
	var code string
	service, err := NewService(store, func(_ Challenge, issued string) error {
		code = issued
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := service.Issue(IssueRequest{Purpose: PurposeExternalEmailBind, Account: "smail", Email: "user@example.com", ResourceID: 10}, "192.0.2.30")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.VerifyBound(challenge.ID, "smail", "user@example.com", code, PurposeExternalEmailBind, 11, "192.0.2.30"); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("cross-resource verification error = %v", err)
	}
	if err := service.VerifyBound(challenge.ID, "smail", "user@example.com", code, PurposeExternalEmailBind, 10, "192.0.2.30"); err != nil {
		t.Fatalf("bound verification failed: %v", err)
	}
}
