package smtp_server

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/internal/passwordtest"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-smtp"
)

func TestSMTPBusyReturnsTemporaryAuthenticationFailure(t *testing.T) {
	engine, user, code := newSMTPProtocolAuthFixture(t)
	oldDB, oldConfig := db.Instance, config.Instance
	db.Instance, config.Instance = engine, &config.Config{Domain: "example.com"}
	t.Cleanup(func() { db.Instance, config.Instance = oldDB, oldConfig })
	session := &Session{RemoteAddress: &net.TCPAddr{IP: net.ParseIP("192.0.2.81"), Port: 465}, Ctx: &context.Context{}}
	release := passwordtest.Hold(t)
	for index := 0; index < 12; index++ {
		var temporary *smtp.SMTPError
		err := session.AuthPlain("alice@example.com", code)
		if !errors.As(err, &temporary) || temporary.Code != 454 || temporary.EnhancedCode != (smtp.EnhancedCode{4, 7, 0}) || session.User != "" {
			t.Fatalf("SMTP 繁忙响应或会话状态错误：%v", err)
		}
	}
	release()
	if err := session.AuthPlain("alice@example.com", code); err != nil || session.User != user.Account {
		t.Fatalf("SMTP 繁忙恢复后无法认证：%v", err)
	}
}

func TestSMTPAuthenticationWireSeparatesBusyAndInvalidCredentials(t *testing.T) {
	engine, _, code := newSMTPProtocolAuthFixture(t)
	oldDB, oldConfig := db.Instance, config.Instance
	db.Instance, config.Instance = engine, &config.Config{Domain: "example.com"}
	t.Cleanup(func() { db.Instance, config.Instance = oldDB, oldConfig })
	certificate := newTestCertificate(t)
	server := smtp.NewServer(&Backend{})
	server.Domain = "localhost"
	server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		t.Fatal(err)
	}
	plain := textproto.NewConn(conn)
	if _, _, err := plain.ReadResponse(220); err != nil {
		t.Fatal(err)
	}
	command := func(client *textproto.Conn, line string, status int, prefix string) {
		t.Helper()
		if err := client.PrintfLine("%s", line); err != nil {
			t.Fatal(err)
		}
		_, message, err := client.ReadResponse(status)
		if err != nil || !strings.HasPrefix(message, prefix) {
			t.Fatalf("SMTP 响应错误：%q，预期 %d %q，错误 %v", message, status, prefix, err)
		}
	}
	command(plain, "EHLO test.example", 250, "")
	command(plain, "STARTTLS", 220, "")
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	tlsConn := tls.Client(conn, &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatal(err)
	}
	secure := textproto.NewConn(tlsConn)
	command(secure, "EHLO test.example", 250, "")
	authLine := func(password string) string {
		return "AUTH PLAIN " + base64.StdEncoding.EncodeToString([]byte("\x00alice@example.com\x00"+password))
	}
	command(secure, authLine("invalid-code"), 535, "5.7.8 Authentication credentials invalid")
	release := passwordtest.Hold(t)
	command(secure, authLine(code), 454, "4.7.0 Temporary authentication failure")
	release()
	command(secure, authLine(code), 235, "2.0.0")
	command(secure, "QUIT", 221, "")
}
