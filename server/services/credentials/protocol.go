package credentials

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/password"
)

const (
	ProtocolIMAP = "imap"
	ProtocolPOP3 = "pop3"
	ProtocolSMTP = "smtp"
)

var (
	ErrInvalidProtocol     = errors.New("invalid mail protocol")
	ErrProtocolDisabled    = errors.New("mail protocol is disabled")
	ErrProtocolCodeMissing = errors.New("protocol authorization code is missing")
)

var supportedProtocols = []string{ProtocolIMAP, ProtocolPOP3, ProtocolSMTP}

// ProtocolCredentialStatus 返回协议开关和授权码状态，不包含授权码哈希。
type ProtocolCredentialStatus struct {
	Protocol   string     `json:"protocol"`
	Enabled    bool       `json:"enabled"`
	Configured bool       `json:"configured"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func NormalizeProtocol(value string) (string, error) {
	protocol := strings.ToLower(strings.TrimSpace(value))
	switch protocol {
	case ProtocolIMAP, ProtocolPOP3, ProtocolSMTP:
		return protocol, nil
	default:
		return "", ErrInvalidProtocol
	}
}

func SupportedProtocols() []string {
	return append([]string(nil), supportedProtocols...)
}

// ListProtocolCredentials 返回当前用户三个邮件协议的完整状态。
func (s *Service) ListProtocolCredentials(userID int) ([]ProtocolCredentialStatus, error) {
	if s == nil || s.engine == nil {
		return nil, ErrUnavailable
	}
	if userID <= 0 {
		return nil, ErrInvalidUser
	}
	var items []models.ProtocolCredential
	if err := s.engine.Where("user_id = ?", userID).Find(&items); err != nil {
		return nil, fmt.Errorf("load protocol credentials: %w", err)
	}
	byProtocol := make(map[string]models.ProtocolCredential, len(items))
	for _, item := range items {
		byProtocol[item.Protocol] = item
	}
	result := make([]ProtocolCredentialStatus, 0, len(supportedProtocols))
	for _, protocol := range supportedProtocols {
		status := ProtocolCredentialStatus{Protocol: protocol}
		if item, ok := byProtocol[protocol]; ok {
			createdAt := item.CreatedAt
			updatedAt := item.UpdatedAt
			status.Enabled = item.Enabled
			status.Configured = item.CodeHash != "" && item.RevokedAt == nil
			status.CreatedAt = &createdAt
			status.UpdatedAt = &updatedAt
			status.LastUsedAt = item.LastUsedAt
		}
		result = append(result, status)
	}
	return result, nil
}

// SetProtocolEnabled 设置用户级协议开关。关闭后授权码会保留，但认证立即失效。
func (s *Service) SetProtocolEnabled(userID int, protocol string, enabled bool) error {
	if s == nil || s.engine == nil {
		return ErrUnavailable
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	protocol, err := NormalizeProtocol(protocol)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var item models.ProtocolCredential
	has, err := s.engine.Where("user_id = ? AND protocol = ?", userID, protocol).Get(&item)
	if err != nil {
		return fmt.Errorf("load protocol credential: %w", err)
	}
	if !has {
		item = models.ProtocolCredential{UserID: userID, Protocol: protocol, Enabled: enabled, CodeVersion: 1}
		if _, err := s.engine.Insert(&item); err != nil {
			return fmt.Errorf("create protocol credential: %w", err)
		}
		return nil
	}
	item.Enabled = enabled
	if _, err := s.engine.ID(item.ID).Cols("enabled", "updated_at").Update(&item); err != nil {
		return fmt.Errorf("update protocol credential: %w", err)
	}
	return nil
}

// GenerateProtocolCode 生成只展示一次的协议授权码，并撤销同协议旧授权码。
func (s *Service) GenerateProtocolCode(userID int, protocol string) (string, error) {
	if s == nil || s.engine == nil {
		return "", ErrUnavailable
	}
	if userID <= 0 {
		return "", ErrInvalidUser
	}
	protocol, err := NormalizeProtocol(protocol)
	if err != nil {
		return "", err
	}
	plain, err := newProtocolCode()
	if err != nil {
		return "", err
	}
	hash, err := password.Hash(plain)
	if err != nil {
		return "", fmt.Errorf("hash protocol authorization code: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var item models.ProtocolCredential
	has, err := s.engine.Where("user_id = ? AND protocol = ?", userID, protocol).Get(&item)
	if err != nil {
		return "", fmt.Errorf("load protocol credential: %w", err)
	}
	if !has || !item.Enabled {
		return "", ErrProtocolDisabled
	}
	version := item.CodeVersion + 1
	if version <= 1 {
		version = 1
	}
	item.CodeHash = hash
	item.CodeVersion = version
	item.RevokedAt = nil
	item.LastUsedAt = nil
	if _, err := s.engine.ID(item.ID).Cols("code_hash", "code_version", "revoked_at", "last_used_at", "updated_at").Update(&item); err != nil {
		return "", fmt.Errorf("update protocol authorization code: %w", err)
	}
	return plain, nil
}

// RevokeProtocolCode 撤销指定协议的授权码，协议开关状态保持不变。
func (s *Service) RevokeProtocolCode(userID int, protocol string) error {
	if s == nil || s.engine == nil {
		return ErrUnavailable
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	protocol, err := NormalizeProtocol(protocol)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if _, err := s.engine.Where("user_id = ? AND protocol = ? AND revoked_at IS NULL", userID, protocol).
		Cols("revoked_at", "updated_at").
		Update(&models.ProtocolCredential{RevokedAt: &now}); err != nil {
		return fmt.Errorf("revoke protocol authorization code: %w", err)
	}
	return nil
}

// VerifyProtocolCode 校验协议开关和授权码，不接受网页登录密码。
func (s *Service) VerifyProtocolCode(userID int, protocol string, plain string) bool {
	if s == nil || s.engine == nil || userID <= 0 || plain == "" || len([]byte(plain)) > 256 {
		password.VerifyDummy(plain)
		return false
	}
	protocol, err := NormalizeProtocol(protocol)
	if err != nil {
		password.VerifyDummy(plain)
		return false
	}
	var item models.ProtocolCredential
	has, err := s.engine.Where("user_id = ? AND protocol = ? AND enabled = ? AND revoked_at IS NULL", userID, protocol, true).Get(&item)
	if err != nil || !has || item.CodeHash == "" {
		password.VerifyDummy(plain)
		return false
	}
	if !password.Verify(item.CodeHash, strings.TrimSpace(plain)) {
		return false
	}
	now := s.now()
	_, _ = s.engine.ID(item.ID).Cols("last_used_at", "updated_at").Update(&models.ProtocolCredential{LastUsedAt: &now})
	return true
}

func newProtocolCode() (string, error) {
	buf := make([]byte, 15)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate protocol authorization code: %w", err)
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	groups := make([]string, 0, 6)
	for len(encoded) > 4 {
		groups = append(groups, encoded[:4])
		encoded = encoded[4:]
	}
	if encoded != "" {
		groups = append(groups, encoded)
	}
	return strings.Join(groups, "-"), nil
}
