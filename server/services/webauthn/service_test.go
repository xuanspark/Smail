package webauthnservice

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/credential"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func newServiceFixture(t *testing.T) (*Service, *xorm.Engine, *models.User) {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "webauthn.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	if err := engine.Sync2(&models.User{}, &models.WebAuthnCredential{}); err != nil {
		t.Fatal(err)
	}
	user := &models.User{Account: "alice", Name: "Alice"}
	if _, err := engine.Insert(user); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(engine, Config{
		RPID:          "example.com",
		RPDisplayName: "Smail",
		RPOrigins:     []string{"https://example.com"},
		VaultRoot:     t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, engine, user
}

func TestCeremonySessionIsInMemoryAndOneTime(t *testing.T) {
	service, engine, user := newServiceFixture(t)
	if _, sessionID, err := service.BeginRegistration(user.ID); err != nil {
		t.Fatal(err)
	} else {
		if sessionID == "" {
			t.Fatal("empty ceremony session id")
		}
		if len(service.sessions) != 1 {
			t.Fatalf("in-memory session count = %d, want 1", len(service.sessions))
		}
		var count int64
		count, err = engine.Where("user_id = ?", user.ID).Count(&models.WebAuthnCredential{})
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("ceremony session was persisted as credential rows: %d", count)
		}
		if _, err := service.takeSession(sessionID, registrationCeremony, user.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := service.takeSession(sessionID, registrationCeremony, user.ID); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("replayed ceremony session error = %v", err)
		}
	}
}

func TestPublicWebAuthnUsesDiscoverableCredentialsWithoutIdentifierList(t *testing.T) {
	service, _, user := newServiceFixture(t)
	creation, registrationSession, err := service.BeginRegistration(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if registrationSession == "" {
		t.Fatal("empty registration session")
	}
	selection := creation.Response.AuthenticatorSelection
	if selection.ResidentKey != "required" || selection.RequireResidentKey == nil || !*selection.RequireResidentKey {
		t.Fatalf("discoverable credential requirement resident=%q legacy=%v", selection.ResidentKey, selection.RequireResidentKey)
	}

	assertion, sessionID, err := service.BeginDiscoverableLogin()
	if err != nil {
		t.Fatal(err)
	}
	if sessionID == "" || len(assertion.Response.Challenge) == 0 {
		t.Fatalf("invalid discoverable login response session=%q challenge=%d", sessionID, len(assertion.Response.Challenge))
	}
	if len(assertion.Response.AllowedCredentials) != 0 {
		t.Fatalf("discoverable login exposed credential identifiers: %d", len(assertion.Response.AllowedCredentials))
	}
	if _, err := service.takeDiscoverableSession(sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.takeDiscoverableSession(sessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("replayed discoverable session error=%v", err)
	}
}

func TestExpiredCeremonySessionIsRemoved(t *testing.T) {
	service, _, user := newServiceFixture(t)
	service.now = func() time.Time { return time.Unix(100, 0) }
	_, sessionID, err := service.BeginRegistration(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Unix(100, 0).Add(ceremonyTTL + time.Second) }
	if _, err := service.takeSession(sessionID, registrationCeremony, user.ID); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expired ceremony error = %v", err)
	}
	if len(service.sessions) != 0 {
		t.Fatal("expired ceremony session was retained")
	}
}

func TestInvalidateUserOnlyRemovesTargetCeremonySessions(t *testing.T) {
	service, _, user := newServiceFixture(t)
	targetToken, err := service.putSession(ceremony{userID: user.ID, kind: loginCeremony, expiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	otherToken, err := service.putSession(ceremony{userID: user.ID + 1, kind: loginCeremony, expiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	service.InvalidateUser(user.ID)
	service.mu.Lock()
	_, targetExists := service.sessions[targetToken]
	_, otherExists := service.sessions[otherToken]
	service.mu.Unlock()
	if targetExists || !otherExists {
		t.Fatalf("unexpected ceremony state: target=%v other=%v", targetExists, otherExists)
	}
}

func TestCredentialDataIsEncryptedAndNeverReturnedByList(t *testing.T) {
	service, engine, user := newServiceFixture(t)
	plain := `{"id":"public-id","publicKey":"public-key"}`
	protected, err := credential.Encrypt(service.vault, plain)
	if err != nil {
		t.Fatal(err)
	}
	record := &models.WebAuthnCredential{UserID: user.ID, CredentialID: "public-id", CredentialData: protected, DisplayName: "主密钥", CreatedAt: time.Now()}
	if _, err := engine.Insert(record); err != nil {
		t.Fatal(err)
	}
	var row struct {
		CredentialData string `xorm:"credential_data"`
	}
	if ok, err := engine.SQL("SELECT credential_data FROM webauthn_credential WHERE id = ?", record.ID).Get(&row); err != nil || !ok {
		t.Fatal(err)
	}
	raw := row.CredentialData
	if raw == plain || !credential.IsEncrypted(raw) {
		t.Fatalf("credential data is not encrypted at rest: %q", raw)
	}
	items, err := service.ListCredentials(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].CredentialData != "" {
		t.Fatalf("credential list leaked protected data: %+v", items)
	}
}

func TestRevokedCredentialIsExcludedFromLoginAndCannotBeRevokedTwice(t *testing.T) {
	service, engine, user := newServiceFixture(t)
	record := &models.WebAuthnCredential{UserID: user.ID, CredentialID: "revoked-id", CredentialData: "enc:v1:opaque", DisplayName: "旧密钥", CreatedAt: time.Now()}
	if _, err := engine.Insert(record); err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeCredential(user.ID, record.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeCredential(user.ID, record.ID); !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("second revoke error = %v", err)
	}
	if _, _, err := service.BeginLogin(user.ID); err == nil {
		t.Fatal("revoked credential was accepted for login")
	}
}

func TestWebAuthnAdapterUsesStableUserHandleAndOnlyOwnedCredentials(t *testing.T) {
	service, _, user := newServiceFixture(t)
	adapter, err := service.loadWebAuthnUser(user)
	if err != nil {
		t.Fatal(err)
	}
	if string(adapter.WebAuthnID()) != "smail-user-1" {
		t.Fatalf("unexpected user handle: %q", adapter.WebAuthnID())
	}
	if len(adapter.WebAuthnCredentials()) != 0 {
		t.Fatal("adapter loaded credentials from another user")
	}
}

var _ webauthn.User = (*webAuthnUser)(nil)

func TestDiscoverableCredentialFilterAndMigrationMetadata(t *testing.T) {
	service, engine, user := newServiceFixture(t)
	legacy := webauthn.Credential{ID: []byte("legacy-id")}
	current := webauthn.Credential{ID: []byte("current-id")}
	legacyJSON, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	currentJSON, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	legacyProtected, err := credential.Encrypt(service.vault, string(legacyJSON))
	if err != nil {
		t.Fatal(err)
	}
	currentProtected, err := credential.Encrypt(service.vault, string(currentJSON))
	if err != nil {
		t.Fatal(err)
	}
	legacyRecord := &models.WebAuthnCredential{
		UserID: user.ID, CredentialID: base64.RawURLEncoding.EncodeToString(legacy.ID),
		CredentialData: legacyProtected, DisplayName: "legacy", CreatedAt: time.Now(), Discoverable: false,
	}
	currentRecord := &models.WebAuthnCredential{
		UserID: user.ID, CredentialID: base64.RawURLEncoding.EncodeToString(current.ID),
		CredentialData: currentProtected, DisplayName: "current", CreatedAt: time.Now(), Discoverable: true,
	}
	if _, err := engine.Insert(legacyRecord, currentRecord); err != nil {
		t.Fatal(err)
	}
	items, err := service.ListCredentials(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Discoverable || !items[1].Discoverable {
		t.Fatalf("credential discoverability metadata = %+v", items)
	}
	if _, err := service.loadDiscoverableUser(legacy.ID, []byte("smail-user-1")); !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("legacy credential was accepted for discoverable login: %v", err)
	}
	adapter, err := service.loadDiscoverableUser(current.ID, []byte("smail-user-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(adapter.WebAuthnCredentials()) != 1 || !bytes.Equal(adapter.WebAuthnCredentials()[0].ID, current.ID) {
		t.Fatalf("discoverable adapter credentials = %+v", adapter.WebAuthnCredentials())
	}
}

func TestCeremonySessionCapacityAndExpiredPruning(t *testing.T) {
	service, _, user := newServiceFixture(t)
	service.now = func() time.Time { return time.Unix(100, 0) }
	for index := 0; index < maxSessions; index++ {
		if _, err := service.putSession(ceremony{userID: user.ID, kind: registrationCeremony, expiresAt: service.now().Add(time.Minute)}); err != nil {
			t.Fatalf("put session %d: %v", index, err)
		}
	}
	if _, err := service.putSession(ceremony{userID: user.ID, kind: registrationCeremony, expiresAt: service.now().Add(time.Minute)}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("capacity error = %v", err)
	}
	service.now = func() time.Time { return time.Unix(100, 0).Add(time.Minute + time.Second) }
	if _, err := service.putSession(ceremony{userID: user.ID, kind: registrationCeremony, expiresAt: service.now().Add(time.Minute)}); err != nil {
		t.Fatalf("expired sessions were not pruned: %v", err)
	}
}

func TestDiscoverableCeremonyIsConsumedOnceUnderConcurrency(t *testing.T) {
	service, _, _ := newServiceFixture(t)
	token, err := service.putSession(ceremony{kind: discoverableLoginCeremony, expiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	const workers = 32
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, takeErr := service.takeDiscoverableSession(token); takeErr == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("concurrent ceremony consumption successes = %d, want 1", successes)
	}
}

func TestCeremonyTypeMismatchStillConsumesSession(t *testing.T) {
	service, _, user := newServiceFixture(t)
	token, err := service.putSession(ceremony{userID: user.ID, kind: registrationCeremony, expiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.takeSession(token, loginCeremony, user.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("wrong ceremony type error = %v", err)
	}
	if _, err := service.takeSession(token, registrationCeremony, user.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("wrong ceremony type left a replayable session: %v", err)
	}
}
