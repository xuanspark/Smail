package parsemail

import (
	"encoding/json"
	"testing"
)

func TestEncodeStoredFields(t *testing.T) {
	email := &Email{
		To:          []*User{{Name: "Alice", EmailAddress: "alice@example.com"}},
		Attachments: []*Attachment{{Filename: "report.txt", ContentType: "text/plain", Content: []byte("data")}},
	}
	fields, err := email.EncodeStoredFields()
	if err != nil {
		t.Fatal(err)
	}
	var recipients []*User
	if err := json.Unmarshal([]byte(fields.To), &recipients); err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 1 || recipients[0].EmailAddress != "alice@example.com" {
		t.Fatalf("unexpected recipients: %#v", recipients)
	}
	var attachments []*Attachment
	if err := json.Unmarshal([]byte(fields.Attachments), &attachments); err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 1 || string(attachments[0].Content) != "data" {
		t.Fatalf("unexpected attachments: %#v", attachments)
	}
}

func TestEncodeStoredFieldsRejectsNilEmail(t *testing.T) {
	var email *Email
	if _, err := email.EncodeStoredFields(); err == nil {
		t.Fatal("nil email was accepted")
	}
}
