package imap_server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
)

// userToAddress 将 parsemail.User 转换为 imap.Address
func userToAddress(u *parsemail.User) imap.Address {
	if u == nil {
		return imap.Address{}
	}
	// 解析邮箱地址的 local 和 domain 部分
	mailbox := u.EmailAddress
	host := ""
	if at := strings.LastIndex(u.EmailAddress, "@"); at >= 0 {
		mailbox = u.EmailAddress[:at]
		host = u.EmailAddress[at+1:]
	}
	return imap.Address{
		Name:    u.Name,
		Mailbox: mailbox,
		Host:    host,
	}
}

// usersToAddresses 将用户列表转换为地址列表
func usersToAddresses(users []*parsemail.User) []imap.Address {
	if len(users) == 0 {
		return nil
	}
	addrs := make([]imap.Address, 0, len(users))
	for _, u := range users {
		if u != nil && u.EmailAddress != "" {
			addrs = append(addrs, userToAddress(u))
		}
	}
	if len(addrs) == 0 {
		return nil
	}
	return addrs
}

// buildEnvelope 构建 IMAP ENVELOPE 结构
func buildEnvelope(email *response.EmailResponseData, traEmail *parsemail.Email) *imap.Envelope {
	// From 地址
	var from []imap.Address
	if email.FromAddress != "" {
		mailbox := email.FromAddress
		host := ""
		if at := strings.LastIndex(email.FromAddress, "@"); at >= 0 {
			mailbox = email.FromAddress[:at]
			host = email.FromAddress[at+1:]
		}
		from = []imap.Address{{
			Name:    email.FromName,
			Mailbox: mailbox,
			Host:    host,
		}}
	}

	// Sender (如果没有单独的 sender，使用 from)
	var sender []imap.Address
	if traEmail.Sender != nil && traEmail.Sender.EmailAddress != "" {
		sender = []imap.Address{userToAddress(traEmail.Sender)}
	} else {
		sender = from
	}

	// Reply-To
	var replyTo []imap.Address
	if len(traEmail.ReplyTo) > 0 {
		replyTo = usersToAddresses(traEmail.ReplyTo)
	} else {
		replyTo = from
	}

	messageID := messageIDForEmail(email)
	if messageID == "" {
		return nil
	}

	return &imap.Envelope{
		Date:      email.CreateTime,
		Subject:   email.Subject,
		From:      from,
		Sender:    sender,
		ReplyTo:   replyTo,
		To:        usersToAddresses(traEmail.To),
		Cc:        usersToAddresses(traEmail.Cc),
		Bcc:       usersToAddresses(traEmail.Bcc),
		MessageID: messageID,
		InReplyTo: parsemail.ThreadMessageIDs(email.InReplyTo),
	}
}

func messageIDForEmail(email *response.EmailResponseData) string {
	if email == nil {
		return ""
	}
	if strings.TrimSpace(email.MsgID) != "" {
		return fmt.Sprintf("<%s>", strings.TrimSpace(email.MsgID))
	}
	cfg := config.CurrentConfig()
	if cfg == nil || strings.TrimSpace(cfg.Domain) == "" || email.Id <= 0 {
		return ""
	}
	return fmt.Sprintf("<%d@%s>", email.Id, strings.TrimSpace(cfg.Domain))
}

func (s *serverSession) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	rows, err := s.selectedMessages(numSet)
	if err != nil {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Fetch failed"}
	}
	return write(s.ctx, w, rows, options, s.readOnly)
}

// 纯文本 text/plain

func bsTextPlain(size uint32, numLines int64) *imap.BodyStructureSinglePart {
	return &imap.BodyStructureSinglePart{
		Type:     "text",
		Subtype:  "plain",
		Params:   map[string]string{"charset": "utf-8"},
		Encoding: "base64",
		Size:     size, // 按字节数
		Text:     &imap.BodyStructureText{NumLines: numLines},
		Extended: &imap.BodyStructureSinglePartExt{},
	}
}

// HTML text/html
func bsTextHTML(size uint32, numLines int64) *imap.BodyStructureSinglePart {
	return &imap.BodyStructureSinglePart{
		Type:     "text",
		Subtype:  "html",
		Params:   map[string]string{"charset": "utf-8"},
		Encoding: "base64",
		Size:     size,
		Text:     &imap.BodyStructureText{NumLines: numLines},
		Extended: &imap.BodyStructureSinglePartExt{},
	}
}

// 通用附件（传入 MIME，如 "application/pdf"）
func bsAttachment(filename, mime string, size uint32, encoding string) *imap.BodyStructureSinglePart {
	mt, st := "application", "octet-stream"
	if slash := strings.IndexByte(mime, '/'); slash > 0 {
		mt, st = mime[:slash], mime[slash+1:]
	}
	return &imap.BodyStructureSinglePart{
		Type:     mt,
		Subtype:  st,
		Params:   map[string]string{"name": filename}, // 备用名
		ID:       "",                                  // 可填 Content-ID
		Encoding: encoding,                            // 常见 "base64"
		Size:     size,
		Extended: &imap.BodyStructureSinglePartExt{
			Disposition: &imap.BodyStructureDisposition{
				Value:  "attachment",
				Params: map[string]string{"filename": filename}, // 客户端优先用这里
			},
		},
	}
}

// multipart/alternative：text + html
// 注意：BodyStructureMultiPart 必须至少有一个子元素，否则 go-imap 会 panic
func bsAlternative(text, html *imap.BodyStructureSinglePart) *imap.BodyStructureMultiPart {
	var children []imap.BodyStructure

	if text != nil {
		children = append(children, text)
	}
	if html != nil {
		children = append(children, html)
	}

	// 如果没有任何内容，创建一个空的 text/plain 占位符
	// 这可以避免 go-imap 的 panic: "imap.BodyStructureMultiPart must have at least one child"
	if len(children) == 0 {
		children = append(children, bsTextPlain(0, 0))
	}

	return &imap.BodyStructureMultiPart{
		Subtype:  "alternative",
		Children: children,
		Extended: &imap.BodyStructureMultiPartExt{},
	}
}

// multipart/mixed：{ alternative(text+html), attachments... }
func bsMixedWithAttachments(alt *imap.BodyStructureMultiPart, extend bool, atts ...imap.BodyStructure) *imap.BodyStructureMultiPart {
	children := []imap.BodyStructure{alt}
	children = append(children, atts...)
	var ext *imap.BodyStructureMultiPartExt
	if extend {
		ext = &imap.BodyStructureMultiPartExt{}
	}
	return &imap.BodyStructureMultiPart{
		Subtype:  "mixed",
		Children: children,
		Extended: ext,
	}
}

func write(ctx *context.Context, w *imapserver.FetchWriter, emailList []*response.EmailResponseData, options *imap.FetchOptions, readOnly ...bool) error {
	for _, email := range emailList {
		sequence, err := imapUint32(email.SerialNumber)
		if err != nil || sequence == 0 {
			return fmt.Errorf("IMAP sequence number out of range")
		}

		traEmail, err := parsemail.NewEmailFromModel(email.Email)
		if err != nil {
			log.WithContext(ctx).WithError(err).Error("decode IMAP message failed")
			return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Fetch failed"}
		}
		var uid imap.UID
		if options.UID {
			uid, err = imapUID(email.UeId)
			if err != nil {
				return err
			}
		}
		var emailContent []byte
		if options.BodyStructure != nil || options.RFC822Size || len(options.BodySection) > 0 {
			emailContent, err = traEmail.BuildIMAPBytes(ctx)
			if err != nil {
				return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Fetch failed"}
			}
		}
		for _, section := range options.BodySection {
			if !section.Peek && !(len(readOnly) > 0 && readOnly[0]) && email.IsRead == 0 {
				if err := updateReadByUID(ctx.UserID, email.UeId, true); err != nil {
					return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Fetch failed"}
				}
				email.IsRead = 1
			}
		}

		writer := w.CreateMessage(sequence)

		if options.UID {
			writer.WriteUID(uid)
		}
		if options.Envelope {
			env := buildEnvelope(email, traEmail)
			writer.WriteEnvelope(env)
		}
		if options.BodyStructure != nil {
			writer.WriteBodyStructure(imapserver.ExtractBodyStructure(bytes.NewReader(emailContent)))
		}
		if options.RFC822Size {
			writer.WriteRFC822Size(cast.ToInt64(len(emailContent)))
		}
		if options.Flags {
			flags := make([]imap.Flag, 0, 2)
			if email.IsRead == 1 {
				flags = append(flags, imap.FlagSeen)
			}
			if email.IsDeleted == 1 {
				flags = append(flags, imap.FlagDeleted)
			}
			writer.WriteFlags(flags)
		}
		if options.InternalDate {
			writer.WriteInternalDate(email.CreateTime)
		}
		for _, section := range options.BodySection {
			content := extractBodySection(emailContent, section)
			bodyWriter := writer.WriteBodySection(section, int64(len(content)))
			if err := writeBodySectionContent(bodyWriter, content); err != nil {
				return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Fetch failed"}
			}
		}
		if err := writer.Close(); err != nil {
			return err
		}
	}
	return nil
}

func extractBodySection(content []byte, section *imap.FetchItemBodySection) []byte {
	item := *section
	item.Partial = nil
	data := imapserver.ExtractBodySection(bytes.NewReader(content), &item)
	if partial := section.Partial; partial != nil {
		if partial.Offset < 0 || partial.Size < 0 || partial.Offset >= int64(len(data)) {
			return nil
		}
		// 先约束剩余长度，不对客户端提供的两个大整数直接求和。
		size := min(partial.Size, int64(len(data))-partial.Offset)
		data = data[partial.Offset : partial.Offset+size]
	}
	return data
}

func writeBodySectionContent(bodyWriter io.WriteCloser, content []byte) error {
	written, writeErr := bodyWriter.Write(content)
	if writeErr == nil && written != len(content) {
		writeErr = io.ErrShortWrite
	}
	closeErr := bodyWriter.Close()
	return errors.Join(writeErr, closeErr)
}
