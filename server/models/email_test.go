package models

import (
	"encoding/json"
	"testing"
)

func TestEmailMarshalJSONRejectsCorruptedAttachments(t *testing.T) {
	email := &Email{Attachments: "{"}
	if _, err := json.Marshal(email); err == nil {
		t.Fatal("corrupted attachment JSON was accepted")
	}
}

func TestEmailMarshalJSONAcceptsBlankAttachments(t *testing.T) {
	email := &Email{Attachments: "  "}
	data, err := json.Marshal(email)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Attachments []attachments `json:"attachments"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Attachments) != 0 {
		t.Fatalf("unexpected attachments: %#v", decoded.Attachments)
	}
}

func TestEmailMarshalJSONIncludesAttachmentMetadata(t *testing.T) {
	email := &Email{Attachments: `[{"Filename":"report.txt","ContentType":"text/plain"}]`}
	data, err := json.Marshal(email)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Attachments []attachments `json:"attachments"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Attachments) != 1 || decoded.Attachments[0].Filename != "report.txt" || decoded.Attachments[0].Index != 0 {
		t.Fatalf("unexpected attachments: %#v", decoded.Attachments)
	}
}
