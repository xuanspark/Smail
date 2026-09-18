package recovery

import (
	"errors"
	"strings"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/verification"
	"git.pcux.cn/xuans/Smail/utils/password"
)

type PublicFlow struct {
	Token   string      `json:"token,omitempty"`
	Role    access.Role `json:"role"`
	Email   string      `json:"email"`
	Methods []string    `json:"methods"`
}

// IdentifyPublicFlow 只公开脱敏邮箱，验证方式由数据库角色和已配置凭据决定。
func (s *Service) IdentifyPublicFlow(identifier string) (*PublicFlow, error) {
	if s == nil || s.engine == nil || s.external == nil || s.credentials == nil {
		return nil, ErrUnavailable
	}
	account, err := auth.NormalizeIdentifier(identifier)
	if err != nil || account == "" {
		return nil, ErrRecoveryDenied
	}
	var user models.User
	found, err := s.engine.Where("account = ? AND disabled = 0", account).Get(&user)
	if err != nil {
		return nil, ErrUnavailable
	}
	if !found {
		return nil, ErrRecoveryDenied
	}
	result := &PublicFlow{Role: user.EffectiveRole(), Methods: []string{}}
	if result.Role == access.RoleSuperAdmin {
		return result, nil
	}
	item := &publicTicket{Flow: true, UserID: user.ID, Account: user.Account, Role: user.EffectiveRole(), SessionVersion: user.SessionVersion, SecurityVersion: user.SecurityVersion}
	bindings, err := s.external.List(user.ID)
	if err != nil {
		return nil, ErrUnavailable
	}
	var binding *models.ExternalEmail
	for i := range bindings {
		candidate := &bindings[i]
		if candidate.Status != models.ExternalEmailStatusVerified {
			continue
		}
		if candidate.EmailType == models.ExternalEmailTypePrimary {
			binding = candidate
			break
		}
		if candidate.EmailType == models.ExternalEmailTypeBackup && binding == nil {
			binding = candidate
		}
	}
	if binding != nil {
		email, err := s.external.Decrypt(*binding)
		if err != nil {
			return nil, ErrUnavailable
		}
		parts := strings.SplitN(email, "@", 2)
		if len(parts) != 2 || parts[0] == "" {
			return nil, ErrUnavailable
		}
		result.Email = string([]rune(parts[0])[0]) + "***@" + parts[1]
		item.CredentialID, item.CredentialVersion = binding.ID, binding.CredentialVersion
		if s.verification != nil {
			result.Methods = append(result.Methods, "email")
		}
	}
	if result.Role == access.RoleUser {
		configured, err := s.credentials.HasValidRecoveryCode(user.ID)
		if err != nil {
			return nil, ErrUnavailable
		}
		if configured {
			result.Methods = append(result.Methods, "security_code")
		}
	}
	if len(result.Methods) == 0 {
		return result, nil
	}
	token, err := newTicketID()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(s.now())
	if len(s.publicTickets) >= maxRecoveryTickets {
		return nil, ErrUnavailable
	}
	item.ExpiresAt = s.now().Add(decoyChallengeTTL)
	s.publicTickets[ticketKey(token)] = item
	result.Token = token
	return result, nil
}

// lockPublicFlow 占用当前步骤，防止验证码发送、核销和密码提交并发复用。
func (s *Service) lockPublicFlow(token string) (*publicTicket, error) {
	if s == nil || strings.TrimSpace(token) == "" {
		return nil, ErrRecoveryDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(s.now())
	item := s.publicTickets[ticketKey(token)]
	if item == nil || !item.Flow || item.Verified || item.Busy {
		return nil, ErrRecoveryExpired
	}
	item.Busy = true
	return item, nil
}

func (s *Service) releasePublicFlow(token string, item *publicTicket) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.publicTickets[ticketKey(token)] == item {
		item.Busy = false
	}
}

func (s *Service) publicFlowIdentity(item *publicTicket) (string, error) {
	var user models.User
	found, err := s.engine.ID(item.UserID).Get(&user)
	if err != nil {
		return "", ErrUnavailable
	}
	if !found || user.Disabled != 0 || user.EffectiveRole() == access.RoleSuperAdmin || user.EffectiveRole() != item.Role || user.SecurityVersion != item.SecurityVersion || user.SessionVersion != item.SessionVersion {
		return "", ErrRecoveryDenied
	}
	if item.CredentialID == 0 {
		return "", nil
	}
	binding, err := s.external.FindVerifiedByID(user.ID, item.CredentialID)
	if err != nil || binding.CredentialVersion != item.CredentialVersion {
		return "", ErrRecoveryDenied
	}
	return s.external.Decrypt(*binding)
}

func (s *Service) SendPublicFlowCode(token, remoteIP string) error {
	item, err := s.lockPublicFlow(token)
	if err != nil {
		return err
	}
	defer s.releasePublicFlow(token, item)
	email, err := s.publicFlowIdentity(item)
	if err != nil {
		return err
	}
	if email == "" || s.verification == nil {
		return ErrRecoveryDenied
	}
	challenge, err := s.verification.Issue(verification.IssueRequest{
		Purpose: verification.PurposePasswordRecover, UserID: item.UserID, Account: item.Account,
		Email: email, ResourceID: item.CredentialID, CredentialVersion: item.CredentialVersion,
		BrowserTransaction: token,
	}, remoteIP)
	if err != nil {
		return err
	}
	s.mu.Lock()
	item.ChallengeID = challenge.ID
	s.mu.Unlock()
	return nil
}

func (s *Service) VerifyPublicFlow(token, method, code, remoteIP string) (string, error) {
	item, err := s.lockPublicFlow(token)
	if err != nil {
		return "", err
	}
	defer s.releasePublicFlow(token, item)
	email, err := s.publicFlowIdentity(item)
	if err != nil {
		return "", err
	}
	// 提前生成下一步令牌，避免随机源失败导致已核销安全码无法继续。
	next, err := newTicketID()
	if err != nil {
		return "", err
	}
	switch method {
	case "email":
		if email == "" || item.ChallengeID == "" || s.verification == nil {
			err = ErrRecoveryDenied
		} else {
			err = s.verification.VerifyBoundWithContext(item.ChallengeID, item.Account, email, strings.TrimSpace(code), verification.PurposePasswordRecover, item.CredentialID, remoteIP, token)
		}
	case "security_code":
		if item.Role != access.RoleUser {
			err = ErrRecoveryDenied
		} else {
			err = s.credentials.ConsumeRecoveryCode(item.UserID, code)
		}
	default:
		err = ErrRecoveryDenied
	}
	if err != nil {
		if errors.Is(err, password.ErrBusy) {
			return "", err
		}
		s.mu.Lock()
		item.Attempts++
		if item.Attempts >= maxPublicRecoveryCodeAttempts {
			delete(s.publicTickets, ticketKey(token))
		}
		s.mu.Unlock()
		return "", ErrRecoveryDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.publicTickets[ticketKey(token)] != item || !s.now().Before(item.ExpiresAt) {
		return "", ErrRecoveryExpired
	}
	delete(s.publicTickets, ticketKey(token))
	item.Verified, item.Method, item.Busy = true, method, false
	item.ExpiresAt = s.now().Add(authorizationTTL)
	s.publicTickets[ticketKey(next)] = item
	return next, nil
}
