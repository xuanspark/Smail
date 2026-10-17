package smtp_server

import (
	"errors"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/notification"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/id"
	"git.pcux.cn/xuans/Smail/utils/password"
	"git.pcux.cn/xuans/Smail/utils/validation"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	log "github.com/sirupsen/logrus"
	"net"
	"strings"
)

// The Backend implements SMTP server methods.
type Backend struct{}

func (bkd *Backend) NewSession(conn *smtp.Conn) (smtp.Session, error) {

	remoteAddress := conn.Conn().RemoteAddr()
	ctx := &context.Context{}
	ctx.SetValue(context.LogID, id.GenLogID())
	log.WithContext(ctx).Debugf("新SMTP连接")

	return &Session{
		RemoteAddress: remoteAddress,
		Ctx:           ctx,
	}, nil
}

// A Session is returned after EHLO.
type Session struct {
	RemoteAddress net.Addr
	User          string
	From          string
	To            []string
	Ctx           *context.Context
	authorization *auth.ProtocolSession
}

// AuthMechanisms returns a slice of available auth mechanisms
// supported in this example.
func (s *Session) AuthMechanisms() []string {
	return []string{sasl.Plain, sasl.Login}
}

// Auth is the handler for supported authenticators.
func (s *Session) Auth(mech string) (sasl.Server, error) {
	log.WithContext(s.Ctx).Debugf("Auth :%s", mech)
	if mech == sasl.Plain {
		return sasl.NewPlainServer(func(identity, username, password string) error {
			return s.AuthPlain(username, password)
		}), nil
	}

	if mech == sasl.Login {
		return NewLoginServer(func(username, password string) error {
			return s.AuthPlain(username, password)
		}), nil
	}

	return nil, errors.New("Auth Not Supported")
}

func (s *Session) AuthPlain(username, pwd string) error {
	user, authorization, err := auth.AuthenticateProtocolSession("smtp", s.RemoteAddress.String(), username, pwd)
	if err != nil {
		if errors.Is(err, password.ErrBusy) {
			return &smtp.SMTPError{Code: 454, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: "Temporary authentication failure"}
		}
		log.WithContext(s.Ctx).WithFields(log.Fields{
			"action": "SMTP 客户端认证",
			"result": "failure",
			"object": s.RemoteAddress.String(),
		}).Info("SMTP 客户端认证失败")
		return &smtp.SMTPError{Code: 535, EnhancedCode: smtp.EnhancedCode{5, 7, 8}, Message: "Authentication credentials invalid"}
	}

	s.User = user.Account
	s.authorization = authorization
	s.Ctx.UserAccount = user.Account
	s.Ctx.UserID = user.ID
	s.Ctx.UserName = user.Name
	s.Ctx.Role = user.EffectiveRole()
	s.Ctx.IsAdmin = s.Ctx.Role == access.RoleSuperAdmin || s.Ctx.Role == access.RoleAdmin
	log.WithContext(s.Ctx).Debug("SMTP authentication succeeded")
	return nil
}
func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	if err := s.checkAuthorization(); err != nil {
		return err
	}
	if s.isNotification() && !validation.IsAuthorizedNotificationSender(from, config.CurrentMailDomains().Primary) {
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "Notification sender is not authorized"}
	}
	log.WithContext(s.Ctx).Debug("SMTP MAIL accepted")
	s.From = from
	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	if err := s.checkAuthorization(); err != nil {
		return err
	}
	if !validation.IsMailboxAddress(to) {
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 3}, Message: "Invalid recipient address"}
	}
	// 匿名连接仅接收受管域名的邮件，不能等到正文阶段再静默丢弃外域收件人。
	if s.authorization == nil {
		domains := config.CurrentMailDomains().Accepted
		if len(domains) == 0 {
			return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "Mail domains are unavailable"}
		}
		domain := to[strings.LastIndex(to, "@")+1:]
		local := false
		for _, accepted := range domains {
			if strings.EqualFold(domain, accepted) {
				local = true
				break
			}
		}
		if !local {
			return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "Relay access denied"}
		}
	}
	if validation.IsNotificationAddress(to, config.CurrentMailDomains().Accepted) {
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "Notification address does not accept mail"}
	}
	if s.isNotification() && len(s.To) >= notification.MaxRecipients {
		return &smtp.SMTPError{Code: 452, EnhancedCode: smtp.EnhancedCode{4, 5, 3}, Message: "Too many notification recipients"}
	}
	log.WithContext(s.Ctx).Debug("SMTP RCPT accepted")

	s.To = append(s.To, to)
	return nil
}

func (s *Session) Reset() {
	s.From = ""
	s.To = nil
}

func (s *Session) Logout() error {
	return nil
}

// 未认证的外部收信保留原流程，已认证连接不得在撤销后降级成匿名连接。
func (s *Session) checkAuthorization() error {
	if s.authorization != nil && s.authorization.Check() != nil {
		return &smtp.SMTPError{Code: 535, EnhancedCode: smtp.EnhancedCode{5, 7, 8}, Message: "Authentication credentials revoked"}
	}
	return nil
}
