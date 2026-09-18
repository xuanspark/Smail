package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/credential"
	"xorm.io/xorm"
)

const (
	Digits               = 6
	PeriodSeconds        = 30
	EnrollmentTTL        = 10 * time.Minute
	maxEnrollmentEntries = 10000
	secretSize           = 32
)

var (
	ErrUnavailable       = errors.New("totp service unavailable")
	ErrInvalidCode       = errors.New("invalid totp code")
	ErrReplay            = errors.New("totp code replayed")
	ErrNotEnabled        = errors.New("totp is not enabled")
	ErrAlreadyEnabled    = errors.New("totp is already enabled")
	ErrEnrollmentExpired = errors.New("totp enrollment expired")
)

type Enrollment struct {
	ID        string    `json:"id"`
	Secret    string    `json:"secret"`
	URI       string    `json:"uri"`
	ExpiresAt time.Time `json:"expires_at"`
}

type enrollmentItem struct {
	userID    int
	secret    []byte
	expiresAt time.Time
	failures  int
}

type Service struct {
	engine *xorm.Engine
	root   string
	now    func() time.Time
	random io.Reader

	mu          sync.Mutex
	enrollments map[string]enrollmentItem
}

func NewService(engine *xorm.Engine, vaultRoot string) (*Service, error) {
	if engine == nil || strings.TrimSpace(vaultRoot) == "" {
		return nil, ErrUnavailable
	}
	if _, err := credential.NewVault(vaultRoot); err != nil {
		return nil, ErrUnavailable
	}
	return &Service{
		engine:      engine,
		root:        vaultRoot,
		now:         time.Now,
		random:      rand.Reader,
		enrollments: make(map[string]enrollmentItem),
	}, nil
}

func (s *Service) IsEnabled(userID int) (bool, error) {
	if s == nil || s.engine == nil || userID <= 0 {
		return false, ErrUnavailable
	}
	var record models.TOTPAuthenticator
	has, err := s.engine.Where("user_id = ? AND enabled = ? AND revoked_at IS NULL", userID, true).Get(&record)
	if err != nil {
		return false, fmt.Errorf("load totp authenticator: %w", err)
	}
	return has, nil
}

func (s *Service) BeginEnrollment(userID int, account string) (*Enrollment, error) {
	if s == nil || s.engine == nil || userID <= 0 || strings.TrimSpace(account) == "" {
		return nil, ErrUnavailable
	}
	enabled, err := s.IsEnabled(userID)
	if err != nil {
		return nil, err
	}
	if enabled {
		return nil, ErrAlreadyEnabled
	}
	secret := make([]byte, secretSize)
	if _, err := io.ReadFull(s.random, secret); err != nil {
		return nil, ErrUnavailable
	}
	idBytes := make([]byte, 32)
	if _, err := io.ReadFull(s.random, idBytes); err != nil {
		clear(secret)
		return nil, ErrUnavailable
	}
	id := base64.RawURLEncoding.EncodeToString(idBytes)
	expiresAt := s.now().UTC().Add(EnrollmentTTL)
	encodedSecret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)

	s.mu.Lock()
	s.cleanupEnrollmentsLocked(s.now().UTC())
	if len(s.enrollments) >= maxEnrollmentEntries {
		s.mu.Unlock()
		clear(secret)
		return nil, ErrUnavailable
	}
	for existingID, item := range s.enrollments {
		if item.userID == userID {
			clear(item.secret)
			delete(s.enrollments, existingID)
		}
	}
	s.enrollments[id] = enrollmentItem{userID: userID, secret: secret, expiresAt: expiresAt}
	s.mu.Unlock()

	return &Enrollment{
		ID:        id,
		Secret:    encodedSecret,
		URI:       provisioningURI(account, encodedSecret),
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) ConfirmEnrollment(userID int, enrollmentID string, code string) error {
	if s == nil || userID <= 0 || strings.TrimSpace(enrollmentID) == "" {
		return ErrEnrollmentExpired
	}
	now := s.now().UTC()
	s.mu.Lock()
	s.cleanupEnrollmentsLocked(now)
	item, ok := s.enrollments[enrollmentID]
	if !ok || item.userID != userID {
		s.mu.Unlock()
		return ErrEnrollmentExpired
	}
	step, valid := validateAt(item.secret, code, now, 1)
	if !valid {
		item.failures++
		if item.failures >= 5 {
			clear(item.secret)
			delete(s.enrollments, enrollmentID)
		} else {
			s.enrollments[enrollmentID] = item
		}
		s.mu.Unlock()
		return ErrInvalidCode
	}
	secret := append([]byte(nil), item.secret...)
	clear(item.secret)
	delete(s.enrollments, enrollmentID)
	s.mu.Unlock()
	defer clear(secret)

	secretText := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	ciphertext, err := credential.Encrypt(s.root, secretText)
	if err != nil {
		return ErrUnavailable
	}
	confirmedAt := now
	record := models.TOTPAuthenticator{
		UserID:            userID,
		SecretCiphertext:  ciphertext,
		CredentialVersion: int(credential.CurrentVersion),
		Enabled:           true,
		ConfirmedAt:       &confirmedAt,
		LastUsedStep:      step,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	var existing models.TOTPAuthenticator
	has, err := s.engine.Where("user_id = ?", userID).Get(&existing)
	if err != nil {
		return fmt.Errorf("load totp authenticator: %w", err)
	}
	if has && existing.Enabled && existing.RevokedAt == nil {
		return ErrAlreadyEnabled
	}
	if has {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		_, err = s.engine.ID(existing.ID).Cols("secret_ciphertext", "credential_version", "enabled", "confirmed_at", "last_used_step", "updated_at", "revoked_at").Update(&record)
	} else {
		_, err = s.engine.Insert(&record)
	}
	if err != nil {
		return fmt.Errorf("save totp authenticator: %w", err)
	}
	return nil
}

func (s *Service) Verify(userID int, code string) error {
	if s == nil || s.engine == nil || userID <= 0 {
		return ErrUnavailable
	}
	var record models.TOTPAuthenticator
	has, err := s.engine.Where("user_id = ? AND enabled = ? AND revoked_at IS NULL", userID, true).Get(&record)
	if err != nil {
		return fmt.Errorf("load totp authenticator: %w", err)
	}
	if !has {
		return ErrNotEnabled
	}
	secretText, err := credential.Decrypt(s.root, record.SecretCiphertext)
	if err != nil {
		return ErrUnavailable
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secretText))
	if err != nil || len(secret) == 0 {
		return ErrUnavailable
	}
	defer clear(secret)

	step, valid := validateAt(secret, code, s.now().UTC(), 1)
	if !valid {
		return ErrInvalidCode
	}
	if step <= record.LastUsedStep {
		return ErrReplay
	}
	updated, err := s.engine.Table(&models.TOTPAuthenticator{}).
		Where("id = ? AND enabled = ? AND revoked_at IS NULL AND last_used_step < ?", record.ID, true, step).
		Cols("last_used_step", "updated_at").
		Update(&models.TOTPAuthenticator{LastUsedStep: step, UpdatedAt: s.now().UTC()})
	if err != nil {
		return fmt.Errorf("update totp authenticator: %w", err)
	}
	if updated != 1 {
		return ErrReplay
	}
	return nil
}

func (s *Service) Disable(userID int, code string) error {
	if err := s.Verify(userID, code); err != nil {
		return err
	}
	now := s.now().UTC()
	updated, err := s.engine.Table(&models.TOTPAuthenticator{}).
		Where("user_id = ? AND enabled = ? AND revoked_at IS NULL", userID, true).
		Cols("enabled", "revoked_at", "updated_at").
		Update(&models.TOTPAuthenticator{Enabled: false, RevokedAt: &now, UpdatedAt: now})
	if err != nil {
		return fmt.Errorf("disable totp authenticator: %w", err)
	}
	if updated != 1 {
		return ErrNotEnabled
	}
	return nil
}

func (s *Service) InvalidateUser(userID int) {
	if s == nil || userID <= 0 {
		return
	}
	s.mu.Lock()
	for id, item := range s.enrollments {
		if item.userID == userID {
			clear(item.secret)
			delete(s.enrollments, id)
		}
	}
	s.mu.Unlock()
}

func (s *Service) cleanupEnrollmentsLocked(now time.Time) {
	for id, item := range s.enrollments {
		if !now.Before(item.expiresAt) {
			clear(item.secret)
			delete(s.enrollments, id)
		}
	}
}

func provisioningURI(account string, secret string) string {
	label := "Smail:" + strings.TrimSpace(account)
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", "Smail")
	query.Set("algorithm", "SHA256")
	query.Set("digits", strconv.Itoa(Digits))
	query.Set("period", strconv.Itoa(PeriodSeconds))
	return "otpauth://totp/" + url.PathEscape(label) + "?" + query.Encode()
}

func validateAt(secret []byte, code string, now time.Time, window int64) (int64, bool) {
	if !validCodeFormat(code) || len(secret) == 0 {
		return 0, false
	}
	current := now.Unix() / PeriodSeconds
	for offset := -window; offset <= window; offset++ {
		step := current + offset
		if step < 0 {
			continue
		}
		expected := generateCode(secret, uint64(step))
		if hmac.Equal([]byte(expected), []byte(code)) {
			return step, true
		}
	}
	return 0, false
}

func validCodeFormat(code string) bool {
	if len(code) != Digits {
		return false
	}
	for index := range code {
		if code[index] < '0' || code[index] > '9' {
			return false
		}
	}
	return true
}

func generateCode(secret []byte, step uint64) string {
	counter := make([]byte, 8)
	binary.BigEndian.PutUint64(counter, step)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(counter)
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	binaryCode := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", Digits, binaryCode%1000000)
}
