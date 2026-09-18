package verification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/models"
)

var (
	ErrRateLimited = errors.New("verification request rate limited")
	ErrServiceDown = errors.New("verification service unavailable")
)

const (
	issueInterval = time.Minute
	issueWindow   = time.Hour
	issueDaily    = 24 * time.Hour
	maxPerHour    = 5
	maxPerDay     = 10
	maxPerIPHour  = 20
	maxPerIPDay   = 20
)

type Service struct {
	Store      *Store
	Events     *EventStore
	Send       Delivery
	RateLimits *PersistentRateLimiter
	mu         sync.Mutex
	limits     map[string]issueCounter
	now        func() time.Time
}

type issueCounter struct {
	last  time.Time
	hour  time.Time
	hourN int
	day   time.Time
	dayN  int
}

func NewService(store *Store, send Delivery, events *EventStore) (*Service, error) {
	if store == nil || send == nil {
		return nil, ErrServiceDown
	}
	return &Service{Store: store, Send: send, Events: events, limits: make(map[string]issueCounter), now: time.Now}, nil
}

func (s *Service) SetRateLimitStore(store *PersistentRateLimiter) {
	if s == nil {
		return
	}
	s.RateLimits = store
}

func (s *Service) Issue(request IssueRequest, remoteIP string) (Challenge, error) {
	if s == nil || s.Store == nil || s.Send == nil || !IsSupportedPurpose(request.Purpose) {
		return Challenge{}, ErrServiceDown
	}
	request.Account = strings.TrimSpace(request.Account)
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	if request.Account == "" || request.Email == "" {
		return Challenge{}, ErrInvalidChallenge
	}
	allowed, release, limitErr := s.allow(request, remoteIP)
	if limitErr != nil {
		s.record(EventInput{Purpose: request.Purpose, Action: models.VerificationActionIssue, Outcome: models.VerificationOutcomeFailure, Account: request.Account, Email: request.Email, RemoteIP: remoteIP})
		return Challenge{}, ErrServiceDown
	}
	if !allowed {
		s.record(EventInput{Purpose: request.Purpose, Action: models.VerificationActionIssue, Outcome: models.VerificationOutcomeBlocked, Account: request.Account, Email: request.Email, RemoteIP: remoteIP})
		return Challenge{}, ErrRateLimited
	}
	challenge, err := s.Store.issue(request, s.Send, release)
	if err != nil {
		s.record(EventInput{Purpose: request.Purpose, Action: models.VerificationActionIssue, Outcome: models.VerificationOutcomeFailure, Account: request.Account, Email: request.Email, RemoteIP: remoteIP})
		return Challenge{}, err
	}
	s.record(EventInput{Purpose: request.Purpose, Action: models.VerificationActionIssue, Outcome: models.VerificationOutcomeSuccess, Account: request.Account, Email: request.Email, RemoteIP: remoteIP})
	return challenge, nil
}

func (s *Service) Verify(id, account, email, code string, purpose Purpose, remoteIP string) error {
	return s.VerifyBound(id, account, email, code, purpose, 0, remoteIP)
}

func (s *Service) VerifyWithContext(id, account, email, code string, purpose Purpose, remoteIP, browserContext string) error {
	return s.verifyBound(id, account, email, code, purpose, 0, remoteIP, browserContext)
}

// VerifyBound 验证绑定到具体业务资源的验证码，避免跨资源复用挑战。
func (s *Service) VerifyBound(id, account, email, code string, purpose Purpose, resourceID int, remoteIP string) error {
	return s.verifyBound(id, account, email, code, purpose, resourceID, remoteIP, "")
}

func (s *Service) VerifyBoundWithContext(id, account, email, code string, purpose Purpose, resourceID int, remoteIP, browserContext string) error {
	return s.verifyBound(id, account, email, code, purpose, resourceID, remoteIP, browserContext)
}

func (s *Service) verifyBound(id, account, email, code string, purpose Purpose, resourceID int, remoteIP, browserContext string) error {
	if s == nil || s.Store == nil || !IsSupportedPurpose(purpose) {
		return ErrServiceDown
	}
	err := s.Store.VerifyBoundWithContext(id, account, email, code, purpose, resourceID, browserContext)
	outcome := models.VerificationOutcomeSuccess
	if err != nil {
		outcome = models.VerificationOutcomeFailure
	}
	s.record(EventInput{Purpose: purpose, Action: models.VerificationActionVerify, Outcome: outcome, Account: account, Email: email, RemoteIP: remoteIP})
	return err
}

func (s *Service) record(input EventInput) {
	if s == nil || s.Events == nil {
		return
	}
	_ = s.Events.Record(input)
}

func (s *Service) allow(request IssueRequest, remoteIP string) (bool, func() error, error) {
	now := s.now()
	if s.RateLimits != nil {
		return s.RateLimits.allowWithRelease(context.Background(), RateLimitRequest{
			Account: request.Account, Email: request.Email, RemoteIP: remoteIP,
			UserID: request.UserID, Purpose: request.Purpose,
			BrowserTransaction: request.BrowserTransaction,
			Environment:        request.Environment, CredentialVersion: request.CredentialVersion,
		}, now)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := []string{"account:" + strings.ToLower(strings.TrimSpace(request.Account)), "email:" + strings.ToLower(strings.TrimSpace(request.Email))}
	if remoteIP = strings.TrimSpace(remoteIP); remoteIP != "" {
		keys = append(keys, "ip:"+remoteIP)
	}
	if len(s.limits) > 10000 {
		for key, value := range s.limits {
			if now.Sub(value.day) >= issueDaily {
				delete(s.limits, key)
			}
		}
	}
	for _, key := range keys {
		hourLimit, dayLimit := keyLimit(key)
		if !counterAllowed(s.limits[key], now, hourLimit, dayLimit) {
			return false, nil, nil
		}
	}
	reserved := make(map[string]issueCounter, len(keys))
	for _, key := range keys {
		item := s.limits[key]
		if item.hour.IsZero() || now.Sub(item.hour) >= issueWindow {
			item.hour = now
			item.hourN = 0
		}
		if item.day.IsZero() || now.Sub(item.day) >= issueDaily {
			item.day = now
			item.dayN = 0
		}
		item.last = now
		item.hourN++
		item.dayN++
		s.limits[key] = item
		reserved[key] = item
	}
	return true, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		// 只释放本次发送的冷却，保留累计配额及后续发送的状态。
		for key, expected := range reserved {
			if item := s.limits[key]; item == expected {
				item.last = time.Time{}
				s.limits[key] = item
			}
		}
		return nil
	}, nil
}

func keyLimit(key string) (int, int) {
	if strings.HasPrefix(key, "ip:") {
		return maxPerIPHour, maxPerIPDay
	}
	return maxPerHour, maxPerDay
}

func counterAllowed(item issueCounter, now time.Time, limits ...int) bool {
	hourLimit, dayLimit := maxPerHour, maxPerDay
	if len(limits) > 0 && limits[0] > 0 {
		hourLimit = limits[0]
	}
	if len(limits) > 1 && limits[1] > 0 {
		dayLimit = limits[1]
	}
	if !item.last.IsZero() && now.Sub(item.last) < issueInterval {
		return false
	}
	hourN := item.hourN
	dayN := item.dayN
	if item.hour.IsZero() || now.Sub(item.hour) >= issueWindow {
		hourN = 0
	}
	if item.day.IsZero() || now.Sub(item.day) >= issueDaily {
		dayN = 0
	}
	return hourN < hourLimit && dayN < dayLimit
}
func (s *Service) String() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("verification service with %d active rate limit keys", len(s.limits))
}
