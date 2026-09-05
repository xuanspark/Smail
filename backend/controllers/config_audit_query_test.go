package controllers

import (
	"errors"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestParseAuditQueryRejectsUnknownAndMalformedParameters(t *testing.T) {
	tests := []string{
		"/api/v1/audits/config?unknown=value",
		"/api/v1/audits/config?limit=not-a-number",
		"/api/v1/audits/config?from=not-a-time",
		"/api/v1/audits/config?operation=unknown",
		"/api/v1/audits/config?limit=10&limit=20",
	}
	for _, target := range tests {
		request := httptest.NewRequest("GET", target, nil)
		if _, err := parseAuditQuery(request); !errors.Is(err, config.ErrInvalidAuditQuery) {
			t.Fatalf("target %s returned %v, want invalid audit query", target, err)
		}
	}
}

func TestParseAuditQueryAcceptsSupportedParameters(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/audits/config?actorId=42&operation=update&section=smtp&limit=10&from=2026-08-01T00:00:00Z&to=2026-08-30T00:00:00Z", nil)
	query, err := parseAuditQuery(request)
	if err != nil {
		t.Fatalf("valid audit query rejected: %v", err)
	}
	if query.ActorID != "42" || query.Limit != 10 || string(query.Operation) != "update" || string(query.Section) != "smtp" {
		t.Fatalf("unexpected parsed query: %+v", query)
	}
}

func TestAuditExportQueryRejectsInvalidStableID(t *testing.T) {
	query, err := auditExportQuery(configAuditExportRequest{ID: "audit-not-valid"})
	if !errors.Is(err, config.ErrInvalidAuditQuery) {
		t.Fatalf("invalid export ID returned query=%+v error=%v", query, err)
	}
}

func TestAuditExportQueryDefaultsLimitAndParsesTimeRange(t *testing.T) {
	query, err := auditExportQuery(configAuditExportRequest{
		ActorID: "42",
		From:    "2026-08-01T00:00:00Z",
		To:      "2026-08-30T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if query.ActorID != "42" || query.From.IsZero() || query.To.IsZero() || query.Limit != 0 {
		t.Fatalf("unexpected export query: %+v", query)
	}
}
