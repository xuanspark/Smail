package verification

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func TestEventStorePersistsOnlyOperationalFingerprintsAndCleansAfterRetention(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.Sync2(&models.VerificationEvent{}); err != nil {
		t.Fatal(err)
	}
	store, err := NewEventStore(engine)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-DetailRetention - time.Hour)
	store.now = func() time.Time { return old }
	if err := store.Record(EventInput{
		UserID:   7,
		Purpose:  PurposePasswordRecover,
		Action:   models.VerificationActionIssue,
		Outcome:  models.VerificationOutcomeSuccess,
		Account:  "smail",
		Email:    "owner@example.com",
		RemoteIP: "127.0.0.1",
	}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if _, err := engine.SQL("SELECT account_fingerprint || '|' || email_fingerprint || '|' || ip_fingerprint FROM verification_event").Get(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "" || strings.Contains(raw, "owner@example.com") || strings.Contains(raw, "smail") || strings.Contains(raw, "127.0.0.1") {
		t.Fatalf("sensitive operational value was persisted: %q", raw)
	}
	deleted, err := store.Cleanup(context.Background(), old.Add(DetailRetention+time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
}

func TestEventStoreRejectsUnsupportedPurpose(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	store, err := NewEventStore(engine)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(EventInput{Purpose: Purpose("high_risk")}); err != ErrInvalidEvent {
		t.Fatalf("error = %v, want invalid event", err)
	}
}
func TestEventStoreKeepsInvestigationHoldAndSupportsRetentionClasses(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "retention.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.Sync2(&models.VerificationEvent{}); err != nil {
		t.Fatal(err)
	}
	store, err := NewEventStore(engine)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	if err := store.Record(EventInput{Purpose: PurposeLogin, Action: models.VerificationActionIssue, Outcome: models.VerificationOutcomeSuccess, RetentionClass: RetentionClassAudit, InvestigationHold: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(EventInput{Purpose: PurposeLogin, Action: models.VerificationActionVerify, Outcome: models.VerificationOutcomeFailure, RetentionClass: RetentionClassAggregate}); err != nil {
		t.Fatal(err)
	}
	var held, aggregate models.VerificationEvent
	if _, err := engine.Where("investigation_hold = ?", true).Get(&held); err != nil {
		t.Fatal(err)
	}
	if held.RetentionClass != RetentionClassAudit || !held.ExpiresAt.Equal(now.Add(AuditRetention)) {
		t.Fatalf("held retention = %+v", held)
	}
	if _, err := engine.Where("retention_class = ?", RetentionClassAggregate).Get(&aggregate); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.Cleanup(context.Background(), now.Add(AuditRetention+time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want aggregate event only", deleted)
	}
	var remaining int
	if _, err := engine.SQL("SELECT COUNT(*) FROM verification_event").Get(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("remaining events = %d, want held event", remaining)
	}
}
