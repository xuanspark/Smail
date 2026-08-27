package email

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/i18n"
	"git.pcux.cn/xuans/Smail/models"

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
	cfg := config.CurrentConfig()
	if cfg == nil || cfg.Domain == "" || len(cfg.Domains) == 0 {
		response.NewErrorResponse(response.ServerError, "服务配置不可用", "").FPrint(w)
		return
	}
	var reqData sendRequest
	if err := decodeEmailJSONRequest(w, req, &reqData, maxSendRequestBytes); err != nil {
		log.WithContext(ctx).Errorf("%+v", err)
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}
	log.WithContext(ctx).Infof("发送邮件")

	if reqData.From.Email != "" && !validation.IsAuthorizedSender(reqData.From.Email, ctx.UserAccount, ctx.IsAdmin, cfg.Domains) {
		response.NewErrorResponse(response.ParamsError, "\u53d1\u4ef6\u4eba\u5730\u5740\u4e0d\u5141\u8bb8", "").FPrint(w)
		return
	}
	if reqData.Sender.Email != "" && !validation.IsAuthorizedSender(reqData.Sender.Email, ctx.UserAccount, ctx.IsAdmin, cfg.Domains) {
		response.NewErrorResponse(response.ParamsError, "\u53d1\u9001\u8005\u5730\u5740\u4e0d\u5141\u8bb8", "").FPrint(w)
		return
	}

	if reqData.From.Email == "" {
		reqData.From.Email = ctx.UserAccount + "@" + cfg.Domain
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

	e.Text = []byte(reqData.Text)
	e.HTML = []byte(reqData.HTML)
	e.Subject = reqData.Subject
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

	if e.From == nil || e.Sender == nil || !validation.IsAuthorizedSender(e.From.EmailAddress, ctx.UserAccount, ctx.IsAdmin, cfg.Domains) || !validation.IsAuthorizedSender(e.Sender.EmailAddress, ctx.UserAccount, ctx.IsAdmin, cfg.Domains) {
		response.NewErrorResponse(response.ParamsError, "\u63d2\u4ef6\u4fee\u6539\u4e86\u672a\u6388\u6743\u7684\u53d1\u4ef6\u4eba", "").FPrint(w)
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
		response.NewErrorResponse(response.ParamsError, "\u63d2\u4ef6\u4fee\u6539\u540e\u7684\u90ae\u4ef6\u8d85\u51fa\u5b89\u5168\u9650\u5236", "").FPrint(w)
		return
	}

	msgID, err := parsemail.GenerateMsgID(cfg.Domain)
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

	modelEmail := models.Email{
		Type:         1,
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
		SendDate:     time.Now(),
		CronSendTime: time.Now(),
		Status:       1,
		CreateTime:   time.Now(),
		MsgID:        msgID,
	}

	_, err = db.Instance.Insert(&modelEmail)

	if err != nil || modelEmail.Id <= 0 {
		if err != nil {
			log.WithContext(ctx).WithError(err).Error("insert outgoing email failed")
		} else {
			log.WithContext(ctx).Error("insert outgoing email returned no identifier")
		}
		response.NewErrorResponse(response.ServerError, i18n.GetText(ctx.Lang, "send_fail"), "").FPrint(w)
		return
	}

	e.MessageId = cast.ToInt64(modelEmail.Id)
	e.MsgID = modelEmail.MsgID

	async.New(ctx).Process(func(p any) {
		errMsg := ""
		err, sendErr := send.Send(ctx, e)

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
			_, err := db.Instance.Exec(db.WithContext(ctx, "update email set status =2 ,error=? where id = ? "), errMsg, modelEmail.Id)
			if err != nil {
				log.WithContext(ctx).Errorf("sql Error :%+v", err)
			}

			ue := models.UserEmail{
				UserID:  ctx.UserID,
				EmailID: modelEmail.Id,
				Status:  2,
				IsRead:  1,
			}
			if _, insertErr := db.Instance.Insert(&ue); insertErr != nil {
				log.WithContext(ctx).WithError(insertErr).Error("insert failed outgoing email relation failed")
			}

		} else {
			_, err := db.Instance.Exec(db.WithContext(ctx, "update email set status =1  where id = ? "), modelEmail.Id)
			if err != nil {
				log.WithContext(ctx).Errorf("sql Error :%+v", err)
			}

			ue := models.UserEmail{
				UserID:  ctx.UserID,
				EmailID: modelEmail.Id,
				Status:  1,
				IsRead:  1,
			}
			if _, insertErr := db.Instance.Insert(&ue); insertErr != nil {
				log.WithContext(ctx).WithError(insertErr).Error("insert sent email relation failed")
			}
		}

	}, nil)

	response.NewSuccessResponse(i18n.GetText(ctx.Lang, "succ")).FPrint(w)
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
