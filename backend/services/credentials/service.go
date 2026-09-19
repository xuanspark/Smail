package credentials

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/password"
	"golang.org/x/text/unicode/norm"
	"xorm.io/xorm"
)

var (
	ErrUnavailable     = errors.New("credential service unavailable")
	ErrInvalidUser     = errors.New("credential user is invalid")
	ErrInvalidQuestion = errors.New("security question is invalid")
	ErrInvalidAnswer   = errors.New("security answer is invalid")
	ErrInvalidCode     = errors.New("recovery code is invalid")
	ErrCodeNotFound    = errors.New("recovery code is not valid")
	ErrCodeConsumed    = errors.New("recovery code is already consumed")
)

const (
	defaultCodeCount = 10
	minCodeCount     = 5
	maxCodeCount     = 20
)

// Service 统一管理密保问题和离线恢复码。
type Service struct {
	engine    *xorm.Engine
	mu        sync.Mutex
	now       func() time.Time
	vaultRoot string
}

func (s *Service) SetVaultRoot(root string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vaultRoot = root
}

func (s *Service) VaultRoot() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vaultRoot
}

func NewService(engine *xorm.Engine) (*Service, error) {
	if engine == nil {
		return nil, ErrUnavailable
	}
	return &Service{engine: engine, now: time.Now}, nil
}

// SetSecurityQuestion 设置或替换密保问题，答案只以 Argon2id 哈希保存。
func (s *Service) SetSecurityQuestion(userID int, question, answer string) error {
	if s == nil || s.engine == nil {
		return ErrUnavailable
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	question = strings.TrimSpace(norm.NFKC.String(question))
	answer = normalizeAnswer(answer)
	if len([]rune(question)) < 1 {
		return ErrInvalidQuestion
	}
	if len([]rune(answer)) < 1 {
		return ErrInvalidAnswer
	}
	hash, err := password.Hash(answer)
	if err != nil {
		return fmt.Errorf("hash security answer: %w", err)
	}
	item := &models.SecurityQuestion{}
	has, err := s.engine.Where("user_id = ?", userID).Get(item)
	if err != nil {
		return fmt.Errorf("load security question: %w", err)
	}
	if has {
		item.Question, item.AnswerHash = question, hash
		if _, err := s.engine.ID(item.ID).Cols("question", "answer_hash", "updated_at").Update(item); err != nil {
			return fmt.Errorf("update security question: %w", err)
		}
		return nil
	}
	if _, err := s.engine.Insert(&models.SecurityQuestion{UserID: userID, Question: question, AnswerHash: hash, Version: 1}); err != nil {
		return fmt.Errorf("create security question: %w", err)
	}
	return nil
}

// GetSecurityQuestion 返回问题文本，不返回答案哈希。
func (s *Service) GetSecurityQuestion(userID int) (string, bool, error) {
	if s == nil || s.engine == nil {
		return "", false, ErrUnavailable
	}
	if userID <= 0 {
		return "", false, ErrInvalidUser
	}
	var item models.SecurityQuestion
	has, err := s.engine.Where("user_id = ?", userID).Get(&item)
	if err != nil {
		return "", false, fmt.Errorf("load security question: %w", err)
	}
	return item.Question, has, nil
}

// VerifySecurityQuestion 以恒定时间比较验证规范化后的答案。
func (s *Service) VerifySecurityQuestion(userID int, answer string) (bool, error) {
	if s == nil || s.engine == nil {
		return false, ErrUnavailable
	}
	if userID <= 0 {
		return false, ErrInvalidUser
	}
	answer = normalizeAnswer(answer)
	var item models.SecurityQuestion
	has, err := s.engine.Where("user_id = ?", userID).Get(&item)
	if err != nil {
		return false, fmt.Errorf("load security question: %w", err)
	}
	if !has {
		return false, password.CheckDummy(answer)
	}
	return password.Check(item.AnswerHash, answer)
}

// GenerateRecoveryCodes 撤销旧码并生成一组只返回一次的离线恢复码。
func (s *Service) GenerateRecoveryCodes(userID int, count int) ([]string, error) {
	if s == nil || s.engine == nil {
		return nil, ErrUnavailable
	}
	if userID <= 0 {
		return nil, ErrInvalidUser
	}
	if count == 0 {
		count = defaultCodeCount
	}
	if count < minCodeCount || count > maxCodeCount {
		return nil, fmt.Errorf("recovery code count must be between %d and %d", minCodeCount, maxCodeCount)
	}
	codes := make([]string, 0, count)
	items := make([]models.RecoveryCode, 0, count)
	for i := 0; i < count; i++ {
		plain, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		hash, err := password.Hash(plain)
		if err != nil {
			return nil, fmt.Errorf("hash recovery code: %w", err)
		}
		codes = append(codes, plain)
		items = append(items, models.RecoveryCode{UserID: userID, CodeHash: hash, CodeVersion: 1})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.engine.NewSession()
	if session == nil {
		return nil, ErrUnavailable
	}
	defer session.Close()
	if err := session.Begin(); err != nil {
		return nil, fmt.Errorf("begin recovery code transaction: %w", err)
	}
	now := s.now()
	if _, err := session.Where("user_id = ? AND used_at IS NULL", userID).Cols("used_at").Update(&models.RecoveryCode{UsedAt: &now}); err != nil {
		_ = session.Rollback()
		return nil, fmt.Errorf("revoke old recovery codes: %w", err)
	}
	for i := range items {
		if _, err := session.Insert(&items[i]); err != nil {
			_ = session.Rollback()
			return nil, fmt.Errorf("store recovery code: %w", err)
		}
	}
	if err := session.Commit(); err != nil {
		return nil, fmt.Errorf("commit recovery codes: %w", err)
	}
	return codes, nil
}

// ConsumeRecoveryCode 原子消费一个恢复码，成功后该码不可再次使用。
func (s *Service) ConsumeRecoveryCode(userID int, code string) error {
	if s == nil || s.engine == nil {
		return ErrUnavailable
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	code = strings.TrimSpace(strings.ToUpper(code))
	if len(code) < 12 || len(code) > 64 {
		return ErrInvalidCode
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.consumeRecoveryCode(s.engine, userID, code)
}

// ConsumeRecoveryCodeWithSession 在调用方已开启的事务中原子核销一枚恢复码。
// 密码重置等复合安全操作必须使用此方法，避免密码更新失败时恢复码被提前消耗。
func (s *Service) ConsumeRecoveryCodeWithSession(session *xorm.Session, userID int, code string) error {
	if s == nil || s.engine == nil || session == nil {
		return ErrUnavailable
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	code = strings.TrimSpace(strings.ToUpper(code))
	if len(code) < 12 || len(code) > 64 {
		return ErrInvalidCode
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.consumeRecoveryCode(session, userID, code)
}

type recoveryCodeSession interface {
	Where(queryInterface interface{}, args ...interface{}) *xorm.Session
	ID(id interface{}) *xorm.Session
}

func (s *Service) consumeRecoveryCode(session recoveryCodeSession, userID int, code string) error {
	var items []models.RecoveryCode
	if err := session.Where("user_id = ? AND used_at IS NULL", userID).Find(&items); err != nil {
		return fmt.Errorf("load recovery codes: %w", err)
	}
	for _, item := range items {
		matched, err := password.Check(item.CodeHash, code)
		if err != nil {
			return err
		}
		if !matched {
			continue
		}
		now := s.now()
		affected, err := session.ID(item.ID).Where("used_at IS NULL").Cols("used_at").Update(&models.RecoveryCode{UsedAt: &now})
		if err != nil {
			return fmt.Errorf("consume recovery code: %w", err)
		}
		if affected != 1 {
			return ErrCodeConsumed
		}
		return nil
	}
	return ErrCodeNotFound
}

// HasValidRecoveryCode 用于高风险操作的前置检查。
func (s *Service) HasValidRecoveryCode(userID int) (bool, error) {
	if s == nil || s.engine == nil {
		return false, ErrUnavailable
	}
	if userID <= 0 {
		return false, ErrInvalidUser
	}
	count, err := s.engine.Where("user_id = ? AND used_at IS NULL", userID).Count(&models.RecoveryCode{})
	if err != nil {
		return false, fmt.Errorf("count recovery codes: %w", err)
	}
	return count > 0, nil
}

// RevokeRecoveryCodes 撤销用户现有的离线恢复码，但保留密保问题。
func (s *Service) RevokeRecoveryCodes(userID int) error {
	if s == nil || s.engine == nil {
		return ErrUnavailable
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if _, err := s.engine.Where("user_id = ? AND used_at IS NULL", userID).Cols("used_at").Update(&models.RecoveryCode{UsedAt: &now}); err != nil {
		return fmt.Errorf("revoke recovery codes: %w", err)
	}
	return nil
}

// RevokeAll 撤销用户的恢复码、密保问题、安全密钥及邮件授权码。
func (s *Service) RevokeAll(userID int) error {
	if s == nil || s.engine == nil {
		return ErrUnavailable
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return revokeAllOnSession(s.engine.NewSession(), s.now, userID)
}

// RevokeAllWithSession 在调用方事务中撤销用户凭据，与密码更新共同提交或回滚。
func (s *Service) RevokeAllWithSession(session *xorm.Session, userID int) error {
	if s == nil || s.engine == nil || session == nil {
		return ErrUnavailable
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if _, err := session.Where("user_id = ? AND used_at IS NULL", userID).Cols("used_at").Update(&models.RecoveryCode{UsedAt: &now}); err != nil {
		return fmt.Errorf("revoke recovery codes: %w", err)
	}
	if _, err := session.Where("user_id = ?", userID).Delete(&models.SecurityQuestion{}); err != nil {
		return fmt.Errorf("revoke security question: %w", err)
	}
	now = s.now()
	if _, err := session.Where("user_id = ? AND revoked_at IS NULL", userID).Cols("revoked_at").Update(&models.WebAuthnCredential{RevokedAt: &now}); err != nil {
		return fmt.Errorf("revoke webauthn credentials: %w", err)
	}
	if _, err := session.Where("user_id = ? AND revoked_at IS NULL", userID).Cols("revoked_at", "updated_at").Update(&models.ProtocolCredential{RevokedAt: &now}); err != nil {
		return fmt.Errorf("revoke protocol credentials: %w", err)
	}
	if _, err := session.Where("user_id = ? AND revoked_at IS NULL", userID).Cols("revoked_at", "updated_at").Update(&models.ClientCredential{RevokedAt: &now}); err != nil {
		return fmt.Errorf("revoke client credentials: %w", err)
	}
	return nil
}

func revokeAllOnSession(session *xorm.Session, now func() time.Time, userID int) error {
	if session == nil {
		return ErrUnavailable
	}
	defer session.Close()
	if err := session.Begin(); err != nil {
		return fmt.Errorf("begin credential revocation: %w", err)
	}
	when := now()
	if _, err := session.Where("user_id = ? AND used_at IS NULL", userID).Cols("used_at").Update(&models.RecoveryCode{UsedAt: &when}); err != nil {
		_ = session.Rollback()
		return fmt.Errorf("revoke recovery codes: %w", err)
	}
	if _, err := session.Where("user_id = ?", userID).Delete(&models.SecurityQuestion{}); err != nil {
		_ = session.Rollback()
		return fmt.Errorf("revoke security question: %w", err)
	}
	when = now()
	if _, err := session.Where("user_id = ? AND revoked_at IS NULL", userID).Cols("revoked_at").Update(&models.WebAuthnCredential{RevokedAt: &when}); err != nil {
		_ = session.Rollback()
		return fmt.Errorf("revoke webauthn credentials: %w", err)
	}
	if _, err := session.Where("user_id = ? AND revoked_at IS NULL", userID).Cols("revoked_at", "updated_at").Update(&models.ProtocolCredential{RevokedAt: &when}); err != nil {
		_ = session.Rollback()
		return fmt.Errorf("revoke protocol credentials: %w", err)
	}
	if _, err := session.Where("user_id = ? AND revoked_at IS NULL", userID).Cols("revoked_at", "updated_at").Update(&models.ClientCredential{RevokedAt: &when}); err != nil {
		_ = session.Rollback()
		return fmt.Errorf("revoke client credentials: %w", err)
	}
	if err := session.Commit(); err != nil {
		return fmt.Errorf("commit credential revocation: %w", err)
	}
	return nil
}

func normalizeAnswer(value string) string {
	return strings.ToLower(strings.TrimSpace(norm.NFKC.String(value)))
}

func newRecoveryCode() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate recovery code: %w", err)
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return encoded[:5] + "-" + encoded[5:10] + "-" + encoded[10:], nil
}
