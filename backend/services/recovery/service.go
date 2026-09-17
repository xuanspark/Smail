package recovery

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/verification"
	"git.pcux.cn/xuans/Smail/utils/password"
	"github.com/go-webauthn/webauthn/protocol"
	"xorm.io/xorm"
)

var (
	ErrUnavailable        = errors.New("recovery service unavailable")
	ErrInvalidRequest     = errors.New("recovery request is invalid")
	ErrRecoveryDenied     = errors.New("recovery is denied")
	ErrRecoveryExpired    = errors.New("recovery authorization expired")
	ErrRecoveryConsumed   = errors.New("recovery authorization already consumed")
	ErrInvalidNewPassword = errors.New("new password is invalid")
)

const (
	authorizationTTL   = 5 * time.Minute
	decoyChallengeTTL  = 10 * time.Minute
	maxDecoyChallenges = 1024
	maxDecoyAttempts   = 5
	maxRecoveryTickets = 10000
)

type WebAuthnVerifier interface {
	BeginDiscoverableLogin() (*protocol.CredentialAssertion, string, error)
	FinishDiscoverableLogin(sessionID string, request *http.Request) (*models.User, *models.WebAuthnCredential, error)
}

type Service struct {
	engine         *xorm.Engine
	verification   *verification.Service
	external       *externalemail.Repository
	credentials    *credentials.Service
	webauthn       WebAuthnVerifier
	invalidate     func(int, string)
	mu             sync.Mutex
	tickets        map[string]ticket
	emailPending   map[string]*emailContinuation
	publicTickets  map[string]*publicTicket
	publicDelivery PublicLinkDelivery
	decoys         map[string]decoyChallenge
	now            func() time.Time
}

type ticket struct {
	Busy                    bool
	UserID                  int
	SessionVersion          int
	Method                  string
	CredentialID            int
	CredentialVersion       int
	BrowserContextDigest    [32]byte
	HasBrowserContextDigest bool
	ExpiresAt               time.Time
}

type decoyChallenge struct {
	AccountDigest           [32]byte
	EmailDigest             [32]byte
	BrowserContextDigest    [32]byte
	HasBrowserContextDigest bool
	Verifier                [32]byte
	ExpiresAt               time.Time
	Attempts                int
}

type BeginRequest struct {
	Identifier     string
	Email          string
	BrowserContext string
}

func NewService(engine *xorm.Engine, verify *verification.Service, external *externalemail.Repository, credential *credentials.Service) (*Service, error) {
	if engine == nil || credential == nil {
		return nil, ErrUnavailable
	}
	return &Service{
		engine: engine, verification: verify, external: external, credentials: credential,
		tickets: make(map[string]ticket), emailPending: make(map[string]*emailContinuation), publicTickets: make(map[string]*publicTicket),
		decoys: make(map[string]decoyChallenge), now: time.Now,
	}, nil
}

func (s *Service) SetWebAuthnVerifier(verifier WebAuthnVerifier) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.webauthn = verifier
}

func (s *Service) SetSecurityStateInvalidator(invalidator func(int, string)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invalidate = invalidator
}

// SetPublicLinkDelivery 设置独立公网恢复链接的系统邮件投递器。
// 投递器只接收目的邮箱和一次性令牌，不得记录令牌或完整链接。
func (s *Service) SetPublicLinkDelivery(delivery PublicLinkDelivery) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publicDelivery = delivery
}

func (s *Service) currentWebAuthnVerifier() WebAuthnVerifier {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.webauthn
}

func (s *Service) invalidateSecurityState(userID int, account string) {
	s.mu.Lock()
	invalidator := s.invalidate
	s.mu.Unlock()
	// 回调可能重新进入本服务，必须在释放内部锁后调用。
	if invalidator != nil {
		invalidator(userID, account)
	}
}

func (s *Service) BeginWebAuthn(identifier string) (*protocol.CredentialAssertion, string, error) {
	verifier := s.currentWebAuthnVerifier()
	if s == nil || s.engine == nil || verifier == nil {
		return nil, "", ErrUnavailable
	}
	options, sessionID, err := verifier.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", ErrRecoveryDenied
	}
	return options, sessionID, nil
}

func (s *Service) VerifyWebAuthn(identifier, sessionID string, request *http.Request) (string, error) {
	verifier := s.currentWebAuthnVerifier()
	if s == nil || s.engine == nil || verifier == nil || request == nil || strings.TrimSpace(sessionID) == "" {
		return "", ErrInvalidRequest
	}
	user, _, err := verifier.FinishDiscoverableLogin(sessionID, request)
	if err != nil || user == nil {
		return "", ErrRecoveryDenied
	}
	account, err := auth.NormalizeIdentifier(identifier)
	if err != nil || !strings.EqualFold(account, user.Account) || user.Disabled != 0 || user.EffectiveRole() == access.RoleSuperAdmin {
		return "", ErrRecoveryDenied
	}
	return s.issueTicketFor(user.ID, user.SessionVersion, "webauthn", 0, 0, "")
}

func (s *Service) VerifyWebAuthnWithContext(identifier, sessionID string, request *http.Request, browserContext string) (string, error) {
	verifier := s.currentWebAuthnVerifier()
	if s == nil || s.engine == nil || verifier == nil || request == nil || strings.TrimSpace(sessionID) == "" {
		return "", ErrInvalidRequest
	}
	user, credential, err := verifier.FinishDiscoverableLogin(sessionID, request)
	if err != nil || user == nil || credential == nil {
		return "", ErrRecoveryDenied
	}
	account, err := auth.NormalizeIdentifier(identifier)
	if err != nil || !strings.EqualFold(account, user.Account) || user.Disabled != 0 || user.EffectiveRole() == access.RoleSuperAdmin {
		return "", ErrRecoveryDenied
	}
	return s.issueTicketFor(user.ID, user.SessionVersion, "webauthn", credential.ID, 0, browserContext)
}

func (s *Service) Begin(request BeginRequest, remoteIP string) (string, error) {
	if s == nil || s.engine == nil || s.verification == nil || s.external == nil {
		return "", ErrUnavailable
	}
	account, err := auth.NormalizeIdentifier(request.Identifier)
	if err != nil || account == "" || strings.TrimSpace(request.Email) == "" {
		return s.issueDecoyChallengeWithContext(request.Identifier, request.Email, request.BrowserContext)
	}
	var user models.User
	has, err := s.engine.Where("account = ? AND disabled = 0", account).Get(&user)
	if err != nil {
		id, decoyErr := s.issueDecoyChallengeWithContext(account, request.Email, request.BrowserContext)
		if decoyErr != nil {
			return "", fmt.Errorf("load recovery user: %w", err)
		}
		return id, fmt.Errorf("load recovery user: %w", err)
	}
	if !has || user.EffectiveRole() == access.RoleSuperAdmin {
		return s.issueDecoyChallengeWithContext(account, request.Email, request.BrowserContext)
	}
	item, err := s.external.FindVerifiedByEmail(request.Email)
	if err != nil || item.UserID != user.ID {
		return s.issueDecoyChallengeWithContext(account, request.Email, request.BrowserContext)
	}
	challenge, err := s.verification.Issue(verification.IssueRequest{
		Purpose:            verification.PurposePasswordRecover,
		Account:            user.Account,
		Email:              request.Email,
		BrowserTransaction: request.BrowserContext,
		CredentialVersion:  item.CredentialVersion,
		ResourceID:         item.ID,
	}, remoteIP)
	if err != nil {
		id, decoyErr := s.issueDecoyChallengeWithContext(account, request.Email, request.BrowserContext)
		if decoyErr != nil {
			return "", err
		}
		return id, err
	}
	return challenge.ID, nil
}

func (s *Service) Verify(challengeID, account, email, code, recoveryCode, remoteIP string) (string, error) {
	return s.VerifyWithContext(challengeID, account, email, code, recoveryCode, remoteIP, "")
}

func (s *Service) VerifyWithContext(challengeID, account, email, code, recoveryCode, remoteIP, browserContext string) (string, error) {
	user, item, err := s.verifyEmailIdentity(challengeID, account, email, code, remoteIP, browserContext)
	if err != nil {
		return "", err
	}
	method := "email"
	if user.EffectiveRole() == access.RoleAdmin {
		if strings.TrimSpace(recoveryCode) == "" {
			return "", ErrRecoveryDenied
		}
		if err := s.credentials.ConsumeRecoveryCode(user.ID, recoveryCode); err != nil {
			if errors.Is(err, password.ErrBusy) {
				return "", err
			}
			return "", ErrRecoveryDenied
		}
		method = "email_admin"
	}
	return s.issueTicketFor(user.ID, user.SessionVersion, method, item.ID, item.CredentialVersion, browserContext)
}

func (s *Service) verifyEmailIdentity(challengeID, account, email, code, remoteIP, browserContext string) (*models.User, *models.ExternalEmail, error) {
	if s == nil || s.verification == nil {
		return nil, nil, ErrUnavailable
	}
	if s.consumeDecoyChallengeWithContext(challengeID, account, email, code, browserContext) {
		return nil, nil, ErrRecoveryDenied
	}
	normalizedAccount, normalizeErr := auth.NormalizeIdentifier(account)
	if normalizeErr != nil {
		return nil, nil, ErrRecoveryDenied
	}
	var user models.User
	has, err := s.engine.Where("account = ? AND disabled = 0", normalizedAccount).Get(&user)
	if err != nil || !has || user.EffectiveRole() == access.RoleSuperAdmin || s.external == nil {
		return nil, nil, ErrRecoveryDenied
	}
	item, err := s.external.FindVerifiedByEmail(email)
	if err != nil || item.UserID != user.ID {
		return nil, nil, ErrRecoveryDenied
	}
	if err := s.verification.VerifyBoundWithContext(challengeID, normalizedAccount, email, code, verification.PurposePasswordRecover, item.ID, remoteIP, browserContext); err != nil {
		return nil, nil, err
	}
	return &user, item, nil
}

func (s *Service) VerifyOffline(identifier, question, answer, recoveryCode, remoteIP string) (string, error) {
	return s.VerifyOfflineWithContext(identifier, question, answer, recoveryCode, remoteIP, "")
}

func (s *Service) VerifyOfflineWithContext(identifier, question, answer, recoveryCode, remoteIP, browserContext string) (string, error) {
	if s == nil || s.engine == nil || s.credentials == nil {
		return "", ErrUnavailable
	}
	defer clearString(&answer)
	defer clearString(&recoveryCode)
	if strings.TrimSpace(question) == "" || strings.TrimSpace(answer) == "" || strings.TrimSpace(recoveryCode) == "" {
		return "", ErrInvalidRequest
	}
	user, err := s.loadRecoverableUser(identifier)
	if err != nil {
		if err := password.CheckDummy(answer); err != nil {
			return "", err
		}
		if err := password.CheckDummy(recoveryCode); err != nil {
			return "", err
		}
		return "", ErrRecoveryDenied
	}
	storedQuestion, configured, questionErr := s.credentials.GetSecurityQuestion(user.ID)
	answerVerified, answerErr := s.credentials.VerifySecurityQuestion(user.ID, answer)
	if errors.Is(answerErr, password.ErrBusy) {
		return "", answerErr
	}
	providedQuestionDigest := sha256.Sum256([]byte(strings.TrimSpace(question)))
	storedQuestionDigest := sha256.Sum256([]byte(strings.TrimSpace(storedQuestion)))
	questionVerified := configured && subtle.ConstantTimeCompare(providedQuestionDigest[:], storedQuestionDigest[:]) == 1
	if questionErr != nil || answerErr != nil || !questionVerified || !answerVerified {
		if err := password.CheckDummy(recoveryCode); err != nil {
			return "", err
		}
		return "", ErrRecoveryDenied
	}
	ticketID, err := s.issueTicketFor(user.ID, user.SessionVersion, "offline", 0, 0, browserContext)
	if err != nil {
		return "", err
	}
	if err := s.credentials.ConsumeRecoveryCode(user.ID, recoveryCode); err != nil {
		s.invalidateTicket(ticketID)
		if errors.Is(err, password.ErrBusy) {
			return "", err
		}
		return "", ErrRecoveryDenied
	}
	return ticketID, nil
}
func (s *Service) Complete(ticketID, newPassword string) error {
	return s.CompleteWithContext(ticketID, newPassword, "")
}

func (s *Service) CompleteWithContext(ticketID, newPassword, browserContext string) error {
	defer clearString(&newPassword)
	if s == nil || s.engine == nil || s.credentials == nil || strings.TrimSpace(ticketID) == "" {
		return ErrInvalidRequest
	}
	if password.ValidateUserPassword(newPassword) != nil {
		return ErrInvalidNewPassword
	}
	s.mu.Lock()
	ticketDigest := ticketKey(ticketID)
	item, ok := s.tickets[ticketDigest]
	if !ok || !s.now().Before(item.ExpiresAt) {
		if ok {
			delete(s.tickets, ticketDigest)
		}
		s.mu.Unlock()
		return ErrRecoveryExpired
	}
	if item.Busy {
		s.mu.Unlock()
		return ErrRecoveryExpired
	}
	if item.HasBrowserContextDigest {
		providedDigest := sha256.Sum256([]byte(strings.TrimSpace(browserContext)))
		if strings.TrimSpace(browserContext) == "" || subtle.ConstantTimeCompare(providedDigest[:], item.BrowserContextDigest[:]) != 1 {
			s.mu.Unlock()
			return ErrRecoveryDenied
		}
	}
	item.Busy = true
	s.tickets[ticketDigest] = item
	s.mu.Unlock()

	// 过载时保留同一张有效票据，已失效或已被撤销的票据不得恢复。
	retry := false
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if current, exists := s.tickets[ticketDigest]; exists && current == item {
			if retry && s.now().Before(item.ExpiresAt) {
				item.Busy = false
				s.tickets[ticketDigest] = item
			} else {
				delete(s.tickets, ticketDigest)
			}
		}
	}()

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		retry = errors.Is(err, password.ErrBusy)
		return err
	}
	defer clearString(&hash)
	var user models.User
	has, err := s.engine.ID(item.UserID).Get(&user)
	if err != nil || !has || user.Disabled != 0 || user.EffectiveRole() == access.RoleSuperAdmin || user.SessionVersion != item.SessionVersion {
		return ErrRecoveryDenied
	}
	if item.Method == "email" || item.Method == "email_admin" {
		if user.EffectiveRole() == access.RoleAdmin && item.Method != "email_admin" {
			return ErrRecoveryDenied
		}
		if s.external == nil || item.CredentialID <= 0 {
			return ErrRecoveryDenied
		}
		externalItem, lookupErr := s.external.FindVerifiedByID(item.UserID, item.CredentialID)
		if lookupErr != nil || externalItem.CredentialVersion != item.CredentialVersion {
			return ErrRecoveryDenied
		}
	}
	if item.Method == "webauthn" && item.CredentialID > 0 {
		var credential models.WebAuthnCredential
		found, lookupErr := s.engine.Where("id = ? AND user_id = ? AND revoked_at IS NULL", item.CredentialID, item.UserID).Get(&credential)
		if lookupErr != nil || !found {
			return ErrRecoveryDenied
		}
	}
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
	if affected, err := session.ID(item.UserID).Where("session_version = ? AND security_version = ? AND disabled = 0", item.SessionVersion, user.SecurityVersion).Cols("password", "session_version", "security_version", "must_change_password", "password_changed_at").Update(&models.User{
		Password:           hash,
		SessionVersion:     version,
		SecurityVersion:    securityVersion,
		MustChangePassword: 0,
		PasswordChangedAt:  &now,
	}); err != nil {
		_ = session.Rollback()
		return err
	} else if affected != 1 {
		_ = session.Rollback()
		return ErrRecoveryDenied
	}
	if err := s.credentials.RevokeAllWithSession(session, item.UserID); err != nil {
		_ = session.Rollback()
		return err
	}
	if err := session.Commit(); err != nil {
		return err
	}
	s.InvalidateUser(item.UserID)
	if s.verification != nil && s.verification.Store != nil {
		_ = s.verification.Store.InvalidateAccount(user.Account)
	}
	s.invalidateSecurityState(item.UserID, user.Account)
	return nil
}

func (s *Service) loadRecoverableUser(identifier string) (*models.User, error) {
	account, err := auth.NormalizeIdentifier(identifier)
	if err != nil || account == "" {
		return nil, ErrRecoveryDenied
	}
	var user models.User
	has, err := s.engine.Where("account = ? AND disabled = 0", account).Get(&user)
	if err != nil || !has || user.EffectiveRole() == access.RoleSuperAdmin {
		return nil, ErrRecoveryDenied
	}
	return &user, nil
}

func (s *Service) issueTicket(userID, sessionVersion int) (string, error) {
	return s.issueTicketFor(userID, sessionVersion, "", 0, 0, "")
}

func (s *Service) issueTicketFor(userID, sessionVersion int, method string, credentialID, credentialVersion int, browserContext string) (string, error) {
	if userID <= 0 || sessionVersion <= 0 {
		return "", ErrRecoveryDenied
	}
	ticketID, err := newTicketID()
	if err != nil {
		return "", err
	}
	now := s.now()
	s.mu.Lock()
	s.cleanupLocked(now)
	if len(s.tickets) >= maxRecoveryTickets {
		s.mu.Unlock()
		return "", ErrUnavailable
	}
	contextValue := strings.TrimSpace(browserContext)
	var contextDigest [32]byte
	if contextValue != "" {
		contextDigest = sha256.Sum256([]byte(contextValue))
	}
	s.tickets[ticketKey(ticketID)] = ticket{
		UserID: userID, SessionVersion: sessionVersion, Method: method,
		CredentialID: credentialID, CredentialVersion: credentialVersion,
		BrowserContextDigest: contextDigest, HasBrowserContextDigest: contextValue != "",
		ExpiresAt: now.Add(authorizationTTL),
	}
	s.mu.Unlock()
	return ticketID, nil
}

func (s *Service) cleanupLocked(now time.Time) {
	for id, item := range s.tickets {
		if !now.Before(item.ExpiresAt) {
			delete(s.tickets, id)
		}
	}
	for id, item := range s.emailPending {
		if !now.Before(item.ExpiresAt) {
			delete(s.emailPending, id)
		}
	}
	for id, item := range s.publicTickets {
		if !now.Before(item.ExpiresAt) {
			delete(s.publicTickets, id)
		}
	}
}

func (s *Service) issueDecoyChallenge(account, email string) (string, error) {
	return s.issueDecoyChallengeWithContext(account, email, "")
}

func (s *Service) issueDecoyChallengeWithContext(account, email, browserContext string) (string, error) {
	if s == nil {
		return "", ErrUnavailable
	}
	id, err := newChallengeID()
	if err != nil {
		return "", err
	}
	var verifier [32]byte
	if _, err := rand.Read(verifier[:]); err != nil {
		return "", err
	}
	now := s.now()
	item := decoyChallenge{
		AccountDigest:           sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(account)))),
		EmailDigest:             sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email)))),
		BrowserContextDigest:    sha256.Sum256([]byte(strings.TrimSpace(browserContext))),
		HasBrowserContextDigest: strings.TrimSpace(browserContext) != "",
		Verifier:                verifier,
		ExpiresAt:               now.Add(decoyChallengeTTL),
	}
	s.mu.Lock()
	s.cleanupDecoysLocked(now)
	if len(s.decoys) < maxDecoyChallenges {
		s.decoys[id] = item
	}
	s.mu.Unlock()
	return id, nil
}

func (s *Service) consumeDecoyChallenge(id, account, email, code string) bool {
	return s.consumeDecoyChallengeWithContext(id, account, email, code, "")
}

func (s *Service) consumeDecoyChallengeWithContext(id, account, email, code, browserContext string) bool {
	if s == nil || strings.TrimSpace(id) == "" {
		return false
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupDecoysLocked(now)
	item, ok := s.decoys[id]
	if !ok {
		return false
	}
	if item.HasBrowserContextDigest {
		providedContext := sha256.Sum256([]byte(strings.TrimSpace(browserContext)))
		if strings.TrimSpace(browserContext) == "" || subtle.ConstantTimeCompare(providedContext[:], item.BrowserContextDigest[:]) != 1 {
			return true
		}
	}
	accountDigest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(account))))
	emailDigest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	provided := sha256.Sum256([]byte(id + "\x00" + account + "\x00" + email + "\x00" + code))
	_ = subtle.ConstantTimeCompare(accountDigest[:], item.AccountDigest[:])
	_ = subtle.ConstantTimeCompare(emailDigest[:], item.EmailDigest[:])
	_ = subtle.ConstantTimeCompare(provided[:], item.Verifier[:])
	item.Attempts++
	if item.Attempts >= maxDecoyAttempts {
		delete(s.decoys, id)
	} else {
		s.decoys[id] = item
	}
	return true
}

func (s *Service) cleanupDecoysLocked(now time.Time) {
	for id, item := range s.decoys {
		if !now.Before(item.ExpiresAt) {
			delete(s.decoys, id)
		}
	}
}

// InvalidateUser 撤销指定用户尚未消费的全部密码恢复票据。
func (s *Service) InvalidateUser(userID int) {
	if s == nil || userID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, item := range s.tickets {
		if item.UserID == userID {
			delete(s.tickets, id)
		}
	}
	for id, item := range s.emailPending {
		if item.UserID == userID {
			delete(s.emailPending, id)
		}
	}
	for id, item := range s.publicTickets {
		if item.UserID == userID {
			delete(s.publicTickets, id)
		}
	}
}

func (s *Service) invalidateTicket(ticketID string) {
	if s == nil || ticketID == "" {
		return
	}
	s.mu.Lock()
	delete(s.tickets, ticketKey(ticketID))
	s.mu.Unlock()
}

func ticketKey(ticketID string) string {
	digest := sha256.Sum256([]byte(ticketID))
	return hex.EncodeToString(digest[:])
}

func newTicketID() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func newChallengeID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func clearString(value *string) {
	if value == nil {
		return
	}
	buffer := []byte(*value)
	for index := range buffer {
		buffer[index] = 0
	}
	*value = ""
}
