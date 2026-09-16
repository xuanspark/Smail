package email

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/i18n"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"

	"git.pcux.cn/xuans/Smail/utils/async"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/send"
	"git.pcux.cn/xuans/Smail/utils/validation"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode"
	"xorm.io/xorm"
)

const (
	maxSendRequestBytes    = 40 << 20
	maxAttachmentBytes     = 10 << 20
	maxTotalAttachmentSize = 25 << 20
	maxAttachmentCount     = 20
	maxRecipientCount      = 100
	maxSendBodyBytes       = 8 << 20
)

type sendRequest struct {
	ReplyTo     []user       `json:"reply_to"`
	From        user         `json:"from"`
	To          []user       `json:"to"`
	Bcc         []user       `json:"bcc"`
	Cc          []user       `json:"cc"`
	Subject     string       `json:"subject"`
	Text        string       `json:"text"`   // Plaintext message (optional)
	HTML        string       `json:"html"`   // Html message (optional)
	Sender      user         `json:"sender"` // override From as SMTP envelope sender (optional)
	ReadReceipt []string     `json:"read_receipt"`
	Attachments []attachment `json:"attrs"`
}

type user struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type attachment struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

func Send(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	domains := config.CurrentMailDomains()
	if domains.Primary == "" || len(domains.Accepted) == 0 {
		response.NewErrorResponse(response.ServerError, "服务配置不可用", "").FPrint(w)
		return
	}
	var reqData sendRequest
	if err := decodeEmailJSONRequest(w, req, &reqData, maxSendRequestBytes); err != nil {
		log.WithContext(ctx).Errorf("%+v", err)
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	log.WithContext(ctx).Infof("发送邮件")

	allowLocalAlias := ctx.EffectiveRole() == access.RoleSuperAdmin
	if reqData.From.Email != "" && !validation.IsAuthorizedSender(reqData.From.Email, ctx.UserAccount, allowLocalAlias, domains.Accepted) {
		response.NewErrorResponse(response.ParamsError, "发件人地址不允许", "").FPrint(w)
		return
	}
	if reqData.Sender.Email != "" && !validation.IsAuthorizedSender(reqData.Sender.Email, ctx.UserAccount, allowLocalAlias, domains.Accepted) {
		response.NewErrorResponse(response.ParamsError, "发送者地址不允许", "").FPrint(w)
		return
	}

	if reqData.From.Email == "" {
		reqData.From.Email = ctx.UserAccount + "@" + domains.Primary
	}

	if reqData.From.Email == "" {
		response.NewErrorResponse(response.ParamsError, "发件人必填", "发件人必填").FPrint(w)
		return
	}

	if reqData.From.Name == "" {
		reqData.From.Name = ctx.UserName
	}

	if reqData.Subject == "" {
		response.NewErrorResponse(response.ParamsError, "邮件标题必填", "邮件标题必填").FPrint(w)
		return
	}

	if len(reqData.To) <= 0 {
		response.NewErrorResponse(response.ParamsError, "收件人必填", "收件人必填").FPrint(w)
		return
	}
	if len(reqData.To)+len(reqData.Cc)+len(reqData.Bcc)+len(reqData.ReplyTo) > maxRecipientCount {
		response.NewErrorResponse(response.ParamsError, "收件人数量超过限制", "").FPrint(w)
		return
	}
	if len(reqData.Attachments) > maxAttachmentCount {
		response.NewErrorResponse(response.ParamsError, "附件数量超过限制", "").FPrint(w)
		return
	}

	e := &parsemail.Email{}

	for _, to := range reqData.To {
		e.To = append(e.To, &parsemail.User{
			Name:         to.Name,
			EmailAddress: to.Email,
		})
	}

	for _, replyTo := range reqData.ReplyTo {
		e.ReplyTo = append(e.ReplyTo, &parsemail.User{
			Name:         replyTo.Name,
			EmailAddress: replyTo.Email,
		})
	}

	for _, bcc := range reqData.Bcc {
		e.Bcc = append(e.Bcc, &parsemail.User{
			Name:         bcc.Name,
			EmailAddress: bcc.Email,
		})
	}

	for _, cc := range reqData.Cc {
		e.Cc = append(e.Cc, &parsemail.User{
			Name:         cc.Name,
			EmailAddress: cc.Email,
		})
	}

	e.From = &parsemail.User{
		Name:         reqData.From.Name,
		EmailAddress: reqData.From.Email,
	}
	if reqData.Sender.Email != "" {
		e.Sender = &parsemail.User{
			Name:         reqData.Sender.Name,
			EmailAddress: reqData.Sender.Email,
		}
	} else {
		e.Sender = &parsemail.User{
			Name:         reqData.From.Name,
			EmailAddress: reqData.From.Email,
		}
	}

	e.Text = []byte(parsemail.SanitizeText(reqData.Text))
	e.HTML = []byte(parsemail.SanitizeHTML(reqData.HTML))
	e.Subject = parsemail.SanitizeText(reqData.Subject)
	totalAttachmentSize := 0
	for _, att := range reqData.Attachments {
		contentType, content, err := decodeAttachment(att.Data)
		if err != nil {
			log.WithContext(ctx).Info("附件参数校验失败")
			response.NewErrorResponse(response.ParamsError, i18n.GetText(ctx.Lang, "att_err"), "").FPrint(w)
			return
		}
		if len(content) > maxAttachmentBytes || totalAttachmentSize > maxTotalAttachmentSize-len(content) {
			response.NewErrorResponse(response.ParamsError, "附件大小超过限制", "").FPrint(w)
			return
		}
		totalAttachmentSize += len(content)
		e.Attachments = append(e.Attachments, &parsemail.Attachment{
			Filename:    safeAttachmentName(att.Name),
			ContentType: contentType,
			Content:     content,
		})

	}

	log.WithContext(ctx).Debugf("插件执行--SendBefore")
	for _, hook := range hooks.Snapshot() {
		if hook == nil {
			continue
		}
		hook.SendBefore(ctx, e)
	}
	log.WithContext(ctx).Debugf("插件执行--SendBefore End")

	if e.From == nil || e.Sender == nil || !validation.IsAuthorizedSender(e.From.EmailAddress, ctx.UserAccount, allowLocalAlias, domains.Accepted) || !validation.IsAuthorizedSender(e.Sender.EmailAddress, ctx.UserAccount, allowLocalAlias, domains.Accepted) {
		response.NewErrorResponse(response.ParamsError, "插件修改了未授权的发件人", "").FPrint(w)
		return
	}
	if err := validation.ValidateEmail(e, validation.EmailLimits{
		MaxRecipients:           maxRecipientCount,
		MaxAttachments:          maxAttachmentCount,
		MaxAttachmentBytes:      maxAttachmentBytes,
		MaxTotalAttachmentBytes: maxTotalAttachmentSize,
		MaxBodyBytes:            maxSendBodyBytes,
		MaxEstimatedBytes:       maxSendRequestBytes,
	}); err != nil {
		response.NewErrorResponse(response.ParamsError, "插件修改后的邮件超出安全限制", "").FPrint(w)
		return
	}

	msgID, err := parsemail.GenerateMsgID(domains.Primary)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("generate outgoing message ID failed")
		response.NewErrorResponse(response.ServerError, i18n.GetText(ctx.Lang, "send_fail"), "").FPrint(w)
		return
	}

	storedFields, err := e.EncodeStoredFields()
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("encode outgoing email fields failed")
		response.NewErrorResponse(response.ServerError, i18n.GetText(ctx.Lang, "send_fail"), "").FPrint(w)
		return
	}

	now := time.Now()
	modelEmail := models.Email{
		Type:         consts.EmailTypeSend,
		Subject:      e.Subject,
		ReplyTo:      storedFields.ReplyTo,
		FromName:     e.From.Name,
		FromAddress:  e.From.EmailAddress,
		To:           storedFields.To,
		Bcc:          storedFields.Bcc,
		Cc:           storedFields.Cc,
		Text:         sql.NullString{String: string(e.Text), Valid: true},
		Html:         sql.NullString{String: string(e.HTML), Valid: true},
		Sender:       storedFields.Sender,
		Attachments:  storedFields.Attachments,
		SPFCheck:     1,
		DKIMCheck:    1,
		SendUserID:   ctx.UserID,
		SendDate:     now,
		CronSendTime: now,
		Status:       consts.EmailStatusDeliveryPending,
		CreateTime:   now,
		MsgID:        msgID,
	}

	err = persistOutgoingEmail(db.Instance, ctx.UserID, &modelEmail)

	if err != nil || modelEmail.Id <= 0 {
		if err != nil {
			log.WithContext(ctx).WithError(err).Error("insert outgoing email transaction failed")
		} else {
			log.WithContext(ctx).Error("insert outgoing email returned no identifier")
		}
		response.NewErrorResponse(response.ServerError, i18n.GetText(ctx.Lang, "send_fail"), "").FPrint(w)
		return
	}

	// 邮件持久化成功后再记录联系人。
	var contactList []models.UserContact
	for _, recipient := range append(append(e.To, e.Cc...), e.Bcc...) {
		if recipient != nil && recipient.EmailAddress != "" {
			contactList = append(contactList, models.UserContact{
				Email: recipient.EmailAddress,
				Name:  recipient.Name,
			})
		}
	}
	RecordUserContacts(ctx.UserID, contactList)

	e.MessageId = cast.ToInt64(modelEmail.Id)
	e.MsgID = modelEmail.MsgID

	async.New(ctx).Process(func(p any) {
		errMsg := ""
		err, sendErr := send.SendTriggered(ctx, e, "网页写信")

		log.WithContext(ctx).Debugf("插件执行--SendAfter")

		as2 := async.New(ctx)
		for _, hook := range hooks.Snapshot() {
			if hook == nil {
				continue
			}
			as2.WaitProcess(func(hk any) {
				hk.(framework.EmailHook).SendAfter(ctx, e, sendErr)
			}, hook)
		}
		as2.Wait()
		log.WithContext(ctx).Debugf("插件执行--SendAfter")

		if err != nil {
			errMsg = err.Error()
			_ = db.Transaction(db.Instance, func(session *xorm.Session) error {
				if _, updateErr := session.Exec(db.WithContext(ctx, "update email set status = ?, error = ? where id = ?"), consts.EmailStatusFail, errMsg, modelEmail.Id); updateErr != nil {
					log.WithContext(ctx).Errorf("update email status failed: %+v", updateErr)
					return updateErr
				}
				if _, updateErr := session.Exec(db.WithContext(ctx, "update user_email set status = ? where email_id = ? and user_id = ?"), consts.EmailStatusFail, modelEmail.Id, ctx.UserID); updateErr != nil {
					log.WithContext(ctx).Errorf("update user_email status failed: %+v", updateErr)
					return updateErr
				}
				return nil
			})
		} else {
			_ = db.Transaction(db.Instance, func(session *xorm.Session) error {
				if _, updateErr := session.Exec(db.WithContext(ctx, "update email set status = ?, error = '' where id = ?"), consts.EmailStatusSent, modelEmail.Id); updateErr != nil {
					log.WithContext(ctx).Errorf("update email status failed: %+v", updateErr)
					return updateErr
				}
				if _, updateErr := session.Exec(db.WithContext(ctx, "update user_email set status = ? where email_id = ? and user_id = ?"), consts.EmailStatusSent, modelEmail.Id, ctx.UserID); updateErr != nil {
					log.WithContext(ctx).Errorf("update user_email status failed: %+v", updateErr)
					return updateErr
				}
				return nil
			})
		}

	}, nil)

	response.NewSuccessResponse(i18n.GetText(ctx.Lang, "succ")).FPrint(w)
}

func persistOutgoingEmail(engine *xorm.Engine, userID int, email *models.Email) error {
	return db.Transaction(engine, func(session *xorm.Session) error {
		candidate := *email
		candidate.Id = 0
		candidate.Status = consts.EmailStatusDeliveryPending
		if _, err := session.Insert(&candidate); err != nil {
			return err
		}
		if candidate.Id <= 0 {
			return fmt.Errorf("insert outgoing email returned no identifier")
		}
		relation := models.UserEmail{
			UserID:  userID,
			EmailID: candidate.Id,
			Status:  consts.EmailStatusDeliveryPending,
			IsRead:  1,
		}
		if _, err := session.Insert(&relation); err != nil {
			return err
		}
		*email = candidate
		return nil
	})
}

func decodeAttachment(data string) (string, []byte, error) {
	if !strings.HasPrefix(data, "data:") {
		return "", nil, fmt.Errorf("invalid data url")
	}
	parts := strings.SplitN(strings.TrimPrefix(data, "data:"), ",", 2)
	if len(parts) != 2 || !strings.Contains(strings.ToLower(parts[0]), ";base64") {
		return "", nil, fmt.Errorf("invalid data url")
	}
	contentType, _, err := mime.ParseMediaType(strings.Replace(parts[0], ";base64", "", 1))
	if err != nil || contentType == "" || strings.ContainsAny(contentType, "\r\n") {
		return "", nil, fmt.Errorf("invalid content type")
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil, err
	}
	return contentType, decoded, nil
}

func safeAttachmentName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '\r' || r == '\n' || r == 0 || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	return name
}
