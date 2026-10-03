package email

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/validation"
	log "github.com/sirupsen/logrus"
	"xorm.io/xorm"
)

var errDraftConflict = errors.New("draft missing, changed or no longer editable")

const ownedDraftRelation = "EXISTS (SELECT 1 FROM user_email WHERE user_id = ? AND email_id = ? AND status = ? AND is_deleted = 0)"

var draftColumns = []string{
	"reply_email_id", "in_reply_to", "thread_references",
	"subject", "reply_to", "from_name", "from_address", "to", "bcc", "cc",
	"text", "html", "sender", "attachments", "spf_check", "dkim_check",
	"status", "send_date", "cron_send_time", "msg_id", "size", "error",
}

type draftReference struct {
	ID      int    `json:"id"`
	Version string `json:"version"`
}

type draftResponse struct {
	draftReference
	Message sendRequest `json:"message"`
}

func writeDraftError(w http.ResponseWriter, err error) {
	if errors.Is(err, errDraftConflict) {
		response.NewErrorResponse(response.ParamsError, "草稿已变更、已删除或已发送，请返回草稿箱刷新后重试", "").FPrint(w)
		return
	}
	log.WithError(err).Error("draft operation failed")
	response.NewErrorResponse(response.ServerError, "草稿操作失败，请稍后重试", "").FPrint(w)
}

func SaveDraft(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var input sendRequest
	if err := decodeEmailJSONRequest(w, req, &input, maxSendRequestBytes); err != nil {
		response.NewErrorResponse(response.ParamsError, "草稿参数错误或内容超过限制", "").FPrint(w)
		return
	}
	if input.DraftID < 0 || (input.DraftID == 0) != (input.DraftVersion == "") {
		response.NewErrorResponse(response.ParamsError, "草稿标识或版本错误", "").FPrint(w)
		return
	}
	candidate, err := buildDraft(ctx, input)
	if err != nil {
		if errors.Is(err, errDraftConflict) {
			writeDraftError(w, err)
			return
		}
		response.NewErrorResponse(response.ParamsError, "草稿内容无效，请检查发件人、邮箱地址及附件大小", "").FPrint(w)
		return
	}
	if err := persistDraft(db.Instance, ctx.UserID, candidate, input.DraftID, input.DraftVersion); err != nil {
		writeDraftError(w, err)
		return
	}
	response.NewSuccessResponse(draftReference{ID: candidate.Id, Version: candidate.MsgID}).FPrint(w)
}

func buildDraft(ctx *context.Context, input sendRequest) (*models.Email, error) {
	domains := config.CurrentMailDomains()
	if domains.Primary == "" || ctx.UserID <= 0 {
		return nil, validation.ErrInvalidEmail
	}
	if input.From.Email == "" {
		input.From.Email = ctx.UserAccount + "@" + domains.Primary
	}
	if input.From.Name == "" {
		input.From.Name = ctx.UserName
	}
	if input.Sender.Email == "" {
		input.Sender = input.From
	}
	allowAlias := ctx.EffectiveRole() == access.RoleSuperAdmin
	if !validation.IsAuthorizedSender(input.From.Email, ctx.UserAccount, allowAlias, domains.Accepted) ||
		!validation.IsAuthorizedSender(input.Sender.Email, ctx.UserAccount, allowAlias, domains.Accepted) {
		return nil, validation.ErrInvalidEmail
	}
	if utf8.RuneCountInString(input.Subject) > 1000 || utf8.RuneCountInString(input.From.Name) > 50 ||
		len(input.Text)+len(input.HTML) > maxSendBodyBytes || len(input.Attachments) > maxAttachmentCount {
		return nil, validation.ErrEmailTooLarge
	}
	convert := func(users []user) []*parsemail.User {
		result := make([]*parsemail.User, 0, len(users))
		for _, recipient := range users {
			result = append(result, &parsemail.User{Name: recipient.Name, EmailAddress: recipient.Email})
		}
		return result
	}
	message := &parsemail.Email{
		From:   &parsemail.User{Name: input.From.Name, EmailAddress: input.From.Email},
		Sender: &parsemail.User{Name: input.Sender.Name, EmailAddress: input.Sender.Email},
		To:     convert(input.To), Cc: convert(input.Cc), Bcc: convert(input.Bcc), ReplyTo: convert(input.ReplyTo),
		Subject: parsemail.SanitizeText(input.Subject),
		Text:    []byte(parsemail.SanitizeText(input.Text)), HTML: []byte(parsemail.SanitizeHTML(input.HTML)),
	}
	total := 0
	if err := applyReplyThread(ctx, input, message); err != nil {
		return nil, err
	}
	for _, item := range input.Attachments {
		contentType, content, err := decodeAttachment(item.Data)
		if err != nil {
			return nil, err
		}
		if len(content) > maxAttachmentBytes || total > maxTotalAttachmentSize-len(content) {
			return nil, validation.ErrAttachmentTooLarge
		}
		total += len(content)
		message.Attachments = append(message.Attachments, &parsemail.Attachment{
			Filename: safeAttachmentName(item.Name), ContentType: contentType, Content: content,
		})
	}
	// 草稿允许暂未填写收件人；占位地址仅用于复用安全校验，不写入草稿。
	checked := *message
	if len(checked.To)+len(checked.Cc)+len(checked.Bcc)+len(checked.ReplyTo) == 0 {
		checked.To = []*parsemail.User{message.From}
	}
	if err := validation.ValidateEmail(&checked, validation.EmailLimits{
		MaxRecipients: maxRecipientCount, MaxAttachments: maxAttachmentCount,
		MaxAttachmentBytes: maxAttachmentBytes, MaxTotalAttachmentBytes: maxTotalAttachmentSize,
		MaxBodyBytes: maxSendBodyBytes, MaxEstimatedBytes: maxSendRequestBytes,
	}); err != nil {
		return nil, err
	}
	fields, err := message.EncodeStoredFields()
	if err != nil {
		return nil, err
	}
	version, err := parsemail.GenerateMsgID(domains.Primary)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	return &models.Email{
		Type: consts.EmailTypeSend, Status: consts.EmailStatusDrafts, SendUserID: ctx.UserID,
		ReplyEmailID: input.ReplyEmailID, InReplyTo: message.InReplyTo, ThreadReferences: message.ThreadReferences,
		Subject: message.Subject, FromName: message.From.Name, FromAddress: message.From.EmailAddress,
		ReplyTo: fields.ReplyTo, To: fields.To, Cc: fields.Cc, Bcc: fields.Bcc, Sender: fields.Sender,
		Text: sql.NullString{String: string(message.Text), Valid: true},
		Html: sql.NullString{String: string(message.HTML), Valid: true}, Attachments: fields.Attachments,
		SPFCheck: 1, DKIMCheck: 1, MsgID: version, CreateTime: now, SendDate: now,
		Size: len(message.Text) + len(message.HTML) + total,
	}, nil
}

func loadOwnedDraft(session *xorm.Session, userID, id int, version string) (*models.Email, error) {
	if userID <= 0 || id <= 0 {
		return nil, errDraftConflict
	}
	var draft models.Email
	found, err := session.ID(id).
		Where("send_user_id = ? AND type = ? AND status = ?", userID, consts.EmailTypeSend, consts.EmailStatusDrafts).
		And(ownedDraftRelation, userID, id, consts.EmailStatusDrafts).Get(&draft)
	if err != nil {
		return nil, err
	}
	if !found || (version != "" && version != draft.MsgID) {
		return nil, errDraftConflict
	}
	return &draft, nil
}

func replaceDraft(session *xorm.Session, userID, id int, version string, candidate *models.Email) error {
	if version == "" {
		return errDraftConflict
	}
	previous, err := loadOwnedDraft(session, userID, id, version)
	if err != nil {
		return err
	}
	candidate.Id = previous.Id
	candidate.CreateTime = previous.CreateTime
	affected, err := session.ID(id).Where("send_user_id = ? AND status = ? AND msg_id = ?", userID, consts.EmailStatusDrafts, version).
		And(ownedDraftRelation, userID, id, consts.EmailStatusDrafts).
		Cols(draftColumns...).Update(candidate)
	if err != nil {
		return err
	}
	if affected != 1 {
		return errDraftConflict
	}
	return nil
}

func persistDraft(engine *xorm.Engine, userID int, draft *models.Email, id int, version string) error {
	var saved models.Email
	err := db.Transaction(engine, func(session *xorm.Session) error {
		candidate := *draft
		if id > 0 {
			if err := replaceDraft(session, userID, id, version, &candidate); err != nil {
				return err
			}
		} else {
			candidate.Id = 0
			if _, err := session.Insert(&candidate); err != nil {
				return err
			}
			if candidate.Id <= 0 {
				return fmt.Errorf("insert draft returned no identifier")
			}
			if _, err := session.Insert(&models.UserEmail{
				UserID: userID, EmailID: candidate.Id, Status: consts.EmailStatusDrafts, IsRead: 1,
			}); err != nil {
				return err
			}
		}
		saved = candidate
		return nil
	})
	if err == nil {
		*draft = saved
	}
	return err
}

func draftMessage(draft models.Email) (sendRequest, error) {
	message := sendRequest{
		DraftID: draft.Id, DraftVersion: draft.MsgID,
		ReplyEmailID: draft.ReplyEmailID,
		From:         user{Name: draft.FromName, Email: draft.FromAddress},
		Subject:      draft.Subject, Text: draft.Text.String, HTML: draft.Html.String,
		Attachments: []attachment{},
	}
	var sender *parsemail.User
	if err := json.Unmarshal([]byte(draft.Sender), &sender); err != nil {
		return sendRequest{}, err
	}
	if sender != nil {
		message.Sender = user{Name: sender.Name, Email: sender.EmailAddress}
	}
	decodeUsers := func(value string, target *[]user) error {
		var users []parsemail.User
		if err := json.Unmarshal([]byte(value), &users); err != nil {
			return err
		}
		*target = make([]user, 0, len(users))
		for _, recipient := range users {
			*target = append(*target, user{Name: recipient.Name, Email: recipient.EmailAddress})
		}
		return nil
	}
	for _, field := range []struct {
		value  string
		target *[]user
	}{
		{draft.To, &message.To}, {draft.Cc, &message.Cc}, {draft.Bcc, &message.Bcc}, {draft.ReplyTo, &message.ReplyTo},
	} {
		if err := decodeUsers(field.value, field.target); err != nil {
			return sendRequest{}, err
		}
	}
	var items []parsemail.Attachment
	if err := json.Unmarshal([]byte(draft.Attachments), &items); err != nil {
		return sendRequest{}, err
	}
	for _, item := range items {
		if strings.ContainsAny(item.ContentType, "\r\n,") {
			return sendRequest{}, validation.ErrInvalidEmail
		}
		message.Attachments = append(message.Attachments, attachment{
			Name: item.Filename, Data: "data:" + item.ContentType + ";base64," + base64.StdEncoding.EncodeToString(item.Content),
		})
	}
	return message, nil
}

func ReadDraft(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var input draftReference
	if err := decodeEmailJSONRequest(w, req, &input, maxEmailJSONRequestBytes); err != nil || input.ID <= 0 {
		response.NewErrorResponse(response.ParamsError, "草稿标识错误", "").FPrint(w)
		return
	}
	session := db.Instance.NewSession()
	defer session.Close()
	draft, err := loadOwnedDraft(session, ctx.UserID, input.ID, "")
	if err != nil {
		writeDraftError(w, err)
		return
	}
	message, err := draftMessage(*draft)
	if err != nil {
		writeDraftError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.NewSuccessResponse(draftResponse{
		draftReference: draftReference{ID: draft.Id, Version: draft.MsgID}, Message: message,
	}).FPrint(w)
}
