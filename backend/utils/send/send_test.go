package send

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"testing"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func TestIsPermanentSMTPResponse(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil"},
		{name: "network error", err: errors.New("connection refused")},
		{name: "temporary SMTP response", err: &textproto.Error{Code: 451, Msg: "try later"}},
		{name: "wrapped temporary SMTP response", err: fmt.Errorf("delivery: %w", &textproto.Error{Code: 421, Msg: "service unavailable"})},
		{name: "permanent SMTP response", err: &textproto.Error{Code: 550, Msg: "mailbox unavailable"}, want: true},
		{name: "wrapped permanent SMTP response", err: fmt.Errorf("delivery: %w", &textproto.Error{Code: 554, Msg: "transaction failed"}), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPermanentSMTPResponse(tt.err); got != tt.want {
				t.Fatalf("isPermanentSMTPResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeliveryFailureCausePreservesTemporaryMXError(t *testing.T) {
	tests := []struct {
		name      string
		lookupErr *net.DNSError
	}{
		{
			name:      "SERVFAIL before fallback NXDOMAIN",
			lookupErr: &net.DNSError{Err: "server misbehaving", Name: "example.com", IsTemporary: true},
		},
		{
			name:      "timeout before fallback NXDOMAIN",
			lookupErr: &net.DNSError{Err: "i/o timeout", Name: "example.com", IsTimeout: true},
		},
	}

	fallbackErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &net.DNSError{Err: "no such host", Name: "smtp.example.com", IsNotFound: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := deliveryFailureCause(tt.lookupErr, fallbackErr)

			var dnsErr *net.DNSError
			if !errors.As(err, &dnsErr) {
				t.Fatalf("deliveryFailureCause() = %T, want wrapped DNS error", err)
			}
			if dnsErr.IsNotFound {
				t.Fatalf("deliveryFailureCause() selected fallback NXDOMAIN: %v", dnsErr)
			}
			if !dnsErr.IsTemporary && !dnsErr.IsTimeout {
				t.Fatalf("deliveryFailureCause() selected non-temporary DNS error: %v", dnsErr)
			}
		})
	}
}

func TestDeliveryFailureCauseKeepsExplicitSMTPRejection(t *testing.T) {
	lookupErr := &net.DNSError{Err: "server misbehaving", Name: "example.com", IsTemporary: true}
	rejection := &textproto.Error{Code: 550, Msg: "mailbox unavailable"}

	if got := deliveryFailureCause(lookupErr, rejection); got != rejection {
		t.Fatalf("deliveryFailureCause() = %v, want explicit SMTP rejection %v", got, rejection)
	}
}

func TestCertificateVerificationErrorStopsFallback(t *testing.T) {
	verificationErr := &tls.CertificateVerificationError{Err: errors.New("hostname mismatch")}
	if !isCertificateVerificationError(fmt.Errorf("STARTTLS failed: %w", verificationErr)) {
		t.Fatal("wrapped certificate verification error was not detected")
	}
	if isCertificateVerificationError(errors.New("connection refused")) {
		t.Fatal("network error was treated as a certificate verification error")
	}
}

func TestAllowUnsafeSMTPRequiresExplicitLoopbackTestTarget(t *testing.T) {
	tests := []struct {
		name   string
		domain mxDomain
		want   bool
	}{
		{name: "explicit local test", domain: mxDomain{domain: "localhost", mxHost: "127.0.0.1", localTest: true}, want: true},
		{name: "external domain", domain: mxDomain{domain: "example.com", mxHost: "mx.example.com"}},
		{name: "forged localhost name", domain: mxDomain{domain: "localhost", mxHost: "127.0.0.1"}},
		{name: "test flag with external host", domain: mxDomain{domain: "localhost", mxHost: "192.0.2.1", localTest: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := allowUnsafeSMTP(tt.domain); got != tt.want {
				t.Fatalf("allowUnsafeSMTP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsConfiguredMailDomain(t *testing.T) {
	accepted := []string{"example.com", "alias.example.com"}
	tests := []struct {
		name   string
		domain string
		want   bool
	}{
		{name: "primary domain", domain: "example.com", want: true},
		{name: "case insensitive domain", domain: "EXAMPLE.COM", want: true},
		{name: "accepted alias", domain: "alias.example.com", want: true},
		{name: "external domain", domain: "external.example.net"},
		{name: "empty domain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isConfiguredMailDomain(tt.domain, accepted); got != tt.want {
				t.Fatalf("isConfiguredMailDomain(%q) = %v, want %v", tt.domain, got, tt.want)
			}
		})
	}
}

func TestParseRecipientAddressRejectsUnsafeInput(t *testing.T) {
	tests := []struct {
		name string
		user *parsemail.User
	}{
		{name: "nil recipient"},
		{name: "empty address", user: &parsemail.User{}},
		{name: "missing domain", user: &parsemail.User{EmailAddress: "recipient"}},
		{name: "surrounding whitespace", user: &parsemail.User{EmailAddress: " recipient@example.com"}},
		{name: "header injection", user: &parsemail.User{EmailAddress: "recipient@example.com\r\nBcc: victim@example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := parseRecipientAddress(tt.user); err == nil {
				t.Fatal("unsafe recipient address was accepted")
			}
		})
	}
}

func TestDoSendReportsInvalidRecipients(t *testing.T) {
	err, failures := doSend(
		&appcontext.Context{},
		"example.com",
		[]byte("message"),
		[]*parsemail.User{{EmailAddress: "invalid"}, nil},
		"sender@example.com",
	)
	if err == nil {
		t.Fatal("invalid recipients were reported as delivered")
	}
	if len(failures) != 2 {
		t.Fatalf("failure count = %d, want 2", len(failures))
	}
}

func TestDoSendRejectsEmptyRecipientList(t *testing.T) {
	err, failures := doSend(&appcontext.Context{}, "example.com", []byte("message"), nil, "sender@example.com")
	if err == nil {
		t.Fatal("empty recipient list was reported as delivered")
	}
	if len(failures) != 1 {
		t.Fatalf("failure count = %d, want 1", len(failures))
	}
}

func TestDeliveryRecipientsKeepsEnvelopeSeparateFromHeaders(t *testing.T) {
	to := &parsemail.User{EmailAddress: "header-to@example.com"}
	cc := &parsemail.User{EmailAddress: "header-cc@example.com"}
	bcc := &parsemail.User{EmailAddress: "header-bcc@example.com"}
	envelope := &parsemail.User{EmailAddress: "envelope@example.com"}
	queued := &parsemail.User{EmailAddress: "queued@example.com"}
	tests := []struct {
		name     string
		envelope []*parsemail.User
		override []*parsemail.User
		want     []*parsemail.User
	}{
		{name: "web message uses header recipients", want: []*parsemail.User{to, cc, bcc}},
		{name: "smtp uses accepted envelope only", envelope: []*parsemail.User{envelope}, want: []*parsemail.User{envelope}},
		{name: "empty envelope never falls back to headers", envelope: []*parsemail.User{}},
		{name: "queue uses its single recipient", envelope: []*parsemail.User{envelope}, override: []*parsemail.User{queued}, want: []*parsemail.User{queued}},
		{name: "explicit empty override never falls back", envelope: []*parsemail.User{envelope}, override: []*parsemail.User{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := &parsemail.Email{
				To: []*parsemail.User{to}, Cc: []*parsemail.User{cc}, Bcc: []*parsemail.User{bcc},
				EnvelopeTo: tt.envelope,
			}
			got := deliveryRecipients(message, tt.override)
			if len(got) != len(tt.want) {
				t.Fatalf("recipient count = %d, want %d", len(got), len(tt.want))
			}
			for index := range got {
				if got[index] != tt.want[index] {
					t.Fatalf("recipient %d = %v, want %v", index, got[index], tt.want[index])
				}
			}
			if len(message.To) != 1 || message.To[0] != to || len(message.Cc) != 1 || message.Cc[0] != cc || len(message.Bcc) != 1 || message.Bcc[0] != bcc {
				t.Fatal("delivery routing changed the message headers")
			}
		})
	}
}
