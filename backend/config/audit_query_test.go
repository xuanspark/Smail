package config

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateConfigAuditQueryAcceptsSupportedFilters(t *testing.T) {
	query := ConfigAuditQuery{
		From:      time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC),
		ActorID:   "42",
		Operation: ConfigOperationUpdate,
		Section:   SectionMail,
		Limit:     1000,
	}
	if err := ValidateConfigAuditQuery(query); err != nil {
		t.Fatalf("valid audit query rejected: %v", err)
	}
}

func TestConfigAuditQueryFiltersCombinedMailSection(t *testing.T) {
	writer := &MemoryAuditWriter{}
	for _, event := range []ConfigAuditEvent{
		{Operation: ConfigOperationUpdate, Section: SectionMail, ActorID: "42", ActorRole: "super_admin", FromGeneration: 1, ToGeneration: 2, Result: "success", CreatedAt: time.Now().UTC()},
		{Operation: ConfigOperationUpdate, Section: SectionSMTP, ActorID: "42", ActorRole: "super_admin", FromGeneration: 2, ToGeneration: 3, Result: "success", CreatedAt: time.Now().UTC()},
	} {
		if err := writer.WriteConfigAudit(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	items, err := writer.Query(context.Background(), ConfigAuditQuery{Section: SectionMail, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Section != SectionMail {
		t.Fatalf("mail audit filter returned %+v", items)
	}
}

func TestValidateConfigAuditQueryRejectsAmbiguousFilters(t *testing.T) {
	tests := []ConfigAuditQuery{
		{Limit: -1},
		{Limit: 1001},
		{From: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
		{ActorID: "bad\nactor"},
		{Operation: ConfigOperation("unknown")},
		{Section: Section("unknown")},
	}
	for index, query := range tests {
		if err := ValidateConfigAuditQuery(query); !errors.Is(err, ErrInvalidAuditQuery) {
			t.Fatalf("case %d returned %v, want ErrInvalidAuditQuery", index, err)
		}
	}
}

func TestConfigManagerRejectsInvalidAuditQueryBeforeStorageAccess(t *testing.T) {
	var manager *ConfigManager
	_, err := manager.AuditQuery(nil, ConfigAuditQuery{Limit: 1001})
	if !errors.Is(err, ErrInvalidAuditQuery) {
		t.Fatalf("AuditQuery returned %v, want ErrInvalidAuditQuery", err)
	}
}

func TestConfigManagerAuditDetailUsesStableIDAndRedactsExport(t *testing.T) {
	writer := &MemoryAuditWriter{}
	manager := &ConfigManager{security: ConfigSecurity{Audit: writer}}
	event := ConfigAuditEvent{
		Operation:          ConfigOperationUpdate,
		Section:            SectionSMTP,
		ActorID:            "42",
		ActorRole:          "super_admin",
		FromGeneration:     1,
		ToGeneration:       2,
		ChangedFields:      []string{"relay.password"},
		ConfirmationMethod: "recent_auth",
		Result:             "success",
		CreatedAt:          time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC),
	}
	if err := writer.WriteConfigAudit(nil, event); err != nil {
		t.Fatal(err)
	}
	items := writer.Events()
	if len(items) != 1 || items[0].ID == "" || !strings.HasPrefix(items[0].ID, "audit-") {
		t.Fatalf("unexpected stable audit ID: %+v", items)
	}
	detail, err := manager.AuditDetail(nil, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != items[0].ID || detail.ChangedFields[0] != "relay.password" {
		t.Fatalf("unexpected audit detail: %+v", detail)
	}
	if _, err := manager.AuditDetail(nil, "audit-not-valid"); !errors.Is(err, ErrInvalidAuditQuery) {
		t.Fatalf("invalid audit ID returned %v", err)
	}
	if _, err := manager.AuditDetail(nil, "audit-00000000000000000000000000000000"); !errors.Is(err, ErrAuditNotFound) {
		t.Fatalf("missing audit ID returned %v", err)
	}
	jsonData, err := manager.ExportAuditJSON(nil, ConfigAuditQuery{ID: items[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(jsonData), "password-value") || !strings.Contains(string(jsonData), items[0].ID) {
		t.Fatalf("unexpected audit export: %s", jsonData)
	}
	csvData, err := manager.ExportAuditCSV(nil, ConfigAuditQuery{ID: items[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(csvData), "id,operation") || !strings.Contains(string(csvData), items[0].ID) {
		t.Fatalf("CSV export does not include stable ID: %s", csvData)
	}
}
