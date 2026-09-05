package smtp_server

import (
	"net"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/utils/context"
	"xorm.io/xorm"
)

func TestSessionAuthPlainUsesFullEmailAndProtocolCode(t *testing.T) {
	engine, user, code := newSMTPProtocolAuthFixture(t)
	oldDB := db.Instance
	oldConfig := config.Instance
	db.Instance = engine
	config.Instance = &config.Config{
		Domain:  "example.com",
		Domains: []string{"example.com"},
	}
	t.Cleanup(func() {
		db.Instance = oldDB
		config.Instance = oldConfig
	})

	session := &Session{
		RemoteAddress: &net.TCPAddr{IP: net.ParseIP("192.0.2.20"), Port: 465},
		Ctx:           &context.Context{},
	}
	if err := session.AuthPlain(" Alice@Example.com ", code); err != nil {
		t.Fatalf("SMTP full email authorization failed: %v", err)
	}
	if session.User != user.Account {
		t.Fatalf("SMTP session account = %q, want %q", session.User, user.Account)
	}

	shortAccountSession := &Session{
		RemoteAddress: &net.TCPAddr{IP: net.ParseIP("192.0.2.21"), Port: 465},
		Ctx:           &context.Context{},
	}
	if err := shortAccountSession.AuthPlain(user.Account, code); err == nil {
		t.Fatal("SMTP short account identifier must be rejected")
	}

	webPasswordSession := &Session{
		RemoteAddress: &net.TCPAddr{IP: net.ParseIP("192.0.2.22"), Port: 465},
		Ctx:           &context.Context{},
	}
	if err := webPasswordSession.AuthPlain("alice@example.com", "WebPass1234"); err == nil {
		t.Fatal("SMTP web password must be rejected")
	}
}

func newSMTPProtocolAuthFixture(t *testing.T) (*xorm.Engine, *models.User, string) {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("create SMTP test database: %v", err)
	}
	engine.SetMaxOpenConns(1)
	for _, table := range []any{&models.User{}, &models.Email{}, &models.UserEmail{}, &models.ProtocolCredential{}} {
		if err := engine.Sync2(table); err != nil {
			t.Fatalf("initialize SMTP test database: %v", err)
		}
	}
	t.Cleanup(func() { _ = engine.Close() })

	user := &models.User{Account: "alice", Name: "Alice"}
	if err := auth.SetPassword(user, "WebPass1234"); err != nil {
		t.Fatalf("hash SMTP test password: %v", err)
	}
	if _, err := engine.Insert(user); err != nil {
		t.Fatalf("insert SMTP test user: %v", err)
	}
	canonicalAddress := "alice@example.com"
	verifiedAt := time.Now().UTC()
	if _, err := engine.Insert(&models.UserEmail{
		UserID:           user.ID,
		Address:          canonicalAddress,
		AddressCanonical: &canonicalAddress,
		VerifiedAt:       &verifiedAt,
	}); err != nil {
		t.Fatalf("insert SMTP test email binding: %v", err)
	}
	service, err := credentials.NewService(engine)
	if err != nil {
		t.Fatalf("create SMTP credential service: %v", err)
	}
	if err := service.SetProtocolEnabled(user.ID, credentials.ProtocolSMTP, true); err != nil {
		t.Fatalf("enable SMTP protocol: %v", err)
	}
	code, err := service.GenerateProtocolCode(user.ID, credentials.ProtocolSMTP)
	if err != nil {
		t.Fatalf("generate SMTP protocol code: %v", err)
	}
	return engine, user, code
}
