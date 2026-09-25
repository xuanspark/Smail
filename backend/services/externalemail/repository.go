package externalemail

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/accountgroup"
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

// BeginReplacement 创建或更新待验证邮箱。旧邮箱会一直保留到新邮箱验证成功。
func (r *Repository) BeginReplacement(request BindRequest, guards ...accountgroup.WriteGuard) (*models.ExternalEmail, error) {
	if r == nil || r.engine == nil {
		return nil, ErrInvalidBinding
	}
	tx := r.engine.NewSession()
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := accountgroup.CheckWrite(tx, guards...); err != nil {
		return nil, err
	}
	item, err := r.beginReplacement(tx, request)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *Repository) beginReplacement(tx *xorm.Session, request BindRequest) (*models.ExternalEmail, error) {
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

	var sameAddress models.ExternalEmail
	hasSameAddress, err := tx.Where("email_fingerprint = ?", fingerprint).Get(&sameAddress)
	if err != nil {
		return nil, fmt.Errorf("check replacement external email: %w", err)
	}
	if hasSameAddress {
		if sameAddress.UserID != request.UserID || sameAddress.Status == models.ExternalEmailStatusVerified {
			return nil, ErrBindingExists
		}
		if sameAddress.Status != models.ExternalEmailStatusDisabled && sameAddress.EmailType != request.Type {
			return nil, ErrBindingExists
		}
		if sameAddress.Status == models.ExternalEmailStatusPending {
			return &sameAddress, nil
		}
	}

	now := r.now().UTC()
	if hasSameAddress && sameAddress.Status == models.ExternalEmailStatusDisabled {
		sameAddress.EmailType = request.Type
		sameAddress.Status = models.ExternalEmailStatusPending
		sameAddress.VerifiedAt = nil
		sameAddress.CredentialVersion++
		sameAddress.UpdatedAt = now
		updated, err := tx.ID(sameAddress.ID).Where("status = ?", models.ExternalEmailStatusDisabled).
			Cols("email_type", "status", "verified_at", "credential_version", "updated_at").Update(&sameAddress)
		if err != nil {
			return nil, fmt.Errorf("reuse disabled replacement external email: %w", err)
		}
		if updated != 1 {
			return nil, ErrBindingExists
		}
		return &sameAddress, nil
	}

	var pending models.ExternalEmail
	hasPending, err := tx.Where(
		"user_id = ? AND email_type = ? AND status = ?",
		request.UserID,
		request.Type,
		models.ExternalEmailStatusPending,
	).Get(&pending)
	if err != nil {
		return nil, fmt.Errorf("load pending replacement external email: %w", err)
	}
	if hasPending {
		pending.EmailCiphertext = ciphertext
		pending.EmailFingerprint = fingerprint
		pending.CredentialVersion++
		pending.UpdatedAt = now
		if _, err := tx.ID(pending.ID).Cols("email_ciphertext", "email_fingerprint", "credential_version", "updated_at").Update(&pending); err != nil {
			return nil, fmt.Errorf("update pending replacement external email: %w", err)
		}
		return &pending, nil
	}

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
	if _, err := tx.Insert(item); err != nil {
		return nil, fmt.Errorf("create replacement external email: %w", err)
	}
	return item, nil
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
	has, err := r.engine.Where("email_fingerprint = ?", fingerprint).Get(&existing)
	if err != nil {
		return nil, fmt.Errorf("check external email binding: %w", err)
	}
	if has && (existing.UserID != request.UserID || existing.Status != models.ExternalEmailStatusDisabled) {
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
	if has {
		// 停用记录可以重新绑定，但必须重新验证，不能沿用之前的验证状态。
		existing.EmailType = request.Type
		existing.Status = models.ExternalEmailStatusPending
		existing.VerifiedAt = nil
		existing.CredentialVersion++
		existing.UpdatedAt = now
		updated, err := r.engine.ID(existing.ID).Where("status = ?", models.ExternalEmailStatusDisabled).
			Cols("email_type", "status", "verified_at", "credential_version", "updated_at").Update(&existing)
		if err != nil {
			return nil, fmt.Errorf("reuse disabled external email binding: %w", err)
		}
		if updated != 1 {
			return nil, ErrBindingExists
		}
		return &existing, nil
	}
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

// FindPending 返回当前用户同一地址和用途尚未完成的绑定，供重新发送验证码复用。
func (r *Repository) FindPending(userID int, email, emailType string) (*models.ExternalEmail, error) {
	if r == nil || r.engine == nil || r.vault == nil || userID <= 0 {
		return nil, ErrInvalidBinding
	}
	if emailType != models.ExternalEmailTypePrimary && emailType != models.ExternalEmailTypeBackup {
		return nil, ErrInvalidBinding
	}
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return nil, ErrInvalidBinding
	}
	fingerprint, err := r.vault.Fingerprint(normalized)
	if err != nil {
		return nil, err
	}
	item := &models.ExternalEmail{}
	has, err := r.engine.Where(
		"user_id = ? AND email_fingerprint = ? AND email_type = ? AND status = ?",
		userID,
		fingerprint,
		emailType,
		models.ExternalEmailStatusPending,
	).Get(item)
	if err != nil {
		return nil, fmt.Errorf("find pending external email binding: %w", err)
	}
	if !has {
		return nil, ErrBindingNotFound
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

// VerifyReplacement 验证新邮箱后原子停用同用途旧邮箱，并启用新邮箱。
func (r *Repository) VerifyReplacement(userID, id int, guards ...accountgroup.WriteGuard) error {
	if r == nil || r.engine == nil || userID <= 0 || id <= 0 {
		return ErrInvalidBinding
	}
	session := r.engine.NewSession()
	if session == nil {
		return ErrInvalidBinding
	}
	defer session.Close()
	if err := session.Begin(); err != nil {
		return fmt.Errorf("begin external email replacement: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = session.Rollback()
		}
	}()

	if err := accountgroup.CheckWrite(session, guards...); err != nil {
		return err
	}
	item := &models.ExternalEmail{}
	has, err := session.Where("id = ? AND user_id = ? AND status = ?", id, userID, models.ExternalEmailStatusPending).Get(item)
	if err != nil {
		return fmt.Errorf("load replacement external email: %w", err)
	}
	if !has {
		return ErrBindingOwnership
	}
	now := r.now().UTC()
	if _, err := session.Where(
		"user_id = ? AND email_type = ? AND id != ? AND status != ?",
		userID,
		item.EmailType,
		id,
		models.ExternalEmailStatusDisabled,
	).Cols("status", "updated_at").Update(&models.ExternalEmail{
		Status:    models.ExternalEmailStatusDisabled,
		UpdatedAt: now,
	}); err != nil {
		return fmt.Errorf("disable old external email: %w", err)
	}
	item.Status = models.ExternalEmailStatusVerified
	item.VerifiedAt = timePtr(now)
	item.UpdatedAt = now
	updated, err := session.ID(id).Where("user_id = ? AND status = ?", userID, models.ExternalEmailStatusPending).
		Cols("status", "verified_at", "updated_at").Update(item)
	if err != nil {
		return fmt.Errorf("verify replacement external email: %w", err)
	}
	if updated != 1 {
		return ErrBindingOwnership
	}
	if err := session.Commit(); err != nil {
		return fmt.Errorf("commit external email replacement: %w", err)
	}
	committed = true
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
