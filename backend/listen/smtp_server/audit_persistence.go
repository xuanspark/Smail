package smtp_server

import (
	stderrors "errors"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/outbox"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/context"
	smtp "github.com/emersion/go-smtp"
	log "github.com/sirupsen/logrus"
	"xorm.io/builder"
	"xorm.io/xorm"
)

const deliveryPendingError = "delivery result pending; outcome unknown after interruption"

type auditEnqueuer func(*xorm.Session, models.Email) error

func (s *Session) deliverOutgoingEmail(email *parsemail.Email, trigger string) error {
	if err := validateOutgoingEmail(email); err != nil {
		return err
	}
	authorization, err := s.authorization.QueueAuthorization()
	if err != nil || s.Ctx == nil || authorization.UserID != s.Ctx.UserID {
		return &smtp.SMTPError{Code: 535, EnhancedCode: smtp.EnhancedCode{5, 7, 8}, Message: "Authentication credentials revoked"}
	}
	submission := outbox.Submission{
		EnvelopeFrom: s.From, Date: email.Date, Trigger: trigger, Authorization: authorization,
	}
	enqueue := func(session *xorm.Session, saved models.Email) error {
		return outbox.EnqueueSMTP(session, saved, s.To, submission)
	}
	_, _, err = saveEmail(s.Ctx, email.Size, email, s.Ctx.UserID, int(consts.EmailTypeSend), nil, true, true, enqueue)
	if err != nil {
		log.WithContext(s.Ctx).Errorf("CRITICAL audit_event=submission_enqueue_failed direction=outbound msg_id=%q error=%v", email.MsgID, err)
		return localPersistenceSMTPError()
	}
	return nil
}

func localPersistenceSMTPError() error {
	return &smtp.SMTPError{
		Code:         451,
		EnhancedCode: smtp.EnhancedCode{4, 3, 0},
		Message:      "Temporary local persistence failure",
	}
}

func persistInitialAudit(ctx *context.Context, modelEmail models.Email, userIDs []int, enqueue ...auditEnqueuer) (*models.Email, error) {
	var saved models.Email
	err := db.Transaction(db.Instance, func(session *xorm.Session) error {
		candidate := modelEmail
		candidate.Id = 0
		if _, err := session.Insert(&candidate); err != nil {
			return err
		}
		if candidate.Id <= 0 {
			return stderrors.New("email insert returned no id")
		}
		for _, userID := range userIDs {
			relation := models.UserEmail{EmailID: candidate.Id, UserID: userID, Status: candidate.Status}
			if _, err := session.Insert(&relation); err != nil {
				return err
			}
		}
		for _, persist := range enqueue {
			if err := persist(session, candidate); err != nil {
				return err
			}
		}
		saved = candidate
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

func resolveIncomingUsers(ctx *context.Context, email *parsemail.Email, emailType int, reallyTo []string, SPFStatus, dkimStatus bool) ([]*models.User, bool, error) {
	if emailType != int(consts.EmailTypeReceive) {
		return nil, false, nil
	}

	policy := config.CurrentEmailPolicy()
	domains := config.CurrentMailDomains()
	if len(domains.Accepted) == 0 {
		return nil, false, stderrors.New("runtime configuration is unavailable")
	}

	accounts := incomingAccounts(email, reallyTo)
	var users []*models.User
	if len(accounts) > 0 {
		where, params, err := builder.ToSQL(builder.And(builder.In("LOWER(account)", accounts), builder.Eq{"disabled": 0}))
		if err != nil {
			return nil, false, err
		}
		if err = db.Instance.Table(&models.User{}).Where(where, params...).Find(&users); err != nil {
			return nil, false, err
		}
	}
	if len(users) > 0 {
		return users, false, nil
	}

	if DropUnknownRecipientEmails &&
		((policy.SpamFilterLevel == 1 && !SPFStatus && !dkimStatus) ||
			(policy.SpamFilterLevel == 2 && !SPFStatus) ||
			(policy.SpamFilterLevel == 3 && !dkimStatus)) {
		log.WithContext(ctx).Infoln("垃圾邮件，拒信")
		log.WithContext(ctx).Infof("收件人不存在且DKIM验证失败，丢弃邮件: recipient_count=%d", len(accounts))
		return nil, true, nil
	}

	log.WithContext(ctx).Infof("收件人不存在但DKIM验证通过，转交管理员: recipient_count=%d", len(accounts))
	if err := db.Instance.Table(&models.User{}).Where(models.SuperAdminPredicate + " AND disabled=0").Find(&users); err != nil {
		return nil, false, err
	}
	if len(users) == 0 {
		return nil, false, stderrors.New("no administrator available for unknown recipient")
	}
	return users, false, nil
}

func incomingAccounts(email *parsemail.Email, reallyTo []string) []string {
	domains := config.CurrentMailDomains()
	var accounts []string
	if len(reallyTo) > 0 {
		for _, recipient := range reallyTo {
			account := parsemail.BuilderUser(recipient)
			if account == nil {
				continue
			}
			name, domain := account.GetDomainAccount()
			if array.InArray(domain, domains.Accepted) && name != "" {
				accounts = append(accounts, strings.ToLower(name))
			}
		}
		return accounts
	}
	for _, recipients := range [][]*parsemail.User{email.To, email.Cc, email.Bcc} {
		for _, user := range recipients {
			account, _ := user.GetDomainAccount()
			if account != "" {
				accounts = append(accounts, strings.ToLower(account))
			}
		}
	}
	return accounts
}

func boolInt8(value bool) int8 {
	if value {
		return 1
	}
	return 0
}
