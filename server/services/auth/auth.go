package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"path/filepath"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/password"
	"git.pcux.cn/xuans/Smail/utils/validation"
	log "github.com/sirupsen/logrus"
)

// Authenticate 统一校验网页、SMTP、IMAP 和 POP3 的用户凭据。
func NormalizeAccount(account string) string {
	account = strings.TrimSpace(account)
	if index := strings.IndexByte(account, '@'); index > 0 {
		account = account[:index]
	}
	return account
}

// AuthenticateIdentifier 支持账号和受信主域名邮箱两种登录标识，最终统一到同一账号密码校验。
func AuthenticateIdentifier(identifier, plain string) (*models.User, error) {
	account, err := NormalizeIdentifier(identifier)
	if err != nil {
		if plain != "" {
			password.VerifyDummy(plain)
		}
		return nil, errors.New("invalid credentials")
	}
	return Authenticate(account, plain)
}

// NormalizeIdentifier 将完整邮箱解析为本地账号，并拒绝未配置的域名。
func NormalizeIdentifier(identifier string) (string, error) {
	identifier = strings.TrimSpace(strings.ToLower(identifier))
	if identifier == "" || strings.ContainsAny(identifier, "\r\n") {
		return "", errors.New("invalid identifier")
	}
	if !strings.Contains(identifier, "@") {
		return validation.NormalizeAccount(identifier)
	}
	parsed, err := mail.ParseAddress(identifier)
	if err != nil || parsed.Address != identifier {
		return "", errors.New("invalid identifier")
	}
	parts := strings.Split(identifier, "@")
	if len(parts) != 2 || parts[0] == "" || !isConfiguredLoginDomain(parts[1]) {
		return "", errors.New("invalid identifier")
	}
	return validation.NormalizeAccount(parts[0])
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

func Authenticate(account, plain string) (*models.User, error) {
	account = NormalizeAccount(account)
	account, err := validation.NormalizeAccount(account)
	if err != nil || plain == "" || len([]byte(plain)) > 256 {
		return nil, errors.New("invalid credentials")
	}
	var user models.User
	has, err := db.Instance.Where("account = ? and disabled = 0", account).Get(&user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			password.VerifyDummy(plain)
			return nil, errors.New("invalid credentials")
		}
		return nil, fmt.Errorf("load user: %w", err)
	}
	if !has || user.ID == 0 {
		password.VerifyDummy(plain)
		return nil, errors.New("invalid credentials")
	}
	if !password.Verify(user.Password, plain) {
		return nil, errors.New("invalid credentials")
	}
	return &user, nil
}

// HashPassword 统一生成新密码哈希。
func HashPassword(plain string) (string, error) {
	return password.Hash(plain)
}

// SetPassword 为用户设置新密码。
func SetPassword(user *models.User, plain string) error {
	hash, err := HashPassword(plain)
	if err != nil {
		return err
	}
	user.Password = hash
	return nil
}

// HasAuth 检查当前用户是否有某个邮件的访问权限。
func HasAuth(ctx *context.Context, email *models.Email) bool {
	if ctx == nil || email == nil || ctx.UserID <= 0 || email.Id <= 0 || db.Instance == nil {
		return false
	}
	var ue []models.UserEmail
	err := db.Instance.Table(&models.UserEmail{}).Where("email_id = ? and user_id = ?", email.Id, ctx.UserID).Find(&ue)
	if err != nil {
		log.Errorf("Error while checking user: %v", err)
		return false
	}

	return len(ue) != 0
}

func DkimGen() (string, error) {
	basePath := filepath.Join(config.ROOT_PATH, "config", "dkim")
	cfg := config.CurrentConfig()
	if cfg == nil {
		return "", fmt.Errorf("runtime configuration is unavailable")
	}
	privatePath := cfg.DkimPrivateKeyPath
	if privatePath == "" {
		privatePath = filepath.Join(basePath, "dkim.priv")
	}
	publicKey, err := ensureDKIMKeyPair(basePath, privatePath)
	if err != nil {
		return "", fmt.Errorf("generate DKIM key pair: %w", err)
	}
	return publicKey, nil
}
