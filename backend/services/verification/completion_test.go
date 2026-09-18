package verification

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func completionService(t *testing.T, persistent bool) (*Service, *string, *time.Time) {
	t.Helper()
	store, err := NewStore(64)
	if err != nil {
		t.Fatal(err)
	}
	var code string
	service, err := NewService(store, func(_ Challenge, value string) error {
		code = value
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	store.now = service.now
	if persistent {
		engine := newRateLimitEngine(t)
		t.Cleanup(func() { _ = engine.Close() })
		limiter, err := NewPersistentRateLimiter(engine)
		if err != nil {
			t.Fatal(err)
		}
		service.SetRateLimitStore(limiter)
	}
	return service, &code, &now
}

func completionRequest(purpose Purpose) IssueRequest {
	return IssueRequest{Purpose: purpose, Account: "owner", Email: "owner@example.com",
		UserID: 7, ResourceID: 9, BrowserTransaction: "browser", Environment: "production", CredentialVersion: 2}
}

func TestCompletionReleasesAllPurposeCooldownsAndPreservesQuota(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(fmt.Sprint(persistent), func(t *testing.T) {
			service, code, _ := completionService(t, persistent)
			purposes := []Purpose{PurposeLogin, PurposePasswordChange, PurposePasswordRecover, PurposeExternalEmailBind, PurposeExternalEmailChange}
			for _, purpose := range purposes {
				request := completionRequest(purpose)
				challenge, err := service.Issue(request, "192.0.2.1")
				if err != nil {
					t.Fatalf("下一业务被已完成的验证阻塞：%s: %v", purpose, err)
				}
				if _, err := service.Issue(request, "192.0.2.1"); !errors.Is(err, ErrRateLimited) {
					t.Fatalf("未使用时应保留发送冷却：%v", err)
				}
				verify := func(value string) error {
					return service.VerifyBoundWithContext(challenge.ID, request.Account, request.Email, value, purpose, request.ResourceID, "192.0.2.99", request.BrowserTransaction)
				}
				if err := verify("invalid"); !errors.Is(err, ErrInvalidChallenge) {
					t.Fatal(err)
				}
				if _, err := service.Issue(request, "192.0.2.1"); !errors.Is(err, ErrRateLimited) {
					t.Fatalf("错误验证码释放了冷却：%v", err)
				}
				if err := verify(*code); err != nil {
					t.Fatal(err)
				}
				if service.Store.Size() != 0 {
					t.Fatal("验证码使用后仍有待验证状态")
				}
				if err := verify(*code); !errors.Is(err, ErrInvalidChallenge) {
					t.Fatalf("已使用验证码被重放：%v", err)
				}
			}
			if _, err := service.Issue(completionRequest(PurposeLogin), "192.0.2.1"); !errors.Is(err, ErrRateLimited) {
				t.Fatalf("成功验证不应清除累计配额：%v", err)
			}
		})
	}
}

func TestCompletionDoesNotReleaseNewerRequest(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(fmt.Sprint(persistent), func(t *testing.T) {
			service, code, now := completionService(t, persistent)
			first, err := service.Issue(completionRequest(PurposeLogin), "192.0.2.1")
			if err != nil {
				t.Fatal(err)
			}
			firstCode := *code
			*now = now.Add(issueInterval)
			second, err := service.Issue(completionRequest(PurposePasswordChange), "192.0.2.1")
			if err != nil {
				t.Fatal(err)
			}
			secondCode := *code
			if err := service.VerifyBoundWithContext(first.ID, first.Account, first.Email, firstCode, first.Purpose, 9, "192.0.2.1", "browser"); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Issue(completionRequest(PurposeExternalEmailChange), "192.0.2.1"); !errors.Is(err, ErrRateLimited) {
				t.Fatalf("旧验证码清除了新请求的冷却：%v", err)
			}
			if err := service.VerifyBoundWithContext(second.ID, second.Account, second.Email, secondCode, second.Purpose, 9, "192.0.2.1", "browser"); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Issue(completionRequest(PurposeExternalEmailChange), "192.0.2.1"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompletionConcurrentVerificationAndPersistentRestart(t *testing.T) {
	service, code, now := completionService(t, true)
	challenge, err := service.Issue(completionRequest(PurposeLogin), "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- service.VerifyBoundWithContext(challenge.ID, challenge.Account, challenge.Email, *code, challenge.Purpose, 9, "192.0.2.1", "browser")
		}()
	}
	group.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("并发验证成功次数：%d", success)
	}
	restarted, _, _ := completionService(t, false)
	restarted.now = func() time.Time { return *now }
	restarted.SetRateLimitStore(service.RateLimits)
	if _, err := restarted.Issue(completionRequest(PurposePasswordChange), "192.0.2.1"); err != nil {
		t.Fatalf("重启后已释放的冷却仍阻塞：%v", err)
	}
}

func TestCompletionKeepsDailyLimit(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(fmt.Sprint(persistent), func(t *testing.T) {
			service, code, now := completionService(t, persistent)
			for i := 0; i < maxPerDay; i++ {
				if i == maxPerHour {
					*now = now.Add(issueWindow)
				}
				challenge, err := service.Issue(completionRequest(PurposeLogin), "192.0.2.1")
				if err != nil {
					t.Fatal(err)
				}
				if err := service.VerifyBoundWithContext(challenge.ID, challenge.Account, challenge.Email, *code, challenge.Purpose, 9, "192.0.2.1", "browser"); err != nil {
					t.Fatal(err)
				}
			}
			*now = now.Add(issueWindow)
			if _, err := service.Issue(completionRequest(PurposeLogin), "192.0.2.1"); !errors.Is(err, ErrRateLimited) {
				t.Fatalf("每日配额被清除：%v", err)
			}
		})
	}
}

func TestCompletionCleanupFailureCanRetryWithoutCompletingBusiness(t *testing.T) {
	service, code, _ := completionService(t, false)
	failing := true
	challenge, err := service.Store.issue(completionRequest(PurposeLogin), service.Send, func() error {
		if failing {
			return ErrRateLimitStoreUnavailable
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	verify := func() error {
		return service.VerifyBoundWithContext(challenge.ID, challenge.Account, challenge.Email, *code, challenge.Purpose, 9, "192.0.2.1", "browser")
	}
	if err := verify(); !errors.Is(err, ErrServiceDown) {
		t.Fatalf("清理失败不应返回业务成功：%v", err)
	}
	failing = false
	if err := verify(); err != nil {
		t.Fatalf("清理恢复后无法重试：%v", err)
	}
	if err := verify(); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("重试成功后仍可重放：%v", err)
	}
}
