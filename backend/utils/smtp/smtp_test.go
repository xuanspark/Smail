package smtp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestDialRejectsNonPublicRemoteAddress(t *testing.T) {
	tests := []string{
		"127.0.0.1",
		"::1",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.0.1",
		"169.254.0.1",
		"100.64.0.1",
		"192.0.2.1",
		"198.51.100.1",
		"203.0.113.1",
		"2001:db8::1",
		"0.0.0.0",
	}
	for _, host := range tests {
		t.Run(host, func(t *testing.T) {
			_, err := Dial(net.JoinHostPort(host, "25"), "example.com")
			if !errors.Is(err, ErrUnsafeRemoteAddress) {
				t.Fatalf("Dial() error = %v, want ErrUnsafeRemoteAddress", err)
			}
		})
	}
}

func TestSendMailUnsafeRejectsNonLoopbackTargets(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		addr   string
	}{
		{name: "external domain", domain: "example.com", addr: "127.0.0.1:25"},
		{name: "private address", domain: "localhost", addr: "10.0.0.1:25"},
		{name: "public address", domain: "localhost", addr: "8.8.8.8:25"},
		{name: "hostname target", domain: "localhost", addr: "localhost:25"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SendMailUnsafe(tt.domain, tt.addr, nil, "sender@example.com", "example.com", []string{"recipient@example.net"}, []byte("test"))
			if !errors.Is(err, ErrUnsafePlaintextTarget) {
				t.Fatalf("SendMailUnsafe() error = %v, want ErrUnsafePlaintextTarget", err)
			}
		})
	}
}

func TestSendMailUnsafeUsesFinalDataResponseAsDeliveryResult(t *testing.T) {
	tests := []struct {
		name                 string
		dataResponse         string
		holdWithoutQuitReply bool
		wantCode             int
		maxDuration          time.Duration
	}{
		{
			name:                 "accepted message returns while server withholds QUIT reply",
			dataResponse:         "250 2.0.0 queued",
			holdWithoutQuitReply: true,
			maxDuration:          time.Second,
		},
		{
			name:         "DATA rejection remains a delivery failure",
			dataResponse: "550 5.0.0 rejected",
			wantCode:     550,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, serverDone := startSMTPTransactionServer(t, tt.dataResponse, tt.holdWithoutQuitReply)

			start := time.Now()
			err := SendMailUnsafe(
				"localhost",
				addr,
				nil,
				"sender@example.com",
				"example.com",
				[]string{"recipient@example.net"},
				[]byte("From: sender@example.com\r\nTo: recipient@example.net\r\nSubject: test\r\n\r\nbody\r\n"),
			)
			duration := time.Since(start)
			if tt.maxDuration > 0 && duration > tt.maxDuration {
				t.Fatalf("SendMailUnsafe() took %v, want at most %v", duration, tt.maxDuration)
			}

			if tt.wantCode == 0 {
				if err != nil {
					t.Fatalf("SendMailUnsafe() = %v, want nil", err)
				}
			} else {
				var protocolErr *textproto.Error
				if !errors.As(err, &protocolErr) {
					t.Fatalf("SendMailUnsafe() error type = %T, want *textproto.Error", err)
				}
				if protocolErr.Code != tt.wantCode {
					t.Fatalf("SMTP code = %d, want %d", protocolErr.Code, tt.wantCode)
				}
			}

			select {
			case serverErr := <-serverDone:
				if serverErr != nil {
					t.Fatal(serverErr)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("fake SMTP server did not finish")
			}
		})
	}
}

func startSMTPTransactionServer(t *testing.T, dataResponse string, holdWithoutQuitReply bool) (string, <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		defer listener.Close()

		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

		text := textproto.NewConn(conn)
		defer text.Close()

		if err := text.PrintfLine("220 test ESMTP ready"); err != nil {
			done <- err
			return
		}
		if err := expectSMTPCommand(text, "EHLO "); err != nil {
			done <- err
			return
		}
		if err := text.PrintfLine("250 test"); err != nil {
			done <- err
			return
		}
		if err := expectSMTPCommand(text, "MAIL FROM:"); err != nil {
			done <- err
			return
		}
		if err := text.PrintfLine("250 2.1.0 sender accepted"); err != nil {
			done <- err
			return
		}
		if err := expectSMTPCommand(text, "RCPT TO:"); err != nil {
			done <- err
			return
		}
		if err := text.PrintfLine("250 2.1.5 recipient accepted"); err != nil {
			done <- err
			return
		}
		if err := expectSMTPCommand(text, "DATA"); err != nil {
			done <- err
			return
		}
		if err := text.PrintfLine("354 end data with <CR><LF>.<CR><LF>"); err != nil {
			done <- err
			return
		}
		body, err := io.ReadAll(text.DotReader())
		if err != nil {
			done <- err
			return
		}
		if !strings.Contains(string(body), "Subject: test") {
			done <- fmt.Errorf("message body was not received: %q", body)
			return
		}
		if err := text.PrintfLine("%s", dataResponse); err != nil {
			done <- err
			return
		}

		if holdWithoutQuitReply {
			if err := expectSMTPCommand(text, "QUIT"); err != nil {
				done <- err
				return
			}

			// Keep the server side open without sending 221. The client must close
			// the accepted transaction without waiting for a reply.
			buf := make([]byte, 1)
			if _, err := conn.Read(buf); err != io.EOF {
				done <- fmt.Errorf("waiting for client close after QUIT: %w", err)
				return
			}
		}
		done <- nil
	}()

	return listener.Addr().String(), done
}

func expectSMTPCommand(conn *textproto.Conn, prefix string) error {
	line, err := conn.ReadLine()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, prefix) {
		return fmt.Errorf("SMTP command = %q, want prefix %q", line, prefix)
	}
	return nil
}

func TestSMTPGreetingTimeout(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	started := time.Now()
	if _, err := newClientWithTimeout(client, "localhost", "example.com", 25*time.Millisecond); err == nil {
		t.Fatal("silent SMTP server did not time out")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("SMTP greeting timeout took too long: %v", elapsed)
	}
}

func TestTLSHandshakeTimeout(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	started := time.Now()
	if _, err := handshakeTLS(client, &tls.Config{MinVersion: tls.VersionTLS12}, 25*time.Millisecond); err == nil {
		t.Fatal("silent TLS peer did not time out")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("TLS handshake timeout took too long: %v", elapsed)
	}
}

func TestDialTlsRejectsUntrustedCertificate(t *testing.T) {
	certificate := newUntrustedTestCertificate(t)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			close(accepted)
			_ = conn.Close()
		}
	}()
	if _, err = dialTLS(listener.Addr().String(), "localhost", "example.com", false); err == nil {
		t.Fatal("DialTls accepted an untrusted certificate")
	}
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("TLS test server did not accept a connection")
	}
}

func newUntrustedTestCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: privateKey}
}
