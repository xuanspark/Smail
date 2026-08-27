package parsemail

import (
	crand "crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

type controlledWriteCloser struct {
	write    func([]byte) (int, error)
	closeErr error
}

func (w *controlledWriteCloser) Write(p []byte) (int, error) {
	return w.write(p)
}

func (w *controlledWriteCloser) Close() error {
	return w.closeErr
}

type failingReader struct {
	err error
}

func (r failingReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestWriteMessagePartPreservesWriteAndCloseErrors(t *testing.T) {
	writeErr := errors.New("write failed")
	closeErr := errors.New("close failed")
	writer := &controlledWriteCloser{
		write: func([]byte) (int, error) {
			return 0, writeErr
		},
		closeErr: closeErr,
	}

	err := writeMessagePart(writer, []byte("message"))
	if !errors.Is(err, writeErr) {
		t.Fatalf("write error was not preserved: %v", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("close error was not preserved: %v", err)
	}
}

func TestWriteMessagePartRejectsShortWrite(t *testing.T) {
	closeErr := errors.New("close failed")
	writer := &controlledWriteCloser{
		write: func(p []byte) (int, error) {
			return len(p) - 1, nil
		},
		closeErr: closeErr,
	}

	err := writeMessagePart(writer, []byte("message"))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write was not detected: %v", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("close error was not preserved: %v", err)
	}
}

func TestGenerateMsgIDRejectsInvalidDomain(t *testing.T) {
	for _, domain := range []string{"", "example.com\r\nBcc: attacker@example.net", "bad domain.example", "bad@example.com"} {
		if _, err := GenerateMsgID(domain); err == nil {
			t.Fatalf("invalid domain was accepted: %q", domain)
		}
	}
}

func TestGenerateMsgIDPropagatesRandomFailure(t *testing.T) {
	readErr := errors.New("random source failed")
	oldReader := crand.Reader
	crand.Reader = failingReader{err: readErr}
	t.Cleanup(func() { crand.Reader = oldReader })

	if _, err := GenerateMsgID("example.com"); !errors.Is(err, readErr) {
		t.Fatalf("random error was not propagated: %v", err)
	}
}

func TestGenerateMsgIDUsesConfiguredDomain(t *testing.T) {
	msgID, err := GenerateMsgID(" example.com ")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(msgID, "@example.com") {
		t.Fatalf("unexpected message ID: %q", msgID)
	}
}

func TestBuildBytesRejectsInvalidPointers(t *testing.T) {
	tests := []struct {
		name  string
		email *Email
	}{
		{
			name: "missing sender",
			email: &Email{
				MsgID: "missing-sender@example.com",
				To:    []*User{{EmailAddress: "recipient@example.com"}},
			},
		},
		{
			name: "nil recipient",
			email: &Email{
				MsgID: "nil-recipient@example.com",
				From:  &User{EmailAddress: "sender@example.com"},
				To:    []*User{nil},
			},
		},
		{
			name: "nil attachment",
			email: &Email{
				MsgID:       "nil-attachment@example.com",
				From:        &User{EmailAddress: "sender@example.com"},
				To:          []*User{{EmailAddress: "recipient@example.com"}},
				Attachments: []*Attachment{nil},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if data, err := tt.email.BuildBytes(nil, false); err == nil || len(data) != 0 {
				t.Fatalf("invalid email was built: bytes=%d err=%v", len(data), err)
			}
		})
	}
}

func TestBuildBytesFailsClosedWithoutDKIMSigner(t *testing.T) {
	oldInstance := instance
	instance = nil
	t.Cleanup(func() { instance = oldInstance })

	email := &Email{
		MsgID: "unsigned@example.com",
		From:  &User{EmailAddress: "sender@example.com"},
		To:    []*User{{EmailAddress: "recipient@example.com"}},
		Text:  []byte("message"),
	}
	if data, err := email.BuildBytes(nil, true); err == nil || len(data) != 0 {
		t.Fatalf("message was returned without a signer: bytes=%d err=%v", len(data), err)
	}
}

func TestBuildBytesDoesNotReturnUnsignedMessageOnDKIMFailure(t *testing.T) {
	oldConfig := config.Instance
	oldInstance := instance
	config.Instance = &config.Config{Domain: "example.com"}
	instance = &Dkim{}
	t.Cleanup(func() {
		config.Instance = oldConfig
		instance = oldInstance
	})

	email := &Email{
		MsgID: "sign-failure@example.com",
		From:  &User{EmailAddress: "sender@example.com"},
		To:    []*User{{EmailAddress: "recipient@example.com"}},
		Text:  []byte("message"),
	}
	if data, err := email.BuildBytes(nil, true); err == nil || len(data) != 0 {
		t.Fatalf("unsigned message was returned after signing failure: bytes=%d err=%v", len(data), err)
	}
}
