package externalemail

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"xorm.io/xorm"
)

var (
	ErrInvalidBinding   = errors.New("invalid external email binding")
	ErrBindingExists    = errors.New("external email binding already exists")
	ErrPrimaryLimit     = errors.New("primary external email already exists")
	ErrBackupLimit      = errors.New("backup external email already exists")
	ErrBindingNotFound  = errors.New("external email binding not found")
	ErrBindingOwnership = errors.New("external email binding ownership denied")
)

type Repository struct {
	engine *xorm.Engine
	vault  *Vault
	now    func() time.Time
}

type BindRequest struct {
	UserID int
	Email  string
	Type   string
}

func NewRepository(engine *xorm.Engine, vault *Vault) (*Repository, error) {
	if engine == nil || vault == nil {
		return nil, ErrVaultUnavailable
	}
	return &Repository{engine: engine, vault: vault, now: time.Now}, nil
}

func (r *Repository) Bind(request BindRequest) (*models.ExternalEmail, error) {
	if r == nil || r.engine == nil || r.vault == nil || request.UserID <= 0 {
		return nil, ErrInvalidBinding
	}
	if request.Type != models.ExternalEmailTypePrimary && request.Type != models.ExternalEmailTypeBackup {
		return nil, ErrInvalidBinding
	}
	normalized, err := NormalizeEmail(request.Email)
	if err != nil {
		return nil, ErrInvalidBinding
	}
	fingerprint, err := r.vault.Fingerprint(normalized)
	if err != nil {
		return nil, err
	}
	ciphertext, err := r.vault.Encrypt(normalized)
	if err != nil {
		return nil, err
	}
	var existing models.ExternalEmail
	has, err := r.engine.Where("user_id = ? AND email_fingerprint = ?", request.UserID, fingerprint).Get(&existing)
	if err != nil {
		return nil, fmt.Errorf("check external email binding: %w", err)
	}
	if has {
		return nil, ErrBindingExists
	}
	var count int64
	count, err = r.engine.Where("user_id = ? AND email_type = ? AND status != ?", request.UserID, request.Type, models.ExternalEmailStatusDisabled).Count(&models.ExternalEmail{})
	if err != nil {
		return nil, fmt.Errorf("count external email bindings: %w", err)
	}
	if request.Type == models.ExternalEmailTypePrimary && count >= 1 {
		return nil, ErrPrimaryLimit
	}
	if request.Type == models.ExternalEmailTypeBackup && count >= 1 {
		return nil, ErrBackupLimit
	}
	now := r.now().UTC()
	item := &models.ExternalEmail{
		UserID:            request.UserID,
		EmailCiphertext:   ciphertext,
		EmailFingerprint:  fingerprint,
		EmailType:         request.Type,
		Status:            models.ExternalEmailStatusPending,
		CredentialVersion: 1,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if _, err := r.engine.Insert(item); err != nil {
		return nil, fmt.Errorf("create external email binding: %w", err)
	}
	return item, nil
}

func (r *Repository) List(userID int) ([]models.ExternalEmail, error) {
	if r == nil || r.engine == nil || userID <= 0 {
		return nil, ErrInvalidBinding
	}
	var items []models.ExternalEmail
	if err := r.engine.Where("user_id = ?", userID).OrderBy("id asc").Find(&items); err != nil {
		return nil, fmt.Errorf("list external email bindings: %w", err)
	}
	return items, nil
}

// FindVerifiedByEmail 仅返回已验证且仍启用的邮箱绑定，调用方不能跨用户使用结果。
func (r *Repository) FindVerifiedByEmail(email string) (*models.ExternalEmail, error) {
	if r == nil || r.engine == nil || r.vault == nil {
		return nil, ErrInvalidBinding
	}
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return nil, ErrBindingNotFound
	}
	fingerprint, err := r.vault.Fingerprint(normalized)
	if err != nil {
		return nil, err
	}
	item := &models.ExternalEmail{}
	has, err := r.engine.Where("email_fingerprint = ? AND status = ?", fingerprint, models.ExternalEmailStatusVerified).Get(item)
	if err != nil {
		return nil, fmt.Errorf("find verified external email: %w", err)
	}
	if !has {
		return nil, ErrBindingNotFound
	}
	return item, nil
}

func (r *Repository) FindVerifiedByID(userID, id int) (*models.ExternalEmail, error) {
	if r == nil || r.engine == nil || userID <= 0 || id <= 0 {
		return nil, ErrInvalidBinding
	}
	item := &models.ExternalEmail{}
	has, err := r.engine.Where("id = ? AND user_id = ? AND status = ?", id, userID, models.ExternalEmailStatusVerified).Get(item)
	if err != nil {
		return nil, fmt.Errorf("find verified external email by id: %w", err)
	}
	if !has {
		return nil, ErrBindingNotFound
	}
	return item, nil
}

func (r *Repository) Verify(userID, id int) error {
	if r == nil || r.engine == nil || userID <= 0 || id <= 0 {
		return ErrInvalidBinding
	}
	item := &models.ExternalEmail{}
	has, err := r.engine.Where("id = ? AND user_id = ?", id, userID).Get(item)
	if err != nil {
		return fmt.Errorf("load external email binding: %w", err)
	}
	if !has {
		return ErrBindingOwnership
	}
	if item.Status == models.ExternalEmailStatusDisabled {
		return ErrBindingNotFound
	}
	item.Status = models.ExternalEmailStatusVerified
	item.VerifiedAt = timePtr(r.now().UTC())
	item.UpdatedAt = r.now().UTC()
	if _, err := r.engine.ID(id).Cols("status", "verified_at", "updated_at").Where("user_id = ?", userID).Update(item); err != nil {
		return fmt.Errorf("verify external email binding: %w", err)
	}
	return nil
}

// AbandonPending 删除当前请求刚创建且尚未验证的绑定，供投递失败时回滚。
func (r *Repository) AbandonPending(userID, id int) error {
	if r == nil || r.engine == nil || userID <= 0 || id <= 0 {
		return ErrInvalidBinding
	}
	affected, err := r.engine.Where("id = ? AND user_id = ? AND status = ?", id, userID, models.ExternalEmailStatusPending).Delete(&models.ExternalEmail{})
	if err != nil {
		return fmt.Errorf("abandon pending external email: %w", err)
	}
	if affected != 1 {
		return ErrBindingOwnership
	}
	return nil
}

func (r *Repository) Disable(userID, id int) error {
	if r == nil || r.engine == nil || userID <= 0 || id <= 0 {
		return ErrInvalidBinding
	}
	updated, err := r.engine.Table(&models.ExternalEmail{}).
		Where("id = ? AND user_id = ? AND status != ?", id, userID, models.ExternalEmailStatusDisabled).
		Cols("status", "updated_at").Update(&models.ExternalEmail{Status: models.ExternalEmailStatusDisabled, UpdatedAt: r.now().UTC()})
	if err != nil {
		return fmt.Errorf("disable external email binding: %w", err)
	}
	if updated == 0 {
		return ErrBindingOwnership
	}
	return nil
}

// FindVerifiedPrimaryByUser 返回当前用户唯一有效的主要外部邮箱。
func (r *Repository) FindVerifiedPrimaryByUser(userID int) (*models.ExternalEmail, error) {
	if r == nil || r.engine == nil || r.vault == nil || userID <= 0 {
		return nil, ErrInvalidBinding
	}
	item := &models.ExternalEmail{}
	has, err := r.engine.Where("user_id = ? AND email_type = ? AND status = ?", userID, models.ExternalEmailTypePrimary, models.ExternalEmailStatusVerified).Get(item)
	if err != nil {
		return nil, fmt.Errorf("find verified primary external email: %w", err)
	}
	if !has {
		return nil, ErrBindingNotFound
	}
	return item, nil
}
func (r *Repository) Decrypt(item models.ExternalEmail) (string, error) {
	if r == nil || r.vault == nil || strings.TrimSpace(item.EmailCiphertext) == "" {
		return "", ErrInvalidBinding
	}
	return r.vault.Decrypt(item.EmailCiphertext)
}

func timePtr(value time.Time) *time.Time {
	return &value
}
