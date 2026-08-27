package config

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/services/access"
)

var (
	ErrAuthorizationRequired = errors.New("configuration authorization required")
	ErrStepUpRequired        = errors.New("configuration step-up confirmation required")
	ErrAuditWrite            = errors.New("configuration audit write failed")
)

type ConfigOperation string

const (
	ConfigOperationUpdate   ConfigOperation = "update"
	ConfigOperationRollback ConfigOperation = "rollback"
)

type ConfigAuditEvent struct {
	Operation          ConfigOperation `json:"operation"`
	Section            Section         `json:"section,omitempty"`
	ActorID            string          `json:"actorId"`
	ActorRole          string          `json:"actorRole"`
	FromGeneration     uint64          `json:"fromGeneration"`
	ToGeneration       uint64          `json:"toGeneration"`
	TargetGeneration   uint64          `json:"targetGeneration,omitempty"`
	ChangedFields      []string        `json:"changedFields,omitempty"`
	ConfirmationMethod string          `json:"confirmationMethod,omitempty"`
	Result             string          `json:"result"`
	CreatedAt          time.Time       `json:"createdAt"`
}

type ConfigAuditWriter interface {
	WriteConfigAudit(context.Context, ConfigAuditEvent) error
}

type ConfigSecurity struct {
	Authorize func(context.Context, AuditActor, ConfigOperation, Section) error
	Confirm   func(context.Context, AuditActor, StepUpConfirmation, ConfigOperation, Section) error
	Audit     ConfigAuditWriter
}

type pendingConfig struct {
	Candidate    RuntimeConfig
	Section      Section
	Actor        AuditActor
	Confirmation StepUpConfirmation
}

type MemoryAuditWriter struct {
	mu     sync.Mutex
	events []ConfigAuditEvent
}

func (w *MemoryAuditWriter) WriteConfigAudit(_ context.Context, event ConfigAuditEvent) error {
	if w == nil {
		return ErrAuditWrite
	}
	event.ChangedFields = append([]string(nil), event.ChangedFields...)
	w.mu.Lock()
	w.events = append(w.events, event)
	w.mu.Unlock()
	return nil
}

func (w *MemoryAuditWriter) Events() []ConfigAuditEvent {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make([]ConfigAuditEvent, len(w.events))
	copy(result, w.events)
	for index := range result {
		result[index].ChangedFields = append([]string(nil), result[index].ChangedFields...)
	}
	return result
}

func DefaultConfigAuthorization(_ context.Context, actor AuditActor, operation ConfigOperation, section Section) error {
	if actor.UserID == "" || !access.Role(actor.Role).Valid() {
		return ErrAuthorizationRequired
	}
	capability := access.CapabilityConfigWriteGlobal
	if operation == ConfigOperationRollback {
		capability = access.CapabilityConfigRollback
	}
	request := access.Request{
		ActorRole:              access.Role(actor.Role),
		Capability:             capability,
		RecentAuth:             actor.RecentAuth,
		RecoveryCodeConfigured: actor.RecoveryCodeConfigured,
	}
	if operation == ConfigOperationRollback && request.ActorRole == access.RoleSuperAdmin {
		if !request.RecentAuth {
			return &access.Denial{Code: access.CodeRecentAuthRequired}
		}
		return nil
	}
	if err := access.Authorize(request); err != nil {
		return err
	}
	if operation == ConfigOperationUpdate && section == SectionRoot && request.ActorRole != access.RoleSuperAdmin {
		return ErrAuthorizationRequired
	}
	return nil
}

func DefaultConfigConfirmation(_ context.Context, _ AuditActor, confirmation StepUpConfirmation, operation ConfigOperation, _ Section) error {
	if operation != ConfigOperationRollback && !strings.EqualFold(confirmation.Method, "webauthn") && !strings.EqualFold(confirmation.Method, "recent_auth") {
		return nil
	}
	if !confirmation.Confirmed {
		return ErrStepUpRequired
	}
	method := strings.ToLower(strings.TrimSpace(confirmation.Method))
	if method != "recent_auth" && method != "webauthn" {
		return ErrStepUpRequired
	}
	return nil
}
