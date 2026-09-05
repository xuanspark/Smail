package parsemail

import (
	"bytes"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-message"
	"github.com/emersion/go-msgauth/dkim"
)

func auditRawMessage(extraHeaders, to string) string {
	return "From: sender@example.com\r\n" +
		"To: " + to + "\r\n" +
		"Message-ID: <audit-message@example.com>\r\n" +
		"Subject: audit\r\n" + extraHeaders +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\nbody"
}

func TestAuditInvalidDKIMMustNotPassForSpecialDomain(t *testing.T) {
	for _, domain := range []string{"ordinary.example.test", "test.domain"} {
		t.Run(domain, func(t *testing.T) {
			// 缺少必要签名标签，验证器在查询 DNS 前就应拒绝。
			raw := auditRawMessage("DKIM-Signature: v=1; d="+domain+"\r\n", "recipient@example.com")
			verifications, err := dkim.Verify(strings.NewReader(raw))
			if err != nil || len(verifications) != 1 || verifications[0].Err == nil {
				t.Fatalf("无效签名前置条件不成立: verifications=%v err=%v", verifications, err)
			}
			accepted := Check(&context.Context{}, strings.NewReader(raw))
			t.Logf("domain=%q verifier_error=%v application_passed=%t", domain, verifications[0].Err, accepted)
			if accepted {
				t.Fatal("不能仅因签名域名是特殊常量就把无效 DKIM 判为通过")
			}
		})
	}
}

func TestAuditReplyToMustSurviveParseAndBuild(t *testing.T) {
	raw := auditRawMessage("Reply-To: replies@example.com\r\n", "recipient@example.com")
	parsed, err := NewEmailFromReader([]string{"recipient@example.com"}, strings.NewReader(raw), len(raw))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("parsed_reply_to_count=%d", len(parsed.ReplyTo))
	if len(parsed.ReplyTo) != 1 || parsed.ReplyTo[0].EmailAddress != "replies@example.com" {
		t.Error("标准 Reply-To 在解析时丢失")
	}
	// 独立补入合法回复地址，检查重建层，避免解析失败遮挡第二层问题。
	parsed.ReplyTo = []*User{{EmailAddress: "replies@example.com"}}
	built, err := parsed.BuildBytes(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	message, err := message.Read(bytes.NewReader(built))
	if err != nil {
		t.Fatal(err)
	}
	replyTo := message.Header.Get("Reply-To")
	t.Logf("rebuilt_reply_to=%q", replyTo)
	if !strings.Contains(replyTo, "replies@example.com") {
		t.Error("合法 ReplyTo 字段在 MIME 重建时丢失")
	}
}

func TestAuditQuotedRecipientNameMustNotSplitAddress(t *testing.T) {
	raw := auditRawMessage("", `"Doe, Jane" <jane@example.com>`)
	parsed, err := NewEmailFromReader([]string{"jane@example.com"}, strings.NewReader(raw), len(raw))
	if err != nil {
		t.Fatal(err)
	}
	addresses := make([]string, 0, len(parsed.To))
	for _, recipient := range parsed.To {
		addresses = append(addresses, recipient.EmailAddress)
	}
	t.Logf("quoted_recipient_addresses=%q", addresses)
	if len(parsed.To) != 1 || parsed.To[0].EmailAddress != "jane@example.com" || parsed.To[0].Name != "Doe, Jane" {
		t.Fatal("带引号姓名中的合法逗号被当作地址列表分隔符")
	}
}

func TestAuditInlineAttachmentMustPreserveContentID(t *testing.T) {
	email := &Email{
		From: &User{EmailAddress: "sender@example.com"},
		To: []*User{{EmailAddress: "recipient@example.com"}},
		MsgID: "audit-cid@example.com", HTML: []byte(`<img src="cid:audit-inline">`),
		Attachments: []*Attachment{{Filename: "inline.png", ContentType: "image/png", ContentID: "audit-inline", Content: []byte{1, 2, 3}}},
	}
	built, err := email.BuildBytes(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := NewEmailFromReader([]string{"recipient@example.com"}, bytes.NewReader(built), len(built))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Attachments) != 1 {
		t.Fatalf("附件重建前置条件失败: count=%d", len(parsed.Attachments))
	}
	t.Logf("original_content_id=%q rebuilt_content_id=%q html_references_cid=%v", email.Attachments[0].ContentID, parsed.Attachments[0].ContentID, bytes.Contains(parsed.HTML, []byte("cid:audit-inline")))
	if parsed.Attachments[0].ContentID != email.Attachments[0].ContentID {
		t.Fatal("附件 Content-ID 在重建时丢失，HTML 的 cid 引用无法匹配")
	}
}
