package dbmigration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/config"
)

type Status string

const (
	StatusPreviewed Status = "previewed"
	StatusCreated   Status = "created"
	StatusCopying   Status = "copying"
	StatusVerifying Status = "verifying"
	StatusSwitching Status = "switching"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

var (
	ErrInvalidRequest      = errors.New("database migration request is invalid")
	ErrInvalidTargetName   = errors.New("database migration target name is invalid")
	ErrImmutableSetting    = errors.New("database migration may change only the database name")
	ErrMigrationNotFound   = errors.New("database migration not found")
	ErrPreviewInvalid      = errors.New("database migration preview is invalid")
	ErrPreviewExpired      = errors.New("database migration preview expired")
	ErrMigrationState      = errors.New("database migration state is invalid")
	ErrExecutorUnavailable = errors.New("database migration executor unavailable")
	ErrSourceDatabase      = errors.New("source database configuration is invalid")
	ErrTargetExists        = errors.New("database migration target already exists")
	ErrSwitchUnavailable   = errors.New("database migration configuration switch unavailable")
	ErrUnsupportedDatabase = errors.New("database migration database type is unsupported")
)

const previewLifetime = 10 * time.Minute

var targetNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,62}$`)
var sqliteTargetNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type DatabaseIdentity struct {
	Type     config.DatabaseType `json:"type"`
	Host     string              `json:"host,omitempty"`
	Port     uint16              `json:"port,omitempty"`
	Username string              `json:"username,omitempty"`
	Name     string              `json:"name,omitempty"`
	LocalRef string              `json:"localRef,omitempty"`
}

type Plan struct {
	Source             DatabaseIdentity
	Target             DatabaseIdentity
	sourceConfig       config.DatabaseConfig
	targetConfig       config.DatabaseConfig
	expectedGeneration uint64
	actor              config.AuditActor
}

type Record struct {
	ID                string           `json:"id"`
	Status            Status           `json:"status"`
	Source            DatabaseIdentity `json:"source"`
	Target            DatabaseIdentity `json:"target"`
	CurrentGeneration uint64           `json:"currentGeneration"`
	CreatedAt         time.Time        `json:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt"`
	ErrorCode         string           `json:"errorCode,omitempty"`
}

type Runner interface {
	Run(context.Context, Plan, func(Status) error) error
}

type Service struct {
	requests requestGate
	mu       sync.RWMutex
	records  map[string]*record
	runner   Runner
	now      func() time.Time
}

type record struct {
	Record
	tokenHash    [32]byte
	expiresAt    time.Time
	sourceConfig config.DatabaseConfig
	targetConfig config.DatabaseConfig
}

var current struct {
	sync.RWMutex
	value *Service
}

func NewService(runner Runner) *Service {
	service := &Service{records: make(map[string]*record), runner: runner, now: func() time.Time { return time.Now().UTC() }}
	service.requests.initialize()
	return service
}

func SetCurrent(service *Service) {
	current.Lock()
	current.value = service
	current.Unlock()
}

func Current() *Service {
	current.RLock()
	defer current.RUnlock()
	return current.value
}

func (s *Service) Preview(ctx context.Context, source config.DatabaseConfig, currentGeneration uint64, targetName string) (Record, string, error) {
	plan, err := buildPlan(source, strings.TrimSpace(targetName))
	if err != nil {
		return Record{}, "", err
	}
	return s.PreviewTarget(ctx, source, currentGeneration, plan.targetConfig)
}

// PreviewTarget 固定完整目标连接；返回记录不包含密码或密文。
func (s *Service) PreviewTarget(ctx context.Context, source config.DatabaseConfig, currentGeneration uint64, target config.DatabaseConfig) (Record, string, error) {
	if err := contextError(ctx); err != nil {
		return Record{}, "", err
	}
	if s == nil || currentGeneration == 0 {
		return Record{}, "", ErrInvalidRequest
	}
	if err := config.ValidateDatabaseConfig(source); err != nil {
		return Record{}, "", ErrSourceDatabase
	}
	plan, err := buildTargetPlan(source, target)
	if err != nil {
		return Record{}, "", err
	}
	if checker, ok := s.runner.(interface {
		Preview(context.Context, Plan) error
	}); ok {
		if err := checker.Preview(ctx, plan); err != nil {
			return Record{}, "", err
		}
	}
	id, err := randomID("dbmig-")
	if err != nil {
		return Record{}, "", ErrInvalidRequest
	}
	token, err := randomID("preview-")
	if err != nil {
		return Record{}, "", ErrInvalidRequest
	}
	if err := contextError(ctx); err != nil {
		return Record{}, "", err
	}
	now := s.currentTime()
	item := &record{Record: Record{ID: id, Status: StatusPreviewed, Source: plan.Source, Target: plan.Target, CurrentGeneration: currentGeneration, CreatedAt: now, UpdatedAt: now}, tokenHash: sha256.Sum256([]byte(token)), expiresAt: now.Add(previewLifetime), sourceConfig: plan.sourceConfig, targetConfig: plan.targetConfig}
	s.mu.Lock()
	s.records[id] = item
	s.mu.Unlock()
	return cloneRecord(item.Record), token, nil
}

func (s *Service) Execute(ctx context.Context, id, token string) (Record, error) {
	return s.execute(ctx, id, token, config.AuditActor{})
}

// ExecuteAuthorized executes a migration while preserving the authenticated
// operator for the final configuration switch.
func (s *Service) ExecuteAuthorized(ctx context.Context, id, token string, actor config.AuditActor) (Record, error) {
	if actor.UserID == "" || actor.SessionID == "" {
		return Record{}, config.ErrAuthorizationRequired
	}
	return s.execute(ctx, id, token, actor)
}

func (s *Service) execute(ctx context.Context, id, token string, actor config.AuditActor) (Record, error) {
	if s == nil || strings.TrimSpace(id) == "" || strings.TrimSpace(token) == "" {
		return Record{}, ErrInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := s.currentTime()
	s.mu.Lock()
	item, ok := s.records[id]
	if !ok {
		s.mu.Unlock()
		return Record{}, ErrMigrationNotFound
	}
	if item.Status != StatusPreviewed {
		snapshot := cloneRecord(item.Record)
		s.mu.Unlock()
		return snapshot, ErrMigrationState
	}
	if now.After(item.expiresAt) {
		item.Status = StatusFailed
		item.ErrorCode = "PREVIEW_EXPIRED"
		item.UpdatedAt = now
		item.sourceConfig = config.DatabaseConfig{}
		item.targetConfig = config.DatabaseConfig{}
		snapshot := cloneRecord(item.Record)
		s.mu.Unlock()
		return snapshot, ErrPreviewExpired
	}
	hash := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(hash[:], item.tokenHash[:]) != 1 {
		s.mu.Unlock()
		return Record{}, ErrPreviewInvalid
	}
	item.tokenHash = [32]byte{}
	item.Status = StatusCreated
	item.UpdatedAt = now
	plan := Plan{Source: item.Source, Target: item.Target, sourceConfig: item.sourceConfig, targetConfig: item.targetConfig, expectedGeneration: item.CurrentGeneration, actor: actor}
	item.sourceConfig = config.DatabaseConfig{}
	item.targetConfig = config.DatabaseConfig{}
	s.mu.Unlock()

	releaseRequests := s.drainRequests(ctx)
	defer releaseRequests()
	if err := contextError(ctx); err != nil {
		return s.fail(id, err)
	}
	if err := s.setStatus(id, StatusCopying); err != nil {
		return s.fail(id, err)
	}
	if s.runner == nil {
		return s.fail(id, ErrExecutorUnavailable)
	}
	if err := s.runner.Run(ctx, plan, func(status Status) error { return s.setStatus(id, status) }); err != nil {
		return s.fail(id, err)
	}
	if err := s.setStatus(id, StatusSucceeded); err != nil {
		return s.fail(id, err)
	}
	return s.Get(id)
}

func (s *Service) Get(id string) (Record, error) {
	if s == nil || strings.TrimSpace(id) == "" {
		return Record{}, ErrInvalidRequest
	}
	s.mu.RLock()
	item, ok := s.records[id]
	if !ok {
		s.mu.RUnlock()
		return Record{}, ErrMigrationNotFound
	}
	value := cloneRecord(item.Record)
	s.mu.RUnlock()
	return value, nil
}

func (s *Service) setStatus(id string, status Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.records[id]
	if !ok {
		return ErrMigrationNotFound
	}
	if !validStatusTransition(item.Status, status) {
		return ErrMigrationState
	}
	item.Status = status
	item.UpdatedAt = s.currentTime()
	return nil
}

func validStatusTransition(current, next Status) bool {
	if current == StatusFailed || current == StatusSucceeded || next == StatusPreviewed || next == StatusFailed {
		return false
	}
	return statusRank(next) >= statusRank(current)
}

func statusRank(status Status) int {
	switch status {
	case StatusCreated:
		return 1
	case StatusCopying:
		return 2
	case StatusVerifying:
		return 3
	case StatusSwitching:
		return 4
	case StatusSucceeded:
		return 5
	default:
		return 0
	}
}

func (s *Service) fail(id string, err error) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.records[id]
	if !ok {
		return Record{}, ErrMigrationNotFound
	}
	item.Status = StatusFailed
	item.ErrorCode = migrationErrorCode(err)
	item.UpdatedAt = s.currentTime()
	return cloneRecord(item.Record), err
}

func (s *Service) currentTime() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

func buildPlan(source config.DatabaseConfig, targetName string) (Plan, error) {
	source = config.NormalizeDatabaseConfig(source)
	identity := DatabaseIdentity{Type: source.Type, Host: source.Host, Port: source.Port, Username: source.Username, Name: source.Name, LocalRef: source.LocalRef}
	target := identity
	if source.Type == config.DatabaseTypeSQLite {
		target.LocalRef = targetName
	} else {
		target.Name = targetName
	}
	if target.Type != source.Type || target.Host != source.Host || target.Port != source.Port || target.Username != source.Username {
		return Plan{}, ErrImmutableSetting
	}
	if target.Name == source.Name && target.LocalRef == source.LocalRef {
		return Plan{}, ErrInvalidTargetName
	}
	return Plan{Source: identity, Target: target, sourceConfig: source, targetConfig: func() config.DatabaseConfig {
		candidate := source
		if source.Type == config.DatabaseTypeSQLite {
			candidate.LocalRef = targetName
		} else {
			candidate.Name = targetName
		}
		return candidate
	}()}, nil
}

func randomID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value), nil
}

func cloneRecord(value Record) Record {
	return value
}

func migrationErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrExecutorUnavailable):
		return "EXECUTOR_UNAVAILABLE"
	case errors.Is(err, ErrPreviewExpired):
		return "PREVIEW_EXPIRED"
	case errors.Is(err, ErrTargetExists):
		return "TARGET_EXISTS"
	case errors.Is(err, ErrSQLTargetExists):
		return "TARGET_EXISTS"
	case errors.Is(err, ErrSQLCopyUnavailable):
		return "COPIER_UNAVAILABLE"
	case errors.Is(err, ErrSQLConnectionUnavailable):
		return "CONNECTION_UNAVAILABLE"
	case errors.Is(err, ErrSQLUnsupportedObject):
		return "UNSUPPORTED_OBJECT"
	case errors.Is(err, ErrSQLDataVerification):
		return "DATA_VERIFICATION_FAILED"
	case errors.Is(err, ErrSQLSourceBusy):
		return "SOURCE_BUSY"
	case errors.Is(err, ErrSwitchUnavailable):
		return "SWITCH_UNAVAILABLE"
	case errors.Is(err, ErrSwitchOutcomeUnknown):
		return "SWITCH_OUTCOME_UNKNOWN"
	case errors.Is(err, ErrUnsupportedDatabase):
		return "UNSUPPORTED_DATABASE"
	default:
		return "MIGRATION_FAILED"
	}
}
