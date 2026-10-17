package notification

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/accountgroup"
	"git.pcux.cn/xuans/Smail/utils/credential"
	"xorm.io/xorm"
)

const (
	UsernamePrefix     = "notify:"
	DefaultMinuteLimit = 10
	DefaultDayLimit    = 100
	MaxRecipients      = 10
	MaxMessageBytes    = 1 << 20
)

var (
	ErrDenied       = errors.New("notification authorization denied")
	ErrConflict     = errors.New("notification authorization changed")
	ErrQuota        = errors.New("notification quota exceeded")
	ErrUnavailable  = errors.New("notification authorization unavailable")
	ErrLimits       = errors.New("invalid notification limits")
	ErrNoCiphertext = errors.New("notification credential must be regenerated before reveal")
)

func IsUsername(username string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(username)), UsernamePrefix)
}

func Load(engine *xorm.Engine, userID int) (models.NotificationGrant, error) {
	var item models.NotificationGrant
	if engine == nil || userID <= 0 {
		return item, ErrUnavailable
	}
	found, err := engine.ID(userID).Get(&item)
	if err == nil && !found {
		item = models.NotificationGrant{UserID: userID, MinuteLimit: DefaultMinuteLimit, DayLimit: DefaultDayLimit}
	}
	return item, err
}

func Configured(item models.NotificationGrant, user models.User) bool {
	return item.Enabled == 1 && item.CodeHash != "" && user.Disabled == 0 && user.MustChangePassword == 0 &&
		item.SessionVersion == user.SessionVersion && item.SecurityVersion == user.SecurityVersion
}

// SetAuthorization 使用版本条件避免覆盖其他管理员的修改；关闭时永久清除旧凭据。
func SetAuthorization(engine *xorm.Engine, user models.User, expected int, enabled bool, minuteLimit, dayLimit int, senderName string, guards ...accountgroup.WriteGuard) error {
	if minuteLimit < 1 || minuteLimit > 100 || dayLimit < minuteLimit || dayLimit > 10000 {
		return ErrLimits
	}
	if enabled && (user.Disabled != 0 || user.MustChangePassword != 0) {
		return ErrDenied
	}
	if engine == nil || user.ID <= 0 {
		return ErrUnavailable
	}
	tx := engine.NewSession()
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		return err
	}
	defer tx.Rollback()
	if err := accountgroup.CheckWrite(tx, guards...); err != nil {
		return err
	}
	var item models.NotificationGrant
	found, err := tx.ID(user.ID).Get(&item)
	if err != nil {
		return err
	}
	if !found {
		item = models.NotificationGrant{UserID: user.ID}
	}
	if item.CodeVersion != expected {
		return ErrConflict
	}
	item.Enabled = 0
	if enabled {
		item.Enabled = 1
	} else {
		item.CodeHash = ""
		item.CodeCiphertext = ""
	}
	senderName = strings.TrimSpace(senderName)
	if len(senderName) > 64 {
		senderName = senderName[:64]
	}
	item.SenderName = senderName
	item.MinuteLimit, item.DayLimit = minuteLimit, dayLimit
	item.CodeVersion++
	if expected == 0 {
		if _, err := tx.Insert(&item); err != nil {
			return err
		}
		return tx.Commit()
	}
	affected, err := tx.ID(user.ID).Where("code_version = ?", expected).
		Cols("enabled", "code_hash", "code_ciphertext", "code_version", "minute_limit", "day_limit", "sender_name").Update(&item)
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

// Rotate 生成独立随机凭据；加密绑定账号，关闭授权时同时清除摘要与密文。
func Rotate(engine *xorm.Engine, user models.User, expected int, root string, guards ...accountgroup.WriteGuard) (string, error) {
	if engine == nil || user.ID <= 0 || strings.TrimSpace(root) == "" {
		return "", ErrUnavailable
	}
	tx := engine.NewSession()
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := accountgroup.CheckWrite(tx, guards...); err != nil {
		return "", err
	}
	var item models.NotificationGrant
	_, err := tx.ID(user.ID).Get(&item)
	if err != nil {
		return "", err
	}
	if item.Enabled != 1 || user.Disabled != 0 || user.MustChangePassword != 0 {
		return "", ErrDenied
	}
	if item.CodeVersion != expected {
		return "", ErrConflict
	}
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	code := "sn_" + base64.RawURLEncoding.EncodeToString(entropy[:])
	ciphertext, err := credential.Encrypt(root, strconv.Itoa(user.ID)+":"+code)
	if err != nil {
		return "", ErrUnavailable
	}
	item.CodeHash = digest(code)
	item.CodeCiphertext = ciphertext
	item.CodeVersion++
	item.SessionVersion, item.SecurityVersion = user.SessionVersion, user.SecurityVersion
	affected, err := tx.ID(user.ID).Where("code_version = ? AND enabled = 1", expected).
		Cols("code_hash", "code_ciphertext", "code_version", "session_version", "security_version").Update(&item)
	if err != nil {
		return "", err
	}
	if affected != 1 {
		return "", ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return code, nil
}

// Reveal 在事务内复核管理范围、账号安全版本和凭据版本，绝不重置用量或凭据。
func Reveal(engine *xorm.Engine, user models.User, expected int, root string, guards ...accountgroup.WriteGuard) (string, error) {
	if engine == nil || user.ID <= 0 || strings.TrimSpace(root) == "" {
		return "", ErrUnavailable
	}
	tx := engine.NewSession()
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := accountgroup.CheckWrite(tx, guards...); err != nil {
		return "", err
	}
	var item models.NotificationGrant
	found, err := tx.ID(user.ID).Get(&item)
	if err != nil {
		return "", err
	}
	if !found || !Configured(item, user) {
		return "", ErrDenied
	}
	if item.CodeVersion != expected {
		return "", ErrConflict
	}
	if item.CodeCiphertext == "" {
		return "", ErrNoCiphertext
	}
	plain, err := credential.Decrypt(root, item.CodeCiphertext)
	if err != nil {
		return "", ErrUnavailable
	}
	account, code, ok := strings.Cut(plain, ":")
	if !ok || account != strconv.Itoa(user.ID) || len(code) != 46 || !strings.HasPrefix(code, "sn_") ||
		subtle.ConstantTimeCompare([]byte(item.CodeHash), []byte(digest(code))) != 1 {
		return "", ErrUnavailable
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return code, nil
}

type Grant struct {
	engine          *xorm.Engine
	userID          int
	version         int
	hash            string
	sessionVersion  int
	securityVersion int
	senderName      string
}

func (g *Grant) SenderName() string {
	if g == nil {
		return ""
	}
	return g.senderName
}

func (g *Grant) QueueBinding() (string, int, int, string) {
	if g == nil {
		return "", 0, 0, ""
	}
	return "notification", g.userID, g.version, digest(g.hash)
}

func Authenticate(engine *xorm.Engine, user models.User, plain string) (*Grant, error) {
	if len(plain) != 46 || !strings.HasPrefix(plain, "sn_") {
		return nil, ErrDenied
	}
	item, err := Load(engine, user.ID)
	if err != nil || !Configured(item, user) {
		return nil, ErrDenied
	}
	if subtle.ConstantTimeCompare([]byte(item.CodeHash), []byte(digest(plain))) != 1 {
		return nil, ErrDenied
	}
	return &Grant{engine, user.ID, item.CodeVersion, item.CodeHash, item.SessionVersion, item.SecurityVersion, item.SenderName}, nil
}

func (g *Grant) Valid(engine *xorm.Engine) bool {
	if g == nil || engine == nil || engine != g.engine {
		return false
	}
	valid, err := engine.ID(g.userID).Where("enabled = 1 AND code_version = ? AND code_hash = ?", g.version, g.hash).
		Exist(&models.NotificationGrant{})
	if err != nil || !valid {
		return false
	}
	valid, err = engine.ID(g.userID).Where("disabled = 0 AND must_change_password = 0 AND session_version = ? AND security_version = ?", g.sessionVersion, g.securityVersion).
		Exist(&models.User{})
	return err == nil && valid
}

// Reserve 将收件人数计入配额，重试和失败也计数；并发连接和服务重启不能绕过限制。
func (g *Grant) Reserve(engine *xorm.Engine, recipients int, now time.Time) error {
	if !g.Valid(engine) {
		return ErrDenied
	}
	if recipients < 1 || recipients > MaxRecipients {
		return ErrLimits
	}
	minute, day := now.UTC().Format("2006-01-02T15:04"), now.UTC().Format("2006-01-02")
	result, err := engine.Exec(`UPDATE notification_grant SET
		minute_count = CASE WHEN minute_key = ? THEN minute_count + ? ELSE ? END,
		day_count = CASE WHEN day_key = ? THEN day_count + ? ELSE ? END,
		minute_key = ?, day_key = ?
		WHERE user_id = ? AND enabled = 1 AND code_version = ? AND code_hash = ?
		AND ? <= minute_limit AND ? <= day_limit
		AND (minute_key <> ? OR minute_count + ? <= minute_limit)
		AND (day_key <> ? OR day_count + ? <= day_limit)`,
		minute, recipients, recipients, day, recipients, recipients, minute, day,
		g.userID, g.version, g.hash, recipients, recipients, minute, recipients, day, recipients)
	if err != nil {
		return ErrUnavailable
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ErrUnavailable
	}
	if affected != 1 {
		return ErrQuota
	}
	return nil
}

func digest(plain string) string {
	hash := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(hash[:])
}
