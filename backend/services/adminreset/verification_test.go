package adminreset

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/totp"
	"git.pcux.cn/xuans/Smail/services/verification"
	"git.pcux.cn/xuans/Smail/utils/password"
	"xorm.io/xorm"
)

func verificationFixture(t *testing.T) (*VerificationService, models.User, *string) {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "verified-reset.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	if err := engine.Sync2(&models.User{}, &models.ExternalEmail{}, &models.TOTPAuthenticator{}, &models.SecurityQuestion{}, &models.RecoveryCode{}, &models.ProtocolCredential{}, &models.WebAuthnCredential{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Account: "root", Role: "super_admin", SessionVersion: 2, SecurityVersion: 3}
	if err := auth.SetPassword(&user, "OldPass123"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Insert(&user); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	vault, err := externalemail.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	email, err := externalemail.NewRepository(engine, vault)
	if err != nil {
		t.Fatal(err)
	}
	codes, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	authenticator, err := totp.NewService(engine, root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := verification.NewStore(20)
	if err != nil {
		t.Fatal(err)
	}
	var delivered string
	verify, err := verification.NewService(store, func(_ verification.Challenge, code string) error { delivered = code; return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	return NewVerificationService(engine, email, codes, authenticator, verify, nil), user, &delivered
}

func addRecovery(t *testing.T, s *VerificationService, user models.User) string {
	t.Helper()
	code := "ABCD-EFGH-IJKL-MNOP"
	hash, err := password.Hash(code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.engine.Insert(&models.RecoveryCode{UserID: user.ID, CodeHash: hash}); err != nil {
		t.Fatal(err)
	}
	return code
}

func TestVerifiedResetWithoutEmailOverProtocol(t *testing.T) {
	s, user, delivered := verificationFixture(t)
	if _, err := s.Begin(); err == nil {
		t.Fatal("unconfigured account allowed")
	}
	code := addRecovery(t, s, user)
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		handleResetConnection(server, server.SetDeadline, s.Begin, NewSessionGuard(time.Minute))
	}()
	defer client.Close()
	steps := 0
	err := SendVerifiedReset(context.Background(), client, func(string) (string, error) {
		steps++
		if steps == 1 {
			return "1", nil
		}
		return code, nil
	}, func() (string, error) {
		if steps != 2 {
			t.Error("password asked before verification")
		}
		return "NewPass123", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-done
	var updated models.User
	if _, err := s.engine.ID(user.ID).Get(&updated); err != nil {
		t.Fatal(err)
	}
	if !password.Verify(updated.Password, "NewPass123") || updated.SessionVersion != 3 || updated.SecurityVersion != 4 {
		t.Fatal("reset did not commit")
	}
	if ready, err := s.codes.HasValidRecoveryCode(user.ID); err != nil || ready {
		t.Fatal("recovery code not consumed")
	}
	if *delivered != "" {
		t.Fatal("mail sent without selection")
	}
}

func TestVerifiedResetRejectsRevokedCredentialAndExpiredGrant(t *testing.T) {
	for _, change := range []string{"code", "account", "expired"} {
		t.Run(change, func(t *testing.T) {
			s, user, _ := verificationFixture(t)
			code := addRecovery(t, s, user)
			flow, err := s.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer flow.Close()
			if err := flow.Start("recovery"); err != nil {
				t.Fatal(err)
			}
			if err := flow.Reset("NewPass123"); err == nil {
				t.Fatal("unverified reset accepted")
			}
			if err := flow.Verify(code); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "code":
				if err := s.codes.RevokeRecoveryCodes(user.ID); err != nil {
					t.Fatal(err)
				}
			case "account":
				if _, err := s.engine.ID(user.ID).Cols("security_version").Update(&models.User{SecurityVersion: 8}); err != nil {
					t.Fatal(err)
				}
			case "expired":
				flow.(*verifiedReset).expires = time.Now().Add(-time.Second)
			}
			if err := flow.Reset("NewPass123"); err == nil {
				t.Fatal("stale grant accepted")
			}
			var unchanged models.User
			if _, err := s.engine.ID(user.ID).Get(&unchanged); err != nil {
				t.Fatal(err)
			}
			if unchanged.Password != user.Password {
				t.Fatal("rejected request changed password")
			}
		})
	}
}

func TestVerifiedResetEmailIsExplicitAndBound(t *testing.T) {
	s, user, delivered := verificationFixture(t)
	item, err := s.email.Bind(externalemail.BindRequest{UserID: user.ID, Email: "root@example.com", Type: models.ExternalEmailTypeBackup})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.engine.ID(item.ID).Cols("status").Update(&models.ExternalEmail{Status: models.ExternalEmailStatusVerified}); err != nil {
		t.Fatal(err)
	}
	flow, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Close()
	if *delivered != "" {
		t.Fatal("mail sent before selection")
	}
	if err := flow.Start(flow.Methods()[0].ID); err != nil {
		t.Fatal(err)
	}
	if *delivered == "" {
		t.Fatal("mail not sent")
	}
	if err := flow.Verify(*delivered); err != nil {
		t.Fatal(err)
	}
	if _, err := s.engine.ID(item.ID).Cols("status").Update(&models.ExternalEmail{Status: models.ExternalEmailStatusDisabled}); err != nil {
		t.Fatal(err)
	}
	if err := flow.Reset("NewPass123"); err == nil {
		t.Fatal("disabled email accepted")
	}
}

func TestVerifiedResetTOTPAndReplay(t *testing.T) {
	s, user, _ := verificationFixture(t)
	enrollment, err := s.totp.BeginEnrollment(user.ID, user.Account)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	makeCode := func(step int64) string {
		counter := make([]byte, 8)
		binary.BigEndian.PutUint64(counter, uint64(step))
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write(counter)
		digest := mac.Sum(nil)
		offset := digest[len(digest)-1] & 15
		return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(digest[offset:offset+4])&0x7fffffff)%1000000)
	}
	step := time.Now().Unix() / 30
	if err := s.totp.ConfirmEnrollment(user.ID, enrollment.ID, makeCode(step-1)); err != nil {
		t.Fatal(err)
	}
	flow, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Close()
	if err := flow.Start("totp"); err != nil {
		t.Fatal(err)
	}
	if err := flow.Verify(makeCode(step)); err != nil {
		t.Fatal(err)
	}
	if err := s.totp.Verify(user.ID, makeCode(step)); err == nil {
		t.Fatal("TOTP replay accepted")
	}
	if err := flow.Reset("NewPass123"); err != nil {
		t.Fatal(err)
	}
}

func TestVerifiedResetLimitsAcrossConnections(t *testing.T) {
	s, user, _ := verificationFixture(t)
	code := addRecovery(t, s, user)
	for i := 0; i < 5; i++ {
		flow, err := s.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := flow.Start("recovery"); err != nil {
			t.Fatal(err)
		}
		if err := flow.Verify("WRONG-CODE-123456"); err == nil {
			t.Fatal("wrong code accepted")
		}
		flow.Close()
	}
	flow, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Close()
	if err := flow.Start("recovery"); err != nil {
		t.Fatal(err)
	}
	if err := flow.Verify(code); err == nil {
		t.Fatal("new connection bypassed limit")
	}
}

func TestProtocolRejectsSkippedVerification(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		handleResetConnection(server, server.SetDeadline, testResetHandler(func(string) error { t.Error("reset bypassed verification"); return nil }), NewSessionGuard(time.Minute))
	}()
	challenge, err := DecodeResponse(client)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(client).Encode(Request{Version: ProtocolVersion, Nonce: challenge.Nonce, Action: "reset", NewPassword: "NewPass123"}); err != nil {
		t.Fatal(err)
	}
	result, err := DecodeResponse(client)
	if err != nil || result.OK {
		t.Fatal("skipped verification accepted")
	}
	<-done
}
