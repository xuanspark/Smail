package smtp_server

import (
	"bytes"
	stdcontext "context"
	"database/sql"
	oerrors "errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/listen/imap_server"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/logstore"
	"git.pcux.cn/xuans/Smail/services/notification"
	"git.pcux.cn/xuans/Smail/services/rule"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/async"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/send"
	"git.pcux.cn/xuans/Smail/utils/validation"
	"github.com/emersion/go-smtp"
	"github.com/mileusna/spf"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
)

// DropUnknownRecipientEmails 是代码级别的功能开关
// 当设置为 true 时，发给不存在用户的邮件将被直接丢弃，不会保存到数据库
// 这可以有效防止扫描器产生的垃圾邮件被存入第一个用户（管理员）的邮箱
// 注意：此开关只能通过修改代码来更改，不暴露给用户配置
const DropUnknownRecipientEmails = true

var errAllRecipientsBlocked = oerrors.New("all recipients blocked by sender policy")

func (s *Session) Data(r io.Reader) (resultErr error) {
	if err := s.checkAuthorization(); err != nil {
		return err
	}
	messageBytes := 0
	var recipientIDs []int
	result := "unknown"
	details := logstore.MailDetails{Trigger: "SMTP 收信", Stage: "读取邮件内容"}
	defer func() {
		direction := "inbound"
		if resultErr != nil {
			result = "failure"
		}
		if s.Ctx.UserID > 0 {
			direction = "submission"
			details.Trigger = "SMTP 客户端提交"
			if s.isNotification() {
				details.Trigger = "系统通知 SMTP 提交"
			}
		}
		if result == "failure" {
			details.Outcome = details.Stage + "失败"
			var protocol *smtp.SMTPError
			if oerrors.As(resultErr, &protocol) {
				details.Outcome += fmt.Sprintf("，SMTP 状态码 %d", protocol.Code)
			}
		}
		if direction == "inbound" && len(recipientIDs) > 0 {
			// 每个真实收件人只看到自己的投递记录，不暴露其他收件人数或地址。
			seen := make(map[int]bool)
			for _, id := range recipientIDs {
				if id > 0 && !seen[id] {
					seen[id] = true
					logstore.MailResult(id, direction, s.From, 1, messageBytes, result, details)
				}
			}
		} else {
			logstore.MailResult(s.Ctx.UserID, direction, s.From, len(s.To), messageBytes, result, details)
		}
	}()

	ctx := s.Ctx

	log.WithContext(ctx).Debugf("收到邮件")

	messageLimit := int64(maxSMTPMessageBytes)
	if s.isNotification() {
		messageLimit = notification.MaxMessageBytes
	}
	emailData, err := io.ReadAll(io.LimitReader(r, messageLimit+1))
	messageBytes = len(emailData)
	if err != nil {
		log.WithContext(ctx).Error("邮件内容无法读取", err)
		return err
	}
	if int64(len(emailData)) > messageLimit {
		return &smtp.SMTPError{Code: 552, EnhancedCode: smtp.EnhancedCode{5, 3, 4}, Message: "Message size exceeds fixed maximum message size"}
	}
	// DATA 上传期间也可能撤销授权，读取完成后必须再次复核。
	if err := s.checkAuthorization(); err != nil {
		return err
	}

	log.WithContext(ctx).Debugf("message_bytes=%d", len(emailData))

	details.Stage = "执行收信解析前插件"
	log.WithContext(ctx).Debugf("开始执行插件ReceiveParseBefore！")
	for _, hook := range hooks.Snapshot() {
		if hook == nil {
			continue
		}
		hook.ReceiveParseBefore(ctx, &emailData)
	}
	log.WithContext(ctx).Debugf("开始执行插件ReceiveParseBefore End！")

	if int64(len(emailData)) > messageLimit {
		return &smtp.SMTPError{Code: 552, EnhancedCode: smtp.EnhancedCode{5, 3, 4}, Message: "Message size exceeds fixed maximum message size"}
	}
	details.Stage = "解析与校验邮件"
	email, err := parsemail.NewEmailFromReader(s.To, bytes.NewReader(emailData), len(emailData))
	if err != nil {
		log.WithContext(ctx).Warnf("audit_event=message_parse_failed message_bytes=%d error=%v", len(emailData), err)
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 6, 0}, Message: "Invalid message content"}
	}

	email.EnvelopeFrom = s.From
	if s.From != "" {
		from := parsemail.BuilderUser(s.From)
		if email.From == nil {
			email.From = from
		}
		if email.From.EmailAddress != from.EmailAddress {
			// 协议中的from和邮件内容中的from不匹配，当成垃圾邮件处理
			//log.WithContext(s.Ctx).Infof("垃圾邮件，拒信")
			//return nil
		}
	}

	if email.From == nil || email.Sender == nil || strings.TrimSpace(email.From.EmailAddress) == "" || strings.TrimSpace(email.Sender.EmailAddress) == "" {
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 7}, Message: "Invalid sender address"}
	}

	// 判断是收信还是转发，只要是登陆了，都当成转发处理
	if s.Ctx.UserID > 0 {
		details.Stage = "校验发件权限"
		if !s.authorizedSubmission(email) {
			return oerrors.New("No Auth")
		}

		details.Stage = "执行发信前插件"
		log.WithContext(ctx).Debugf("开始执行插件SendBefore！")
		for _, hook := range hooks.Snapshot() {
			if hook == nil {
				continue
			}
			hook.SendBefore(ctx, email)
		}
		log.WithContext(ctx).Debugf("开始执行插件SendBefore！End")

		if email == nil {
			return nil
		}
		if !s.authorizedSubmission(email) {
			return oerrors.New("No Auth")
		}

		details.Stage = "保存并投递客户端邮件"
		sender := outgoingSender(send.Send)
		if s.isNotification() {
			details.Stage = "校验系统通知配额并投递"
			if err := s.prepareNotification(email, len(emailData)); err != nil {
				return err
			}
			sender = func(ctx *context.Context, email *parsemail.Email) (error, map[string]error) {
				return send.SendTriggered(ctx, email, "系统通知 SMTP 发信")
			}
		}
		resultErr = s.deliverOutgoingEmail(email, sender)
		if resultErr == nil {
			result = "accepted"
		}
		return resultErr

	} else {
		// 收件
		var dkimStatus, SPFStatus bool

		details.Stage = "验证收信身份与插件处理"
		// DKIM校验
		dkimStatus = parsemail.Check(ctx, bytes.NewReader(emailData))

		SPFStatus = spfCheck(s.RemoteAddress.String(), email.Sender, email.Sender.EmailAddress)

		_, formDomain := email.From.GetDomainAccount()
		domains := config.CurrentMailDomains()
		spoofed := array.InArray(formDomain, domains.Accepted) && SPFStatus == false
		if spoofed {
			dkimStatus = false
		}
		email.Authentication = parsemail.NewEmailAuthentication(SPFStatus, dkimStatus)

		log.WithContext(ctx).Debugf("开始执行插件ReceiveParseAfter！")
		for _, hook := range hooks.Snapshot() {
			if hook == nil {
				continue
			}
			email.Authentication = parsemail.NewEmailAuthentication(SPFStatus, dkimStatus)
			hook.ReceiveParseAfter(ctx, email)
		}
		log.WithContext(ctx).Debugf("开始执行插件ReceiveParseAfter！End")
		email.Authentication = parsemail.NewEmailAuthentication(SPFStatus, dkimStatus)
		// 伪造邮件状态必须覆盖插件分类结果，保持原有处理优先级。
		if spoofed {
			email.Status = 3
		}

		details.Stage = "保存收到的邮件"
		users, dbEmail, saveErr := saveEmail(ctx, len(emailData), email, 0, 0, s.To, SPFStatus, dkimStatus)
		if oerrors.Is(saveErr, errAllRecipientsBlocked) {
			return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "Message rejected by recipient policy"}
		}
		if saveErr != nil {
			log.WithContext(ctx).Errorf("CRITICAL audit_event=initial_persist_failed direction=inbound msg_id=%q error=%v", email.MsgID, saveErr)
			return localPersistenceSMTPError()
		}
		if dbEmail == nil {
			result = "discarded"
			return nil
		}
		for _, user := range users {
			recipientIDs = append(recipientIDs, user.ID)
		}
		details.Stage = "执行收信规则与通知"

		if email.MessageId > 0 {
			ruleBaseContext := ctx.Context
			if ruleBaseContext == nil {
				ruleBaseContext = stdcontext.Background()
			}
			ruleExecutionContext, cancelRules := stdcontext.WithTimeout(ruleBaseContext, maxSMTPRuleProcessingTime)
			ruleContext := *ctx
			ruleContext.Context = ruleExecutionContext
			log.WithContext(ctx).Debugf("开始执行邮件规则！")
			for _, user := range users {
				ruleContext.UserID, ruleContext.Role = user.ID, user.EffectiveRole()
				if ruleExecutionContext.Err() != nil {
					log.WithContext(&ruleContext).WithFields(log.Fields{"action": "执行收信规则", "result": "failure"}).Warn("规则处理超时，已停止后续规则")
					break
				}
				// 执行邮件规则
				rs, ruleErr := rule.GetAllRules(&ruleContext, user.ID)
				if ruleErr != nil {
					log.WithContext(&ruleContext).WithFields(log.Fields{"action": "读取收信规则", "result": "failure", "user_id": user.ID}).WithError(ruleErr).Error("读取收信规则失败")
					continue
				}
				for _, r := range rs {
					if rule.MatchRule(&ruleContext, r, email) {
						if err := rule.DoRule(&ruleContext, r, email, user, emailData); err != nil {
							log.WithContext(&ruleContext).WithFields(log.Fields{"action": "执行收信规则", "result": "failure", "user_id": user.ID}).WithError(err).Error("执行收信规则失败")
						}
					}
				}
			}
			cancelRules()
		}

		log.WithContext(ctx).Debugf("开始执行插件ReceiveSaveAfter！")
		var ue []*models.UserEmail
		err = db.Instance.Table(&models.UserEmail{}).Where("email_id=?", email.MessageId).Find(&ue)
		if err != nil {
			log.WithContext(ctx).Errorf("sql Error :%+v", err)
		}
		as3 := async.New(ctx)
		for _, hook := range hooks.Snapshot() {
			if hook == nil {
				continue
			}
			as3.WaitProcess(func(hk any) {
				hk.(framework.EmailHook).ReceiveSaveAfter(ctx, email, ue)
			}, hook)
		}
		as3.Wait()
		log.WithContext(ctx).Debugf("开始执行插件ReceiveSaveAfter！End")

		// IDLE命令通知
		for _, user := range users {
			noticeContext := *ctx
			noticeContext.UserID, noticeContext.Role = user.ID, user.EffectiveRole()
			if err := imap_server.IdleNotice(&noticeContext, user.ID, dbEmail); err != nil {
				log.WithContext(&noticeContext).WithFields(log.Fields{"action": "通知 IMAP 客户端新邮件", "result": "failure"}).WithError(err).Error("邮件已保存，但新邮件通知失败")
			}
		}

	}

	result = "success"
	details.Stage = "接收并保存邮件"
	details.Outcome = "邮件已写入本地邮箱；规则与通知的异常单独记录在运行日志"
	return nil
}

func validateOutgoingEmail(email *parsemail.Email) error {
	if err := validation.ValidateEmail(email, validation.EmailLimits{
		MaxRecipients:           50,
		MaxAttachments:          maxSMTPAttachmentCount,
		MaxAttachmentBytes:      maxSMTPMessageBytes,
		MaxTotalAttachmentBytes: maxSMTPMessageBytes,
		MaxBodyBytes:            maxSMTPMessageBytes,
		MaxEstimatedBytes:       maxSMTPMessageBytes,
	}); err != nil {
		return &smtp.SMTPError{Code: 552, EnhancedCode: smtp.EnhancedCode{5, 3, 4}, Message: "Message exceeds safety limits"}
	}
	return nil
}

func authorizedSubmissionEmail(email *parsemail.Email, envelopeFrom string, ctx *context.Context) bool {
	domains := config.CurrentMailDomains()
	if email == nil || email.From == nil || email.Sender == nil || ctx == nil || len(domains.Accepted) == 0 {
		return false
	}
	allowLocalAlias := ctx.EffectiveRole() == access.RoleSuperAdmin
	if !validation.IsAuthorizedSender(email.From.EmailAddress, ctx.UserAccount, allowLocalAlias, domains.Accepted) {
		return false
	}
	if !validation.IsAuthorizedSender(email.Sender.EmailAddress, ctx.UserAccount, allowLocalAlias, domains.Accepted) {
		return false
	}
	return envelopeFrom == "" || validation.IsAuthorizedSender(envelopeFrom, ctx.UserAccount, allowLocalAlias, domains.Accepted)
}

func saveEmail(ctx *context.Context, size int, email *parsemail.Email, sendUserID int, emailType int, reallyTo []string, SPFStatus, dkimStatus bool) ([]*models.User, *models.Email, error) {
	if email == nil {
		return nil, nil, oerrors.New("email is missing")
	}
	if email.From == nil {
		return nil, nil, oerrors.New("email sender is missing")
	}
	if emailType == int(consts.EmailTypeReceive) && email.Authentication != nil {
		SPFStatus = email.Authentication.SPFPassed
		dkimStatus = email.Authentication.DKIMPassed
	}

	users, drop, err := resolveIncomingUsers(ctx, email, emailType, reallyTo, SPFStatus, dkimStatus)
	if err != nil || drop {
		return users, nil, err
	}
	if emailType == int(consts.EmailTypeReceive) {
		userIDs := make([]int, 0, len(users))
		for _, user := range users {
			userIDs = append(userIDs, user.ID)
		}
		session := db.Instance.NewSession()
		blocked, filterErr := models.BlacklistedRecipientIDs(session, userIDs, email.SenderFilterAddresses()...)
		session.Close()
		if filterErr != nil {
			return nil, nil, filterErr
		}
		allowed := make([]*models.User, 0, len(users))
		for _, user := range users {
			if !blocked[user.ID] {
				allowed = append(allowed, user)
			}
		}
		users = allowed
		if len(users) == 0 {
			return nil, nil, errAllRecipientsBlocked
		}
	}

	msgID := email.MsgID
	if msgID == "" {
		domains := config.CurrentMailDomains()
		if domains.Primary == "" {
			return nil, nil, fmt.Errorf("runtime configuration is unavailable")
		}
		msgID, err = parsemail.GenerateMsgID(domains.Primary)
		if err != nil {
			return users, nil, fmt.Errorf("generate message ID: %w", err)
		}
	}
	if email.Size == 0 {
		email.Size = size
	}

	storedFields, err := email.EncodeStoredFields()
	if err != nil {
		return users, nil, fmt.Errorf("encode stored email fields: %w", err)
	}

	modelEmail := models.Email{
		Type:         cast.ToInt8(emailType),
		Subject:      email.Subject,
		ReplyTo:      storedFields.ReplyTo,
		FromName:     email.From.Name,
		FromAddress:  email.From.EmailAddress,
		To:           storedFields.To,
		Bcc:          storedFields.Bcc,
		Cc:           storedFields.Cc,
		Text:         sql.NullString{String: string(email.Text), Valid: true},
		Html:         sql.NullString{String: string(email.HTML), Valid: true},
		Sender:       storedFields.Sender,
		Attachments:  storedFields.Attachments,
		Size:         email.Size,
		SPFCheck:     boolInt8(SPFStatus),
		DKIMCheck:    boolInt8(dkimStatus),
		SendUserID:   sendUserID,
		SendDate:     time.Now(),
		Status:       cast.ToInt8(email.Status),
		CreateTime:   time.Now(),
		CronSendTime: time.Now(),
		MsgID:        msgID,
	}

	var userIDs []int
	if emailType == int(consts.EmailTypeReceive) {
		for _, user := range users {
			userIDs = append(userIDs, user.ID)
		}
	} else {
		modelEmail.Status = consts.EmailStatusDeliveryPending
		modelEmail.Error = sql.NullString{String: deliveryPendingError, Valid: true}
		userIDs = []int{sendUserID}
	}

	log.WithContext(ctx).Debugf("开始入库！")
	saved, err := persistInitialAudit(ctx, modelEmail, userIDs)
	if err != nil {
		return users, nil, err
	}
	email.MessageId = cast.ToInt64(saved.Id)
	email.MsgID = saved.MsgID
	return users, saved, nil
}

func spfCheck(remoteAddress string, sender *parsemail.User, senderString string) bool {
	//spf校验
	if sender == nil {
		return false
	}

	ipAddress, err := netip.ParseAddrPort(remoteAddress)
	if err != nil || !ipAddress.Addr().IsValid() {
		return false
	}

	ip := net.ParseIP(ipAddress.Addr().String())
	if ip == nil {
		return false
	}
	if ip.IsPrivate() {
		return true
	}

	tmp := strings.Split(sender.EmailAddress, "@")
	if len(tmp) < 2 {
		return false
	}

	res := spf.CheckHost(ip, tmp[1], senderString, "")

	if res == spf.None || res == spf.Pass {
		// spf校验通过
		return true
	}
	return false
}
