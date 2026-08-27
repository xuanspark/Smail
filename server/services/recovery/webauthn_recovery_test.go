package recovery

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"github.com/go-webauthn/webauthn/protocol"
)

type fakeWebAuthnVerifier struct {
	beginCalls  int
	finishCalls int
	resultUser  *models.User
	sessionID   string
	finishErr   error
}

func (f *fakeWebAuthnVerifier) BeginDiscoverableLogin() (*protocol.CredentialAssertion, string, error) {
	f.beginCalls++
	if f.sessionID == "" {
		f.sessionID = "webauthn-recovery-session"
	}
	return &protocol.CredentialAssertion{}, f.sessionID, nil
}

func (f *fakeWebAuthnVerifier) FinishDiscoverableLogin(sessionID string, request *http.Request) (*models.User, *models.WebAuthnCredential, error) {
	f.finishCalls++
	if f.finishErr != nil {
		return nil, nil, f.finishErr
	}
	if request == nil || sessionID == "" || sessionID != f.sessionID || f.resultUser == nil {
		return nil, nil, errors.New("invalid assertion")
	}
	return f.resultUser, &models.WebAuthnCredential{UserID: f.resultUser.ID}, nil
}

func TestWebAuthnRecoveryIssuesOneTimeTicketAndPreservesCredentials(t *testing.T) {
	service, _, engine, user := newRecoveryFixture(t)
	verifier := &fakeWebAuthnVerifier{resultUser: user}
	service.SetWebAuthnVerifier(verifier)

	if err := service.credentials.SetSecurityQuestion(user.ID, "First pet?", "BlueBird123"); err != nil {
		t.Fatal(err)
	}
	codes, err := service.credentials.GenerateRecoveryCodes(user.ID, 5)
	if err != nil || len(codes) != 5 {
		t.Fatalf("recovery codes: %v, %v", codes, err)
	}
	if _, err := engine.Insert(&models.WebAuthnCredential{UserID: user.ID, CredentialID: "credential-id", CredentialData: "enc:v1:opaque", DisplayName: "Key", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	options, sessionID, err := service.BeginWebAuthn(user.Account)
	if err != nil || options == nil || sessionID == "" || verifier.beginCalls != 1 {
		t.Fatalf("begin webauthn recovery: options=%v session=%q calls=%d err=%v", options, sessionID, verifier.beginCalls, err)
	}
	ticketID, err := service.VerifyWebAuthn(user.Account, sessionID, &http.Request{})
	if err != nil || ticketID == "" || verifier.finishCalls != 1 {
		t.Fatalf("verify webauthn recovery: ticket=%q calls=%d err=%v", ticketID, verifier.finishCalls, err)
	}
	if err := service.Complete(ticketID, "WebAuthnPassword@123"); err != nil {
		t.Fatal(err)
	}
	if err := service.Complete(ticketID, "ReplayPassword@123"); !errors.Is(err, ErrRecoveryExpired) {
		t.Fatalf("replayed ticket error = %v", err)
	}

	if count, err := engine.Where("user_id = ?", user.ID).Count(&models.SecurityQuestion{}); err != nil || count != 1 {
		t.Fatalf("security questions count=%d err=%v", count, err)
	}
	if count, err := engine.Where("user_id = ? AND used_at IS NULL", user.ID).Count(&models.RecoveryCode{}); err != nil || count != 5 {
		t.Fatalf("active recovery codes count=%d err=%v", count, err)
	}
	if count, err := engine.Where("user_id = ? AND revoked_at IS NULL", user.ID).Count(&models.WebAuthnCredential{}); err != nil || count != 1 {
		t.Fatalf("active webauthn credentials count=%d err=%v", count, err)
	}
}

func TestWebAuthnRecoveryTicketExpiresAndSuperAdminIsDenied(t *testing.T) {
	service, _, engine, user := newRecoveryFixture(t)
	verifier := &fakeWebAuthnVerifier{resultUser: user}
	service.SetWebAuthnVerifier(verifier)

	_, sessionID, err := service.BeginWebAuthn(user.Account)
	if err != nil {
		t.Fatal(err)
	}
	ticketID, err := service.VerifyWebAuthn(user.Account, sessionID, &http.Request{})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Now().Add(authorizationTTL + time.Second) }
	if err := service.Complete(ticketID, "ExpiredPassword@123"); !errors.Is(err, ErrRecoveryExpired) {
		t.Fatalf("expired ticket error = %v", err)
	}

	super := &models.User{Account: "root-webauthn", Name: "Root", Password: "unused", Role: "super_admin", SessionVersion: 1}
	if _, err := engine.Insert(super); err != nil {
		t.Fatal(err)
	}
	verifier.resultUser = super
	_, superSessionID, err := service.BeginWebAuthn(super.Account)
	if err != nil {
		t.Fatalf("super administrator begin error = %v", err)
	}
	if _, err := service.VerifyWebAuthn(super.Account, superSessionID, &http.Request{}); !errors.Is(err, ErrRecoveryDenied) {
		t.Fatalf("super administrator verification error = %v", err)
	}
}

func TestWebAuthnRecoveryRejectsFailedAssertion(t *testing.T) {
	service, _, _, user := newRecoveryFixture(t)
	verifier := &fakeWebAuthnVerifier{resultUser: user, finishErr: errors.New("signature rejected")}
	service.SetWebAuthnVerifier(verifier)
	_, sessionID, err := service.BeginWebAuthn(user.Account)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyWebAuthn(user.Account, sessionID, &http.Request{}); !errors.Is(err, ErrRecoveryDenied) {
		t.Fatalf("failed assertion error = %v", err)
	}
}
