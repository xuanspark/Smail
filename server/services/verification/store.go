package verification

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidChallenge = errors.New("verification challenge is invalid")
	ErrExpiredChallenge = errors.New("verification challenge is expired")
	ErrTooManyAttempts  = errors.New("verification challenge attempts exceeded")
	ErrCapacityExceeded = errors.New("verification challenge capacity exceeded")
)

const (
	DefaultCodeLength  = 8
	DefaultTTL         = 10 * time.Minute
	DefaultMaxEntries  = 10000
	DefaultMaxPerUser  = 4
	DefaultMaxAttempts = 5
)

type Purpose string

const (
	PurposeLogin               Purpose = "login"
	PurposePasswordRecover     Purpose = "password_recover"
	PurposeExternalEmailBind   Purpose = "external_email_bind"
	PurposeExternalEmailChange Purpose = "external_email_change"
)

type Challenge struct {
	ID          string
	Purpose     Purpose
	Account     string
	Email       string
	ResourceID  int
	IssuedAt    time.Time
	ExpiresAt   time.Time
	Attempts    int
	MaxAttempts int
}

type IssueRequest struct {
	Purpose            Purpose
	Account            string
	Email              string
	UserID             int
	BrowserTransaction string
	Environment        string
	CredentialVersion  int
	ResourceID         int
	TTL                time.Duration
	MaxAttempts        int
}

type Delivery func(challenge Challenge, code string) error

type entry struct {
	challenge               Challenge
	mac                     [32]byte
	browserContextDigest    [32]byte
	hasBrowserContextDigest bool
}

type Store struct {
	mu          sync.Mutex
	key         [32]byte
	entries     map[string]entry
	active      map[string]string
	perUser     map[string]int
	maxEntries  int
	maxPerUser  int
	codeLength  int
	maxAttempts int
	now         func() time.Time
}

func NewStore(maxEntries int) (*Store, error) {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	store := &Store{
		entries:     make(map[string]entry),
		active:      make(map[string]string),
		perUser:     make(map[string]int),
		maxEntries:  maxEntries,
		maxPerUser:  DefaultMaxPerUser,
		codeLength:  DefaultCodeLength,
		maxAttempts: DefaultMaxAttempts,
		now:         time.Now,
	}
	if _, err := rand.Read(store.key[:]); err != nil {
		return nil, fmt.Errorf("generate verification process key: %w", err)
	}
	return store, nil
}

func (s *Store) Issue(request IssueRequest, delivery Delivery) (Challenge, error) {
	if s == nil || delivery == nil || !IsSupportedPurpose(request.Purpose) || strings.TrimSpace(request.Account) == "" || strings.TrimSpace(request.Email) == "" {
		return Challenge{}, ErrInvalidChallenge
	}
	if request.TTL <= 0 {
		request.TTL = DefaultTTL
	}
	if request.MaxAttempts <= 0 || request.MaxAttempts > s.maxAttempts {
		request.MaxAttempts = s.maxAttempts
	}
	now := s.now()
	code, err := randomCode(s.codeLength)
	if err != nil {
		return Challenge{}, err
	}
	defer clearString(&code)
	id, err := randomID()
	if err != nil {
		return Challenge{}, err
	}
	account := strings.TrimSpace(request.Account)
	email := strings.TrimSpace(strings.ToLower(request.Email))
	browserContext := strings.TrimSpace(request.BrowserTransaction)
	var browserContextDigest [32]byte
	if browserContext != "" {
		browserContextDigest = sha256.Sum256([]byte(browserContext))
	}
	challenge := Challenge{
		ID:          id,
		Purpose:     request.Purpose,
		Account:     account,
		Email:       email,
		ResourceID:  request.ResourceID,
		IssuedAt:    now,
		ExpiresAt:   now.Add(request.TTL),
		MaxAttempts: request.MaxAttempts,
	}
	s.mu.Lock()
	s.cleanupLocked(now)
	activeKey := challengeKey(account, request.Purpose)
	if previousID := s.active[activeKey]; previousID != "" {
		s.deleteLocked(previousID)
	}
	if len(s.entries) >= s.maxEntries || s.perUser[account] >= s.maxPerUser {
		s.mu.Unlock()
		return Challenge{}, ErrCapacityExceeded
	}
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(challenge.ID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(challenge.Purpose))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(challenge.Account))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(challenge.Email))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(fmt.Sprintf("%d", challenge.ResourceID)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(browserContextDigest[:])
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(code))
	var digest [32]byte
	copy(digest[:], mac.Sum(nil))
	s.entries[challenge.ID] = entry{challenge: challenge, mac: digest, browserContextDigest: browserContextDigest, hasBrowserContextDigest: browserContext != ""}
	s.active[activeKey] = challenge.ID
	s.perUser[account]++
	s.mu.Unlock()

	if err := delivery(challenge, code); err != nil {
		s.mu.Lock()
		s.deleteLocked(challenge.ID)
		s.mu.Unlock()
		return Challenge{}, err
	}
	return challenge, nil
}

// IsSupportedPurpose 保证验证码只能用于已确认的业务流程。
func IsSupportedPurpose(purpose Purpose) bool {
	switch purpose {
	case PurposeLogin, PurposePasswordRecover, PurposeExternalEmailBind, PurposeExternalEmailChange:
		return true
	default:
		return false
	}
}

func (s *Store) Verify(id, account, email, code string, purpose Purpose) error {
	return s.VerifyBound(id, account, email, code, purpose, 0)
}

// VerifyBound 验证并原子消费绑定到具体业务资源的验证码。
func (s *Store) VerifyBound(id, account, email, code string, purpose Purpose, resourceID int) error {
	return s.VerifyBoundWithContext(id, account, email, code, purpose, resourceID, "")
}

func (s *Store) VerifyBoundWithContext(id, account, email, code string, purpose Purpose, resourceID int, browserContext string) error {
	if s == nil || strings.TrimSpace(id) == "" || strings.TrimSpace(account) == "" || strings.TrimSpace(email) == "" || strings.TrimSpace(code) == "" || purpose == "" {
		return ErrInvalidChallenge
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	item, ok := s.entries[id]
	if !ok || item.challenge.Purpose != purpose || item.challenge.Account != strings.TrimSpace(account) || item.challenge.Email != strings.TrimSpace(strings.ToLower(email)) || item.challenge.ResourceID != resourceID {
		return ErrInvalidChallenge
	}
	if item.hasBrowserContextDigest {
		providedDigest := sha256.Sum256([]byte(strings.TrimSpace(browserContext)))
		if strings.TrimSpace(browserContext) == "" || subtle.ConstantTimeCompare(providedDigest[:], item.browserContextDigest[:]) != 1 {
			return ErrInvalidChallenge
		}
	}
	if !now.Before(item.challenge.ExpiresAt) {
		s.deleteLocked(id)
		return ErrExpiredChallenge
	}
	item.challenge.Attempts++
	if item.challenge.Attempts > item.challenge.MaxAttempts {
		s.deleteLocked(id)
		return ErrTooManyAttempts
	}
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(item.challenge.ID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(item.challenge.Purpose))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(item.challenge.Account))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(item.challenge.Email))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(fmt.Sprintf("%d", item.challenge.ResourceID)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(item.browserContextDigest[:])
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(code))
	actual := mac.Sum(nil)
	if subtle.ConstantTimeCompare(actual, item.mac[:]) != 1 {
		if item.challenge.Attempts >= item.challenge.MaxAttempts {
			s.deleteLocked(id)
			return ErrTooManyAttempts
		}
		s.entries[id] = item
		return ErrInvalidChallenge
	}
	s.deleteLocked(id)
	return nil
}
func (s *Store) Cleanup() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before := len(s.entries)
	s.cleanupLocked(s.now())
	return before - len(s.entries)
}

func (s *Store) ResetForProcessRestart() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = rand.Read(s.key[:])
	s.entries = make(map[string]entry)
	s.active = make(map[string]string)
	s.perUser = make(map[string]int)
}

// InvalidateAccount 删除指定账号尚未完成的全部验证码挑战。
func (s *Store) InvalidateAccount(account string) int {
	if s == nil {
		return 0
	}
	account = strings.TrimSpace(account)
	if account == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, item := range s.entries {
		if item.challenge.Account == account {
			s.deleteLocked(id)
			removed++
		}
	}
	return removed
}

// InvalidateResource 删除绑定到指定业务资源的全部未完成挑战。
func (s *Store) InvalidateResource(resourceID int, purpose Purpose) int {
	if s == nil || resourceID <= 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, item := range s.entries {
		if item.challenge.ResourceID == resourceID && (purpose == "" || item.challenge.Purpose == purpose) {
			s.deleteLocked(id)
			removed++
		}
	}
	return removed
}

// InvalidateChallenge 删除指定的未完成挑战。
func (s *Store) InvalidateChallenge(id string) bool {
	if s == nil || strings.TrimSpace(id) == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return false
	}
	s.deleteLocked(id)
	return true
}

func (s *Store) Size() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

func (s *Store) cleanupLocked(now time.Time) {
	for id, item := range s.entries {
		if !now.Before(item.challenge.ExpiresAt) {
			s.deleteLocked(id)
		}
	}
}

func randomCode(length int) (string, error) {
	if length <= 0 || length > 12 {
		return "", ErrInvalidChallenge
	}
	const digits = "0123456789"
	code := make([]byte, length)
	random := make([]byte, length*2)
	for written := 0; written < length; {
		if _, err := io.ReadFull(rand.Reader, random); err != nil {
			clearBytes(random)
			clearBytes(code)
			return "", err
		}
		for _, value := range random {
			if value >= 250 {
				continue
			}
			code[written] = digits[int(value)%len(digits)]
			written++
			if written == length {
				break
			}
		}
	}
	clearBytes(random)
	result := string(code)
	clearBytes(code)
	return result, nil
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func challengeKey(account string, purpose Purpose) string {
	return account + "\x00" + string(purpose)
}

func (s *Store) deleteLocked(id string) {
	item, ok := s.entries[id]
	if !ok {
		return
	}
	delete(s.entries, id)
	key := challengeKey(item.challenge.Account, item.challenge.Purpose)
	if s.active[key] == id {
		delete(s.active, key)
	}
	if count := s.perUser[item.challenge.Account]; count <= 1 {
		delete(s.perUser, item.challenge.Account)
	} else {
		s.perUser[item.challenge.Account] = count - 1
	}
	for index := range item.mac {
		item.mac[index] = 0
	}
}

func clearString(value *string) {
	if value == nil {
		return
	}
	buffer := []byte(*value)
	clearBytes(buffer)
	*value = ""
}
