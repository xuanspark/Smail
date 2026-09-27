package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync/atomic"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/notification"
	"xorm.io/xorm"
)

// ProtocolSession 保存不可变身份与凭据快照，拒绝后的连接不能通过重新启用恢复权限。
type ProtocolSession struct {
	user         models.User
	grant        *credentials.ProtocolGrant
	notification *notification.Grant
	invalid      atomic.Bool
}

type QueuedAuthorization struct {
	UserID     int    `json:"user_id"`
	Kind       string `json:"kind"`
	ID         int    `json:"id"`
	Version    int    `json:"version"`
	Credential string `json:"credential"`
	Identity   string `json:"identity"`
}

func (s *ProtocolSession) QueueAuthorization() (QueuedAuthorization, error) {
	var result QueuedAuthorization
	if err := s.Check(); err != nil {
		return result, err
	}
	if s.IsNotification() {
		result.Kind, result.ID, result.Version, result.Credential = s.notification.QueueBinding()
	} else {
		result.Kind, result.ID, result.Version, result.Credential = s.grant.QueueBinding()
	}
	if result.Kind == "" || result.ID <= 0 || result.Version <= 0 {
		return QueuedAuthorization{}, notification.ErrDenied
	}
	result.Identity = queuedUserIdentity(s.user)
	result.UserID = s.user.ID
	return result, nil
}

func queuedDigest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func queuedUserIdentity(user models.User) string {
	value, _ := json.Marshal([]any{
		user.ID, user.Account, user.Password, user.SessionVersion,
		user.SecurityVersion, user.EffectiveRole(), user.MustChangePassword,
	})
	return queuedDigest(string(value))
}

func (a QueuedAuthorization) Check(engine *xorm.Engine, user models.User) (bool, error) {
	if engine == nil {
		return false, notification.ErrUnavailable
	}
	if user.ID <= 0 || a.UserID != user.ID || user.Disabled != 0 || user.MustChangePassword != 0 || a.ID <= 0 || a.Version <= 0 ||
		subtle.ConstantTimeCompare([]byte(a.Identity), []byte(queuedUserIdentity(user))) != 1 {
		return false, nil
	}
	var hash string
	switch a.Kind {
	case "smtp":
		var item models.ProtocolCredential
		found, err := engine.ID(a.ID).Where("user_id = ? AND protocol = ? AND enabled = ? AND code_version = ? AND revoked_at IS NULL", user.ID, "smtp", true, a.Version).Get(&item)
		if err != nil || !found {
			return false, err
		}
		hash = item.CodeHash
	case "client":
		var item models.ClientCredential
		found, err := engine.ID(a.ID).Where("user_id = ? AND code_version = ? AND revoked_at IS NULL", user.ID, a.Version).Get(&item)
		if err != nil || !found {
			return false, err
		}
		hash = item.CodeHash
	case "notification":
		if a.ID != user.ID {
			return false, nil
		}
		item, err := notification.Load(engine, user.ID)
		if err != nil || !notification.Configured(item, user) || item.CodeVersion != a.Version {
			return false, err
		}
		hash = item.CodeHash
	default:
		return false, nil
	}
	return hash != "" && subtle.ConstantTimeCompare([]byte(a.Credential), []byte(queuedDigest(hash))) == 1, nil
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
