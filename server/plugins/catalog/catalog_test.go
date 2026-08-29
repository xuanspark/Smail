package catalog

import "testing"

func TestDefinitionsDeclareRuntimeCapabilities(t *testing.T) {
	tests := map[string][]string{
		"spam_block":    {"network"},
		"wechat_push":   {"network"},
		"telegram_push": {"network"},
	}
	for logicalID, want := range tests {
		definition, ok := Lookup(logicalID)
		if !ok {
			t.Fatalf("plugin definition %q is missing", logicalID)
		}
		if len(definition.Capabilities) != len(want) || definition.Capabilities[0] != want[0] {
			t.Fatalf("plugin %q capabilities = %#v, want %#v", logicalID, definition.Capabilities, want)
		}
	}
}

func TestDefinitionsDeclareHandshakeAndSettings(t *testing.T) {
	tests := map[string]struct {
		capabilities []string
		settings     []string
	}{
		"spam_block":    {capabilities: []string{"network"}, settings: []string{"url", "timeout", "threshold"}},
		"wechat_push":   {capabilities: []string{"network"}, settings: []string{"appId", "templateId", "userId", "webDomain"}},
		"telegram_push": {capabilities: []string{"network", "userSettings", "settingsHtml"}, settings: []string{"proxy", "timeout", "debug", "webDomain", "httpsEnabled"}},
	}
	for logicalID, want := range tests {
		definition, ok := Lookup(logicalID)
		if !ok {
			t.Fatalf("plugin definition %q is missing", logicalID)
		}
		if len(definition.HandshakeCapabilities) != len(want.capabilities) || len(definition.SettingsFields) != len(want.settings) {
			t.Fatalf("plugin %q handshake declaration is incomplete: %+v", logicalID, definition)
		}
		for index, value := range want.capabilities {
			if definition.HandshakeCapabilities[index] != value {
				t.Fatalf("plugin %q handshake capabilities = %#v, want %#v", logicalID, definition.HandshakeCapabilities, want.capabilities)
			}
		}
		for index, value := range want.settings {
			if definition.SettingsFields[index] != value {
				t.Fatalf("plugin %q settings = %#v, want %#v", logicalID, definition.SettingsFields, want.settings)
			}
		}
	}
}
