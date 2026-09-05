package smtp_server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	smtp "github.com/emersion/go-smtp"
)

func TestSubmissionServerRequiresStartTLSBeforeAuth(t *testing.T) {
	certificate := newTestCertificate(t)
	server := smtp.NewServer(&Backend{})
	server.Domain = "localhost"
	server.AllowInsecureAuth = false
	server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	plain := textproto.NewConn(conn)
	if _, _, err = plain.ReadResponse(220); err != nil {
		t.Fatal(err)
	}
	if err = plain.PrintfLine("EHLO test.example"); err != nil {
		t.Fatal(err)
	}
	_, capabilities, err := plain.ReadResponse(250)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capabilities, "STARTTLS") || strings.Contains(capabilities, "AUTH ") {
		t.Fatalf("plaintext capabilities = %q", capabilities)
	}
	if err = plain.PrintfLine("AUTH PLAIN AGFAYgBj"); err != nil {
		t.Fatal(err)
	}
	code, _, err := plain.ReadResponse(0)
	if err != nil || code < 400 {
		t.Fatalf("plaintext AUTH result = code %d, err %v", code, err)
	}
	if err = plain.PrintfLine("STARTTLS"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = plain.ReadResponse(220); err != nil {
		t.Fatal(err)
	}

	tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	if err = tlsConn.Handshake(); err != nil {
		t.Fatal(err)
	}
	secure := textproto.NewConn(tlsConn)
	if err = secure.PrintfLine("EHLO test.example"); err != nil {
		t.Fatal(err)
	}
	_, capabilities, err = secure.ReadResponse(250)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capabilities, "AUTH PLAIN LOGIN") {
		t.Fatalf("TLS capabilities = %q", capabilities)
	}
}

func newTestCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	certificateTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, certificateTemplate, certificateTemplate, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{certificateDER}, PrivateKey: privateKey}
}
