package recovery

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/utils/password"
)

const maxPublicRecoveryCodeAttempts = 5

// PublicLinkDelivery 投递匿名恢复入口的一次性链接令牌。
// token 仅在调用期间存在，调用方不得写入日志或持久化。
type PublicLinkDelivery func(email, token string) error

// publicTicket 只保存在进程内存中，map key 为令牌摘要而非令牌明文。
type publicTicket struct {
	Flow              bool
	Verified          bool
	Method            string
	ChallengeID       string
	UserID            int
	Account           string
	Role              access.Role
	SessionVersion    int
	SecurityVersion   int
	CredentialID      int
	CredentialVersion int
	ExpiresAt         time.Time
	Attempts          int
	Busy              bool
}

// BeginPublicLink 为非超级管理员生成独立匿名恢复链接。
// 为避免账户枚举，调用方应将所有错误映射为同一已受理响应。
func (s *Service) BeginPublicLink(identifier string) error {
	if s == nil || s.engine == nil || s.external == nil {
		return ErrUnavailable
	}
	account, err := auth.NormalizeIdentifier(identifier)
	if err != nil || account == "" {
		return ErrRecoveryDenied
	}
	var user models.User
	has, err := s.engine.Where("account = ? AND disabled = 0", account).Get(&user)
	if err != nil || !has || user.EffectiveRole() == access.RoleSuperAdmin {
		return ErrRecoveryDenied
	}
	binding, err := s.external.FindVerifiedPrimaryByUser(user.ID)
	if err != nil {
		return ErrRecoveryDenied
	}
	email, err := s.external.Decrypt(*binding)
	if err != nil || strings.TrimSpace(email) == "" {
		return ErrRecoveryDenied
	}

	token, err := newTicketID()
	if err != nil {
		return err
	}
	key := ticketKey(token)
	s.mu.Lock()
	s.cleanupLocked(s.now())
	delivery := s.publicDelivery
	if delivery == nil || len(s.publicTickets) >= maxRecoveryTickets {
		s.mu.Unlock()
		return ErrUnavailable
	}
	for ticketKey, item := range s.publicTickets {
		if item.UserID == user.ID {
			// 正在提交的旧链接优先完成，期间不签发会与其竞争的新链接。
			// 调用方统一隐藏该结果，因此不会泄露账户或链接状态。
			if item.Busy {
				s.mu.Unlock()
				return ErrRecoveryDenied
			}
			delete(s.publicTickets, ticketKey)
		}
	}
	s.publicTickets[key] = &publicTicket{
		UserID: user.ID, Account: user.Account, Role: user.EffectiveRole(),
		SessionVersion: user.SessionVersion, SecurityVersion: user.SecurityVersion,
		CredentialID: binding.ID, CredentialVersion: binding.CredentialVersion,
		ExpiresAt: s.now().Add(authorizationTTL),
	}
	s.mu.Unlock()

	if err := delivery(email, token); err != nil {
		s.mu.Lock()
		delete(s.publicTickets, key)
		s.mu.Unlock()
		return fmt.Errorf("deliver public recovery link: %w", err)
	}
	return nil
}

// PublicLinkRequiresRecoveryCode 返回当前一次性链接是否属于管理员。
// 此方法只为恢复页决定是否展示恢复码字段；令牌必须由收件人持有，且不会返回账号或角色详情。
func (s *Service) PublicLinkRequiresRecoveryCode(token string) (bool, error) {
	if s == nil || strings.TrimSpace(token) == "" {
		return false, ErrInvalidRequest
	}
	key := ticketKey(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(s.now())
	item, ok := s.publicTickets[key]
	if !ok || item.Busy {
		return false, ErrRecoveryExpired
	}
	return item.Role == access.RoleAdmin, nil
}

// CompletePublicLink 在同一数据库事务中完成管理员恢复码核销、密码更新和旧凭据撤销。
func (s *Service) CompletePublicLink(token, newPassword, recoveryCode string) error {
	return s.completePublicLink(token, newPassword, recoveryCode, false)
}

func (s *Service) CompletePublicFlow(token, newPassword string) error {
	return s.completePublicLink(token, newPassword, "", true)
}

func (s *Service) completePublicLink(token, newPassword, recoveryCode string, flow bool) error {
	defer clearString(&newPassword)
	defer clearString(&recoveryCode)
	if s == nil || s.engine == nil || s.external == nil || s.credentials == nil || strings.TrimSpace(token) == "" {
		return ErrInvalidRequest
	}
	if password.ValidateUserPassword(newPassword) != nil {
		return ErrInvalidNewPassword
	}
	key := ticketKey(token)
	s.mu.Lock()
	s.cleanupLocked(s.now())
	item, ok := s.publicTickets[key]
	if !ok {
		s.mu.Unlock()
		return ErrRecoveryExpired
	}
	if item.Busy {
		s.mu.Unlock()
		return ErrRecoveryDenied
	}
	if item.Flow != flow || (flow && (!item.Verified || (item.Role == access.RoleAdmin && item.Method != "email"))) {
		s.mu.Unlock()
		return ErrRecoveryDenied
	}
	item.Busy = true
	s.mu.Unlock()

	consume := false
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.publicTickets[key] != item {
			return
		}
		if consume || !s.now().Before(item.ExpiresAt) {
			delete(s.publicTickets, key)
			return
		}
		item.Busy = false
	}()

	var user models.User
	has, err := s.engine.ID(item.UserID).Get(&user)
	if err != nil || !has || user.Disabled != 0 || user.EffectiveRole() == access.RoleSuperAdmin ||
		user.EffectiveRole() != item.Role || user.SessionVersion != item.SessionVersion || user.SecurityVersion != item.SecurityVersion {
		consume = true
		return ErrRecoveryDenied
	}
	if !flow || item.Method == "email" {
		binding, err := s.external.FindVerifiedByID(item.UserID, item.CredentialID)
		if err != nil || binding.CredentialVersion != item.CredentialVersion {
			consume = true
			return ErrRecoveryDenied
		}
	}

	if !flow && user.EffectiveRole() == access.RoleAdmin {
		if strings.TrimSpace(recoveryCode) == "" {
			if s.recordPublicRecoveryCodeFailure(key, item) {
				consume = true
			}
			return ErrRecoveryDenied
		}
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}
	defer clearString(&hash)
	version := user.SessionVersion + 1
	if version <= 0 {
		version = 1
	}
	securityVersion := user.SecurityVersion + 1
	if securityVersion <= 0 {
		securityVersion = 1
	}
	now := s.now().UTC()
	session := s.engine.NewSession()
	if session == nil {
		return ErrUnavailable
	}
	defer session.Close()
	if err := session.Begin(); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = session.Rollback()
		}
	}()
	if !flow && user.EffectiveRole() == access.RoleAdmin {
		if err := s.credentials.ConsumeRecoveryCodeWithSession(session, user.ID, recoveryCode); err != nil {
			if errors.Is(err, password.ErrBusy) {
				return err
			}
			if s.recordPublicRecoveryCodeFailure(key, item) {
				consume = true
			}
			return ErrRecoveryDenied
		}
	}
	if affected, err := session.ID(item.UserID).Where("session_version = ? AND security_version = ? AND disabled = 0", item.SessionVersion, item.SecurityVersion).
		Cols("password", "session_version", "security_version", "must_change_password", "password_changed_at").Update(&models.User{
		Password: hash, SessionVersion: version, SecurityVersion: securityVersion, MustChangePassword: 0, PasswordChangedAt: &now,
	}); err != nil {
		return err
	} else if affected != 1 {
		consume = true
		return ErrRecoveryDenied
	}
	if err := s.credentials.RevokeAllWithSession(session, item.UserID); err != nil {
		return err
	}
	if err := session.Commit(); err != nil {
		return err
	}
	committed = true
	consume = true
	s.InvalidateUser(item.UserID)
	if s.verification != nil && s.verification.Store != nil {
		_ = s.verification.Store.InvalidateAccount(user.Account)
	}
	s.invalidateSecurityState(item.UserID, user.Account)
	return nil
}

func (s *Service) recordPublicRecoveryCodeFailure(key string, item *publicTicket) bool {
	if s == nil || item == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.publicTickets[key] != item {
		return true
	}
	item.Attempts++
	return item.Attempts >= maxPublicRecoveryCodeAttempts
}
