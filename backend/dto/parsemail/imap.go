package parsemail

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
)

// BuildIMAPBytes 为同一封存储邮件生成稳定的 MIME，确保分段读取与大小、结构一致。
// 收件视图的密送信息由调用方过滤；此方法只用于 IMAP，不能用于 SMTP 投递。
func (e *Email) BuildIMAPBytes(_ *context.Context) ([]byte, error) {
	if e == nil || e.From == nil || e.From.EmailAddress == "" {
		return nil, errors.New("invalid sender")
	}
	encoded, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	boundary := fmt.Sprintf("smail_%x", digest[:16])
	var h mail.Header
	date, err := time.Parse(time.RFC3339, e.Date)
	if err != nil {
		date, err = time.ParseInLocation(time.DateTime, e.Date, time.Local)
	}
	if err != nil {
		// 历史异常日期不能使用当前时间，否则重复 FETCH 的字节内容会变化。
		date = time.Unix(0, 0).UTC()
	}
	h.SetDate(date)
	messageID := e.MsgID
	if messageID == "" {
		cfg := config.CurrentConfig()
		if cfg == nil || cfg.Domain == "" {
			return nil, errors.New("message id domain is not configured")
		}
		messageID = fmt.Sprintf("%d@%s", e.MessageId, cfg.Domain)
	}
	h.SetMessageID(messageID)
	h.SetSubject(e.Subject)
	sender := e.Sender
	if sender == nil {
		sender = e.From
	}
	for _, field := range []struct {
		name  string
		users []*User
	}{
		{"From", []*User{e.From}}, {"Sender", []*User{sender}},
		{"To", e.To}, {"Cc", e.Cc}, {"Bcc", e.Bcc}, {"Reply-To", e.ReplyTo},
	} {
		if len(field.users) == 0 {
			continue
		}
		addresses := make([]*mail.Address, 0, len(field.users))
		for _, user := range field.users {
			if user == nil || user.EmailAddress == "" {
				return nil, errors.New("invalid recipient")
			}
			addresses = append(addresses, &mail.Address{Name: user.Name, Address: user.EmailAddress})
		}
		h.SetAddressList(field.name, addresses)
	}
	h.SetContentType("multipart/mixed", map[string]string{"boundary": boundary + "_mixed"})
	var output bytes.Buffer
	writer, err := message.CreateWriter(&output, h.Header)
	if err != nil {
		return nil, err
	}
	var alternative message.Header
	alternative.SetContentType("multipart/alternative", map[string]string{"boundary": boundary + "_alternative"})
	inline, err := writer.CreatePart(alternative)
	if err != nil {
		return nil, err
	}
	writePart := func(parent *message.Writer, header message.Header, content []byte) error {
		header.Set("Content-Transfer-Encoding", "base64")
		part, err := parent.CreatePart(header)
		if err != nil {
			return err
		}
		return writeMessagePart(part, content)
	}
	if len(e.Text) > 0 || len(e.HTML) == 0 {
		var header message.Header
		header.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		if err := writePart(inline, header, e.Text); err != nil {
			return nil, err
		}
	}
	if len(e.HTML) > 0 {
		var header message.Header
		header.SetContentType("text/html", map[string]string{"charset": "utf-8"})
		if err := writePart(inline, header, e.HTML); err != nil {
			return nil, err
		}
	}
	if err := inline.Close(); err != nil {
		return nil, err
	}
	for _, attachment := range e.Attachments {
		if attachment == nil {
			return nil, errors.New("nil attachment")
		}
		var header mail.AttachmentHeader
		contentType := attachment.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		header.Set("Content-Type", contentType)
		header.SetFilename(attachment.Filename)
		if attachment.ContentID != "" {
			header.Set("Content-ID", fmt.Sprintf("<%s>", attachment.ContentID))
		}
		if err := writePart(writer, header.Header, attachment.Content); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
