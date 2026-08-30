package auth

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/validation"
)

const maxLoginIdentifierBytes = 254

type IdentifierKind string

const (
	IdentifierAccount IdentifierKind = "account"
	IdentifierEmail   IdentifierKind = "email"
)

type ResolvedIdentity struct {
	UserID       int
	Account      string
	LoginAddress string
	Kind         IdentifierKind
}

// NormalizeIdentifier 保留旧调用方的账号规范化行为。
// 完整邮箱的认证解析由 ResolveIdentifier 负责，避免在这里截断域名。
func NormalizeIdentifier(identifier string) (string, error) {
	normalized, kind, err := parseLoginIdentifier(identifier)
	if err != nil {
		return "", err
	}
	if kind == IdentifierAccount {
		return validation.NormalizeAccount(normalized)
	}
	parts := strings.SplitN(normalized, "@", 2)
	if len(parts) != 2 || !isConfiguredLoginDomain(parts[1]) {
		return "", errors.New("invalid identifier")
	}
	return validation.NormalizeAccount(parts[0])
}

// NormalizeIdentifierKey 返回限流和审计使用的稳定标识，不泄露原始输入格式。
func NormalizeIdentifierKey(identifier string) (string, error) {
	normalized, _, err := parseLoginIdentifier(identifier)
	return normalized, err
}

// ResolveIdentifier 将账号或已验证邮箱绑定解析为唯一用户身份。
func ResolveIdentifier(identifier string) (ResolvedIdentity, error) {
	normalized, kind, err := parseLoginIdentifier(identifier)
	if err != nil {
		return ResolvedIdentity{}, err
	}
	if kind == IdentifierAccount {
		return ResolvedIdentity{Account: normalized, Kind: IdentifierAccount}, nil
	}
	if db.Instance == nil {
		return ResolvedIdentity{}, errors.New("database is unavailable")
	}

	var bindings []models.UserEmail
	if err := db.Instance.Table(&models.UserEmail{}).
		Where("address_canonical = ? AND verified_at IS NOT NULL AND status != ?", normalized, 3).
		Limit(2).
		Find(&bindings); err != nil {
		return ResolvedIdentity{}, fmt.Errorf("load login email binding: %w", err)
	}
	if len(bindings) != 1 || bindings[0].UserID <= 0 {
		return ResolvedIdentity{}, errors.New("invalid identifier")
	}

	var user models.User
	has, err := db.Instance.ID(bindings[0].UserID).Get(&user)
	if err != nil {
		return ResolvedIdentity{}, fmt.Errorf("load login user: %w", err)
	}
	if !has || user.ID <= 0 || user.Account == "" {
		return ResolvedIdentity{}, errors.New("invalid identifier")
	}
	return ResolvedIdentity{
		UserID:       user.ID,
		Account:      user.Account,
		LoginAddress: normalized,
		Kind:         IdentifierEmail,
	}, nil
}

func parseLoginIdentifier(identifier string) (string, IdentifierKind, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" || len([]byte(identifier)) > maxLoginIdentifierBytes || strings.ContainsAny(identifier, "\r\n") {
		return "", "", errors.New("invalid identifier")
	}
	if !strings.Contains(identifier, "@") {
		account, err := validation.NormalizeAccount(identifier)
		if err != nil {
			return "", "", errors.New("invalid identifier")
		}
		return account, IdentifierAccount, nil
	}
	if strings.Count(identifier, "@") != 1 {
		return "", "", errors.New("invalid identifier")
	}

	normalized := strings.ToLower(identifier)
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized {
		return "", "", errors.New("invalid identifier")
	}
	parts := strings.SplitN(normalized, "@", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", errors.New("invalid identifier")
	}
	if _, err := validation.NormalizeDomain(parts[1]); err != nil {
		return "", "", errors.New("invalid identifier")
	}
	for _, char := range parts[0] {
		if unicode.IsSpace(char) || unicode.IsControl(char) || strings.ContainsRune(`"(),:;<>[\\]`, char) {
			return "", "", errors.New("invalid identifier")
		}
	}
	return normalized, IdentifierEmail, nil
}

func isConfiguredLoginDomain(domain string) bool {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if manager := config.CurrentManager(); manager != nil {
		runtime := manager.Snapshot()
		if strings.TrimSuffix(strings.ToLower(strings.TrimSpace(runtime.Root.Domains.Primary)), ".") == domain {
			return true
		}
		for _, item := range runtime.Root.Domains.Accepted {
			if strings.TrimSuffix(strings.ToLower(strings.TrimSpace(item)), ".") == domain {
				return true
			}
		}
		return false
	}
	current := config.CurrentConfig()
	if current == nil {
		return false
	}
	if strings.TrimSuffix(strings.ToLower(strings.TrimSpace(current.Domain)), ".") == domain {
		return true
	}
	for _, item := range current.Domains {
		if strings.TrimSuffix(strings.ToLower(strings.TrimSpace(item)), ".") == domain {
			return true
		}
	}
	return false
}
