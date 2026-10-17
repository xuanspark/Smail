package verification

import (
	"strings"
	"testing"
	"time"
)

func TestSupportedPurposes(t *testing.T) {
	for _, purpose := range []Purpose{PurposeLogin, PurposePasswordRecover, PurposePasswordChange, PurposeExternalEmailBind, PurposeExternalEmailChange} {
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

func TestSMTPDeliveryUsesReferenceHTMLTemplate(t *testing.T) {
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
	issuedAt := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)
	challenge := Challenge{
		Purpose:   PurposeLogin,
		Account:   "<xuans>",
		Email:     "user@example.com",
		IssuedAt:  issuedAt,
		ExpiresAt: issuedAt.Add(7 * time.Minute),
	}
	if err := delivery.Delivery()(challenge, "123456"); err != nil {
		t.Fatal(err)
	}
	raw := string(message)
	for _, want := range []string{
		"Content-Type: multipart/alternative;",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Type: text/html; charset=UTF-8",
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\">",
		"<h1>邮箱验证码</h1>",
		"<p>&lt;xuans&gt;，您好：</p>",
		">123456</p>",
		"<strong>7</strong> 分钟",
		"本邮件由 Smail 发送",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("message does not contain %q: %s", want, raw)
		}
	}
	if strings.Contains(raw, "<p><xuans>") {
		t.Fatal("recipient name was not HTML escaped")
	}
	clearBytes(message)
}

func TestCodeMessageDescribesExternalEmailBinding(t *testing.T) {
	title, text := CodeMessage(PurposeExternalEmailBind, "12345678")
	if title != "Smail 安全验证码" {
		t.Fatalf("title = %q", title)
	}
	if !strings.Contains(text, "外部邮箱绑定验证") || !strings.Contains(text, "12345678") {
		t.Fatalf("unexpected verification message: %q", text)
	}
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
