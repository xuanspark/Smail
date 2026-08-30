package dbmigration

import (
	"context"
	"errors"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/utils/credential"
)

type testRunner struct{}

func (testRunner) Run(_ context.Context, _ Plan, update func(Status) error) error {
	if err := update(StatusCopying); err != nil {
		return err
	}
	if err := update(StatusVerifying); err != nil {
		return err
	}
	return update(StatusSwitching)
}

func testDatabase(t *testing.T) config.DatabaseConfig {
	t.Helper()
	vault, err := credential.NewVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := vault.Encrypt("password")
	if err != nil {
		t.Fatal(err)
	}
	return config.DatabaseConfig{
		Type:     config.DatabaseTypeMySQL,
		Host:     "db.internal",
		Port:     3306,
		Username: "smail",
		Name:     "smail",
		Password: config.StoredSecret{Ciphertext: ciphertext, Version: vault.Version(), UpdatedAt: time.Unix(1, 0).UTC()},
	}
}

func TestPreviewAndExecuteUseOnlyDatabaseName(t *testing.T) {
	service := NewService(testRunner{})
	service.now = func() time.Time { return time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC) }
	record, token, err := service.Preview(context.Background(), testDatabase(t), 7, "smail_next")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StatusPreviewed || record.Source.Name != "smail" || record.Target.Name != "smail_next" {
		t.Fatalf("unexpected preview: %+v", record)
	}
	if record.Source.Host != record.Target.Host || record.Source.Port != record.Target.Port || record.Source.Username != record.Target.Username {
		t.Fatalf("immutable database settings changed: %+v", record)
	}
	if token == "" {
		t.Fatal("preview token is empty")
	}
	completed, err := service.Execute(context.Background(), record.ID, token)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusSucceeded {
		t.Fatalf("migration status = %s, want succeeded", completed.Status)
	}
	if _, err := service.Execute(context.Background(), record.ID, token); !errors.Is(err, ErrMigrationState) {
		t.Fatalf("replayed token returned %v, want state error", err)
	}
}

func TestPreviewRejectsUnsafeTargetName(t *testing.T) {
	service := NewService(testRunner{})
	if _, _, err := service.Preview(context.Background(), testDatabase(t), 1, "smail;drop"); !errors.Is(err, ErrInvalidTargetName) {
		t.Fatalf("unsafe target returned %v", err)
	}
}

func TestFailedExecutionKeepsSafeState(t *testing.T) {
	service := NewService(nil)
	record, token, err := service.Preview(context.Background(), testDatabase(t), 1, "smail_next")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := service.Execute(context.Background(), record.ID, token)
	if !errors.Is(err, ErrExecutorUnavailable) {
		t.Fatalf("execution returned %v, want executor unavailable", err)
	}
	if failed.Status != StatusFailed || failed.ErrorCode != "EXECUTOR_UNAVAILABLE" {
		t.Fatalf("unexpected failed state: %+v", failed)
	}
}

func TestSQLitePreviewChangesOnlyLocalReference(t *testing.T) {
	service := NewService(testRunner{})
	source := config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "smail.db"}
	record, _, err := service.Preview(context.Background(), source, 1, "smail-next.db")
	if err != nil {
		t.Fatal(err)
	}
	if record.Target.LocalRef != "smail-next.db" || record.Target.Type != config.DatabaseTypeSQLite || record.Target.Name != "" {
		t.Fatalf("unexpected sqlite target: %+v", record.Target)
	}
}

type invalidTransitionRunner struct{}

func (invalidTransitionRunner) Run(_ context.Context, _ Plan, update func(Status) error) error {
	return update(StatusCreated)
}

func TestMigrationRejectsBackwardStatusTransition(t *testing.T) {
	service := NewService(invalidTransitionRunner{})
	record, token, err := service.Preview(context.Background(), testDatabase(t), 1, "smail_next")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := service.Execute(context.Background(), record.ID, token)
	if !errors.Is(err, ErrMigrationState) {
		t.Fatalf("execution returned %v, want state error", err)
	}
	if failed.Status != StatusFailed || failed.ErrorCode != "MIGRATION_FAILED" {
		t.Fatalf("unexpected failed migration: %+v", failed)
	}
}

func TestMigrationRecordsRemainIndependentDuringConcurrentExecution(t *testing.T) {
	service := NewService(testRunner{})
	first, firstToken, err := service.Preview(context.Background(), testDatabase(t), 1, "smail_first")
	if err != nil {
		t.Fatal(err)
	}
	second, secondToken, err := service.Preview(context.Background(), testDatabase(t), 1, "smail_second")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan struct {
		record Record
		err    error
	}, 2)
	go func() {
		record, err := service.Execute(context.Background(), first.ID, firstToken)
		results <- struct {
			record Record
			err    error
		}{record: record, err: err}
	}()
	go func() {
		record, err := service.Execute(context.Background(), second.ID, secondToken)
		results <- struct {
			record Record
			err    error
		}{record: record, err: err}
	}()
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent execution failed: %v", result.err)
		}
		if result.record.Status != StatusSucceeded {
			t.Fatalf("unexpected concurrent status: %+v", result.record)
		}
		if result.record.Target.Name != "smail_first" && result.record.Target.Name != "smail_second" {
			t.Fatalf("unexpected concurrent target: %+v", result.record.Target)
		}
	}
	firstState, err := service.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondState, err := service.Get(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstState.Target.Name != "smail_first" || secondState.Target.Name != "smail_second" {
		t.Fatalf("migration records crossed: first=%+v second=%+v", firstState.Target, secondState.Target)
	}
}
