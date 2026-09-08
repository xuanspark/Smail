package auth

import (
	"database/sql"
	"errors"
	"fmt"
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
// NormalizeAccount 只清理外围空白，不再截断完整邮箱标识。
func NormalizeAccount(account string) string {
	return strings.TrimSpace(account)
}

// AuthenticateIdentifier 支持账号和已验证邮箱两种登录标识，最终统一到同一账号密码校验。
func AuthenticateIdentifier(identifier, plain string) (*models.User, error) {
	identity, err := ResolveIdentifier(identifier)
	if err != nil {
		if plain != "" {
			password.VerifyDummy(plain)
		}
		return nil, errors.New("invalid credentials")
	}
	if identity.Kind == IdentifierEmail {
		return authenticateUserByID(identity.UserID, plain)
	}
	return Authenticate(identity.Account, plain)
}

func Authenticate(account, plain string) (*models.User, error) {
	account = NormalizeAccount(account)
	account, err := validation.NormalizeAccount(account)
	if err != nil || plain == "" || len([]byte(plain)) > 256 {
		return nil, errors.New("invalid credentials")
	}
	var user models.User
	has, err := db.Instance.Where("account = ? and disabled = 0", account).Get(&user)
	return authenticateLoadedUser(&user, has, err, plain)
}

// AuthenticateUserByID 使用当前会话用户 ID 校验密码，避免重新认证时切换账号。
func AuthenticateUserByID(userID int, plain string) (*models.User, error) {
	return authenticateUserByID(userID, plain)
}

func authenticateUserByID(userID int, plain string) (*models.User, error) {
	if userID <= 0 {
		if plain != "" {
			password.VerifyDummy(plain)
		}
		return nil, errors.New("invalid credentials")
	}
	if plain == "" || len([]byte(plain)) > 256 {
		return nil, errors.New("invalid credentials")
	}
	var user models.User
	has, err := db.Instance.ID(userID).Where("disabled = 0").Get(&user)
	return authenticateLoadedUser(&user, has, err, plain)
}

func authenticateLoadedUser(user *models.User, has bool, err error, plain string) (*models.User, error) {
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
	return user, nil
}

// HashPassword 统一生成新密码哈希。
func HashPassword(plain string) (string, error) {
	return password.HashUserPassword(plain)
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
	basePath := filepath.Join(config.ROOT_PATH, "data", "dkim")
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
