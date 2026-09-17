package auth

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/internal/passwordtest"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/utils/password"
)

func TestProtocolBusyDoesNotCountAsCredentialFailure(t *testing.T) {
	oldConfig, oldDB := config.Instance, db.Instance
	oldPair, oldAccount, oldRemote := protocolPairAttempts, protocolAccountAttempts, protocolRemoteAttempts
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "capacity.db"), Domain: "example.com"}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Instance.Close()
		config.Instance, db.Instance = oldConfig, oldDB
		protocolPairAttempts, protocolAccountAttempts, protocolRemoteAttempts = oldPair, oldAccount, oldRemote
	})
	protocolPairAttempts = newProtocolLoginLimiter(1)
	protocolAccountAttempts = newProtocolLoginLimiter(1)
	protocolRemoteAttempts = newProtocolLoginLimiter(1)
	user := models.User{Account: "capacity-user"}
	if err := SetPassword(&user, "CorrectP12"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}
	address, now := "capacity-user@example.com", time.Now().UTC()
	if _, err := db.Instance.Insert(&models.UserEmail{UserID: user.ID, Address: address, AddressCanonical: &address, VerifiedAt: &now}); err != nil {
		t.Fatal(err)
	}
	service, err := credentials.NewService(db.Instance)
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := service.CreateClientCredential(user.ID, "容量验收")
	if err != nil {
		t.Fatal(err)
	}
	release := passwordtest.Hold(t)
	for index := 0; index < 12; index++ {
		for _, protocol := range []string{"smtp", "imap", "pop3"} {
			if got, err := AuthenticateProtocol(protocol, "192.0.2.70:1234", address, code); got != nil || !errors.Is(err, password.ErrBusy) {
				t.Fatalf("%s 繁忙被当成凭据失败：%v", protocol, err)
			}
		}
	}
	for _, account := range []string{user.Account, "missing-user"} {
		if _, err := Authenticate(account, "CorrectP12"); !errors.Is(err, password.ErrBusy) {
			t.Fatalf("真实或模拟密码校验未传递繁忙：%v", err)
		}
	}
	if len(protocolPairAttempts.attempts)+len(protocolAccountAttempts.attempts)+len(protocolRemoteAttempts.attempts) != 0 {
		t.Fatal("资源繁忙增加了协议失败计数")
	}
	release()
	for _, protocol := range []string{"smtp", "imap", "pop3"} {
		if got, err := AuthenticateProtocol(protocol, "192.0.2.70:1234", address, code); err != nil || got.ID != user.ID {
			t.Fatalf("%s 繁忙结束后无法正常登录：%v", protocol, err)
		}
	}
}
