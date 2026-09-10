package controllers

import (
	"strings"
	"testing"
)

func TestSafeDownloadName(t *testing.T) {
	for input, expected := range map[string]string{
		"report.txt":              "report.txt",
		"../report.txt":           ".._report.txt",
		"report\r\nX-Test: value": "report__X-Test: value",
		"":                        "attachment",
		"..":                      "attachment",
	} {
		if got := safeDownloadName(input); got != expected {
			t.Fatalf("safeDownloadName(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestSafeDownloadNameLength(t *testing.T) {
	input := strings.Repeat("a", 300)
	if got := safeDownloadName(input); len([]rune(got)) != 180 {
		t.Fatalf("unexpected filename length: %d", len([]rune(got)))
	}
	if got := safeDownloadName("中文-report.txt"); got != "中文-report.txt" {
		t.Fatalf("unicode filename changed: %q", got)
	}
}
