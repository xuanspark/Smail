package pop3_server

import (
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/internal/gopop"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/utils/context"
	"xorm.io/xorm"
)

func TestActionPassUsesFullEmailAndProtocolCode(t *testing.T) {
	engine, user, code := newPOP3ProtocolAuthFixture(t)
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

	session := &gopop.Session{
		User:  " Alice@Example.com ",
		InTls: true,
		Ctx:   &context.Context{},
	}
	if err := (action{}).Pass(session, code); err != nil {
		t.Fatalf("POP3 full email authorization failed: %v", err)
	}
	if session.Status != gopop.TRANSACTION || session.Ctx.(*context.Context).UserAccount != user.Account {
		t.Fatalf("POP3 session state = %v, account = %q", session.Status, session.Ctx.(*context.Context).UserAccount)
	}

	shortAccountSession := &gopop.Session{
		User:  user.Account,
		InTls: true,
		Ctx:   &context.Context{},
	}
	if err := (action{}).Pass(shortAccountSession, code); err == nil {
		t.Fatal("POP3 short account identifier must be rejected")
	}

	webPasswordSession := &gopop.Session{
		User:  "alice@example.com",
		InTls: true,
		Ctx:   &context.Context{},
	}
	if err := (action{}).Pass(webPasswordSession, "WebPass1234"); err == nil {
		t.Fatal("POP3 web password must be rejected")
	}
}

func TestActionPassRejectsAuthenticationBeforeTLS(t *testing.T) {
	session := &gopop.Session{
		User:  "alice@example.com",
		InTls: false,
		Ctx:   &context.Context{},
	}
	if err := (action{}).Pass(session, "any-code"); err == nil {
		t.Fatal("POP3 authentication before TLS must be rejected")
	}
}

func newPOP3ProtocolAuthFixture(t *testing.T) (*xorm.Engine, *models.User, string) {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("create POP3 test database: %v", err)
	}
	engine.SetMaxOpenConns(1)
	for _, table := range []any{&models.User{}, &models.Email{}, &models.UserEmail{}, &models.ProtocolCredential{}} {
		if err := engine.Sync2(table); err != nil {
			t.Fatalf("initialize POP3 test database: %v", err)
		}
	}
	t.Cleanup(func() { _ = engine.Close() })

	user := &models.User{Account: "alice", Name: "Alice"}
	if err := auth.SetPassword(user, "WebPass1234"); err != nil {
		t.Fatalf("hash POP3 test password: %v", err)
	}
	if _, err := engine.Insert(user); err != nil {
		t.Fatalf("insert POP3 test user: %v", err)
	}
	canonicalAddress := "alice@example.com"
	verifiedAt := time.Now().UTC()
	if _, err := engine.Insert(&models.UserEmail{
		UserID:           user.ID,
		Address:          canonicalAddress,
		AddressCanonical: &canonicalAddress,
		VerifiedAt:       &verifiedAt,
	}); err != nil {
		t.Fatalf("insert POP3 test email binding: %v", err)
	}
	service, err := credentials.NewService(engine)
	if err != nil {
		t.Fatalf("create POP3 credential service: %v", err)
	}
	if err := service.SetProtocolEnabled(user.ID, credentials.ProtocolPOP3, true); err != nil {
		t.Fatalf("enable POP3 protocol: %v", err)
	}
	code, err := service.GenerateProtocolCode(user.ID, credentials.ProtocolPOP3)
	if err != nil {
		t.Fatalf("generate POP3 protocol code: %v", err)
	}
	return engine, user, code
}
