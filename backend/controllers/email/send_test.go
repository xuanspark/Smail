package email

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
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
		Status:       consts.EmailStatusDeliveryPending,
		CreateTime:   now,
		MsgID:        "msg-123@example.com",
	}

	err = persistOutgoingEmail(engine, ctx.UserID, &modelEmail)
	if err != nil {
		t.Fatalf("insert email transaction failed: %v", err)
	}

	var email models.Email
	has, err := engine.ID(modelEmail.Id).Get(&email)
	if err != nil || !has {
		t.Fatalf("email not found in database: %v", err)
	}
	if email.Status != consts.EmailStatusDeliveryPending || email.Type != consts.EmailTypeSend {
		t.Fatalf("unexpected email status/type: status=%d, type=%d", email.Status, email.Type)
	}

	var userEmail models.UserEmail
	has, err = engine.Where("email_id = ? AND user_id = ?", modelEmail.Id, ctx.UserID).Get(&userEmail)
	if err != nil || !has {
		t.Fatalf("user_email not found: %v", err)
	}
	if userEmail.Status != consts.EmailStatusDeliveryPending {
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

func TestPersistOutgoingEmailRollsBackRelationFailure(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	engine.SetMaxOpenConns(1)
	if err := engine.Sync2(&models.Email{}); err != nil {
		t.Fatal(err)
	}
	email := models.Email{Subject: "pending", Status: consts.EmailStatusSent}
	if err := persistOutgoingEmail(engine, 1, &email); err == nil {
		t.Fatal("missing relation table did not fail")
	}
	count, err := engine.Count(&models.Email{})
	if err != nil || count != 0 || email.Id != 0 {
		t.Fatalf("rollback failed: count=%d id=%d error=%v", count, email.Id, err)
	}
}

func TestOutboxExposesDeliveryStatus(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	engine.SetMaxOpenConns(1)
	if err := engine.Sync2(&models.Email{}, &models.UserEmail{}, &models.UserTrustedSender{}); err != nil {
		t.Fatal(err)
	}
	previous := db.Instance
	db.Instance = engine
	defer func() { db.Instance = previous }()
	ctx := &context.Context{UserID: 1}
	email := models.Email{Subject: "pending", Type: consts.EmailTypeSend, Status: consts.EmailStatusSent}
	if err := persistOutgoingEmail(engine, ctx.UserID, &email); err != nil {
		t.Fatal(err)
	}
	for _, status := range []int8{consts.EmailStatusDeliveryPending, consts.EmailStatusSent, consts.EmailStatusFail} {
		if _, err := engine.ID(email.Id).Cols("status").Update(&models.Email{Status: status}); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", "/api/v1/email/list", strings.NewReader(`{"tag":"{\"type\":1,\"status\":-1}"}`))
		writer := httptest.NewRecorder()
		EmailList(ctx, writer, request)
		var response struct {
			ErrorNo int               `json:"errorNo"`
			Data    emailListResponse `json:"data"`
		}
		if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.ErrorNo != 0 || len(response.Data.List) != 1 || response.Data.List[0].Status != status {
			t.Fatalf("outbox status %d: %s", status, writer.Body.String())
		}
	}
}
