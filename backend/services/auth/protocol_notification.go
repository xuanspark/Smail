package auth

import (
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/notification"
)

// 通知身份拥有独立用户名空间，不会回退到普通协议或网页认证。
func authenticateNotification(protocol, remoteAddress, account, plain string) (*models.User, *ProtocolSession, error) {
	account = strings.ToLower(strings.TrimSpace(account))
	identifier := strings.TrimPrefix(account, notification.UsernamePrefix)
	remote := protocolRemoteHost(remoteAddress)
	pairKey := hashedProtocolLoginKey("pair", "notification", remote, identifier)
	accountKey := hashedProtocolLoginKey("account", identifier)
	remoteKey := hashedProtocolLoginKey("remote", remote)
	fail := func() (*models.User, *ProtocolSession, error) {
		protocolPairAttempts.recordFailure(pairKey)
		protocolAccountAttempts.recordFailure(accountKey)
		protocolRemoteAttempts.recordFailure(remoteKey)
		return nil, nil, notification.ErrDenied
	}
	if protocolPairAttempts.blocked(pairKey) || protocolAccountAttempts.blocked(accountKey) || protocolRemoteAttempts.blocked(remoteKey) {
		return nil, nil, notification.ErrDenied
	}
	if protocol != "smtp" || db.Instance == nil {
		return fail()
	}
	identifier, err := NormalizeProtocolIdentifier(identifier)
	if err != nil {
		return fail()
	}
	identity, err := ResolveIdentifier(identifier)
	if err != nil || identity.UserID <= 0 {
		return fail()
	}
	var user models.User
	found, err := db.Instance.ID(identity.UserID).Get(&user)
	if err != nil || !found {
		return fail()
	}
	grant, err := notification.Authenticate(db.Instance, user, plain)
	if err != nil {
		return fail()
	}
	session := &ProtocolSession{user: user, notification: grant}
	if err := session.Check(); err != nil {
		return fail()
	}
	protocolPairAttempts.clear(pairKey)
	protocolAccountAttempts.clear(accountKey)
	return &user, session, nil
}

func (s *ProtocolSession) IsNotification() bool {
	return s != nil && s.notification != nil
}

func (s *ProtocolSession) ReserveNotification(recipients int) error {
	if !s.IsNotification() || s.Check() != nil {
		return notification.ErrDenied
	}
	return s.notification.Reserve(db.Instance, recipients, time.Now())
}

func (s *ProtocolSession) NotificationSenderName() string {
	if !s.IsNotification() {
		return ""
	}
	return s.notification.SenderName()
}
