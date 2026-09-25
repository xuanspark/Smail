package smtp_server

import (
	"errors"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/services/notification"
	"git.pcux.cn/xuans/Smail/utils/validation"
	"github.com/emersion/go-smtp"
)

func (s *Session) isNotification() bool {
	return s.authorization != nil && s.authorization.IsNotification()
}

func (s *Session) authorizedSubmission(email *parsemail.Email) bool {
	if !s.isNotification() {
		return authorizedSubmissionEmail(email, s.From, s.Ctx)
	}
	sender := validation.NotificationAddress(config.CurrentMailDomains().Primary)
	return sender != "" && email != nil && email.From != nil && email.Sender != nil &&
		strings.EqualFold(s.From, sender) && strings.EqualFold(email.From.EmailAddress, sender) &&
		strings.EqualFold(email.Sender.EmailAddress, sender) && strings.EqualFold(email.EnvelopeFrom, sender)
}

// 插件处理后再校验最终内容，不能通过邮件头额外添加信封中没有的收件人。
func (s *Session) prepareNotification(email *parsemail.Email, rawBytes int) error {
	if err := s.checkAuthorization(); err != nil {
		return err
	}
	if rawBytes > notification.MaxMessageBytes {
		return &smtp.SMTPError{Code: 552, EnhancedCode: smtp.EnhancedCode{5, 3, 4}, Message: "Notification message exceeds size limit"}
	}
	if err := validation.ValidateEmail(email, validation.EmailLimits{
		MaxRecipients: notification.MaxRecipients, MaxAttachments: 10,
		MaxAttachmentBytes: notification.MaxMessageBytes, MaxTotalAttachmentBytes: notification.MaxMessageBytes,
		MaxBodyBytes: notification.MaxMessageBytes, MaxEstimatedBytes: notification.MaxMessageBytes,
	}); err != nil {
		return &smtp.SMTPError{Code: 552, EnhancedCode: smtp.EnhancedCode{5, 3, 4}, Message: "Notification content exceeds safety limits"}
	}
	allowed := make(map[string]bool, len(s.To))
	for _, address := range s.To {
		allowed[strings.ToLower(address)] = true
	}
	recipients := append(append(append([]*parsemail.User{}, email.To...), email.Cc...), email.Bcc...)
	for _, recipient := range recipients {
		if recipient == nil || !allowed[strings.ToLower(recipient.EmailAddress)] {
			return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "Notification recipient is not authorized by envelope"}
		}
	}
	if err := s.authorization.ReserveNotification(len(recipients)); err != nil {
		if errors.Is(err, notification.ErrQuota) {
			return &smtp.SMTPError{Code: 452, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: "Notification sending quota exceeded"}
		}
		if errors.Is(err, notification.ErrDenied) {
			return &smtp.SMTPError{Code: 535, EnhancedCode: smtp.EnhancedCode{5, 7, 8}, Message: "Notification authorization revoked"}
		}
		return localPersistenceSMTPError()
	}
	// 系统通知不能冒充账号恢复邮件，也不能请求向任意地址回执。
	email.From.Name, email.Sender.Name = "系统通知", "系统通知"
	email.ReplyTo, email.ReadReceipt = nil, nil
	return nil
}
