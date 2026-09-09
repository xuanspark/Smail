package config

import (
	"context"
	"time"
)

type PendingView struct {
	ID          string        `json:"candidateId"`
	Generation  uint64        `json:"generation"`
	Section     Section       `json:"section"`
	SameSession bool          `json:"sameSession"`
	Committed   bool          `json:"committed"`
	Candidate   RuntimeConfig `json:"-"`
}

// Pending 返回独立快照，不向调用者泄露候选操作者的会话标识。
func (m *ConfigManager) Pending(actor AuditActor) *PendingView {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.pending == nil {
		return nil
	}
	if actor.Role != "super_admin" && (actor.Role != "admin" || m.pending.Section != SectionPlugins || actor.UserID == "" || actor.UserID != m.pending.Actor.UserID) {
		return nil
	}
	return &PendingView{
		ID:         m.pending.ID,
		Generation: m.pending.Candidate.Generation, Section: m.pending.Section,
		SameSession: actor.UserID != "" && actor.UserID == m.pending.Actor.UserID && actor.SessionID != "" && actor.SessionID == m.pending.Actor.SessionID,
		Committed:   m.pending.Committed, Candidate: cloneRuntimeConfig(m.pending.Candidate),
	}
}

// ManagePending 显式恢复或取消指定代次；跨会话必须重新验证近期身份并确认操作。
func (m *ConfigManager) ManagePending(ctx context.Context, generation uint64, id string, actor AuditActor, confirmation StepUpConfirmation, cancel bool) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if m == nil || actor.UserID == "" || actor.SessionID == "" {
		return ErrAuthorizationRequired
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	m.mu.RLock()
	pending := m.pending
	current := m.current.Generation
	m.mu.RUnlock()
	if pending == nil || id == "" || pending.ID != id || pending.Candidate.Generation != generation {
		return ErrGenerationConflict
	}
	if err := m.authorize(ctx, actor, ConfigOperationUpdate, pending.Section); err != nil {
		return err
	}
	sameSession := actor.UserID == pending.Actor.UserID && actor.SessionID == pending.Actor.SessionID
	systemRecovery := pending.Committed && pending.Actor.UserID == "system" && actor.Role == "super_admin"
	if actor.UserID != pending.Actor.UserID && !systemRecovery && (!cancel || actor.Role != "super_admin") {
		return ErrAuthorizationRequired
	}
	if !sameSession && (!actor.RecentAuth || !validStepUpConfirmation(confirmation)) {
		return ErrStepUpRequired
	}
	if err := m.confirm(ctx, actor, confirmation, ConfigOperationUpdate, pending.Section); err != nil {
		return err
	}
	if cancel && pending.Committed {
		return ErrRuntimeSnapshotUnavailable
	}
	field := "pending.resume"
	if cancel {
		field = "pending.cancel"
	}
	if err := m.writeAudit(ctx, ConfigAuditEvent{
		Operation: ConfigOperationUpdate, Section: pending.Section, ActorID: actor.UserID, ActorRole: actor.Role,
		FromGeneration: current, ToGeneration: current, ChangedFields: []string{field},
		ConfirmationMethod: confirmation.Method, Result: "success", CreatedAt: time.Now().UTC(),
	}); err != nil {
		return err
	}
	m.mu.Lock()
	activeSSL := m.current.SSL.TLS
	if cancel {
		m.pending = nil
		m.status.PendingRestart = append([]ServiceName(nil), pending.PreviousRestart...)
	} else {
		m.pending.Actor = actor
		m.pending.Confirmation = confirmation
	}
	m.mu.Unlock()
	if cancel {
		releaseManagedSSL(m.loader.root, pending.Candidate.SSL.TLS)
		maintainManagedSSL(m.loader.root, activeSSL)
	}
	return nil
}
