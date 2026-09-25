package auth

import (
	"errors"
	"sync/atomic"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/notification"
)

// ProtocolSession 保存不可变身份与凭据快照，拒绝后的连接不能通过重新启用恢复权限。
type ProtocolSession struct {
	user         models.User
	grant        *credentials.ProtocolGrant
	notification *notification.Grant
	invalid      atomic.Bool
}

func AuthenticateProtocolSession(protocol, remoteAddress, account, plain string) (*models.User, *ProtocolSession, error) {
	if notification.IsUsername(account) {
		return authenticateNotification(protocol, remoteAddress, account, plain)
	}
	var grant *credentials.ProtocolGrant
	user, err := authenticateProtocol(protocol, remoteAddress, account, plain, &grant)
	if err != nil {
		return nil, nil, err
	}
	session := &ProtocolSession{user: *user, grant: grant}
	if err := session.Check(); err != nil {
		return nil, nil, err
	}
	return user, session, nil
}

// Check 在每条数据命令前复核授权；数据库故障或切换时也不沿用旧权限。
func (s *ProtocolSession) Check() error {
	denied := errors.New("protocol authorization revoked")
	if s == nil || s.invalid.Load() {
		return denied
	}
	engine := db.Instance
	validGrant := engine != nil && (s.grant.Valid(engine) || s.notification.Valid(engine))
	if validGrant {
		var user models.User
		has, err := engine.ID(s.user.ID).Where("disabled = 0").Get(&user)
		if err == nil && has && user.Account == s.user.Account && user.Password == s.user.Password &&
			user.SessionVersion == s.user.SessionVersion && user.SecurityVersion == s.user.SecurityVersion &&
			user.EffectiveRole() == s.user.EffectiveRole() && user.MustChangePassword == s.user.MustChangePassword && !s.invalid.Load() {
			return nil
		}
	}
	s.invalid.Store(true)
	return denied
}
