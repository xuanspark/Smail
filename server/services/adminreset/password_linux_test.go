//go:build linux

package adminreset

import (
	"testing"
)

func TestStripPasswordLineEndingPreservesPasswordWhitespace(t *testing.T) {
	value := []byte("  Pass123  \r\n")
	got := string(stripPasswordLineEnding(value))
	if got != "  Pass123  " {
		t.Fatalf("password output = %q, want surrounding spaces preserved", got)
	}
}

func TestStripPasswordLineEndingDoesNotTrimOtherWhitespace(t *testing.T) {
	value := []byte("\tPass123\t")
	got := string(stripPasswordLineEnding(value))
	if got != "\tPass123\t" {
		t.Fatalf("password output = %q, want non-line-ending whitespace preserved", got)
	}
}
