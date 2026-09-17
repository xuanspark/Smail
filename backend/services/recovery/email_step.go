package recovery

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"

	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/utils/password"
)

const maxEmailContinuationAttempts = 5

// EmailVerificationResult 仅在邮箱核验通过后告知是否需要管理员附加验证。
type EmailVerificationResult struct {
	Ticket               string `json:"ticket,omitempty"`
	RequiresRecoveryCode bool   `json:"requires_recovery_code,omitempty"`
	Continuation         string `json:"continuation,omitempty"`
	ExpiresIn            int    `json:"expires_in,omitempty"`
}

// 中间凭证与重置票据分开存储，不能用于直接修改密码。
type emailContinuation struct {
	ticket
	Account         string
	SecurityVersion int
	Attempts        int
	Busy            bool
}

func (s *Service) VerifyEmailStepWithContext(challengeID, account, email, code, remoteIP, browserContext string) (EmailVerificationResult, error) {
	if strings.TrimSpace(browserContext) == "" {
		return EmailVerificationResult{}, ErrRecoveryDenied
	}
	user, binding, err := s.verifyEmailIdentity(challengeID, account, email, code, remoteIP, browserContext)
	if err != nil {
		return EmailVerificationResult{}, err
	}
	if user.EffectiveRole() != access.RoleAdmin {
		id, err := s.issueTicketFor(user.ID, user.SessionVersion, "email", binding.ID, binding.CredentialVersion, browserContext)
		return EmailVerificationResult{Ticket: id}, err
	}
	id, err := newTicketID()
	if err != nil {
		return EmailVerificationResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.cleanupLocked(now)
	if len(s.emailPending) >= maxRecoveryTickets {
		return EmailVerificationResult{}, ErrUnavailable
	}
	s.emailPending[ticketKey(id)] = &emailContinuation{
		ticket: ticket{
			UserID: user.ID, SessionVersion: user.SessionVersion,
			CredentialID: binding.ID, CredentialVersion: binding.CredentialVersion,
			BrowserContextDigest:    sha256.Sum256([]byte(strings.TrimSpace(browserContext))),
			HasBrowserContextDigest: true, ExpiresAt: now.Add(authorizationTTL),
		},
		Account: user.Account, SecurityVersion: user.SecurityVersion,
	}
	return EmailVerificationResult{
		RequiresRecoveryCode: true, Continuation: id, ExpiresIn: int(authorizationTTL.Seconds()),
	}, nil
}

func (s *Service) ContinueEmailWithContext(continuation, identifier, email, recoveryCode, browserContext string) (string, error) {
	defer clearString(&recoveryCode)
	if s == nil || s.engine == nil || s.credentials == nil || s.external == nil {
		return "", ErrUnavailable
	}
	account, err := auth.NormalizeIdentifier(identifier)
	if err != nil || strings.TrimSpace(continuation) == "" || strings.TrimSpace(browserContext) == "" {
		return "", ErrRecoveryDenied
	}
	key := ticketKey(continuation)
	contextDigest := sha256.Sum256([]byte(strings.TrimSpace(browserContext)))
	s.mu.Lock()
	s.cleanupLocked(s.now())
	pending, ok := s.emailPending[key]
	if !ok {
		s.mu.Unlock()
		return "", ErrRecoveryExpired
	}
	if pending.Busy || !strings.EqualFold(account, pending.Account) || subtle.ConstantTimeCompare(contextDigest[:], pending.BrowserContextDigest[:]) != 1 {
		s.mu.Unlock()
		return "", ErrRecoveryDenied
	}
	pending.Attempts++
	pending.Busy = true
	s.mu.Unlock()

	// 数据库和恢复码哈希验证不占用全局锁；同一凭证同时只能有一个请求验证。
	invalidate := false
	busy := false
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.emailPending[key] == pending {
			if busy {
				pending.Attempts--
			}
			if invalidate || pending.Attempts >= maxEmailContinuationAttempts {
				delete(s.emailPending, key)
			} else {
				pending.Busy = false
			}
		}
	}()
	user, err := s.loadRecoverableUser(account)
	if err != nil || user.ID != pending.UserID || user.EffectiveRole() != access.RoleAdmin || user.SessionVersion != pending.SessionVersion || user.SecurityVersion != pending.SecurityVersion {
		invalidate = true
		return "", ErrRecoveryDenied
	}
	binding, err := s.external.FindVerifiedByEmail(email)
	if err != nil || binding.UserID != pending.UserID || binding.ID != pending.CredentialID || binding.CredentialVersion != pending.CredentialVersion {
		invalidate = true
		return "", ErrRecoveryDenied
	}
	if err := s.credentials.ConsumeRecoveryCode(user.ID, recoveryCode); err != nil {
		if errors.Is(err, password.ErrBusy) {
			busy = true
			return "", err
		}
		return "", ErrRecoveryDenied
	}
	invalidate = true
	ticketID, err := s.issueTicketFor(user.ID, user.SessionVersion, "email_admin", binding.ID, binding.CredentialVersion, browserContext)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	active := s.emailPending[key] == pending && s.now().Before(pending.ExpiresAt)
	delete(s.emailPending, key)
	if !active {
		delete(s.tickets, ticketKey(ticketID))
	}
	s.mu.Unlock()
	if !active {
		return "", ErrRecoveryExpired
	}
	return ticketID, nil
}
