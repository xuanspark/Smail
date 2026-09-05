package email

import (
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"xorm.io/xorm"
)

func TestSendTransactionalPersistenceConsistency(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("create test engine: %v", err)
	}
	engine.SetMaxOpenConns(1)
	if err := engine.Sync2(&models.Email{}, &models.UserEmail{}); err != nil {
		t.Fatalf("sync test database: %v", err)
	}
	defer engine.Close()

	ctx := &context.Context{
		UserID:      1,
		UserAccount: "testuser",
		UserName:    "Test User",
	}

	now := time.Now()
	modelEmail := models.Email{
		Type:         consts.EmailTypeSend,
		Subject:      "Test Email",
		FromName:     "Test User",
		FromAddress:  "testuser@example.com",
		SendUserID:   ctx.UserID,
		SendDate:     now,
		CronSendTime: now,
		Status:       consts.EmailStatusSent,
		CreateTime:   now,
		MsgID:        "msg-123@example.com",
	}

	err = db.Transaction(engine, func(session *xorm.Session) error {
		candidate := modelEmail
		candidate.Id = 0
		if _, insertErr := session.Insert(&candidate); insertErr != nil {
			return insertErr
		}
		relation := models.UserEmail{
			UserID:  ctx.UserID,
			EmailID: candidate.Id,
			Status:  consts.EmailStatusSent,
			IsRead:  1,
		}
		if _, insertErr := session.Insert(&relation); insertErr != nil {
			return insertErr
		}
		modelEmail = candidate
		return nil
	})
	if err != nil {
		t.Fatalf("insert email transaction failed: %v", err)
	}

	var email models.Email
	has, err := engine.ID(modelEmail.Id).Get(&email)
	if err != nil || !has {
		t.Fatalf("email not found in database: %v", err)
	}
	if email.Status != consts.EmailStatusSent || email.Type != consts.EmailTypeSend {
		t.Fatalf("unexpected email status/type: status=%d, type=%d", email.Status, email.Type)
	}

	var userEmail models.UserEmail
	has, err = engine.Where("email_id = ? AND user_id = ?", modelEmail.Id, ctx.UserID).Get(&userEmail)
	if err != nil || !has {
		t.Fatalf("user_email not found: %v", err)
	}
	if userEmail.Status != consts.EmailStatusSent {
		t.Fatalf("unexpected user_email status: %d", userEmail.Status)
	}
}

func TestSafeAttachmentName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "normal", input: "test.pdf", expected: "test.pdf"},
		{name: "path traversal", input: "../../etc/passwd", expected: ".._.._etc_passwd"},
		{name: "windows path", input: "C:\\Users\\file.txt", expected: "C:_Users_file.txt"},
		{name: "control chars", input: "file\r\nname\x00.png", expected: "file__name_.png"},
		{name: "empty", input: "", expected: "attachment"},
		{name: "dots", input: "..", expected: "attachment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := safeAttachmentName(tt.input)
			if result != tt.expected {
				t.Errorf("safeAttachmentName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
