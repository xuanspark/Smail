package config

import (
	"errors"
	"testing"
)

func TestParseStrictRejectsDuplicateUnknownAndTrailingData(t *testing.T) {
	var target map[string]any
	if !errors.Is(ParseStrict([]byte(`{"a":1,"a":2}`), &target), ErrConfigDuplicate) {
		t.Fatal("duplicate key was accepted")
	}
	if err := ParseStrict([]byte(`{"a":1} {"b":2}`), &target); !errors.Is(err, ErrConfigTrailing) {
		t.Fatalf("trailing data error = %v", err)
	}
	var typed struct {
		A int `json:"a"`
	}
	if err := ParseStrict([]byte(`{"b":1}`), &typed); err == nil {
		t.Fatal("unknown field was accepted")
	}
}
