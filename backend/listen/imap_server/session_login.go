package imap_server

import (
	"errors"

	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/utils/password"
	"github.com/emersion/go-imap/v2"
	log "github.com/sirupsen/logrus"
)

func (s *serverSession) Login(username, pwd string) error {
	user, err := auth.AuthenticateProtocol("imap", s.remoteAddress, username, pwd)
	if err != nil {
		if errors.Is(err, password.ErrBusy) {
			return &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeUnavailable, Text: "Authentication temporarily unavailable"}
		}
		log.WithContext(s.ctx).WithFields(log.Fields{
			"action": "IMAP 客户端登录",
			"result": "failure",
			"object": s.remoteAddress,
		}).Info("IMAP 客户端认证失败")
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Code: imap.ResponseCodeAuthenticationFailed,
			Text: "Invalid credentials (Failure)",
		}
	}

	s.status = AUTHORIZED
	s.ctx.UserID = user.ID
	s.ctx.UserName = user.Name
	s.ctx.UserAccount = user.Account
	s.ctx.Role = user.EffectiveRole()
	s.ctx.IsAdmin = s.ctx.Role == access.RoleSuperAdmin || s.ctx.Role == access.RoleAdmin
	log.WithContext(s.ctx).WithFields(log.Fields{
		"action": "IMAP 客户端登录",
		"result": "success",
		"object": s.remoteAddress,
	}).Debug("IMAP 客户端认证成功")
	return nil
}
