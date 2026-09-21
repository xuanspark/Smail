package credentials

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/password"
	"xorm.io/xorm/contexts"
)

func seedRecoveryCode(t *testing.T, s *Service) {
	t.Helper()
	if _, err := s.engine.Insert(&models.RecoveryCode{UserID: 1, CodeHash: "preserved-hash", CodeVersion: 1}); err != nil {
		t.Fatal(err)
	}
}

func assertRecoveryCodePreserved(t *testing.T, s *Service) {
	t.Helper()
	var rows []models.RecoveryCode
	if err := s.engine.Find(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].CodeHash != "preserved-hash" || rows[0].UsedAt != nil {
		t.Fatal("取消或失败后旧恢复码发生变化")
	}
}

func TestRecoveryGenerationAlreadyCancelledDoesNotHash(t *testing.T) {
	s := newFixture(t)
	seedRecoveryCode(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	codes, err := s.generateRecoveryCodes(ctx, 1, 10, func(string) (string, error) {
		t.Fatal("取消后不应执行密码计算")
		return "", nil
	})
	if codes != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("取消结果错误：%v", err)
	}
	assertRecoveryCodePreserved(t, s)
}

func TestRecoveryGenerationCancelledBetweenHashesPreservesOldCodes(t *testing.T) {
	for _, cancelAt := range []int{1, 5, 10} {
		t.Run(time.Duration(cancelAt).String(), func(t *testing.T) {
			s := newFixture(t)
			seedRecoveryCode(t, s)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			codes, err := s.generateRecoveryCodes(ctx, 1, 10, func(string) (string, error) {
				calls++
				if calls == cancelAt {
					cancel()
				}
				return "test-hash", nil
			})
			if codes != nil || !errors.Is(err, context.Canceled) || calls != cancelAt {
				t.Fatalf("取消未停止后续计算：调用=%d，错误=%v", calls, err)
			}
			assertRecoveryCodePreserved(t, s)
		})
	}
}

func TestRecoveryGenerationCancelsWhileWaitingForCredentialLock(t *testing.T) {
	s := newFixture(t)
	seedRecoveryCode(t, s)
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	codes, err := s.generateRecoveryCodes(ctx, 1, 10, func(string) (string, error) { return "test-hash", nil })
	if codes != nil || !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("等待凭据锁时未及时取消：%v", err)
	}
	assertRecoveryCodePreserved(t, s)
}

func TestRecoveryGenerationCancelsWhileWaitingForDatabase(t *testing.T) {
	s := newFixture(t)
	seedRecoveryCode(t, s)
	s.engine.SetMaxOpenConns(1)
	conn, err := s.engine.DB().DB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	codes, err := s.generateRecoveryCodes(ctx, 1, 10, func(string) (string, error) { return "test-hash", nil })
	_ = conn.Close()
	if codes != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("数据库等待未取消：%v", err)
	}
	assertRecoveryCodePreserved(t, s)
}

type recoveryCancelHook struct {
	cancel context.CancelFunc
	index  int
	seen   int
}

func (h *recoveryCancelHook) BeforeProcess(c *contexts.ContextHook) (context.Context, error) {
	return c.Ctx, nil
}

func (h *recoveryCancelHook) AfterProcess(c *contexts.ContextHook) error {
	if strings.HasPrefix(c.SQL, "INSERT") {
		h.seen++
		if h.seen == h.index {
			h.cancel()
		}
	}
	return nil
}

func TestRecoveryGenerationCancellationRollsBackRevocationAndInserts(t *testing.T) {
	for _, cancelAt := range []int{1, 10} {
		t.Run(time.Duration(cancelAt).String(), func(t *testing.T) {
			s := newFixture(t)
			seedRecoveryCode(t, s)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hook := &recoveryCancelHook{cancel: cancel, index: cancelAt}
			s.engine.AddHook(hook)
			codes, err := s.generateRecoveryCodes(ctx, 1, 10, func(string) (string, error) { return "test-hash", nil })
			if codes != nil || err == nil || hook.seen != cancelAt {
				t.Fatalf("事务取消失败：%v", err)
			}
			assertRecoveryCodePreserved(t, s)
		})
	}
}

func TestRecoveryGenerationHashFailurePreservesOldCodes(t *testing.T) {
	s := newFixture(t)
	seedRecoveryCode(t, s)
	calls := 0
	codes, err := s.generateRecoveryCodes(context.Background(), 1, 10, func(string) (string, error) {
		calls++
		if calls == 3 {
			return "", password.ErrBusy
		}
		return "test-hash", nil
	})
	if codes != nil || !errors.Is(err, password.ErrBusy) || calls != 3 {
		t.Fatalf("计算失败未保留状态：%v", err)
	}
	assertRecoveryCodePreserved(t, s)
}

func TestRecoveryGenerationContextKeepsTenStrongHashes(t *testing.T) {
	s := newFixture(t)
	codes, err := s.GenerateRecoveryCodesContext(context.Background(), 1, 10)
	if err != nil || len(codes) != 10 {
		t.Fatalf("十枚恢复码生成失败：%v", err)
	}
	var rows []models.RecoveryCode
	if err := s.engine.Find(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 10 {
		t.Fatal("恢复码数量不一致")
	}
	seen := make(map[string]bool)
	for i, row := range rows {
		if !strings.HasPrefix(row.CodeHash, "$argon2id$v=19$m=65536,t=3,p=1$") || row.UsedAt != nil || row.CodeVersion != 1 || seen[codes[i]] {
			t.Fatal("密码参数、版本或随机性发生变化")
		}
		seen[codes[i]] = true
	}
}
