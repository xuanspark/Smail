package validation

import (
	"errors"
	"testing"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
)

func TestValidateEmail(t *testing.T) {
	valid := func() *parsemail.Email {
		return &parsemail.Email{
			From:   &parsemail.User{EmailAddress: "alice@example.com"},
			Sender: &parsemail.User{EmailAddress: "alice@example.com"},
			To:     []*parsemail.User{{EmailAddress: "bob@example.net"}},
			Text:   []byte("hello"),
		}
	}
	limits := EmailLimits{
		MaxRecipients:           2,
		MaxAttachments:          1,
		MaxAttachmentBytes:      4,
		MaxTotalAttachmentBytes: 4,
		MaxBodyBytes:            16,
		MaxEstimatedBytes:       128 << 10,
	}

	if err := ValidateEmail(valid(), limits); err != nil {
		t.Fatalf("ValidateEmail() error = %v", err)
	}

	tests := []struct {
		name string
		edit func(*parsemail.Email)
		want error
	}{
		{name: "invalid recipient", edit: func(email *parsemail.Email) { email.To[0].EmailAddress = "Bob <bob@example.net>" }, want: ErrInvalidEmail},
		{name: "too many recipients", edit: func(email *parsemail.Email) {
			email.Cc = []*parsemail.User{{EmailAddress: "c@example.net"}, {EmailAddress: "d@example.net"}}
		}, want: ErrTooManyRecipients},
		{name: "too many attachments", edit: func(email *parsemail.Email) { email.Attachments = []*parsemail.Attachment{{}, {}} }, want: ErrTooManyAttachments},
		{name: "single attachment too large", edit: func(email *parsemail.Email) { email.Attachments = []*parsemail.Attachment{{Content: make([]byte, 5)}} }, want: ErrAttachmentTooLarge},
		{name: "body too large", edit: func(email *parsemail.Email) { email.HTML = make([]byte, 17) }, want: ErrEmailTooLarge},
		{name: "invalid content type", edit: func(email *parsemail.Email) {
			email.Attachments = []*parsemail.Attachment{{ContentType: "text/plain\r\nX-Test: 1"}}
		}, want: ErrInvalidEmail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := valid()
			tt.edit(email)
			if err := ValidateEmail(email, limits); !errors.Is(err, tt.want) {
				t.Fatalf("ValidateEmail() error = %v, want %v", err, tt.want)
			}
		})
	}
}
