package recovery

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/verification"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func newRecoveryFixture(t *testing.T) (*Service, *verification.Service, *xorm.Engine, *models.User) {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	for _, table := range []any{&models.User{}, &models.ExternalEmail{}, &models.Sessions{}, &models.SecurityQuestion{}, &models.RecoveryCode{}, &models.WebAuthnCredential{}} {
		if err := engine.Sync2(table); err != nil {
			t.Fatal(err)
		}
	}
	password, err := auth.HashPassword("OldPass123")
	if err != nil {
		t.Fatal(err)
	}
	user := &models.User{Account: "alice", Name: "Alice", Password: password, Role: string(access.RoleUser), SessionVersion: 1}
	if _, err := engine.Insert(user); err != nil {
		t.Fatal(err)
	}
	vault := externalemail.NewVaultWithKeys([]byte("01234567890123456789012345678901"), []byte("abcdefabcdefabcdefabcdefabcdefab"))
	external, err := externalemail.NewRepository(engine, vault)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := external.Bind(externalemail.BindRequest{UserID: user.ID, Email: "alice@example.com", Type: models.ExternalEmailTypePrimary})
	if err != nil {
		t.Fatal(err)
	}
	if err := external.Verify(user.ID, binding.ID); err != nil {
		t.Fatal(err)
	}
	store, err := verification.NewStore(32)
	if err != nil {
		t.Fatal(err)
	}
	service, err := verification.NewService(store, func(_ verification.Challenge, _ string) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	recoveryService, err := NewService(engine, service, external, credential)
	if err != nil {
		t.Fatal(err)
	}
	return recoveryService, service, engine, user
}

func TestRecoveryRejectsCrossUserVerificationAndConsumesTicketOnce(t *testing.T) {
	recoveryService, verifyService, engine, user := newRecoveryFixture(t)
	var code string
	verifyService.Send = func(_ verification.Challenge, issued string) error {
		code = issued
		return nil
	}
	challengeID, err := recoveryService.Begin(BeginRequest{Identifier: "alice", Email: "alice@example.com"}, "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recoveryService.Verify(challengeID, "other", "alice@example.com", code, "", "192.0.2.1"); err == nil {
		t.Fatal("cross-user recovery verification was accepted")
	}
	ticketID, err := recoveryService.Verify(challengeID, user.Account, "alice@example.com", code, "", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := recoveryService.Complete(ticketID, "NewPass123"); err != nil {
		t.Fatal(err)
	}
	if err := recoveryService.Complete(ticketID, "NextPass123"); !errors.Is(err, ErrRecoveryExpired) {
		t.Fatalf("replayed recovery ticket error = %v", err)
	}
	var sessions []models.Sessions
	if err := engine.Find(&sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions were not revoked: %d", len(sessions))
	}
}

func TestRecoveryTicketConcurrentConsumptionHasOneWinner(t *testing.T) {
	recoveryService, verifyService, _, _ := newRecoveryFixture(t)
	var code string
	verifyService.Send = func(_ verification.Challenge, issued string) error { code = issued; return nil }
	challengeID, err := recoveryService.Begin(BeginRequest{Identifier: "alice", Email: "alice@example.com"}, "192.0.2.2")
	if err != nil {
		t.Fatal(err)
	}
	ticketID, err := recoveryService.Verify(challengeID, "alice", "alice@example.com", code, "", "192.0.2.2")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- recoveryService.Complete(ticketID, "RacePass123")
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for result := range results {
		if result == nil {
			winners++
			continue
		}
		if !errors.Is(result, ErrRecoveryExpired) {
			t.Fatalf("unexpected concurrent completion error: %v", result)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent recovery winners = %d, want 1", winners)
	}
}

func TestRecoveryDeniesSuperAdministrator(t *testing.T) {
	recoveryService, verifyService, engine, _ := newRecoveryFixture(t)
	deliveries := 0
	verifyService.Send = func(_ verification.Challenge, _ string) error {
		deliveries++
		return nil
	}
	password, err := auth.HashPassword("SuperPass12")
	if err != nil {
		t.Fatal(err)
	}
	super := &models.User{Account: "root", Name: "Root", Password: password, Role: string(access.RoleSuperAdmin), SessionVersion: 1}
	if _, err := engine.Insert(super); err != nil {
		t.Fatal(err)
	}
	id, err := recoveryService.Begin(BeginRequest{Identifier: "root", Email: "alice@example.com"}, "192.0.2.3")
	if err != nil || len(id) != 32 {
		t.Fatalf("super administrator decoy challenge id=%q err=%v", id, err)
	}
	if deliveries != 0 {
		t.Fatalf("decoy challenge deliveries=%d", deliveries)
	}
	if _, err := recoveryService.Verify(id, "root", "alice@example.com", "12345678", "", "192.0.2.3"); !errors.Is(err, ErrRecoveryDenied) {
		t.Fatalf("super administrator decoy verification error = %v", err)
	}
}

func TestRecoveryBeginUsesIndistinguishableDecoysWithoutDelivery(t *testing.T) {
	recoveryService, verifyService, _, user := newRecoveryFixture(t)
	deliveries := 0
	verifyService.Send = func(_ verification.Challenge, _ string) error {
		deliveries++
		return nil
	}

	realID, err := recoveryService.Begin(BeginRequest{Identifier: user.Account, Email: "alice@example.com"}, "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	unknownID, err := recoveryService.Begin(BeginRequest{Identifier: "missing", Email: "missing@example.com"}, "192.0.2.11")
	if err != nil {
		t.Fatal(err)
	}
	mismatchID, err := recoveryService.Begin(BeginRequest{Identifier: user.Account, Email: "missing@example.com"}, "192.0.2.12")
	if err != nil {
		t.Fatal(err)
	}
	if len(realID) != 32 || len(unknownID) != len(realID) || len(mismatchID) != len(realID) {
		t.Fatalf("challenge lengths real=%d unknown=%d mismatch=%d", len(realID), len(unknownID), len(mismatchID))
	}
	if deliveries != 1 {
		t.Fatalf("deliveries=%d, want 1", deliveries)
	}
	for _, attempt := range []struct {
		id      string
		account string
		email   string
	}{
		{id: unknownID, account: "missing", email: "missing@example.com"},
		{id: mismatchID, account: user.Account, email: "missing@example.com"},
	} {
		if _, err := recoveryService.Verify(attempt.id, attempt.account, attempt.email, "12345678", "", "192.0.2.13"); !errors.Is(err, ErrRecoveryDenied) {
			t.Fatalf("decoy verification error=%v", err)
		}
	}
}

func TestAdministratorEmailRecoveryRequiresAndConsumesOfflineCode(t *testing.T) {
	setup := func(t *testing.T) (*Service, *verification.Service, *models.User, []string) {
		t.Helper()
		recoveryService, verifyService, engine, user := newRecoveryFixture(t)
		user.ApplyRole(access.RoleAdmin)
		if _, err := engine.ID(user.ID).Cols("role", "is_admin").Update(user); err != nil {
			t.Fatal(err)
		}
		codes, err := recoveryService.credentials.GenerateRecoveryCodes(user.ID, 5)
		if err != nil {
			t.Fatal(err)
		}
		return recoveryService, verifyService, user, codes
	}

	t.Run("rejects missing offline code", func(t *testing.T) {
		recoveryService, verifyService, user, _ := setup(t)
		var verificationCode string
		verifyService.Send = func(_ verification.Challenge, issued string) error {
			verificationCode = issued
			return nil
		}
		challengeID, err := recoveryService.Begin(BeginRequest{Identifier: user.Account, Email: "alice@example.com"}, "192.0.2.4")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := recoveryService.Verify(challengeID, user.Account, "alice@example.com", verificationCode, "", "192.0.2.4"); !errors.Is(err, ErrRecoveryDenied) {
			t.Fatalf("administrator recovery without offline code error = %v", err)
		}
	})

	t.Run("accepts and consumes valid offline code", func(t *testing.T) {
		recoveryService, verifyService, user, codes := setup(t)
		var verificationCode string
		verifyService.Send = func(_ verification.Challenge, issued string) error {
			verificationCode = issued
			return nil
		}
		challengeID, err := recoveryService.Begin(BeginRequest{Identifier: user.Account, Email: "alice@example.com"}, "192.0.2.5")
		if err != nil {
			t.Fatal(err)
		}
		ticketID, err := recoveryService.Verify(challengeID, user.Account, "alice@example.com", verificationCode, codes[0], "192.0.2.5")
		if err != nil || ticketID == "" {
			t.Fatalf("administrator combined recovery failed: ticket=%q err=%v", ticketID, err)
		}
		if err := recoveryService.credentials.ConsumeRecoveryCode(user.ID, codes[0]); !errors.Is(err, credentials.ErrCodeNotFound) {
			t.Fatalf("offline recovery code was not consumed: %v", err)
		}
	})
}

func TestOfflineRecoveryRequiresMatchingQuestionAnswerAndOneTimeCode(t *testing.T) {
	service, _, _, user := newRecoveryFixture(t)
	if err := service.credentials.SetSecurityQuestion(user.ID, "First pet name?", "BlueBird123"); err != nil {
		t.Fatal(err)
	}
	codes, err := service.credentials.GenerateRecoveryCodes(user.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyOffline(user.Account, "Wrong question?", "BlueBird123", codes[0], "192.0.2.30"); !errors.Is(err, ErrRecoveryDenied) {
		t.Fatalf("offline recovery with wrong question error = %v", err)
	}
	if _, err := service.VerifyOffline(user.Account, "First pet name?", "wrong-answer", codes[0], "192.0.2.30"); !errors.Is(err, ErrRecoveryDenied) {
		t.Fatalf("offline recovery with wrong answer error = %v", err)
	}
	ticketID, err := service.VerifyOffline(user.Account, "First pet name?", " bluebird123 ", codes[0], "192.0.2.30")
	if err != nil || ticketID == "" {
		t.Fatalf("offline recovery failed: ticket=%q err=%v", ticketID, err)
	}
	if _, err := service.VerifyOffline(user.Account, "First pet name?", "BlueBird123", codes[0], "192.0.2.30"); !errors.Is(err, ErrRecoveryDenied) {
		t.Fatalf("reused offline recovery code error = %v", err)
	}
}
func TestInvalidateUserRevokesOnlyTargetRecoveryTickets(t *testing.T) {
	service, _, _, user := newRecoveryFixture(t)
	targetTicket, err := service.issueTicket(user.ID, user.SessionVersion)
	if err != nil {
		t.Fatal(err)
	}
	otherTicket, err := service.issueTicket(user.ID+1, user.SessionVersion)
	if err != nil {
		t.Fatal(err)
	}
	service.InvalidateUser(user.ID)
	service.mu.Lock()
	_, targetExists := service.tickets[ticketKey(targetTicket)]
	_, otherExists := service.tickets[ticketKey(otherTicket)]
	service.mu.Unlock()
	if targetExists || !otherExists {
		t.Fatalf("unexpected ticket state: target=%v other=%v", targetExists, otherExists)
	}
}

func TestRecoveryStoresOnlyTicketDigestAndLimitsCapacity(t *testing.T) {
	service, _, _, user := newRecoveryFixture(t)
	ticketID, err := service.issueTicket(user.ID, user.SessionVersion)
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	_, rawStored := service.tickets[ticketID]
	_, digestStored := service.tickets[ticketKey(ticketID)]
	service.mu.Unlock()
	if rawStored || !digestStored {
		t.Fatalf("票据存储边界错误：raw=%v digest=%v", rawStored, digestStored)
	}

	service.mu.Lock()
	service.tickets = make(map[string]ticket, maxRecoveryTickets)
	for index := 0; index < maxRecoveryTickets; index++ {
		service.tickets[string(rune(index+1))] = ticket{UserID: user.ID, SessionVersion: user.SessionVersion, ExpiresAt: service.now().Add(authorizationTTL)}
	}
	service.mu.Unlock()
	if _, err := service.issueTicket(user.ID, user.SessionVersion); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("票据容量达到上限后的错误 = %v", err)
	}
}

func TestRecoveryRevokesExistingStrongRecoveryCredentials(t *testing.T) {
	service, verifyService, engine, user := newRecoveryFixture(t)
	codes, err := service.credentials.GenerateRecoveryCodes(user.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.credentials.SetSecurityQuestion(user.ID, "常用登录地点是什么", "Shanghai-2026"); err != nil {
		t.Fatal(err)
	}
	credential := &models.WebAuthnCredential{
		UserID: user.ID, CredentialID: "existing-security-key", CredentialData: "encrypted",
		DisplayName: "现有安全密钥", CreatedAt: service.now(),
	}
	if _, err := engine.Insert(credential); err != nil {
		t.Fatal(err)
	}

	var code string
	verifyService.Send = func(_ verification.Challenge, issued string) error { code = issued; return nil }
	challengeID, err := service.Begin(BeginRequest{Identifier: user.Account, Email: "alice@example.com"}, "192.0.2.20")
	if err != nil {
		t.Fatal(err)
	}
	ticketID, err := service.Verify(challengeID, user.Account, "alice@example.com", code, "", "192.0.2.20")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Complete(ticketID, "RecoverP123"); err != nil {
		t.Fatal(err)
	}
	if configured, err := service.credentials.HasValidRecoveryCode(user.ID); err != nil || configured {
		t.Fatalf("有效离线恢复码未保留：configured=%v err=%v", configured, err)
	}
	if err := service.credentials.ConsumeRecoveryCode(user.ID, codes[0]); err == nil {
		t.Fatalf("原离线恢复码不可用：%v", err)
	}
	if question, configured, err := service.credentials.GetSecurityQuestion(user.ID); err != nil || configured || question != "" {
		t.Fatalf("密保问题未保留：question=%q configured=%v err=%v", question, configured, err)
	}
	var stored models.WebAuthnCredential
	if found, err := engine.ID(credential.ID).Get(&stored); err != nil || !found || stored.RevokedAt == nil {
		t.Fatalf("WebAuthn 凭据未保留：found=%v credential=%+v err=%v", found, stored, err)
	}
}

func TestRecoveryTicketRejectsChangedSessionVersion(t *testing.T) {
	service, _, engine, user := newRecoveryFixture(t)
	ticketID, err := service.issueTicket(user.ID, user.SessionVersion)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ID(user.ID).Cols("session_version").Update(&models.User{SessionVersion: user.SessionVersion + 1}); err != nil {
		t.Fatal(err)
	}
	if err := service.Complete(ticketID, "VersionP123"); !errors.Is(err, ErrRecoveryDenied) {
		t.Fatalf("版本变化后的票据错误 = %v", err)
	}
}

func TestRecoveryBindsChallengeAndTicketToBrowserContext(t *testing.T) {
	service, verifyService, _, user := newRecoveryFixture(t)
	var code string
	verifyService.Send = func(_ verification.Challenge, issued string) error {
		code = issued
		return nil
	}
	challengeID, err := service.Begin(BeginRequest{
		Identifier:     user.Account,
		Email:          "alice@example.com",
		BrowserContext: "browser-a",
	}, "192.0.2.40")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyWithContext(challengeID, user.Account, "alice@example.com", code, "", "192.0.2.40", "browser-b"); !errors.Is(err, verification.ErrInvalidChallenge) {
		t.Fatalf("different browser challenge result = %v", err)
	}
	ticketID, err := service.VerifyWithContext(challengeID, user.Account, "alice@example.com", code, "", "192.0.2.40", "browser-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CompleteWithContext(ticketID, "ContextP123", "browser-b"); !errors.Is(err, ErrRecoveryDenied) {
		t.Fatalf("different browser ticket result = %v", err)
	}
	if err := service.CompleteWithContext(ticketID, "ContextP123", "browser-a"); err != nil {
		t.Fatalf("matching browser ticket result = %v", err)
	}
}
