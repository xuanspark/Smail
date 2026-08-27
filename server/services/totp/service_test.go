package totp

import (
	"encoding/base32"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func newTestService(t *testing.T) (*Service, *xorm.Engine) {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "totp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	if err := engine.Sync2(&models.TOTPAuthenticator{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	service, err := NewService(engine, root)
	if err != nil {
		t.Fatal(err)
	}
	return service, engine
}

func TestEnrollmentVerifyReplayAndDisable(t *testing.T) {
	service, _ := newTestService(t)
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	enrollment, err := service.BeginEnrollment(1, "root")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	code := generateCode(secret, uint64(now.Unix()/PeriodSeconds))
	if err := service.ConfirmEnrollment(1, enrollment.ID, code); err != nil {
		t.Fatal(err)
	}
	if enabled, err := service.IsEnabled(1); err != nil || !enabled {
		t.Fatalf("enabled=%v err=%v", enabled, err)
	}
	if err := service.Verify(1, code); !errors.Is(err, ErrReplay) {
		t.Fatalf("expected replay error, got %v", err)
	}

	now = now.Add(PeriodSeconds * time.Second)
	nextCode := generateCode(secret, uint64(now.Unix()/PeriodSeconds))
	if err := service.Verify(1, nextCode); err != nil {
		t.Fatal(err)
	}
	now = now.Add(PeriodSeconds * time.Second)
	disableCode := generateCode(secret, uint64(now.Unix()/PeriodSeconds))
	if err := service.Disable(1, disableCode); err != nil {
		t.Fatal(err)
	}
	if enabled, err := service.IsEnabled(1); err != nil || enabled {
		t.Fatalf("enabled=%v err=%v", enabled, err)
	}
}

func TestEnrollmentInvalidCodeLimit(t *testing.T) {
	service, _ := newTestService(t)
	enrollment, err := service.BeginEnrollment(2, "alice")
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 5; attempt++ {
		if err := service.ConfirmEnrollment(2, enrollment.ID, "000000"); !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("attempt %d: %v", attempt+1, err)
		}
	}
	if err := service.ConfirmEnrollment(2, enrollment.ID, "000000"); !errors.Is(err, ErrEnrollmentExpired) {
		t.Fatalf("expected expired enrollment, got %v", err)
	}
}

func TestValidateAtAcceptsAdjacentWindow(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	now := time.Unix(1_800_000_000, 0).UTC()
	previousStep := now.Unix()/PeriodSeconds - 1
	code := generateCode(secret, uint64(previousStep))
	step, ok := validateAt(secret, code, now, 1)
	if !ok || step != previousStep {
		t.Fatalf("step=%d ok=%v", step, ok)
	}
	if _, ok := validateAt(secret, code, now.Add(PeriodSeconds*time.Second), 1); ok {
		t.Fatal("expired adjacent code was accepted")
	}
}

func TestProvisioningURIUsesSHA256(t *testing.T) {
	uri := provisioningURI("root@example.com", "ABCDEF")
	for _, value := range []string{"otpauth://totp/", "issuer=Smail", "algorithm=SHA256", "digits=6", "period=30", "secret=ABCDEF"} {
		if !strings.Contains(uri, value) {
			t.Fatalf("URI %q does not contain %q", uri, value)
		}
	}
}
