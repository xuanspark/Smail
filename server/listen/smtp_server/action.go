package smtp_server

import (
	"errors"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/id"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	log "github.com/sirupsen/logrus"
	"net"
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
	username = auth.NormalizeAccount(username)
	user, err := auth.AuthenticateProtocol("smtp", s.RemoteAddress.String(), username, pwd)
	if err != nil {
		log.WithContext(s.Ctx).Info("SMTP authentication failed")
		return errors.New("password error")
	}

	s.User = user.Account
	s.Ctx.UserAccount = user.Account
	s.Ctx.UserID = user.ID
	s.Ctx.UserName = user.Name
	s.Ctx.Role = user.EffectiveRole()
	s.Ctx.IsAdmin = s.Ctx.Role == access.RoleSuperAdmin || s.Ctx.Role == access.RoleAdmin
	log.WithContext(s.Ctx).Debug("SMTP authentication succeeded")
	return nil
}
func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	log.WithContext(s.Ctx).Debug("SMTP MAIL accepted")
	s.From = from
	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
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
