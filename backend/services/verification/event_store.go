package verification

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"xorm.io/xorm"
)

const (
	OperationalRetention = 14 * 24 * time.Hour
	DetailRetention      = 30 * 24 * time.Hour
	AggregateRetention   = 90 * 24 * time.Hour
	AuditRetention       = 365 * 24 * time.Hour
	CleanupBatchSize     = 1000
)

const (
	RetentionClassDetail    = "detail"
	RetentionClassAggregate = "aggregate"
	RetentionClassAudit     = "audit"
)

var (
	ErrEventStoreUnavailable = errors.New("verification event store unavailable")
	ErrInvalidEvent          = errors.New("verification event is invalid")
)

const cleanupStateKey = "verification_event"
const cleanupLeaseDuration = 30 * time.Minute
const cleanupRetryInterval = 6 * time.Hour
const cleanupScheduleInterval = 14 * 24 * time.Hour

type cleanupLease struct {
	Token string
}
type EventInput struct {
	UserID            int
	Purpose           Purpose
	Action            string
	Outcome           string
	Account           string
	Email             string
	RemoteIP          string
	RetentionClass    string
	InvestigationHold bool
}

type EventStore struct {
	engine *xorm.Engine
	key    [32]byte
	now    func() time.Time
}

func NewEventStore(engine *xorm.Engine) (*EventStore, error) {
	if engine == nil {
		return nil, ErrEventStoreUnavailable
	}
	store := &EventStore{engine: engine, now: time.Now}
	if _, err := rand.Read(store.key[:]); err != nil {
		return nil, fmt.Errorf("generate verification event key: %w", err)
	}
	return store, nil
}

func (s *EventStore) Record(input EventInput) error {
	if s == nil || s.engine == nil || !IsSupportedPurpose(input.Purpose) || !isEventValue(input.Action) || !isEventValue(input.Outcome) {
		return ErrInvalidEvent
	}
	if input.UserID < 0 {
		return ErrInvalidEvent
	}
	now := s.now().UTC()
	retentionClass := strings.ToLower(strings.TrimSpace(input.RetentionClass))
	if retentionClass == "" {
		retentionClass = RetentionClassDetail
	}
	retention, ok := retentionDuration(retentionClass)
	if !ok {
		return ErrInvalidEvent
	}
	event := models.VerificationEvent{
		UserID:             input.UserID,
		Purpose:            string(input.Purpose),
		Action:             input.Action,
		Outcome:            input.Outcome,
		AccountFingerprint: s.fingerprint(input.Account),
		EmailFingerprint:   s.fingerprint(strings.ToLower(strings.TrimSpace(input.Email))),
		IPFingerprint:      s.fingerprint(strings.TrimSpace(input.RemoteIP)),
		ExpiresAt:          now.Add(retention),
		RetentionClass:     retentionClass,
		InvestigationHold:  input.InvestigationHold,
		CreatedAt:          now,
	}
	if event.AccountFingerprint == "" {
		event.AccountFingerprint = ""
	}
	_, err := s.engine.Insert(&event)
	if err != nil {
		return fmt.Errorf("record verification event: %w", err)
	}
	return nil
}

func (s *EventStore) Cleanup(ctx context.Context, now time.Time) (int, error) {
	if s == nil || s.engine == nil {
		return 0, ErrEventStoreUnavailable
	}
	if now.IsZero() {
		now = s.now()
	}
	cutoff := now.UTC()
	deleted := 0
	for {
		select {
		case <-ctx.Done():
			return deleted, ctx.Err()
		default:
		}
		var events []models.VerificationEvent
		if err := s.engine.Where("expires_at < ? AND investigation_hold = ?", cutoff, false).
			OrderBy("id asc").Limit(CleanupBatchSize).Find(&events); err != nil {
			return deleted, fmt.Errorf("load expired verification events: %w", err)
		}
		if len(events) == 0 {
			return deleted, nil
		}
		ids := make([]int, 0, len(events))
		for _, event := range events {
			ids = append(ids, event.ID)
		}
		count, err := s.engine.In("id", ids).Delete(&models.VerificationEvent{})
		if err != nil {
			return deleted, fmt.Errorf("delete expired verification events: %w", err)
		}
		deleted += int(count)
		if len(events) < CleanupBatchSize {
			return deleted, nil
		}
	}
}

func retentionDuration(class string) (time.Duration, bool) {
	switch class {
	case RetentionClassDetail:
		return DetailRetention, true
	case RetentionClassAggregate:
		return AggregateRetention, true
	case RetentionClassAudit:
		return AuditRetention, true
	default:
		return 0, false
	}
}

func (s *EventStore) fingerprint(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(value))
	return fmt.Sprintf("v1:%x", mac.Sum(nil))
}

func isEventValue(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 32 && !strings.ContainsAny(value, "\r\n")
}

// TryAcquireCleanupLease 使用数据库事务竞争清理租约，确保同一环境只有一个清理实例。
func (s *EventStore) TryAcquireCleanupLease(ctx context.Context, now time.Time) (cleanupLease, bool, error) {
	if s == nil || s.engine == nil {
		return cleanupLease{}, false, ErrEventStoreUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = s.now()
	}
	session := s.engine.NewSession().Context(ctx)
	if err := session.Begin(); err != nil {
		return cleanupLease{}, false, fmt.Errorf("begin cleanup lease transaction: %w", err)
	}
	rollback := func(err error) (cleanupLease, bool, error) {
		_ = session.Rollback()
		return cleanupLease{}, false, err
	}
	if err := insertCleanupState(session, now); err != nil {
		return rollback(fmt.Errorf("initialize cleanup state: %w", err))
	}
	query := session.Where("state_key = ?", cleanupStateKey)
	if s.engine.DriverName() != "sqlite3" && s.engine.DriverName() != "sqlite" {
		query = query.ForUpdate()
	}
	var state models.VerificationCleanupState
	has, err := query.Get(&state)
	if err != nil {
		return rollback(fmt.Errorf("load cleanup state: %w", err))
	}
	if !has {
		return rollback(ErrEventStoreUnavailable)
	}
	if state.LeaseUntil.After(now) || state.NextRunAt.After(now) {
		if err := session.Rollback(); err != nil {
			return cleanupLease{}, false, err
		}
		return cleanupLease{}, false, nil
	}
	token, err := randomID()
	if err != nil {
		return rollback(fmt.Errorf("generate cleanup lease token: %w", err))
	}
	state.LeaseToken = token
	state.LeaseUntil = now.Add(cleanupLeaseDuration).UTC()
	state.UpdatedAt = now.UTC()
	if _, err := session.Where("state_key = ?", cleanupStateKey).Cols("lease_until", "lease_token", "updated_at").Update(&state); err != nil {
		return rollback(fmt.Errorf("claim cleanup lease: %w", err))
	}
	if err := session.Commit(); err != nil {
		return cleanupLease{}, false, fmt.Errorf("commit cleanup lease: %w", err)
	}
	return cleanupLease{Token: token}, true, nil
}

// FinishCleanup 标记本次清理成功，并安排下一次 14 天周期任务。
func (s *EventStore) FinishCleanup(ctx context.Context, lease cleanupLease, now time.Time) error {
	return s.updateCleanupState(ctx, lease, now, false)
}

// FailCleanup 标记本次清理失败，并安排 6 小时后的重试。
func (s *EventStore) FailCleanup(ctx context.Context, lease cleanupLease, now time.Time) error {
	return s.updateCleanupState(ctx, lease, now, true)
}

func (s *EventStore) updateCleanupState(ctx context.Context, lease cleanupLease, now time.Time, failed bool) error {
	if s == nil || s.engine == nil {
		return ErrEventStoreUnavailable
	}
	if strings.TrimSpace(lease.Token) == "" {
		return ErrInvalidEvent
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = s.now()
	}
	state := models.VerificationCleanupState{StateKey: cleanupStateKey}
	has, err := s.engine.Context(ctx).Where("state_key = ? AND lease_token = ?", cleanupStateKey, lease.Token).Get(&state)
	if err != nil {
		return fmt.Errorf("load cleanup lease: %w", err)
	}
	if !has {
		return ErrInvalidEvent
	}
	state.LeaseUntil = now.UTC()
	state.LeaseToken = ""
	state.UpdatedAt = now.UTC()
	if failed {
		state.FailureCount++
		state.NextRunAt = now.Add(cleanupRetryInterval).UTC()
	} else {
		state.FailureCount = 0
		state.NextRunAt = now.Add(cleanupScheduleInterval).UTC()
	}
	if _, err := s.engine.Context(ctx).Where("state_key = ? AND lease_token = ?", cleanupStateKey, lease.Token).Cols("next_run_at", "lease_until", "lease_token", "failure_count", "updated_at").Update(&state); err != nil {
		return fmt.Errorf("update cleanup state: %w", err)
	}
	return nil
}

func insertCleanupState(session *xorm.Session, now time.Time) error {
	if session == nil || session.Engine() == nil {
		return ErrEventStoreUnavailable
	}
	epoch := time.Unix(0, 0).UTC()
	query := cleanupStateInsertQuery(session.Engine().DriverName())
	_, err := session.Exec(query, cleanupStateKey, now.UTC(), epoch, "", 0, now.UTC())
	return err
}

func cleanupStateInsertQuery(driver string) string {
	const columns = "(state_key, next_run_at, lease_until, lease_token, failure_count, updated_at)"
	switch driver {
	case "mysql":
		return "INSERT IGNORE INTO verification_cleanup_state " + columns + " VALUES (?, ?, ?, ?, ?, ?)"
	case "postgres":
		return "INSERT INTO verification_cleanup_state " + columns + " VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (state_key) DO NOTHING"
	default:
		return "INSERT OR IGNORE INTO verification_cleanup_state " + columns + " VALUES (?, ?, ?, ?, ?, ?)"
	}
}
