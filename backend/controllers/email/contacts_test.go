package email

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func setupContactsTestDB(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Sync2(&models.UserContact{}, &models.UserTrustedSender{}); err != nil {
		t.Fatal(err)
	}
	db.Instance = engine
}

func TestContactsListAndTenantIsolation(t *testing.T) {
	setupContactsTestDB(t)

	// User 1 联系人
	_, _ = db.Instance.Insert(&models.UserContact{
		UserID:     1,
		Email:      "user1_friend@example.com",
		Name:       "Friend One",
		LastUsedAt: time.Now(),
	})
	// User 2 联系人（隔离测试）
	_, _ = db.Instance.Insert(&models.UserContact{
		UserID:     2,
		Email:      "user2_secret@example.com",
		Name:       "Friend Two",
		LastUsedAt: time.Now(),
	})

	// 测试 User 1 查询联系人，只能看到自己的
	ctxUser1 := &context.Context{UserID: 1}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/email/contacts", nil)
	w := httptest.NewRecorder()
	ContactsList(ctxUser1, w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Code int                  `json:"code"`
		Data []models.UserContact `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 contact for user 1, got %d", len(resp.Data))
	}
	if resp.Data[0].Email != "user1_friend@example.com" {
		t.Fatalf("expected user1_friend@example.com, got %s", resp.Data[0].Email)
	}

	// 测试删除联系人
	delReqBody, _ := json.Marshal(deleteContactRequest{Email: "user1_friend@example.com"})
	delReq := httptest.NewRequest(http.MethodPost, "/api/v1/email/contacts/delete", bytes.NewReader(delReqBody))
	delW := httptest.NewRecorder()
	DeleteContact(ctxUser1, delW, delReq)

	if delW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", delW.Code)
	}

	// 再次查询应该为空
	w2 := httptest.NewRecorder()
	ContactsList(ctxUser1, w2, req)
	var resp2 struct {
		Code int                  `json:"code"`
		Data []models.UserContact `json:"data"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
	if len(resp2.Data) != 0 {
		t.Fatalf("expected 0 contacts after delete, got %d", len(resp2.Data))
	}

	// 确认 User 2 的联系人未受影响
	var u2Count int
	has, err := db.Instance.Table(&models.UserContact{}).Where("user_id = 2").Count(&models.UserContact{})
	if err != nil || has != 1 {
		t.Fatalf("user 2 contact was corrupted: count=%d, err=%v", u2Count, err)
	}
}

func TestTrustSenderToggle(t *testing.T) {
	setupContactsTestDB(t)
	ctx := &context.Context{UserID: 1}

	// 添加信任
	body, _ := json.Marshal(trustSenderRequest{Email: "trusted@example.com", Trusted: true})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/email/trust-sender", bytes.NewReader(body))
	w := httptest.NewRecorder()
	TrustSender(ctx, w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	has, err := db.Instance.Table(&models.UserTrustedSender{}).Where("user_id = ? AND email = ?", 1, "trusted@example.com").Exist(&models.UserTrustedSender{})
	if err != nil || !has {
		t.Fatalf("expected trusted sender to exist")
	}

	// 取消信任
	body2, _ := json.Marshal(trustSenderRequest{Email: "trusted@example.com", Trusted: false})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/email/trust-sender", bytes.NewReader(body2))
	w2 := httptest.NewRecorder()
	TrustSender(ctx, w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}

	hasAfter, _ := db.Instance.Table(&models.UserTrustedSender{}).Where("user_id = ? AND email = ?", 1, "trusted@example.com").Exist(&models.UserTrustedSender{})
	if hasAfter {
		t.Fatalf("expected trusted sender to be removed")
	}
}
