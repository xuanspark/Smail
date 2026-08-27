package config

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFileAuditWriterPersistsOnlyRedactedMetadata(t *testing.T) {
	root := t.TempDir()
	writer, err := NewFileAuditWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	event := ConfigAuditEvent{
		Operation: ConfigOperationUpdate, Section: SectionSMTP, ActorID: "user-1", ActorRole: "super_admin",
		FromGeneration: 1, ToGeneration: 2, ChangedFields: []string{"relay.password"},
		ConfirmationMethod: "recent_auth", Result: "success", CreatedAt: time.Now().UTC(),
	}
	if err := writer.WriteConfigAudit(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "plaintext") || strings.Contains(string(data), "password-value") {
		t.Fatal("audit file contains secret-like plaintext")
	}
	items, err := writer.Query(context.Background(), ConfigAuditQuery{ActorID: "user-1"})
	if err != nil || len(items) != 1 || items[0].ChangedFields[0] != "relay.password" {
		t.Fatalf("query result = %+v, error = %v", items, err)
	}
	jsonData, err := writer.ExportJSON(context.Background(), ConfigAuditQuery{})
	if err != nil || !strings.Contains(string(jsonData), "relay.password") {
		t.Fatalf("json export = %s, error = %v", jsonData, err)
	}
	csvData, err := writer.ExportCSV(context.Background(), ConfigAuditQuery{})
	if err != nil || !strings.Contains(string(csvData), "changed_fields") {
		t.Fatalf("csv export = %s, error = %v", csvData, err)
	}
}

func TestFileAuditWriterCleanupKeepsRecentAndDeletesExpiredEvents(t *testing.T) {
	root := t.TempDir()
	writer, err := NewFileAuditWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	for index, createdAt := range []time.Time{now.Add(-ConfigAuditRetention - time.Nanosecond), now.Add(-ConfigAuditRetention + time.Nanosecond), now} {
		event := ConfigAuditEvent{
			Operation: ConfigOperationUpdate, Section: SectionEmail, ActorID: "user-" + string(rune('1'+index)), ActorRole: "super_admin",
			FromGeneration: uint64(index + 1), ToGeneration: uint64(index + 2), Result: "success", CreatedAt: createdAt,
		}
		if err := writer.WriteConfigAudit(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := writer.Cleanup(context.Background(), now)
	if err != nil || deleted != 1 {
		t.Fatalf("cleanup deleted = %d, error = %v", deleted, err)
	}
	items, err := writer.Query(context.Background(), ConfigAuditQuery{})
	if err != nil || len(items) != 2 {
		t.Fatalf("remaining events = %d, error = %v", len(items), err)
	}
}
