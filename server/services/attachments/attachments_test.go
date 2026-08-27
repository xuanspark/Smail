package attachments

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
)

func TestAttachmentAccessIsRestrictedToMailboxOwner(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "attachments.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.Instance != nil {
			_ = db.Instance.Close()
		}
		db.Instance = oldDB
		config.Instance = oldConfig
	})

	encoded, err := json.Marshal([]parsemail.Attachment{{
		Filename: "report.txt", ContentType: "text/plain", Content: []byte("secret"), ContentID: "cid-1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	email := models.Email{Attachments: string(encoded), MsgID: "attachment-access@example.com"}
	if _, err := db.Instance.Insert(&email); err != nil {
		t.Fatal(err)
	}
	relation := models.UserEmail{UserID: 1, EmailID: email.Id}
	if _, err := db.Instance.Insert(&relation); err != nil {
		t.Fatal(err)
	}

	owner := &context.Context{UserID: 1}
	other := &context.Context{UserID: 2}
	admin := &context.Context{UserID: 3, IsAdmin: true}

	if contentType, content, err := GetAttachments(owner, email.Id, "cid-1"); err != nil || contentType != "text/plain" || string(content) != "secret" {
		t.Fatalf("owner could not read attachment: type=%q content=%q", contentType, content)
	}
	if _, content, err := GetAttachments(other, email.Id, "cid-1"); err == nil || len(content) != 0 {
		t.Fatal("other user read attachment by content ID")
	}
	if _, content, err := GetAttachmentsByIndex(other, email.Id, 0); err == nil || len(content) != 0 {
		t.Fatal("other user downloaded attachment by index")
	}
	if _, content, err := GetAttachmentsByIndex(admin, email.Id, 0); err == nil || len(content) != 0 {
		t.Fatal("administrator read another user's attachment without mailbox ownership")
	}
}

func TestAttachmentDecodeRejectsCorruptedStoredJSON(t *testing.T) {
	if _, err := decodeAttachments("{"); err == nil {
		t.Fatal("corrupted attachment JSON was accepted")
	}
	if atts, err := decodeAttachments("  "); err != nil || atts != nil {
		t.Fatalf("blank attachment JSON was not accepted: atts=%#v err=%v", atts, err)
	}
}
