package http_server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/attachments"
	"git.pcux.cn/xuans/Smail/utils/context"
)

// 使用真实路由、TLS 会话及附件数据；服务层成功和非所有者拒绝为对照。
func TestAuditAttachmentPageDownloadMustReturnStoredContent(t *testing.T) {
	fixture := newAuditExternalHTTPFixture(t, time.Minute)
	payload := "audit attachment content"
	encoded, err := json.Marshal([]parsemail.Attachment{{
		Filename: "audit.txt", ContentType: "text/plain", Content: []byte(payload),
	}})
	if err != nil {
		t.Fatal(err)
	}
	email := &models.Email{Subject: "Audit attachment", Attachments: string(encoded)}
	if _, err := fixture.engine.Insert(email); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.Insert(&models.UserEmail{UserID: fixture.user.ID, EmailID: email.Id}); err != nil {
		t.Fatal(err)
	}
	name, content, err := attachments.GetAttachmentsByIndex(&context.Context{UserID: fixture.user.ID}, email.Id, 0)
	if err != nil || name != "audit.txt" || string(content) != payload {
		t.Fatal("附件服务正向对照未成立")
	}
	if _, _, err := attachments.GetAttachmentsByIndex(&context.Context{UserID: fixture.user.ID + 1}, email.Id, 0); !errors.Is(err, attachments.ErrAttachmentUnavailable) {
		t.Fatal("非所有者访问附件未被拒绝")
	}
	path := fmt.Sprintf("/attachments/download/%d/0", email.Id)
	result, err := fixture.client.Get(fixture.server.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("service_content_matches=true outsider_denied=true path=%s status=%d content_type=%s body=%s", path, result.StatusCode, result.Header.Get("Content-Type"), body)
	if string(body) != payload || result.Header.Get("Content-Disposition") == "" {
		t.Fatal("邮件详情页生成的正常下载链接未返回附件内容")
	}
}
