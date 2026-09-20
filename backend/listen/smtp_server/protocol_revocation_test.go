package smtp_server

import (
	"errors"
	"io"
	"net"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-smtp"
)

type revocationReader struct{ revoke func() }

func (r revocationReader) Read(p []byte) (int, error) { r.revoke(); return 0, io.EOF }

func TestSMTPExistingSessionRejectsRevokedCredential(t *testing.T) {
	for _, kind := range []string{"protocol", "client", "during_data"} {
		t.Run(kind, func(t *testing.T) {
			engine, user, code := newSMTPProtocolAuthFixture(t)
			oldDB, oldConfig := db.Instance, config.Instance
			db.Instance, config.Instance = engine, &config.Config{Domain: "example.com"}
			t.Cleanup(func() { db.Instance, config.Instance = oldDB, oldConfig })
			if err := engine.Sync2(&models.ClientCredential{}); err != nil {
				t.Fatal(err)
			}
			service, err := credentials.NewService(engine)
			if err != nil {
				t.Fatal(err)
			}
			revoke := func() {
				if err := service.RevokeProtocolCode(user.ID, "smtp"); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "client" {
				var item *credentials.ClientCredentialItem
				code, item, err = service.CreateClientCredential(user.ID, "SMTP 撤销验收")
				if err != nil {
					t.Fatal(err)
				}
				revoke = func() {
					if err := service.RevokeClientCredential(user.ID, item.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			s := &Session{RemoteAddress: &net.TCPAddr{IP: net.ParseIP("192.0.2.85"), Port: 465}, Ctx: &context.Context{}}
			if err := s.AuthPlain("alice@example.com", code); err != nil {
				t.Fatal(err)
			}
			if err := s.Mail("alice@example.com", nil); err != nil {
				t.Fatal(err)
			}
			if err := s.Rcpt("alice@example.com", nil); err != nil {
				t.Fatal(err)
			}
			assertDenied := func(err error) {
				t.Helper()
				var denied *smtp.SMTPError
				if !errors.As(err, &denied) || denied.Code != 535 {
					t.Fatalf("未明确拒绝已撤销连接：%v", err)
				}
			}
			if kind == "during_data" {
				assertDenied(s.Data(revocationReader{revoke: revoke}))
			} else {
				revoke()
			}
			assertDenied(s.Mail("alice@example.com", nil))
			assertDenied(s.Rcpt("alice@example.com", nil))
			assertDenied(s.Data(nil))
			s.Reset()
			assertDenied(s.Mail("alice@example.com", nil))
		})
	}
}
