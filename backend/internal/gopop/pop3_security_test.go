package gopop

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

type securityTestAction struct {
	quitErr    error
	quitCalls  int
	passErrors []error
}

func (a *securityTestAction) User(session *Session, username string) error {
	session.User = username
	return nil
}

func (a *securityTestAction) Pass(session *Session, password string) error {
	if len(a.passErrors) > 0 {
		err := a.passErrors[0]
		a.passErrors = a.passErrors[1:]
		if err != nil {
			return err
		}
	}
	session.Status = TRANSACTION
	return nil
}

func (a *securityTestAction) Apop(session *Session, username, digest string) error {
	return errors.New("not supported")
}

func (a *securityTestAction) Stat(session *Session) (int64, int64, error) { return 0, 0, nil }
func (a *securityTestAction) Uidl(session *Session, msg string) ([]UidlItem, error) {
	return nil, nil
}
func (a *securityTestAction) List(session *Session, msg string) ([]MailInfo, error) {
	return nil, nil
}
func (a *securityTestAction) Retr(session *Session, id int64) (string, int64, error) {
	return "", 0, nil
}
func (a *securityTestAction) Delete(session *Session, id int64) error { return nil }
func (a *securityTestAction) Rest(session *Session) error             { return nil }
func (a *securityTestAction) Top(session *Session, id int64, n int) (string, error) {
	return "", nil
}
func (a *securityTestAction) Noop(session *Session) error { return nil }
func (a *securityTestAction) Quit(session *Session) error {
	a.quitCalls++
	return a.quitErr
}
func (a *securityTestAction) Capa(session *Session) ([]string, error) {
	return nil, nil
}
func (a *securityTestAction) Custom(session *Session, cmd string, args []string) ([]string, error) {
	return nil, nil
}

func TestQuitReportsMailboxUpdateResult(t *testing.T) {
	tests := []struct {
		name       string
		quitErr    error
		wantPrefix string
	}{
		{name: "success", wantPrefix: "+OK Bye"},
		{name: "failure", quitErr: errors.New("commit failed"), wantPrefix: "-ERR mailbox update failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serverConn, clientConn := net.Pipe()
			t.Cleanup(func() { _ = clientConn.Close() })
			if err := clientConn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			action := &securityTestAction{quitErr: test.quitErr}
			server := &Server{Domain: "test", Action: action}
			done := make(chan struct{})
			go func() {
				server.handleClient(serverConn)
				close(done)
			}()

			reader := bufio.NewReader(clientConn)
			readResponseLine(t, reader, "+OK")
			writeCommand(t, clientConn, "USER user")
			readResponseLine(t, reader, "+OK")
			writeCommand(t, clientConn, "PASS test-password")
			readResponseLine(t, reader, "+OK")
			writeCommand(t, clientConn, "QUIT")
			readResponseLine(t, reader, test.wantPrefix)

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("server did not close the connection after QUIT")
			}
			if action.quitCalls != 1 {
				t.Fatalf("邮箱更新次数错误：%d", action.quitCalls)
			}
		})
	}
}

func TestQuitBeforeAuthenticationClosesWithoutMailboxUpdate(t *testing.T) {
	tests := []struct {
		name     string
		commands []string
		passErr  error
	}{
		{name: "initial"},
		{name: "after_user", commands: []string{"USER user"}},
		{name: "failed_password", commands: []string{"USER user", "PASS incorrect"}, passErr: errors.New("invalid password")},
		{name: "temporary_failure", commands: []string{"USER user", "PASS password"}, passErr: ErrAuthenticationTemporary},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serverConn, clientConn := net.Pipe()
			t.Cleanup(func() { _ = clientConn.Close() })
			if err := clientConn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			action := &securityTestAction{passErrors: []error{test.passErr}}
			server := &Server{Domain: "test", Action: action}
			done := make(chan struct{})
			go func() {
				server.handleClient(serverConn)
				close(done)
			}()
			reader := bufio.NewReader(clientConn)
			readResponseLine(t, reader, "+OK")
			for _, command := range test.commands {
				writeCommand(t, clientConn, command)
				if strings.HasPrefix(command, "PASS ") {
					readResponseLine(t, reader, "-ERR")
				} else {
					readResponseLine(t, reader, "+OK")
				}
			}
			writeCommand(t, clientConn, "QUIT")
			readResponseLine(t, reader, "+OK Bye")
			if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
				t.Fatalf("退出后连接未关闭：%v", err)
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("未认证连接退出超时")
			}
			if action.quitCalls != 0 {
				t.Fatalf("未认证退出不应提交邮箱更新：%d", action.quitCalls)
			}
		})
	}
}

func TestPassReportsTemporaryFailureWithoutLeakingInternalErrors(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	if err := clientConn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	server := &Server{Domain: "test", Action: &securityTestAction{passErrors: []error{
		ErrAuthenticationTemporary,
		fmt.Errorf("private database detail: %w", ErrAuthenticationTemporary),
		errors.New("[SYS/TEMP] untrusted detail\r\n+OK injected"),
		errors.New("private credential detail"),
	}}}
	done := make(chan struct{})
	go func() {
		server.handleClient(serverConn)
		close(done)
	}()
	reader := bufio.NewReader(clientConn)
	readResponseLine(t, reader, "+OK")
	writeCommand(t, clientConn, "USER user")
	readResponseLine(t, reader, "+OK")
	for _, want := range []string{
		"-ERR [SYS/TEMP] Authentication temporarily unavailable\r\n",
		"-ERR [SYS/TEMP] Authentication temporarily unavailable\r\n",
		"-ERR Password incorrect!\r\n",
		"-ERR Password incorrect!\r\n",
		"+OK User signed in\r\n",
	} {
		writeCommand(t, clientConn, "PASS test-password")
		line, err := reader.ReadString('\n')
		if err != nil || line != want {
			t.Fatalf("POP3 认证响应错误：%q，预期 %q，错误 %v", line, want, err)
		}
	}
	writeCommand(t, clientConn, "QUIT")
	readResponseLine(t, reader, "+OK Bye")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("POP3 恢复认证后未正常退出")
	}
}

func TestSTLSUpgradesConnectionBeforeAuthentication(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	if err := clientConn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	server := &Server{Domain: "test", Action: &securityTestAction{}, TlsConfig: pop3TestTLSConfig(t)}
	done := make(chan struct{})
	go func() {
		server.handleClient(serverConn)
		close(done)
	}()

	reader := bufio.NewReader(clientConn)
	readResponseLine(t, reader, "+OK")
	writeCommand(t, clientConn, "STLS")
	readResponseLine(t, reader, "+OK Begin TLS negotiation")

	tlsConn := tls.Client(clientConn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatal(err)
	}
	tlsReader := bufio.NewReader(tlsConn)
	writeCommand(t, tlsConn, "USER user")
	readResponseLine(t, tlsReader, "+OK")
	writeCommand(t, tlsConn, "PASS test-password")
	readResponseLine(t, tlsReader, "+OK User signed in")
	writeCommand(t, tlsConn, "QUIT")
	readResponseLine(t, tlsReader, "+OK Bye")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not close the TLS connection after QUIT")
	}
}

func pop3TestTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{certificate}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	}
}

func TestOversizedCommandWithoutLineEndingIsRejected(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	if err := clientConn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	server := &Server{Domain: "test", Action: &securityTestAction{}}
	done := make(chan struct{})
	go func() {
		server.handleClient(serverConn)
		close(done)
	}()

	reader := bufio.NewReader(clientConn)
	readResponseLine(t, reader, "+OK")
	if _, err := clientConn.Write([]byte(strings.Repeat("A", maxPOP3CommandLineBytes+1))); err != nil {
		t.Fatal(err)
	}
	readResponseLine(t, reader, "-ERR command line too long")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not close the connection after an oversized command")
	}
}

func writeCommand(t *testing.T, conn net.Conn, command string) {
	t.Helper()
	if _, err := fmt.Fprintf(conn, "%s\r\n", command); err != nil {
		t.Fatal(err)
	}
}

func readResponseLine(t *testing.T, reader *bufio.Reader, wantPrefix string) {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, wantPrefix) {
		t.Fatalf("response = %q, want prefix %q", line, wantPrefix)
	}
}
