package credentials

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/credential"
	"git.pcux.cn/xuans/Smail/utils/password"
)

var (
	ErrClientNameRequired = errors.New("client name is required")
	ErrClientNotFound     = errors.New("client credential not found")
)

// ClientCredentialItem 表示返回给前端的客户端授权码视图
type ClientCredentialItem struct {
	ID          int        `json:"id"`
	ClientName  string     `json:"client_name"`
	Configured  bool       `json:"configured"`
	CanReveal   bool       `json:"can_reveal"`
	CodeVersion int        `json:"code_version"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
}

// CleanAuthCode 统一授权码格式：去除短横线、空格并转大写，提升各类客户端容错率
func CleanAuthCode(raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, "-", "")
	raw = strings.ReplaceAll(raw, " ", "")
	return raw
}

// FormatAuthCode 将清洗后的授权码格式化为 4 字符一组带横线的易读格式
func FormatAuthCode(raw string) string {
	cleaned := CleanAuthCode(raw)
	if len(cleaned) == 0 {
		return ""
	}
	var groups []string
	for len(cleaned) > 4 {
		groups = append(groups, cleaned[:4])
		cleaned = cleaned[4:]
	}
	if len(cleaned) > 0 {
		groups = append(groups, cleaned)
	}
	return strings.Join(groups, "-")
}

// ListClientCredentials 返回指定用户的所有有效（未撤销）客户端授权凭证
func (s *Service) ListClientCredentials(userID int) ([]ClientCredentialItem, error) {
	if s == nil || s.engine == nil {
		return nil, ErrUnavailable
	}
	if userID <= 0 {
		return nil, ErrInvalidUser
	}
	var items []models.ClientCredential
	if err := s.engine.Where("user_id = ? AND revoked_at IS NULL", userID).OrderBy("id ASC").Find(&items); err != nil {
		return nil, fmt.Errorf("load client credentials: %w", err)
	}
	result := make([]ClientCredentialItem, 0, len(items))
	for _, item := range items {
		result = append(result, ClientCredentialItem{
			ID:          item.ID,
			ClientName:  item.ClientName,
			Configured:  item.CodeHash != "",
			CanReveal:   item.CodeCiphertext != "",
			CodeVersion: item.CodeVersion,
			CreatedAt:   item.CreatedAt,
			UpdatedAt:   item.UpdatedAt,
			LastUsedAt:  item.LastUsedAt,
		})
	}
	return result, nil
}

// CreateClientCredential 为指定客户端名称创建专属授权码
func (s *Service) CreateClientCredential(userID int, clientName string) (string, *ClientCredentialItem, error) {
	if s == nil || s.engine == nil {
		return "", nil, ErrUnavailable
	}
	if userID <= 0 {
		return "", nil, ErrInvalidUser
	}
	clientName = strings.TrimSpace(clientName)
	if clientName == "" {
		return "", nil, ErrClientNameRequired
	}
	if len([]rune(clientName)) > 32 {
		return "", nil, errors.New("client name is too long (max 32 chars)")
	}

	plain, err := newProtocolCode()
	if err != nil {
		return "", nil, err
	}
	normalizedCode := CleanAuthCode(plain)

	ciphertext := ""
	if s.vaultRoot != "" {
		ciphertext, err = credential.Encrypt(s.vaultRoot, plain)
		if err != nil {
			return "", nil, fmt.Errorf("protect client authorization code: %w", err)
		}
	}
	hash, err := password.Hash(normalizedCode)
	if err != nil {
		return "", nil, fmt.Errorf("hash client authorization code: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var existing models.ClientCredential
	has, err := s.engine.Where("user_id = ? AND client_name = ?", userID, clientName).Get(&existing)
	if err != nil {
		return "", nil, fmt.Errorf("check existing client credential: %w", err)
	}
	if has {
		// 如果此前被撤销，重新启用并更新密钥；如果未被撤销则不允许重名创建
		if existing.RevokedAt == nil {
			return "", nil, errors.New("client name already exists")
		}
		version := existing.CodeVersion + 1
		existing.CodeHash = hash
		existing.CodeCiphertext = ciphertext
		existing.CodeVersion = version
		existing.RevokedAt = nil
		existing.LastUsedAt = nil
		existing.UpdatedAt = s.now()
		if _, err := s.engine.ID(existing.ID).Cols("code_hash", "code_ciphertext", "code_version", "revoked_at", "last_used_at", "updated_at").Update(&existing); err != nil {
			return "", nil, fmt.Errorf("re-activate client credential: %w", err)
		}
		item := &ClientCredentialItem{
			ID:          existing.ID,
			ClientName:  existing.ClientName,
			Configured:  true,
			CanReveal:   existing.CodeCiphertext != "",
			CodeVersion: existing.CodeVersion,
			CreatedAt:   existing.CreatedAt,
			UpdatedAt:   existing.UpdatedAt,
		}
		return plain, item, nil
	}

	record := models.ClientCredential{
		UserID:         userID,
		ClientName:     clientName,
		CodeHash:       hash,
		CodeCiphertext: ciphertext,
		CodeVersion:    1,
		CreatedAt:      s.now(),
		UpdatedAt:      s.now(),
	}
	if _, err := s.engine.Insert(&record); err != nil {
		return "", nil, fmt.Errorf("insert client credential: %w", err)
	}

	item := &ClientCredentialItem{
		ID:          record.ID,
		ClientName:  record.ClientName,
		Configured:  true,
		CanReveal:   record.CodeCiphertext != "",
		CodeVersion: record.CodeVersion,
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}
	return plain, item, nil
}

// RegenerateClientCredential 重新生成已有客户端专属授权码
func (s *Service) RegenerateClientCredential(userID int, clientID int) (string, error) {
	if s == nil || s.engine == nil {
		return "", ErrUnavailable
	}
	if userID <= 0 || clientID <= 0 {
		return "", ErrInvalidUser
	}

	plain, err := newProtocolCode()
	if err != nil {
		return "", err
	}
	normalizedCode := CleanAuthCode(plain)

	ciphertext := ""
	if s.vaultRoot != "" {
		ciphertext, err = credential.Encrypt(s.vaultRoot, plain)
		if err != nil {
			return "", fmt.Errorf("protect client authorization code: %w", err)
		}
	}
	hash, err := password.Hash(normalizedCode)
	if err != nil {
		return "", fmt.Errorf("hash client authorization code: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var existing models.ClientCredential
	has, err := s.engine.Where("id = ? AND user_id = ? AND revoked_at IS NULL", clientID, userID).Get(&existing)
	if err != nil {
		return "", fmt.Errorf("load client credential: %w", err)
	}
	if !has {
		return "", ErrClientNotFound
	}

	version := existing.CodeVersion + 1
	existing.CodeHash = hash
	existing.CodeCiphertext = ciphertext
	existing.CodeVersion = version
	existing.LastUsedAt = nil
	existing.UpdatedAt = s.now()

	if _, err := s.engine.ID(existing.ID).Cols("code_hash", "code_ciphertext", "code_version", "last_used_at", "updated_at").Update(&existing); err != nil {
		return "", fmt.Errorf("update client authorization code: %w", err)
	}
	return plain, nil
}

// RevokeClientCredential 撤销指定客户端授权码
func (s *Service) RevokeClientCredential(userID int, clientID int) error {
	if s == nil || s.engine == nil {
		return ErrUnavailable
	}
	if userID <= 0 || clientID <= 0 {
		return ErrInvalidUser
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	affected, err := s.engine.Where("id = ? AND user_id = ? AND revoked_at IS NULL", clientID, userID).
		Cols("revoked_at", "code_hash", "code_ciphertext", "updated_at").
		Update(&models.ClientCredential{RevokedAt: &now, CodeHash: "", CodeCiphertext: ""})
	if err != nil {
		return fmt.Errorf("revoke client credential: %w", err)
	}
	if affected == 0 {
		return ErrClientNotFound
	}
	return nil
}

// RevealClientCredentials 解密当前用户所有有效客户端授权码明文，仅在密码验证后调用
func (s *Service) RevealClientCredentials(userID int) (map[int]string, error) {
	if s == nil || s.engine == nil {
		return nil, ErrUnavailable
	}
	if userID <= 0 {
		return nil, ErrInvalidUser
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	vaultRoot := s.vaultRoot
	if vaultRoot == "" {
		return make(map[int]string), nil
	}
	var items []models.ClientCredential
	if err := s.engine.Where("user_id = ? AND revoked_at IS NULL", userID).Find(&items); err != nil {
		return nil, fmt.Errorf("load client credentials for reveal: %w", err)
	}
	result := make(map[int]string, len(items))
	for _, item := range items {
		if item.CodeCiphertext == "" || item.CodeHash == "" {
			continue
		}
		plain, err := credential.Decrypt(vaultRoot, item.CodeCiphertext)
		if err != nil {
			continue
		}
		result[item.ID] = plain
	}
	return result, nil
}

// VerifyClientOrProtocolCode 同时支持客户端专属授权码以及遗留的协议授权码
func (s *Service) VerifyClientOrProtocolCode(userID int, protocol string, rawPlain string) bool {
	if s == nil || s.engine == nil || userID <= 0 || rawPlain == "" || len([]byte(rawPlain)) > 256 {
		password.VerifyDummy(rawPlain)
		return false
	}
	cleanedPlain := CleanAuthCode(rawPlain)
	if cleanedPlain == "" {
		password.VerifyDummy(rawPlain)
		return false
	}

	// 1. 优先校验客户端专属授权码
	var clients []models.ClientCredential
	err := s.engine.Where("user_id = ? AND revoked_at IS NULL AND code_hash != ''", userID).Find(&clients)
	if err == nil && len(clients) > 0 {
		for _, client := range clients {
			// 比对清洗后的统一大写无横线授权码
			if password.Verify(client.CodeHash, cleanedPlain) {
				now := s.now()
				_, _ = s.engine.ID(client.ID).
					Where("revoked_at IS NULL AND code_version = ? AND code_hash = ?", client.CodeVersion, client.CodeHash).
					Cols("last_used_at", "updated_at").Update(&models.ClientCredential{LastUsedAt: &now})
				return true
			}
		}
	}

	// 2. 回退校验旧版按协议划分的授权码（支持 cleanedPlain 或原樣 rawPlain）
	protocol, err = NormalizeProtocol(protocol)
	if err != nil {
		password.VerifyDummy(rawPlain)
		return false
	}
	var item models.ProtocolCredential
	has, err := s.engine.Where("user_id = ? AND protocol = ? AND enabled = ? AND revoked_at IS NULL", userID, protocol, true).Get(&item)
	if err != nil || !has || item.CodeHash == "" {
		password.VerifyDummy(rawPlain)
		return false
	}

	// 兼容旧哈希直接比对原始输入，或者比对清洗后的输入
	matched := password.Verify(item.CodeHash, strings.TrimSpace(rawPlain)) || password.Verify(item.CodeHash, cleanedPlain)
	if !matched {
		return false
	}

	now := s.now()
	affected, err := s.engine.ID(item.ID).
		Where("enabled = ? AND revoked_at IS NULL AND code_version = ? AND code_hash = ?", true, item.CodeVersion, item.CodeHash).
		Cols("last_used_at", "updated_at").Update(&models.ProtocolCredential{LastUsedAt: &now})
	if err == nil && affected == 1 {
		return true
	}

	valid, err := s.engine.ID(item.ID).
		Where("enabled = ? AND revoked_at IS NULL AND code_version = ? AND code_hash = ?", true, item.CodeVersion, item.CodeHash).
		Exist(&models.ProtocolCredential{})
	return err == nil && valid
}
