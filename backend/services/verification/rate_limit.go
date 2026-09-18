package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"xorm.io/xorm"
)

const (
	RateLimitDimensionAccount     = "account"
	RateLimitDimensionEmail       = "email"
	RateLimitDimensionIP          = "ip"
	RateLimitDimensionUser        = "user"
	RateLimitDimensionPurpose     = "purpose"
	RateLimitDimensionTransaction = "transaction"
	RateLimitDimensionEnvironment = "environment"
	RateLimitDimensionCredential  = "credential"

	PersistentRateLimitStateRetention = 30 * 24 * time.Hour
)

var ErrRateLimitStoreUnavailable = errors.New("verification rate limit store unavailable")

type RateLimitRequest struct {
	Account            string
	Email              string
	RemoteIP           string
	UserID             int
	Purpose            Purpose
	BrowserTransaction string
	Environment        string
	CredentialVersion  int
}

type rateLimitDimension struct {
	typ       string
	value     string
	hourLimit int
	dayLimit  int
}

// PersistentRateLimiter 使用数据库事务保存限流计数，数据库中只保存哈希后的维度键。
type PersistentRateLimiter struct {
	engine *xorm.Engine
	mu     sync.Mutex
	now    func() time.Time
}

func NewPersistentRateLimiter(engine *xorm.Engine) (*PersistentRateLimiter, error) {
	if engine == nil {
		return nil, ErrRateLimitStoreUnavailable
	}
	return &PersistentRateLimiter{engine: engine, now: time.Now}, nil
}

func (r *PersistentRateLimiter) Allow(ctx context.Context, request RateLimitRequest, now time.Time) (bool, error) {
	allowed, _, err := r.allowWithRelease(ctx, request, now)
	return allowed, err
}

func (r *PersistentRateLimiter) allowWithRelease(ctx context.Context, request RateLimitRequest, now time.Time) (bool, func() error, error) {
	if r == nil || r.engine == nil {
		return false, nil, ErrRateLimitStoreUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = r.now()
	}
	dimensions := buildRateLimitDimensions(request)
	if len(dimensions) == 0 {
		return false, nil, ErrRateLimitStoreUnavailable
	}

	// SQLite 使用单连接和数据库锁，MySQL/PostgreSQL 通过行锁保证跨进程原子更新。
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.engine.NewSession().Context(ctx)
	defer session.Close()
	if err := session.Begin(); err != nil {
		return false, nil, fmt.Errorf("begin verification rate limit transaction: %w", err)
	}
	rollback := func(err error) (bool, func() error, error) {
		_ = session.Rollback()
		return false, nil, err
	}
	rows := make([]models.VerificationRateLimit, 0, len(dimensions))
	for _, dimension := range dimensions {
		key := hashRateLimitKey(dimension.typ, dimension.value)
		if err := insertRateLimitRow(session, key, dimension.typ, now); err != nil {
			return rollback(fmt.Errorf("initialize verification rate limit: %w", err))
		}
		var row models.VerificationRateLimit
		query := session.Where("dimension_key = ?", key)
		if r.engine.DriverName() != "sqlite3" && r.engine.DriverName() != "sqlite" {
			query = query.ForUpdate()
		}
		has, err := query.Get(&row)
		if err != nil {
			return rollback(fmt.Errorf("load verification rate limit: %w", err))
		}
		if !has {
			return rollback(ErrRateLimitStoreUnavailable)
		}
		if !rateLimitRowAllowed(row, now, dimension.hourLimit, dimension.dayLimit) {
			return rollback(nil)
		}
		rows = append(rows, row)
	}
	for i, dimension := range dimensions {
		row := rows[i]
		updated := now.UTC()
		if row.HourStartedAt == nil || now.Sub(*row.HourStartedAt) >= issueWindow {
			row.HourStartedAt = timePtr(now)
			row.HourCount = 0
		}
		if row.DayStartedAt == nil || now.Sub(*row.DayStartedAt) >= issueDaily {
			row.DayStartedAt = timePtr(now)
			row.DayCount = 0
		}
		row.HourCount++
		row.DayCount++
		row.LastIssuedAt = timePtr(now)
		row.ExpiresAt = updated.Add(PersistentRateLimitStateRetention)
		row.UpdatedAt = updated
		rows[i] = row
		if _, err := session.ID(row.ID).Cols("hour_started_at", "hour_count", "day_started_at", "day_count", "last_issued_at", "expires_at", "updated_at").Update(&row); err != nil {
			return rollback(fmt.Errorf("update verification rate limit %s: %w", dimension.typ, err))
		}
	}
	if err := session.Commit(); err != nil {
		return false, nil, fmt.Errorf("commit verification rate limit transaction: %w", err)
	}
	return true, func() error { return r.releaseCooldown(rows) }, nil
}

func (r *PersistentRateLimiter) releaseCooldown(rows []models.VerificationRateLimit) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	session := r.engine.NewSession().Context(ctx)
	defer session.Close()
	if err := session.Begin(); err != nil {
		return err
	}
	defer session.Rollback()
	for _, row := range rows {
		// 同一维度已有新发送时，不清理新请求的冷却，也不重置小时和每日计数。
		var current models.VerificationRateLimit
		query := session.ID(row.ID)
		if r.engine.DriverName() != "sqlite3" && r.engine.DriverName() != "sqlite" {
			query = query.ForUpdate()
		}
		has, err := query.Get(&current)
		if err != nil {
			return err
		}
		if !has || current.LastIssuedAt == nil || current.HourCount != row.HourCount || current.DayCount != row.DayCount || current.LastIssuedAt.Unix() != row.LastIssuedAt.Unix() {
			continue
		}
		_, err = session.Table(new(models.VerificationRateLimit)).ID(row.ID).
			Update(map[string]interface{}{"last_issued_at": nil})
		if err != nil {
			return err
		}
	}
	return session.Commit()
}

func (r *PersistentRateLimiter) Cleanup(ctx context.Context, now time.Time) (int, error) {
	if r == nil || r.engine == nil {
		return 0, ErrRateLimitStoreUnavailable
	}
	if now.IsZero() {
		now = r.now()
	}
	cutoff := now.UTC()
	result, err := r.engine.Context(ctx).Where("expires_at < ?", cutoff).Delete(&models.VerificationRateLimit{})
	if err != nil {
		return 0, fmt.Errorf("cleanup verification rate limits: %w", err)
	}
	return int(result), nil
}

func buildRateLimitDimensions(request RateLimitRequest) []rateLimitDimension {
	account := strings.ToLower(strings.TrimSpace(request.Account))
	email := strings.ToLower(strings.TrimSpace(request.Email))
	remoteIP := normalizeRateLimitIP(request.RemoteIP)
	purpose := strings.ToLower(strings.TrimSpace(string(request.Purpose)))
	environment := strings.TrimSpace(request.Environment)
	if environment == "" {
		environment = "default"
	}
	scope := func(value string) string {
		return environment + "\x00" + value
	}
	dimensions := make([]rateLimitDimension, 0, 8)
	appendDimension := func(typ, value string, hourLimit, dayLimit int) {
		if strings.TrimSpace(value) != "" {
			dimensions = append(dimensions, rateLimitDimension{typ: typ, value: value, hourLimit: hourLimit, dayLimit: dayLimit})
		}
	}
	appendDimension(RateLimitDimensionAccount, scope(account), maxPerHour, maxPerDay)
	appendDimension(RateLimitDimensionEmail, scope(email), maxPerHour, maxPerDay)
	appendDimension(RateLimitDimensionIP, scope(remoteIP), maxPerIPHour, maxPerIPDay)
	if request.UserID > 0 {
		appendDimension(RateLimitDimensionUser, scope(fmt.Sprintf("%d", request.UserID)), maxPerHour, maxPerDay)
	}
	if purpose != "" && account != "" {
		appendDimension(RateLimitDimensionPurpose, scope(purpose+"\x00"+account), maxPerHour, maxPerDay)
	}
	if transaction := strings.TrimSpace(request.BrowserTransaction); transaction != "" {
		appendDimension(RateLimitDimensionTransaction, scope(transaction), maxPerHour, maxPerDay)
	}
	if request.CredentialVersion > 0 && account != "" {
		appendDimension(RateLimitDimensionCredential, scope(fmt.Sprintf("%d\x00%s", request.CredentialVersion, account)), maxPerHour, maxPerDay)
	}
	return dimensions
}
func hashRateLimitKey(typ, value string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("smail:verification-rate-limit:v1:"))
	_, _ = hash.Write([]byte(typ))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(value))
	return hex.EncodeToString(hash.Sum(nil))
}

func rateLimitRowAllowed(row models.VerificationRateLimit, now time.Time, hourLimit, dayLimit int) bool {
	if row.LastIssuedAt != nil && now.Sub(*row.LastIssuedAt) < issueInterval {
		return false
	}
	hourCount := row.HourCount
	if row.HourStartedAt == nil || now.Sub(*row.HourStartedAt) >= issueWindow {
		hourCount = 0
	}
	dayCount := row.DayCount
	if row.DayStartedAt == nil || now.Sub(*row.DayStartedAt) >= issueDaily {
		dayCount = 0
	}
	return hourCount < hourLimit && dayCount < dayLimit
}

func insertRateLimitRow(session *xorm.Session, key, typ string, now time.Time) error {
	var query string
	switch session.Engine().DriverName() {
	case "mysql":
		query = "INSERT IGNORE INTO verification_rate_limit (dimension_key, dimension_type, expires_at, updated_at) VALUES (?, ?, ?, ?)"
	case "postgres":
		query = "INSERT INTO verification_rate_limit (dimension_key, dimension_type, expires_at, updated_at) VALUES ($1, $2, $3, $4) ON CONFLICT (dimension_key) DO NOTHING"
	default:
		query = "INSERT OR IGNORE INTO verification_rate_limit (dimension_key, dimension_type, expires_at, updated_at) VALUES (?, ?, ?, ?)"
	}
	updated := now.UTC()
	_, err := session.Exec(query, key, typ, updated.Add(PersistentRateLimitStateRetention), updated)
	return err
}

func normalizeRateLimitIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	} else {
		value = strings.Trim(value, "[]")
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Unmap().String()
	}
	return strings.ToLower(value)
}

func timePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}
