package credentials

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"git.pcux.cn/xuans/Smail/models"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func newFixture(t *testing.T) *Service {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "credentials.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	if err := engine.Sync2(&models.User{}, &models.SecurityQuestion{}, &models.RecoveryCode{}, &models.WebAuthnCredential{}, &models.ProtocolCredential{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Insert(&models.User{Account: "alice", Name: "Alice", Password: "unused", Role: "user"}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestSecurityQuestionSupportsSingleCharacterAndUnlimited(t *testing.T) {
	service := newFixture(t)
	if err := service.SetSecurityQuestion(1, "Q", "A"); err != nil {
		t.Fatalf("setting 1 char question and answer failed: %v", err)
	}
	question, ok, err := service.GetSecurityQuestion(1)
	if err != nil || !ok || question != "Q" {
		t.Fatalf("question = %q, ok=%v, err=%v", question, ok, err)
	}
	if verified, err := service.VerifySecurityQuestion(1, "A"); err != nil || !verified {
		t.Fatalf("verification failed: %v, %v", verified, err)
	}
	if err := service.SetSecurityQuestion(1, " ", "A"); !errors.Is(err, ErrInvalidQuestion) {
		t.Fatalf("want ErrInvalidQuestion, got: %v", err)
	}
	if err := service.SetSecurityQuestion(1, "Q", " "); !errors.Is(err, ErrInvalidAnswer) {
		t.Fatalf("want ErrInvalidAnswer, got: %v", err)
	}
	if err := service.SetSecurityQuestion(1, "UpdatedQ", "UpdatedA"); err != nil {
		t.Fatalf("update security question failed: %v", err)
	}
	updatedQuestion, ok, err := service.GetSecurityQuestion(1)
	if err != nil || !ok || updatedQuestion != "UpdatedQ" {
		t.Fatalf("updated question = %q, ok=%v, err=%v", updatedQuestion, ok, err)
	}
	if verified, err := service.VerifySecurityQuestion(1, "UpdatedA"); err != nil || !verified {
		t.Fatalf("verification of updated answer failed: %v, %v", verified, err)
	}
}

func TestSecurityQuestionIsCaseInsensitiveAndNeverReturnsHash(t *testing.T) {
	service := newFixture(t)
	if err := service.SetSecurityQuestion(1, "第一只宠物叫什么？", "BlueBird123"); err != nil {
		t.Fatal(err)
	}
	question, ok, err := service.GetSecurityQuestion(1)
	if err != nil || !ok || question == "" {
		t.Fatalf("question = %q, ok=%v, err=%v", question, ok, err)
	}
	if verified, err := service.VerifySecurityQuestion(1, " bluebird123 "); err != nil || !verified {
		t.Fatalf("verify = %v, %v", verified, err)
	}
	if verified, err := service.VerifySecurityQuestion(1, "wronganswer"); err != nil || verified {
		t.Fatalf("wrong answer = %v, %v", verified, err)
	}
	var stored models.SecurityQuestion
	if _, err := service.engine.ID(1).Get(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.AnswerHash == "BlueBird123" || stored.AnswerHash == "bluebird123" {
		t.Fatal("answer was stored in plaintext")
	}
}

func TestRecoveryCodesAreShownOnceAndConsumedAtomically(t *testing.T) {
	service := newFixture(t)
	codes, err := service.GenerateRecoveryCodes(1, 5)
	if err != nil || len(codes) != 5 {
		t.Fatalf("codes = %v, err=%v", codes, err)
	}
	var stored []models.RecoveryCode
	if err := service.engine.Find(&stored); err != nil {
		t.Fatal(err)
	}
	for _, item := range stored {
		if item.CodeHash == codes[0] {
			t.Fatal("recovery code was stored in plaintext")
		}
	}
	if err := service.ConsumeRecoveryCode(1, codes[0]); err != nil {
		t.Fatal(err)
	}
	if err := service.ConsumeRecoveryCode(1, codes[0]); !errors.Is(err, ErrCodeNotFound) {
		t.Fatalf("replay error = %v", err)
	}
}

func TestRecoveryCodeConcurrentConsumptionHasOneWinner(t *testing.T) {
	service := newFixture(t)
	codes, err := service.GenerateRecoveryCodes(1, 5)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- service.ConsumeRecoveryCode(1, codes[0])
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrCodeNotFound) && !errors.Is(err, ErrCodeConsumed) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d", winners)
	}
}

func TestProtocolCredentialLifecycle(t *testing.T) {
	service := newFixture(t)
	if err := service.SetProtocolEnabled(1, ProtocolSMTP, true); err != nil {
		t.Fatal(err)
	}
	statuses, err := service.ListProtocolCredentials(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 3 || !statuses[2].Enabled || statuses[2].Configured {
		t.Fatalf("unexpected initial protocol status: %+v", statuses)
	}
	if _, err := service.GenerateProtocolCode(1, ProtocolIMAP); !errors.Is(err, ErrProtocolDisabled) {
		t.Fatalf("generating a disabled protocol code error = %v", err)
	}
	code, err := service.GenerateProtocolCode(1, ProtocolSMTP)
	if err != nil {
		t.Fatal(err)
	}
	if code == "" || strings.Contains(code, " ") {
		t.Fatalf("invalid generated protocol code %q", code)
	}
	if !service.VerifyProtocolCode(1, ProtocolSMTP, code) {
		t.Fatal("generated protocol code was rejected")
	}
	if service.VerifyProtocolCode(1, ProtocolSMTP, "wrong-code") {
		t.Fatal("wrong protocol code was accepted")
	}
	if err := service.RevokeProtocolCode(1, ProtocolSMTP); err != nil {
		t.Fatal(err)
	}
	if service.VerifyProtocolCode(1, ProtocolSMTP, code) {
		t.Fatal("revoked protocol code was accepted")
	}
}
