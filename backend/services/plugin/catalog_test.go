package plugin

import (
	"errors"
	"testing"
)

func TestExecutableNameUsesFixedPlatformMapping(t *testing.T) {
	cases := []struct {
		logicalID string
		goos      string
		want      string
	}{
		{logicalID: "spam_block", goos: "windows", want: "spam_block.exe"},
		{logicalID: "wechat_push", goos: "linux", want: "wechat_push"},
		{logicalID: "telegram_push", goos: "windows", want: "pmail_telegram_push.exe"},
		{logicalID: "telegram_push", goos: "darwin", want: "pmail_telegram_push"},
	}
	for _, testCase := range cases {
		t.Run(testCase.logicalID+"/"+testCase.goos, func(t *testing.T) {
			got, err := ExecutableName(testCase.logicalID, testCase.goos)
			if err != nil {
				t.Fatal(err)
			}
			if got != testCase.want {
				t.Fatalf("executable = %q", got)
			}
		})
	}
}

func TestEnabledDefinitionsOnlyAcceptsRegisteredIdentifiers(t *testing.T) {
	definitions, err := EnabledDefinitions([]string{"spam_block.exe", "wechat_push", "pmail_telegram_push.exe"}, "windows")
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 3 || definitions[0].LogicalID != "spam_block" || definitions[1].LogicalID != "telegram_push" || definitions[2].LogicalID != "wechat_push" {
		t.Fatalf("definitions = %+v", definitions)
	}
	for _, value := range []string{"../spam_block", "Spam_Block", "unknown", "spam_block", "spam_block"} {
		_, err := EnabledDefinitions([]string{value}, "linux")
		if value == "spam_block" {
			if err != nil {
				t.Fatalf("logical ID was rejected: %v", err)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidPluginTarget) && !errors.Is(err, ErrUnknownPluginID) {
			t.Fatalf("value %q error = %v", value, err)
		}
	}
	if _, err := EnabledDefinitions([]string{"spam_block", "spam_block.exe"}, "windows"); !errors.Is(err, ErrInvalidPluginTarget) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestDefinitionsReturnsIndependentCopies(t *testing.T) {
	definitions := Definitions()
	definitions[1].Capabilities[0] = "changed"
	again := Definitions()
	if again[1].Capabilities[0] != "network" {
		t.Fatalf("catalog was mutated: %+v", again)
	}
}
