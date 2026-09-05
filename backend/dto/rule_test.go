package dto

import (
	"testing"

	"git.pcux.cn/xuans/Smail/models"
)

func TestRuleDecodeRejectsCorruptedStoredJSON(t *testing.T) {
	if _, err := (&Rule{}).Decode(&models.Rule{Value: "{"}); err == nil {
		t.Fatal("corrupted stored rule was accepted")
	}
}

func TestRuleDecodeAcceptsBlankStoredJSON(t *testing.T) {
	decoded, err := (&Rule{}).Decode(&models.Rule{Id: 1, Value: "  "})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Id != 1 || len(decoded.Rules) != 0 {
		t.Fatalf("unexpected decoded rule: %#v", decoded)
	}
}

func TestRuleEncodeRoundTrip(t *testing.T) {
	original := &Rule{Id: 1, Name: "test", Action: MOVE, Rules: []*Value{{Field: "Subject", Type: "contains", Rule: "report"}}}
	stored, err := original.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := (&Rule{}).Decode(stored)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Rules) != 1 || decoded.Rules[0].Rule != "report" {
		t.Fatalf("unexpected decoded rule: %#v", decoded)
	}
}
