package config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/utils/credential"
)

type ServiceName string

const (
	ServiceDatabase ServiceName = "database"
	ServiceHTTP     ServiceName = "http"
	ServiceSMTP     ServiceName = "smtp"
	ServiceEmail    ServiceName = "email"
	ServiceSSL      ServiceName = "ssl"
	ServicePlugins  ServiceName = "plugins"
)

type PublicIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ConfigStatus struct {
	SetupState       SetupState    `json:"setupState"`
	ActiveGeneration uint64        `json:"activeGeneration"`
	LastValidAt      *time.Time    `json:"lastValidAt,omitempty"`
	PendingRestart   []ServiceName `json:"pendingRestart,omitempty"`
	DegradedReasons  []PublicIssue `json:"degradedReasons,omitempty"`
}

type ValidateRequest struct {
	Section            Section
	ExpectedGeneration uint64
	Payload            json.RawMessage
	SecretChanges      map[string]SecretChange
}

type UpdateRequest struct {
	Section            Section
	ExpectedGeneration uint64
	Payload            json.RawMessage
	SecretChanges      map[string]SecretChange
	Actor              AuditActor
	Confirmation       StepUpConfirmation
}

type AuditActor struct {
	UserID                 string
	Role                   string
	SessionID              string
	RecentAuth             bool
	RecoveryCodeConfigured bool
}

type StepUpConfirmation struct {
	Confirmed bool
	Method    string
}

type ValidationResult struct {
	Valid      bool          `json:"valid"`
	Generation uint64        `json:"generation"`
	Issues     []PublicIssue `json:"issues,omitempty"`
}

type UpdateResult struct {
	Generation uint64
	Candidate  RuntimeConfig
	Pending    bool
}

type ApplyResult struct {
	Snapshot   RuntimeConfig
	Generation uint64
	AppliedAt  time.Time
}

type RollbackPreview struct {
	CurrentGeneration   uint64    `json:"currentGeneration"`
	TargetGeneration    uint64    `json:"targetGeneration"`
	CandidateGeneration uint64    `json:"candidateGeneration"`
	ChangedSections     []Section `json:"changedSections"`
}

type rollbackTicket struct {
	ActorID          string
	SessionID        string
	TargetGeneration uint64
	ExpiresAt        time.Time
}

type LegacyPluginSecret struct {
	PluginID  string
	FieldName string
	Plaintext string
}

type LegacyPluginSecretImporter func(context.Context, []LegacyPluginSecret) error

var ErrPluginSecretMigrationRequired = errors.New("plugin secret migration is required")

type runtimeSnapshotStore interface {
	Validate(RuntimeConfig) error
	Publish(RuntimeConfig) error
	Snapshot() (RuntimeConfig, error)
}

type ConfigManager struct {
	mu              sync.RWMutex
	writeMu         sync.Mutex
	loader          *ConfigLoader
	snapshots       runtimeSnapshotStore
	current         RuntimeConfig
	pending         *pendingConfig
	status          ConfigStatus
	transactions    *TransactionStore
	vault           *credential.Vault
	options         RuntimeValidationOptions
	security        ConfigSecurity
	ticketMu        sync.Mutex
	rollbackTickets map[string]rollbackTicket
}

func NewConfigManager(root string, options ConfigLoaderOptions) (*ConfigManager, error) {
	audit, err := NewFileAuditWriter(root)
	if err != nil {
		return nil, err
	}
	return newConfigManager(root, options, ConfigSecurity{
		Authorize: DefaultConfigAuthorization,
		Confirm:   DefaultConfigConfirmation,
		Audit:     audit,
	})
}

func NewConfigManagerWithSecurity(root string, options ConfigLoaderOptions, security ConfigSecurity) (*ConfigManager, error) {
	if security.Authorize == nil {
		security.Authorize = DefaultConfigAuthorization
	}
	if security.Confirm == nil {
		security.Confirm = DefaultConfigConfirmation
	}
	if security.Audit == nil {
		audit, err := NewFileAuditWriter(root)
		if err != nil {
			return nil, err
		}
		security.Audit = audit
	}
	return newConfigManager(root, options, security)
}

func newConfigManager(root string, options ConfigLoaderOptions, security ConfigSecurity) (*ConfigManager, error) {
	transactions := NewTransactionStore(root, options.Validation)
	if err := transactions.Recover(); err != nil {
		return nil, err
	}
	vault, err := credential.NewVault(root)
	if err != nil {
		return nil, err
	}
	loader := NewConfigLoader(root, options)
	runtime, err := loader.Load()
	if err != nil {
		return nil, err
	}
	if err := ValidateRuntimeSecrets(vault, runtime); err != nil {
		return nil, err
	}
	snapshots, err := NewSnapshotStore(runtime, options.Validation)
	if err != nil {
		return nil, err
	}
	now := runtime.LoadedAt
	return &ConfigManager{
		loader:          loader,
		snapshots:       snapshots,
		transactions:    transactions,
		current:         runtime,
		vault:           vault,
		options:         options.Validation,
		security:        security,
		rollbackTickets: make(map[string]rollbackTicket),
		status:          ConfigStatus{SetupState: runtime.SetupState, ActiveGeneration: runtime.Generation, LastValidAt: &now},
	}, nil
}

func (m *ConfigManager) Load(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if m == nil || m.loader == nil {
		return ErrManagerStorage
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	runtime, err := m.loader.Load()
	if err != nil {
		return err
	}
	if err := ValidateRuntimeSecrets(m.vault, runtime); err != nil {
		return err
	}
	if err := m.snapshots.Publish(runtime); err != nil {
		return err
	}
	now := runtime.LoadedAt
	m.mu.Lock()
	m.current = runtime
	m.pending = nil
	m.status = ConfigStatus{SetupState: runtime.SetupState, ActiveGeneration: runtime.Generation, LastValidAt: &now}
	m.mu.Unlock()
	return nil
}

func (m *ConfigManager) Snapshot() RuntimeConfig {
	if m == nil || m.snapshots == nil {
		return RuntimeConfig{}
	}
	snapshot, err := m.snapshots.Snapshot()
	if err != nil {
		return RuntimeConfig{}
	}
	return snapshot
}

func (m *ConfigManager) Status() ConfigStatus {
	if m == nil {
		return ConfigStatus{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := m.status
	status.PendingRestart = append([]ServiceName(nil), status.PendingRestart...)
	status.DegradedReasons = append([]PublicIssue(nil), status.DegradedReasons...)
	if status.LastValidAt != nil {
		value := *status.LastValidAt
		status.LastValidAt = &value
	}
	return status
}

func (m *ConfigManager) Validate(ctx context.Context, req ValidateRequest) (*ValidationResult, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrManagerStorage
	}
	m.mu.RLock()
	pending := m.pending != nil
	currentGeneration := m.current.Generation
	if req.ExpectedGeneration != 0 && (req.ExpectedGeneration != currentGeneration || pending) {
		m.mu.RUnlock()
		return nil, ErrGenerationConflict
	}
	current := cloneRuntimeConfig(m.current)
	m.mu.RUnlock()
	candidate, err := applySectionPayload(current, req.Section, req.Payload, req.SecretChanges, m.vault)
	if err != nil {
		return nil, err
	}
	setCandidateGeneration(&candidate, current.Generation+1)
	if err := ValidateRuntimeConfig(candidate, m.options); err != nil {
		return &ValidationResult{Valid: false, Generation: current.Generation, Issues: []PublicIssue{{Code: "CONFIG_INVALID", Message: err.Error()}}}, nil
	}
	return &ValidationResult{Valid: true, Generation: candidate.Generation}, nil
}

func (m *ConfigManager) Update(ctx context.Context, req UpdateRequest) (*UpdateResult, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrManagerStorage
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	m.mu.RLock()
	current := cloneRuntimeConfig(m.current)
	pending := m.pending != nil
	m.mu.RUnlock()
	if req.ExpectedGeneration != current.Generation || pending {
		return nil, ErrGenerationConflict
	}
	if err := m.authorize(ctx, req.Actor, ConfigOperationUpdate, req.Section); err != nil {
		return nil, err
	}
	candidate, err := applySectionPayload(current, req.Section, req.Payload, req.SecretChanges, m.vault)
	if err != nil {
		return nil, err
	}
	setCandidateGeneration(&candidate, current.Generation+1)
	if err := ValidateRuntimeConfig(candidate, m.options); err != nil {
		return nil, err
	}
	if requiresSecretStepUp(req.SecretChanges) && !validStepUpConfirmation(req.Confirmation) {
		return nil, ErrStepUpRequired
	}
	if err := m.confirm(ctx, req.Actor, req.Confirmation, ConfigOperationUpdate, req.Section); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.pending = &pendingConfig{Candidate: candidate, Section: req.Section, Actor: req.Actor, Confirmation: req.Confirmation}
	m.status.PendingRestart = servicesForSection(req.Section)
	m.mu.Unlock()
	return &UpdateResult{Generation: candidate.Generation, Candidate: cloneRuntimeConfig(candidate), Pending: true}, nil
}

func (m *ConfigManager) Apply(ctx context.Context, generation uint64) (*ApplyResult, error) {
	return m.apply(ctx, generation, AuditActor{})
}

// ApplyAuthorized 仅允许发起候选配置的同一操作者应用配置。
func (m *ConfigManager) ApplyAuthorized(ctx context.Context, generation uint64, actor AuditActor) (*ApplyResult, error) {
	if actor.UserID == "" || actor.SessionID == "" {
		return nil, ErrAuthorizationRequired
	}
	return m.apply(ctx, generation, actor)
}

func (m *ConfigManager) apply(ctx context.Context, generation uint64, actor AuditActor) (*ApplyResult, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrManagerStorage
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	m.mu.RLock()
	current := cloneRuntimeConfig(m.current)
	var pending pendingConfig
	if m.pending != nil {
		pending = *m.pending
		pending.Candidate = cloneRuntimeConfig(pending.Candidate)
	}
	m.mu.RUnlock()
	candidate := pending.Candidate
	if actor.UserID != "" && (pending.Actor.UserID != actor.UserID || pending.Actor.SessionID != actor.SessionID) {
		return nil, ErrAuthorizationRequired
	}
	if generation != candidate.Generation || candidate.Generation != current.Generation+1 {
		return nil, ErrGenerationConflict
	}
	if err := ValidateRuntimeConfig(candidate, m.options); err != nil {
		return nil, err
	}
	if err := ValidateRuntimeSecrets(m.vault, current); err != nil {
		return nil, err
	}
	if err := ValidateRuntimeSecrets(m.vault, candidate); err != nil {
		return nil, err
	}
	if err := m.snapshots.Validate(candidate); err != nil {
		return nil, err
	}
	if _, _, err := m.transactions.CreateBackupIfDue(current, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := m.transactions.Commit(current, candidate); err != nil {
		return nil, err
	}
	if err := m.snapshots.Publish(candidate); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	m.mu.Lock()
	m.current = cloneRuntimeConfig(candidate)
	m.pending = nil
	pendingRestart := append([]ServiceName(nil), m.status.PendingRestart...)
	m.status = ConfigStatus{SetupState: candidate.SetupState, ActiveGeneration: candidate.Generation, LastValidAt: &now, PendingRestart: pendingRestart}
	m.mu.Unlock()
	if err := m.writeAudit(ctx, ConfigAuditEvent{
		Operation:          ConfigOperationUpdate,
		Section:            pending.Section,
		ActorID:            pending.Actor.UserID,
		ActorRole:          pending.Actor.Role,
		FromGeneration:     current.Generation,
		ToGeneration:       candidate.Generation,
		ChangedFields:      changedFields(current, candidate, pending.Section),
		ConfirmationMethod: pending.Confirmation.Method,
		Result:             "success",
		CreatedAt:          now,
	}); err != nil {
		return nil, err
	}
	return &ApplyResult{Snapshot: cloneRuntimeConfig(candidate), Generation: candidate.Generation, AppliedAt: now}, nil
}

func (m *ConfigManager) Rollback(ctx context.Context, generation uint64) (*ApplyResult, error) {
	return m.rollback(ctx, generation, AuditActor{}, StepUpConfirmation{})
}

func (m *ConfigManager) RollbackAuthorized(ctx context.Context, generation uint64, actor AuditActor, confirmation StepUpConfirmation) (*ApplyResult, error) {
	ticket, err := m.IssueRollbackTicket(ctx, generation, actor, confirmation)
	if err != nil {
		return nil, err
	}
	return m.RollbackWithTicket(ctx, ticket, actor)
}

func (m *ConfigManager) PreviewRollback(ctx context.Context, generation uint64, actor AuditActor) (*RollbackPreview, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if m == nil || generation == 0 {
		return nil, ErrManagerStorage
	}
	if err := m.authorize(ctx, actor, ConfigOperationRollback, SectionRoot); err != nil {
		return nil, err
	}
	m.mu.RLock()
	currentGeneration := m.current.Generation
	pending := m.pending != nil
	m.mu.RUnlock()
	if pending || generation >= currentGeneration {
		return nil, ErrGenerationConflict
	}
	backup, err := m.transactions.LoadBackup(generation)
	if err != nil {
		return nil, err
	}
	if err := ValidateRuntimeSecrets(m.vault, backup); err != nil {
		return nil, err
	}
	return &RollbackPreview{
		CurrentGeneration:   currentGeneration,
		TargetGeneration:    generation,
		CandidateGeneration: currentGeneration + 1,
		ChangedSections:     RequiredPartitionSections(),
	}, nil
}

func (m *ConfigManager) IssueRollbackTicket(ctx context.Context, generation uint64, actor AuditActor, confirmation StepUpConfirmation) (string, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}
	if m == nil || generation == 0 || actor.UserID == "" || actor.SessionID == "" {
		return "", ErrAuthorizationRequired
	}
	if err := m.authorize(ctx, actor, ConfigOperationRollback, SectionRoot); err != nil {
		return "", err
	}
	if err := m.confirm(ctx, actor, confirmation, ConfigOperationRollback, SectionRoot); err != nil {
		return "", err
	}
	m.mu.RLock()
	currentGeneration := m.current.Generation
	pending := m.pending != nil
	m.mu.RUnlock()
	if pending || generation >= currentGeneration {
		return "", ErrGenerationConflict
	}
	backup, err := m.transactions.LoadBackup(generation)
	if err != nil {
		return "", err
	}
	if err := ValidateRuntimeSecrets(m.vault, backup); err != nil {
		return "", err
	}
	token, err := newRollbackTicketID()
	if err != nil {
		return "", err
	}
	m.ticketMu.Lock()
	m.cleanupRollbackTicketsLocked(time.Now().UTC())
	m.rollbackTickets[token] = rollbackTicket{ActorID: actor.UserID, SessionID: actor.SessionID, TargetGeneration: generation, ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}
	m.ticketMu.Unlock()
	return token, nil
}

func (m *ConfigManager) RollbackWithTicket(ctx context.Context, token string, actor AuditActor) (*ApplyResult, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if m == nil || token == "" || actor.UserID == "" || actor.SessionID == "" {
		return nil, ErrAuthorizationRequired
	}
	m.ticketMu.Lock()
	now := time.Now().UTC()
	m.cleanupRollbackTicketsLocked(now)
	ticket, ok := m.rollbackTickets[token]
	if !ok || ticket.ActorID != actor.UserID || ticket.SessionID != actor.SessionID || !now.Before(ticket.ExpiresAt) {
		m.ticketMu.Unlock()
		return nil, ErrAuthorizationRequired
	}
	delete(m.rollbackTickets, token)
	m.ticketMu.Unlock()
	return m.rollback(ctx, ticket.TargetGeneration, actor, StepUpConfirmation{Confirmed: true, Method: "ticket"})
}

func (m *ConfigManager) rollback(ctx context.Context, generation uint64, actor AuditActor, confirmation StepUpConfirmation) (*ApplyResult, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrManagerStorage
	}
	if generation == 0 {
		return nil, ErrGenerationConflict
	}
	if err := m.authorize(ctx, actor, ConfigOperationRollback, SectionRoot); err != nil {
		return nil, err
	}
	if confirmation.Method != "ticket" {
		if err := m.confirm(ctx, actor, confirmation, ConfigOperationRollback, SectionRoot); err != nil {
			return nil, err
		}
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	m.mu.RLock()
	current := cloneRuntimeConfig(m.current)
	pending := m.pending != nil
	m.mu.RUnlock()
	if pending || generation >= current.Generation {
		return nil, ErrGenerationConflict
	}
	candidate, err := m.transactions.LoadBackup(generation)
	if err != nil {
		return nil, err
	}
	if err := ValidateRuntimeSecrets(m.vault, candidate); err != nil {
		return nil, err
	}
	setCandidateGeneration(&candidate, current.Generation+1)
	candidate.Root.UpdatedAt = time.Now().UTC()
	if err := ValidateRuntimeConfig(candidate, m.options); err != nil {
		return nil, err
	}
	if err := m.snapshots.Validate(candidate); err != nil {
		return nil, err
	}
	if _, _, err := m.transactions.CreateBackupIfDue(current, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := m.transactions.Commit(current, candidate); err != nil {
		return nil, err
	}
	if err := m.snapshots.Publish(candidate); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	m.mu.Lock()
	m.current = cloneRuntimeConfig(candidate)
	m.pending = nil
	m.status = ConfigStatus{
		SetupState:       candidate.SetupState,
		ActiveGeneration: candidate.Generation,
		LastValidAt:      &now,
		PendingRestart:   []ServiceName{ServiceDatabase, ServiceHTTP, ServiceSMTP, ServiceEmail, ServiceSSL, ServicePlugins},
	}
	m.mu.Unlock()
	if err := m.writeAudit(ctx, ConfigAuditEvent{
		Operation:          ConfigOperationRollback,
		Section:            SectionRoot,
		ActorID:            actor.UserID,
		ActorRole:          actor.Role,
		FromGeneration:     current.Generation,
		ToGeneration:       candidate.Generation,
		TargetGeneration:   generation,
		ConfirmationMethod: confirmation.Method,
		Result:             "success",
		CreatedAt:          now,
	}); err != nil {
		return nil, err
	}
	return &ApplyResult{Snapshot: cloneRuntimeConfig(candidate), Generation: candidate.Generation, AppliedAt: now}, nil
}

func (m *ConfigManager) authorize(ctx context.Context, actor AuditActor, operation ConfigOperation, section Section) error {
	if m == nil || m.security.Authorize == nil {
		return ErrAuthorizationRequired
	}
	return m.security.Authorize(ctx, actor, operation, section)
}

func (m *ConfigManager) confirm(ctx context.Context, actor AuditActor, confirmation StepUpConfirmation, operation ConfigOperation, section Section) error {
	if m == nil || m.security.Confirm == nil {
		return ErrStepUpRequired
	}
	return m.security.Confirm(ctx, actor, confirmation, operation, section)
}

func (m *ConfigManager) writeAudit(ctx context.Context, event ConfigAuditEvent) error {
	if m == nil || m.security.Audit == nil {
		m.mu.Lock()
		m.status.SetupState = SetupStateDegraded
		m.status.DegradedReasons = []PublicIssue{{Code: "AUDIT_UNAVAILABLE", Message: "配置审计服务不可用"}}
		m.mu.Unlock()
		return ErrAuditWrite
	}
	if err := m.security.Audit.WriteConfigAudit(ctx, event); err != nil {
		m.mu.Lock()
		m.status.SetupState = SetupStateDegraded
		m.status.DegradedReasons = []PublicIssue{{Code: "AUDIT_WRITE_FAILED", Message: "配置审计写入失败"}}
		m.mu.Unlock()
		return errors.Join(ErrAuditWrite, err)
	}
	return nil
}

func changedFields(current, candidate RuntimeConfig, section Section) []string {
	if current.Generation == candidate.Generation && section == "" {
		return nil
	}
	return []string{string(section)}
}

func servicesForSection(section Section) []ServiceName {
	switch section {
	case SectionRoot:
		return []ServiceName{ServiceDatabase, ServiceHTTP}
	case SectionSMTP:
		return []ServiceName{ServiceSMTP}
	case SectionEmail:
		return []ServiceName{ServiceEmail}
	case SectionSSL:
		return []ServiceName{ServiceHTTP, ServiceSMTP, ServiceSSL}
	case SectionPlugins:
		return []ServiceName{ServicePlugins}
	default:
		return nil
	}
}

func newRollbackTicketID() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (m *ConfigManager) cleanupRollbackTicketsLocked(now time.Time) {
	for token, ticket := range m.rollbackTickets {
		if !now.Before(ticket.ExpiresAt) {
			delete(m.rollbackTickets, token)
		}
	}
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func applySectionPayload(current RuntimeConfig, section Section, payload json.RawMessage, changes map[string]SecretChange, vault *credential.Vault) (RuntimeConfig, error) {
	if len(payload) == 0 {
		return RuntimeConfig{}, errors.New("configuration payload is empty")
	}
	candidate := cloneRuntimeConfig(current)
	switch section {
	case SectionRoot:
		var value struct {
			RootConfig
			DatabasePasswordStatus json.RawMessage `json:"databasePasswordStatus"`
		}
		if err := ParseStrict(payload, &value); err != nil {
			return RuntimeConfig{}, err
		}
		if value.RootConfig.Database.Password.Configured() {
			return RuntimeConfig{}, ErrInvalidSecretChange
		}
		value.RootConfig.Database.Password = current.Root.Database.Password
		candidate.Root = normalizeRootConfig(value.RootConfig)
	case SectionSMTP:
		var value struct {
			SMTPSectionConfig
			RelayPasswordStatus json.RawMessage `json:"relayPasswordStatus"`
		}
		if err := ParseStrict(payload, &value); err != nil {
			return RuntimeConfig{}, err
		}
		if value.SMTPSectionConfig.Relay.Password.Configured() {
			return RuntimeConfig{}, ErrInvalidSecretChange
		}
		value.SMTPSectionConfig.Relay.Password = current.SMTP.Relay.Password
		candidate.SMTP = value.SMTPSectionConfig
	case SectionEmail:
		var value EmailSectionConfig
		if err := ParseStrict(payload, &value); err != nil {
			return RuntimeConfig{}, err
		}
		candidate.Email = value
	case SectionSSL:
		var value SSLSectionConfig
		if err := ParseStrict(payload, &value); err != nil {
			return RuntimeConfig{}, err
		}
		candidate.SSL = value
	case SectionPlugins:
		var value PluginsSectionConfig
		if err := ParseStrict(payload, &value); err != nil {
			return RuntimeConfig{}, err
		}
		for _, plugin := range value.Plugins {
			if storedSecretHasMaterial(plugin.Secret) {
				return RuntimeConfig{}, ErrInvalidSecretChange
			}
		}
		for _, plugin := range current.Plugins.Plugins {
			if storedSecretHasMaterial(plugin.Secret) {
				return RuntimeConfig{}, ErrPluginSecretMigrationRequired
			}
		}
		candidate.Plugins = value
	default:
		return RuntimeConfig{}, ErrInvalidSection
	}
	if err := applySecretChanges(&candidate, section, changes, vault); err != nil {
		return RuntimeConfig{}, err
	}
	candidate.SetupState = candidate.Root.SetupState
	return candidate, nil
}

func setCandidateGeneration(candidate *RuntimeConfig, generation uint64) {
	if candidate == nil {
		return
	}
	candidate.Generation = generation
	candidate.Root.Generation = generation
	candidate.Root.Sections = sectionReferencesForRuntime(*candidate)
	candidate.SMTP.Meta.Generation = generation
	candidate.Email.Meta.Generation = generation
	candidate.SSL.Meta.Generation = generation
	candidate.Plugins.Meta.Generation = generation
}

func sectionReferencesForRuntime(runtime RuntimeConfig) map[Section]SectionRef {
	result := make(map[Section]SectionRef, len(requiredPartitionSections))
	for _, section := range requiredPartitionSections {
		file, _ := FixedSectionFile(section)
		schema, _ := FixedSectionSchema(section)
		var data []byte
		switch section {
		case SectionSMTP:
			data, _ = json.Marshal(runtime.SMTP)
		case SectionEmail:
			data, _ = json.Marshal(runtime.Email)
		case SectionSSL:
			data, _ = json.Marshal(runtime.SSL)
		case SectionPlugins:
			data, _ = json.Marshal(runtime.Plugins)
		}
		result[section] = SectionRef{File: file, Schema: schema, Generation: runtime.Generation, SHA256: SectionDigest(data)}
	}
	return result
}

func applySecretChanges(candidate *RuntimeConfig, section Section, changes map[string]SecretChange, vault *credential.Vault) error {
	if len(changes) == 0 {
		return nil
	}
	if candidate == nil || vault == nil {
		return ErrSecretUnavailable
	}
	now := time.Now().UTC()
	for name, change := range changes {
		switch {
		case section == SectionRoot && name == "database.password":
			stored, err := ApplySecretChange(vault, candidate.Root.Database.Password, change, SecretPolicy{Required: candidate.Root.Database.Type != DatabaseTypeSQLite, AllowClear: false, MaxLength: 4096}, now)
			if err != nil {
				return err
			}
			candidate.Root.Database.Password = stored
		case section == SectionSMTP && name == "relay.password":
			stored, err := ApplySecretChange(vault, candidate.SMTP.Relay.Password, change, SecretPolicy{Required: candidate.SMTP.Relay.Username != "", AllowClear: candidate.SMTP.Relay.Username == "", MaxLength: 4096}, now)
			if err != nil {
				return err
			}
			candidate.SMTP.Relay.Password = stored
		default:
			return ErrInvalidSecretChange
		}
	}
	return nil
}

func storedSecretHasMaterial(secret StoredSecret) bool {
	return secret.Ciphertext != "" || secret.Version != 0 || !secret.UpdatedAt.IsZero()
}

// MigrateLegacyPluginSecrets moves compatibility-only plugin secrets out of plugins.json.
// The importer must persist all received values atomically before this method commits the new configuration generation.
func (m *ConfigManager) MigrateLegacyPluginSecrets(ctx context.Context, importer LegacyPluginSecretImporter) (bool, error) {
	if err := contextError(ctx); err != nil {
		return false, err
	}
	if m == nil || importer == nil || m.vault == nil {
		return false, ErrPluginSecretMigrationRequired
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	m.mu.RLock()
	current := cloneRuntimeConfig(m.current)
	pending := m.pending != nil
	m.mu.RUnlock()
	if pending {
		return false, ErrGenerationConflict
	}

	values := make([]LegacyPluginSecret, 0, len(current.Plugins.Plugins))
	for logicalID, instance := range current.Plugins.Plugins {
		if !storedSecretHasMaterial(instance.Secret) {
			continue
		}
		fieldName, ok := legacyPluginSecretField(logicalID)
		if !ok || !instance.Secret.Configured() {
			return false, ErrPluginSecretMigrationRequired
		}
		plaintext, err := m.vault.Decrypt(instance.Secret.Version, instance.Secret.Ciphertext)
		if err != nil || plaintext == "" {
			return false, ErrPluginSecretMigrationRequired
		}
		values = append(values, LegacyPluginSecret{PluginID: logicalID, FieldName: fieldName, Plaintext: plaintext})
	}
	if len(values) == 0 {
		return false, nil
	}
	if err := importer(ctx, values); err != nil {
		return false, err
	}

	candidate := cloneRuntimeConfig(current)
	for logicalID, instance := range candidate.Plugins.Plugins {
		instance.Secret = StoredSecret{}
		candidate.Plugins.Plugins[logicalID] = instance
	}
	setCandidateGeneration(&candidate, current.Generation+1)
	if err := ValidateRuntimeConfig(candidate, m.options); err != nil {
		return false, err
	}
	if err := m.snapshots.Validate(candidate); err != nil {
		return false, err
	}
	if _, _, err := m.transactions.CreateBackupIfDue(current, time.Now().UTC()); err != nil {
		return false, err
	}
	if err := m.transactions.Commit(current, candidate); err != nil {
		return false, err
	}
	if err := m.snapshots.Publish(candidate); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	m.mu.Lock()
	m.current = cloneRuntimeConfig(candidate)
	m.pending = nil
	m.status = ConfigStatus{SetupState: candidate.SetupState, ActiveGeneration: candidate.Generation, LastValidAt: &now, PendingRestart: []ServiceName{ServicePlugins}}
	m.mu.Unlock()
	if err := m.writeAudit(ctx, ConfigAuditEvent{
		Operation:      ConfigOperationMigration,
		Section:        SectionPlugins,
		ActorID:        "system",
		ActorRole:      "system",
		FromGeneration: current.Generation,
		ToGeneration:   candidate.Generation,
		ChangedFields:  []string{"plugins.legacySecret"},
		Result:         "success",
		CreatedAt:      now,
	}); err != nil {
		return false, err
	}
	return true, nil
}

func legacyPluginSecretField(logicalID string) (string, bool) {
	switch logicalID {
	case "wechat_push":
		return "appSecret", true
	case "telegram_push":
		return "botToken", true
	default:
		return "", false
	}
}

func requiresSecretStepUp(changes map[string]SecretChange) bool {
	for _, change := range changes {
		if change.Operation == SecretClear || change.Operation == SecretReplace {
			return true
		}
	}
	return false
}

func validStepUpConfirmation(confirmation StepUpConfirmation) bool {
	if !confirmation.Confirmed {
		return false
	}
	method := strings.ToLower(strings.TrimSpace(confirmation.Method))
	return method == "recent_auth" || method == "webauthn"
}
