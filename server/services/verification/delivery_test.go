package verification

import (
	"strings"
	"testing"
)

func TestSupportedPurposes(t *testing.T) {
	for _, purpose := range []Purpose{PurposeLogin, PurposePasswordRecover, PurposeExternalEmailBind, PurposeExternalEmailChange} {
		if !IsSupportedPurpose(purpose) {
			t.Fatalf("purpose %q was rejected", purpose)
		}
	}
	if IsSupportedPurpose(Purpose("sensitive_operation")) {
		t.Fatal("high risk operation purpose was accepted")
	}
}

func TestSMTPDeliveryKeepsCodeOutOfChallenge(t *testing.T) {
	settings := SMTPSettings{Host: "smtp.example.com", Port: 587, Security: SecurityStartTLS, From: "no-reply@example.com"}
	delivery, err := NewSMTPDelivery(settings)
	if err != nil {
		t.Fatal(err)
	}
	var message []byte
	delivery.send = func(_ SMTPSettings, _ string, data []byte) error {
		message = append([]byte(nil), data...)
		return nil
	}
	challenge := Challenge{ID: "challenge", Purpose: PurposeLogin, Account: "smail", Email: "user@example.com"}
	if err := delivery.Delivery()(challenge, "123456"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(message), "123456") {
		t.Fatal("verification code was not delivered")
	}
	if challenge.ID == "123456" || challenge.Account == "123456" {
		t.Fatal("verification code leaked into challenge metadata")
	}
	clearBytes(message)
}

func TestSMTPModesAreValidated(t *testing.T) {
	base := SMTPSettings{Host: "localhost", Port: 25, Security: SecurityPlain, From: "no-reply@example.com"}
	if _, err := NewSMTPDelivery(base); err != nil {
		t.Fatal(err)
	}
	base.Security = SecurityMode("unknown")
	if _, err := NewSMTPDelivery(base); err != ErrUnsupportedSMTP {
		t.Fatalf("unexpected error: %v", err)
	}
	base.Security = SecurityPlain
	base.Host = "smtp.example.com"
	if _, err := NewSMTPDelivery(base); err != ErrInvalidSMTPConfig {
		t.Fatalf("remote plaintext SMTP was accepted: %v", err)
	}
}

func TestSMTPHostValidation(t *testing.T) {
	base := SMTPSettings{Host: "smtp.example.com", Port: 587, Security: SecurityStartTLS, From: "no-reply@example.com"}
	for _, host := range []string{"smtp.example.com", "127.0.0.1", "2001:db8::1"} {
		base.Host = host
		if _, err := NewSMTPDelivery(base); err != nil {
			t.Fatalf("host %q rejected: %v", host, err)
		}
	}
	for _, host := range []string{"", "smtp.example.com:587", "user@smtp.example.com", "smtp example.com", "-smtp.example.com", "smtp..example.com"} {
		base.Host = host
		if _, err := NewSMTPDelivery(base); err != ErrInvalidSMTPConfig {
			t.Fatalf("host %q error = %v, want invalid configuration", host, err)
		}
	}
}
