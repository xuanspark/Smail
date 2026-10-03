package email

import (
	"encoding/json"
	"git.pcux.cn/xuans/Smail/config"
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

func TestReplyDraftConflictResponsesPreserveStoredMessage(t *testing.T) {
	engine, err := xorm.NewEngine(`sqlite`, `:memory:`)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	engine.SetMaxOpenConns(1)
	if err := engine.Sync2(&models.Email{}, &models.UserEmail{}, &models.OutboundDelivery{}); err != nil {
		t.Fatal(err)
	}
	previousDB := db.Instance
	previousConfig := config.CurrentConfig()
	previousManager := config.CurrentManager()
	db.Instance = engine
	config.SetRuntimeManager(nil)
	config.PublishConfigSnapshot(&config.Config{Domain: `example.com`, Domains: []string{`example.com`}})
	defer func() {
		db.Instance = previousDB
		config.PublishConfigSnapshot(previousConfig)
		config.SetRuntimeManager(previousManager)
	}()

	draft := models.Email{
		Type: consts.EmailTypeSend, Status: consts.EmailStatusDrafts,
		SendUserID: 1, ReplyEmailID: 99, Subject: `saved version`, MsgID: `current@example.com`,
	}
	if err := persistDraft(engine, 1, &draft, 0, ``); err != nil {
		t.Fatal(err)
	}
	for _, send := range []bool{false, true} {
		for _, test := range []struct {
			name    string
			userID  int
			draftID int
			version string
		}{
			{name: `stale version`, userID: 1, draftID: draft.Id, version: `stale@example.com`},
			{name: `foreign owner`, userID: 2, draftID: draft.Id, version: draft.MsgID},
			{name: `missing draft`, userID: 1, draftID: draft.Id + 1, version: draft.MsgID},
		} {
			t.Run(test.name+map[bool]string{false: `/save`, true: `/send`}[send], func(t *testing.T) {
				input := sendRequest{
					DraftID: test.draftID, DraftVersion: test.version, ReplyEmailID: 99,
					From: user{Email: `testuser@example.com`}, To: []user{{Email: `recipient@example.com`}},
					Subject: `must not overwrite`, Text: `unsaved text`,
				}
				payload, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				ctx := &context.Context{UserID: test.userID, UserAccount: `testuser`, UserName: `Test User`}
				writer := httptest.NewRecorder()
				request := httptest.NewRequest(`POST`, `/`, strings.NewReader(string(payload)))
				if send {
					Send(ctx, writer, request)
				} else {
					SaveDraft(ctx, writer, request)
				}
				var result struct {
					ErrorNo  int
					ErrorMsg string
				}
				if err := json.Unmarshal(writer.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.ErrorNo == 0 || result.ErrorMsg != `草稿已变更、已删除或已发送，请返回草稿箱刷新后重试` {
					t.Fatalf(`unexpected conflict response: %s`, writer.Body.String())
				}
				var saved models.Email
				found, err := engine.ID(draft.Id).Get(&saved)
				if err != nil || !found || saved.Subject != draft.Subject || saved.MsgID != draft.MsgID || saved.Status != consts.EmailStatusDrafts {
					t.Fatalf(`stored draft changed: found=%v error=%v draft=%+v`, found, err, saved)
				}
				count, err := engine.Count(&models.Email{})
				if err != nil || count != 1 {
					t.Fatalf(`unexpected email count: %d error=%v`, count, err)
				}
				queued, err := engine.Count(&models.OutboundDelivery{})
				if err != nil || queued != 0 {
					t.Fatalf(`conflict queued delivery: %d error=%v`, queued, err)
				}
			})
		}
	}
}

func TestSendTransactionalPersistenceConsistency(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("create test engine: %v", err)
	}
	engine.SetMaxOpenConns(1)
	if err := engine.Sync2(&models.Email{}, &models.UserEmail{}, &models.OutboundDelivery{}); err != nil {
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
		To:           `[{"EmailAddress":"recipient@example.com"}]`,
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
	if err := engine.Sync2(&models.Email{}, &models.UserEmail{}, &models.UserTrustedSender{}, &models.OutboundDelivery{}); err != nil {
		t.Fatal(err)
	}
	previous := db.Instance
	db.Instance = engine
	defer func() { db.Instance = previous }()
	ctx := &context.Context{UserID: 1}
	email := models.Email{Subject: "pending", Type: consts.EmailTypeSend, Status: consts.EmailStatusSent, To: `[{"EmailAddress":"recipient@example.com"}]`}
	if err := persistOutgoingEmail(engine, ctx.UserID, &email); err != nil {
		t.Fatal(err)
	}
	for _, status := range []int8{consts.EmailStatusDeliveryPending, consts.EmailStatusSent, consts.EmailStatusFail, consts.EmailStatusDeliveryUncertain} {
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

func TestPersistOutgoingEmailRollsBackQueueFailure(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	engine.SetMaxOpenConns(1)
	if err := engine.Sync2(&models.Email{}, &models.UserEmail{}, &models.OutboundDelivery{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Exec("CREATE TRIGGER reject_queue BEFORE INSERT ON outbound_delivery BEGIN SELECT RAISE(ABORT, 'injected queue failure'); END"); err != nil {
		t.Fatal(err)
	}
	email := models.Email{To: `[{"EmailAddress":"recipient@example.com"}]`}
	if err := persistOutgoingEmail(engine, 1, &email); err == nil {
		t.Fatal("queue failure ignored")
	}
	for _, model := range []any{&models.Email{}, &models.UserEmail{}, &models.OutboundDelivery{}} {
		count, err := engine.Count(model)
		if err != nil || count != 0 || email.Id != 0 {
			t.Fatalf("rollback failed for %T: count=%d id=%d error=%v", model, count, email.Id, err)
		}
	}
}
