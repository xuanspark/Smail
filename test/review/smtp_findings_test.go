package smtp_server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/detail"
	"git.pcux.cn/xuans/Smail/utils/context"
)

func TestAuditInboundBCCMustNotLeakToVisibleRecipient(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	visible := models.User{Account: "visible", Name: "Visible"}
	hidden := models.User{Account: "hidden", Name: "Hidden"}
	if _, err := engine.Insert(&visible, &hidden); err != nil {
		t.Fatal(err)
	}
	raw := "From: sender@example.net\r\n" +
		"To: visible@example.com\r\n" +
		"Message-ID: <audit-private-recipient@example.net>\r\n" +
		"Subject: privacy\r\nContent-Type: text/plain\r\n\r\nbody"
	envelope := []string{"visible@example.com", "hidden@example.com"}
	parsed, err := parsemail.NewEmailFromReader(envelope, strings.NewReader(raw), len(raw))
	if err != nil {
		t.Fatal(err)
	}
	users, stored, err := saveEmail(&context.Context{}, len(raw), parsed, 0, 0, envelope, true, true)
	if err != nil || stored == nil || len(users) != 2 {
		t.Fatalf("多收件人入库前置条件失败: users=%d stored=%v err=%v", len(users), stored != nil, err)
	}
	data, err := detail.GetEmailDetail(&context.Context{UserID: visible.ID}, stored.Id, false)
	if err != nil {
		t.Fatal(err)
	}
	// 执行网页详情使用的真实服务和响应序列化，不依赖 UI 是否显示该字段。
	recorder := httptest.NewRecorder()
	response.NewSuccessResponse(data).FPrint(recorder)
	leaked := strings.Contains(recorder.Body.String(), "hidden@example.com")
	t.Logf("raw_contains_hidden_recipient=%v persisted_recipient_count=%d visible_response_contains_hidden_recipient=%v", strings.Contains(raw, "hidden@example.com"), len(users), leaked)
	if leaked {
		t.Fatal("普通可见收件人的邮件详情响应泄露了仅存在于信封中的密送地址")
	}
}
