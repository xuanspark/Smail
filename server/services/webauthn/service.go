package webauthnservice

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/credential"
	"github.com/go-webauthn/webauthn/protocol"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"xorm.io/xorm"
)

var (
	ErrUnavailable       = errors.New("webauthn service unavailable")
	ErrInvalidRequest    = errors.New("webauthn request is invalid")
	ErrSessionNotFound   = errors.New("webauthn ceremony session is invalid")
	ErrSessionExpired    = errors.New("webauthn ceremony session expired")
	ErrCredentialMissing = errors.New("webauthn credential is missing")
	ErrCredentialExists  = errors.New("webauthn credential already exists")
)

const (
	ceremonyTTL = 5 * time.Minute
	maxSessions = 1024
)

// Config 保存 WebAuthn 信赖方和凭据加密存储配置。
type Config struct {
	RPID          string
	RPDisplayName string
	RPOrigins     []string
	VaultRoot     string
}

// Service 管理 WebAuthn 注册、登录和凭据生命周期。
type Service struct {
	engine *xorm.Engine
	vault  string
	authn  *webauthn.WebAuthn

	mu           sync.Mutex
	credentialMu sync.Mutex
	sessions     map[string]ceremony
	now          func() time.Time
}

type ceremony struct {
	userID    int
	kind      ceremonyKind
	data      webauthn.SessionData
	expiresAt time.Time
}

type ceremonyKind uint8

const (
	registrationCeremony ceremonyKind = iota + 1
	loginCeremony
	discoverableLoginCeremony
)

// NewService 创建 WebAuthn 服务并校验必要配置。
func NewService(engine *xorm.Engine, cfg Config) (*Service, error) {
	if engine == nil || strings.TrimSpace(cfg.RPID) == "" || strings.TrimSpace(cfg.RPDisplayName) == "" || len(cfg.RPOrigins) == 0 || strings.TrimSpace(cfg.VaultRoot) == "" {
		return nil, ErrUnavailable
	}
	authn, err := webauthn.New(&webauthn.Config{
		RPID:                  strings.TrimSpace(cfg.RPID),
		RPDisplayName:         strings.TrimSpace(cfg.RPDisplayName),
		RPOrigins:             append([]string(nil), cfg.RPOrigins...),
		AttestationPreference: protocol.PreferNoAttestation,
	})
	if err != nil {
		return nil, fmt.Errorf("create webauthn configuration: %w", err)
	}
	return &Service{engine: engine, vault: cfg.VaultRoot, authn: authn, sessions: make(map[string]ceremony), now: time.Now}, nil
}

// BeginRegistration 开始指定用户的 WebAuthn 注册流程。
func (s *Service) BeginRegistration(userID int) (*protocol.CredentialCreation, string, error) {
	user, err := s.loadUser(userID)
	if err != nil {
		return nil, "", err
	}
	adapter, err := s.loadWebAuthnUser(user)
	if err != nil {
		return nil, "", err
	}
	creation, data, err := s.authn.BeginRegistration(adapter, webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired))
	if err != nil {
		return nil, "", fmt.Errorf("begin webauthn registration: %w", err)
	}
	sessionID, err := s.putSession(ceremony{userID: user.ID, kind: registrationCeremony, data: *data, expiresAt: s.now().Add(ceremonyTTL)})
	if err != nil {
		return nil, "", err
	}
	return creation, sessionID, nil
}

// FinishRegistration 完成指定用户的 WebAuthn 注册流程。
func (s *Service) FinishRegistration(userID int, sessionID string, request *http.Request, displayName string) (*models.WebAuthnCredential, error) {
	item, err := s.takeSession(sessionID, registrationCeremony, userID)
	if err != nil {
		return nil, err
	}
	if request == nil || strings.TrimSpace(displayName) == "" || len([]byte(displayName)) > 128 {
		return nil, ErrInvalidRequest
	}
	user, err := s.loadUser(userID)
	if err != nil {
		return nil, err
	}
	adapter, err := s.loadWebAuthnUser(user)
	if err != nil {
		return nil, err
	}
	validated, err := s.authn.FinishRegistration(adapter, item.data, request)
	if err != nil {
		return nil, fmt.Errorf("finish webauthn registration: %w", err)
	}
	credentialID := base64.RawURLEncoding.EncodeToString(validated.ID)
	if credentialID == "" {
		return nil, ErrInvalidRequest
	}
	var existing models.WebAuthnCredential
	has, err := s.engine.Where("credential_id = ?", credentialID).Get(&existing)
	if err != nil {
		return nil, fmt.Errorf("check webauthn credential: %w", err)
	}
	if has {
		return nil, ErrCredentialExists
	}

	encoded, err := json.Marshal(validated)
	if err != nil {
		return nil, fmt.Errorf("encode webauthn credential: %w", err)
	}
	protected, err := credential.Encrypt(s.vault, string(encoded))
	if err != nil {
		return nil, fmt.Errorf("protect webauthn credential: %w", err)
	}
	record := &models.WebAuthnCredential{
		UserID:         userID,
		CredentialID:   credentialID,
		CredentialData: protected,
		DisplayName:    strings.TrimSpace(displayName),
		Discoverable:   true,
		CreatedAt:      s.now(),
	}
	if _, err := s.engine.Insert(record); err != nil {
		return nil, fmt.Errorf("save webauthn credential: %w", err)
	}
	return record, nil
}

// BeginLogin 开始指定用户的 WebAuthn 登录流程。
func (s *Service) BeginLogin(userID int) (*protocol.CredentialAssertion, string, error) {
	user, err := s.loadUser(userID)
	if err != nil {
		return nil, "", err
	}
	adapter, err := s.loadWebAuthnUser(user)
	if err != nil {
		return nil, "", err
	}
	assertion, data, err := s.authn.BeginLogin(adapter)
	if err != nil {
		return nil, "", fmt.Errorf("begin webauthn login: %w", err)
	}
	sessionID, err := s.putSession(ceremony{userID: user.ID, kind: loginCeremony, data: *data, expiresAt: s.now().Add(ceremonyTTL)})
	if err != nil {
		return nil, "", err
	}
	return assertion, sessionID, nil
}

// FinishLogin 完成指定用户的 WebAuthn 登录流程。
func (s *Service) FinishLogin(userID int, sessionID string, request *http.Request) (*models.WebAuthnCredential, error) {
	item, err := s.takeSession(sessionID, loginCeremony, userID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, ErrInvalidRequest
	}
	user, err := s.loadUser(userID)
	if err != nil {
		return nil, err
	}
	adapter, err := s.loadWebAuthnUser(user)
	if err != nil {
		return nil, err
	}
	validated, err := s.authn.FinishLogin(adapter, item.data, request)
	if err != nil {
		return nil, fmt.Errorf("finish webauthn login: %w", err)
	}
	credentialID := base64.RawURLEncoding.EncodeToString(validated.ID)
	s.credentialMu.Lock()
	defer s.credentialMu.Unlock()
	var record models.WebAuthnCredential
	has, err := s.engine.Where("user_id = ? AND credential_id = ? AND revoked_at IS NULL AND discoverable = ?", userID, credentialID, true).Get(&record)
	if err != nil {
		return nil, fmt.Errorf("load webauthn login credential: %w", err)
	}
	if !has {
		return nil, ErrCredentialMissing
	}

	currentPlain, err := credential.Decrypt(s.vault, record.CredentialData)
	if err != nil {
		return nil, fmt.Errorf("decrypt current webauthn credential: %w", err)
	}
	var current webauthn.Credential
	if err := json.Unmarshal([]byte(currentPlain), &current); err != nil {
		return nil, fmt.Errorf("decode current webauthn credential: %w", err)
	}
	if validated.Authenticator.SignCount < current.Authenticator.SignCount {
		return nil, ErrInvalidRequest
	}
	encoded, err := json.Marshal(validated)
	if err != nil {
		return nil, fmt.Errorf("encode webauthn login credential: %w", err)
	}
	protected, err := credential.Encrypt(s.vault, string(encoded))
	if err != nil {
		return nil, fmt.Errorf("protect webauthn login credential: %w", err)
	}
	now := s.now()
	updated := &models.WebAuthnCredential{CredentialData: protected, LastUsedAt: &now}
	if affected, err := s.engine.ID(record.ID).Cols("credential_data", "last_used_at").Update(updated); err != nil {
		return nil, fmt.Errorf("update webauthn login credential: %w", err)
	} else if affected != 1 {
		return nil, ErrCredentialMissing
	}
	record.CredentialData = protected
	record.LastUsedAt = &now
	return &record, nil
}

func (s *Service) BeginDiscoverableLogin() (*protocol.CredentialAssertion, string, error) {
	if s == nil || s.authn == nil {
		return nil, "", ErrUnavailable
	}
	assertion, data, err := s.authn.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", fmt.Errorf("begin discoverable webauthn login: %w", err)
	}
	sessionID, err := s.putSession(ceremony{kind: discoverableLoginCeremony, data: *data, expiresAt: s.now().Add(ceremonyTTL)})
	if err != nil {
		return nil, "", err
	}
	return assertion, sessionID, nil
}

func (s *Service) FinishDiscoverableLogin(sessionID string, request *http.Request) (*models.User, *models.WebAuthnCredential, error) {
	item, err := s.takeDiscoverableSession(sessionID)
	if err != nil {
		return nil, nil, err
	}
	if request == nil {
		return nil, nil, ErrInvalidRequest
	}
	var selected *webAuthnUser
	validated, err := s.authn.FinishDiscoverableLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		adapter, loadErr := s.loadDiscoverableUser(rawID, userHandle)
		if loadErr != nil {
			return nil, loadErr
		}
		selected = adapter
		return adapter, nil
	}, item.data, request)
	if err != nil || selected == nil {
		return nil, nil, ErrCredentialMissing
	}
	record, err := s.persistLoginCredential(selected.user.ID, validated)
	if err != nil {
		return nil, nil, err
	}
	user := selected.user
	return &user, record, nil
}

// ListCredentials 列出指定用户的非敏感 WebAuthn 凭据摘要。
func (s *Service) ListCredentials(userID int) ([]models.WebAuthnCredential, error) {
	if s == nil || s.engine == nil || userID <= 0 {
		return nil, ErrInvalidRequest
	}
	var records []models.WebAuthnCredential
	if err := s.engine.Where("user_id = ?", userID).Cols("id", "user_id", "credential_id", "display_name", "discoverable", "created_at", "last_used_at", "revoked_at").Find(&records); err != nil {
		return nil, fmt.Errorf("list webauthn credentials: %w", err)
	}
	for index := range records {
		records[index].CredentialData = ""
	}
	return records, nil
}

// RenameCredential 修改指定用户凭据的显示名称。
func (s *Service) RenameCredential(userID, credentialID int, displayName string) error {
	if userID <= 0 || credentialID <= 0 || strings.TrimSpace(displayName) == "" || len([]byte(displayName)) > 128 {
		return ErrInvalidRequest
	}
	if affected, err := s.engine.ID(credentialID).Where("user_id = ? AND revoked_at IS NULL", userID).Cols("display_name").Update(&models.WebAuthnCredential{DisplayName: strings.TrimSpace(displayName)}); err != nil {
		return fmt.Errorf("rename webauthn credential: %w", err)
	} else if affected != 1 {
		return ErrCredentialMissing
	}
	return nil
}

// RevokeCredential 撤销指定用户的一项 WebAuthn 凭据。
func (s *Service) RevokeCredential(userID, credentialID int) error {
	if userID <= 0 || credentialID <= 0 {
		return ErrInvalidRequest
	}
	now := s.now()
	if affected, err := s.engine.ID(credentialID).Where("user_id = ? AND revoked_at IS NULL", userID).Cols("revoked_at").Update(&models.WebAuthnCredential{RevokedAt: &now}); err != nil {
		return fmt.Errorf("revoke webauthn credential: %w", err)
	} else if affected != 1 {
		return ErrCredentialMissing
	}
	return nil
}

// RevokeAllCredentials 撤销指定用户的全部 WebAuthn 凭据。
func (s *Service) RevokeAllCredentials(userID int) error {
	if userID <= 0 {
		return ErrInvalidRequest
	}
	now := s.now()
	if _, err := s.engine.Where("user_id = ? AND revoked_at IS NULL", userID).Cols("revoked_at").Update(&models.WebAuthnCredential{RevokedAt: &now}); err != nil {
		return fmt.Errorf("revoke all webauthn credentials: %w", err)
	}
	return nil
}

func (s *Service) loadUser(userID int) (*models.User, error) {
	if s == nil || s.engine == nil || s.authn == nil || userID <= 0 {
		return nil, ErrUnavailable
	}
	var user models.User
	has, err := s.engine.Where("id = ? AND disabled = 0", userID).Get(&user)
	if err != nil {
		return nil, fmt.Errorf("load webauthn user: %w", err)
	}
	if !has {
		return nil, ErrCredentialMissing
	}
	return &user, nil
}

func (s *Service) loadWebAuthnUser(user *models.User) (*webAuthnUser, error) {
	if user == nil {
		return nil, ErrInvalidRequest
	}
	var records []models.WebAuthnCredential
	if err := s.engine.Where("user_id = ? AND revoked_at IS NULL AND discoverable = ?", user.ID, true).Find(&records); err != nil {
		return nil, fmt.Errorf("load webauthn credentials: %w", err)
	}
	result := make([]webauthn.Credential, 0, len(records))
	for _, record := range records {
		plain, err := credential.Decrypt(s.vault, record.CredentialData)
		if err != nil {
			return nil, fmt.Errorf("decrypt webauthn credential: %w", err)
		}
		var item webauthn.Credential
		if err := json.Unmarshal([]byte(plain), &item); err != nil {
			return nil, fmt.Errorf("decode webauthn credential: %w", err)
		}
		if base64.RawURLEncoding.EncodeToString(item.ID) != record.CredentialID {
			return nil, ErrCredentialMissing
		}
		result = append(result, item)
	}
	return &webAuthnUser{user: *user, credentials: result}, nil
}

func (s *Service) loadDiscoverableUser(rawID, userHandle []byte) (*webAuthnUser, error) {
	const prefix = "smail-user-"
	if !bytes.HasPrefix(userHandle, []byte(prefix)) || len(rawID) == 0 {
		return nil, ErrCredentialMissing
	}
	userID, err := strconv.Atoi(string(userHandle[len(prefix):]))
	if err != nil || userID <= 0 {
		return nil, ErrCredentialMissing
	}
	user, err := s.loadUser(userID)
	if err != nil {
		return nil, ErrCredentialMissing
	}
	adapter, err := s.loadWebAuthnUser(user)
	if err != nil {
		return nil, ErrCredentialMissing
	}
	for _, item := range adapter.credentials {
		if bytes.Equal(item.ID, rawID) {
			return adapter, nil
		}
	}
	return nil, ErrCredentialMissing
}

func (s *Service) persistLoginCredential(userID int, validated *webauthn.Credential) (*models.WebAuthnCredential, error) {
	if validated == nil || userID <= 0 {
		return nil, ErrInvalidRequest
	}
	credentialID := base64.RawURLEncoding.EncodeToString(validated.ID)
	s.credentialMu.Lock()
	defer s.credentialMu.Unlock()
	var record models.WebAuthnCredential
	has, err := s.engine.Where("user_id = ? AND credential_id = ? AND revoked_at IS NULL", userID, credentialID).Get(&record)
	if err != nil {
		return nil, fmt.Errorf("load webauthn login credential: %w", err)
	}
	if !has {
		return nil, ErrCredentialMissing
	}
	currentPlain, err := credential.Decrypt(s.vault, record.CredentialData)
	if err != nil {
		return nil, fmt.Errorf("decrypt current webauthn credential: %w", err)
	}
	var current webauthn.Credential
	if err := json.Unmarshal([]byte(currentPlain), &current); err != nil {
		return nil, fmt.Errorf("decode current webauthn credential: %w", err)
	}
	if validated.Authenticator.SignCount < current.Authenticator.SignCount {
		return nil, ErrInvalidRequest
	}
	encoded, err := json.Marshal(validated)
	if err != nil {
		return nil, fmt.Errorf("encode webauthn login credential: %w", err)
	}
	protected, err := credential.Encrypt(s.vault, string(encoded))
	if err != nil {
		return nil, fmt.Errorf("protect webauthn login credential: %w", err)
	}
	now := s.now()
	updated := &models.WebAuthnCredential{CredentialData: protected, LastUsedAt: &now}
	if affected, err := s.engine.ID(record.ID).Cols("credential_data", "last_used_at").Update(updated); err != nil {
		return nil, fmt.Errorf("update webauthn login credential: %w", err)
	} else if affected != 1 {
		return nil, ErrCredentialMissing
	}
	record.CredentialData = protected
	record.LastUsedAt = &now
	return &record, nil
}

func (s *Service) putSession(item ceremony) (string, error) {
	if s == nil {
		return "", ErrUnavailable
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("create webauthn session: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneSessionsLocked(s.now())
	if len(s.sessions) >= maxSessions {
		return "", ErrUnavailable
	}
	s.sessions[token] = item
	return token, nil
}

func (s *Service) takeDiscoverableSession(token string) (ceremony, error) {
	if s == nil || strings.TrimSpace(token) == "" {
		return ceremony{}, ErrSessionNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[token]
	if !ok {
		return ceremony{}, ErrSessionNotFound
	}
	delete(s.sessions, token)
	if item.kind != discoverableLoginCeremony {
		return ceremony{}, ErrSessionNotFound
	}
	now := s.now()
	if !item.expiresAt.After(now) || (!item.data.Expires.IsZero() && !item.data.Expires.After(now)) {
		return ceremony{}, ErrSessionExpired
	}
	return item, nil
}

func (s *Service) takeSession(token string, kind ceremonyKind, userID int) (ceremony, error) {
	if s == nil || strings.TrimSpace(token) == "" || userID <= 0 {
		return ceremony{}, ErrSessionNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[token]
	if !ok {
		return ceremony{}, ErrSessionNotFound
	}
	delete(s.sessions, token)
	if item.kind != kind || item.userID != userID {
		return ceremony{}, ErrSessionNotFound
	}
	now := s.now()
	if !item.expiresAt.After(now) || (!item.data.Expires.IsZero() && !item.data.Expires.After(now)) {
		return ceremony{}, ErrSessionExpired
	}
	return item, nil
}

// InvalidateUser 删除指定用户尚未完成的全部 WebAuthn 仪式会话。
func (s *Service) InvalidateUser(userID int) {
	if s == nil || userID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, item := range s.sessions {
		if item.userID == userID {
			delete(s.sessions, token)
		}
	}
}

func (s *Service) pruneSessionsLocked(now time.Time) {
	for token, item := range s.sessions {
		if !item.expiresAt.After(now) || (!item.data.Expires.IsZero() && !item.data.Expires.After(now)) {
			delete(s.sessions, token)
		}
	}
}

type webAuthnUser struct {
	user        models.User
	credentials []webauthn.Credential
}

func (u *webAuthnUser) WebAuthnID() []byte { return []byte(fmt.Sprintf("smail-user-%d", u.user.ID)) }

func (u *webAuthnUser) WebAuthnName() string { return u.user.Account }

func (u *webAuthnUser) WebAuthnDisplayName() string { return u.user.Name }

func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }
