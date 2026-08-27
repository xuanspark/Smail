package main

import (
	"strings"
	"testing"
)

func TestSafeExportTextPreventsRowAndFormulaInjection(t *testing.T) {
	got := safeExportText("=cmd\r\nnext\trow")
	if !strings.HasPrefix(got, "'=") {
		t.Fatalf("formula prefix was not neutralized: %q", got)
	}
	if strings.ContainsAny(got, "\r\n\t") {
		t.Fatalf("row separators were retained: %q", got)
	}
}

func TestSafeExportTextPreservesNormalText(t *testing.T) {
	if got := safeExportText("normal message"); got != "normal message" {
		t.Fatalf("normal text changed: %q", got)
	}
}
