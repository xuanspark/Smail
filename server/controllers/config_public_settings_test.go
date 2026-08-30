package controllers

import (
	"encoding/json"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestPublicPluginConfigIncludesNonSensitiveSettings(t *testing.T) {
	runtime := config.RuntimeConfig{
		Plugins: config.PluginsSectionConfig{
			Meta: config.SectionMeta{Schema: config.CurrentSectionSchema, Generation: 3},
			Plugins: map[string]config.PluginInstance{
				"spam_block": {
					Enabled:    true,
					Executable: "spam_block.exe",
					Settings:   json.RawMessage(`{"url":"https://model.example.com/v1/predict","timeout":3000,"threshold":20}`),
				},
			},
		},
	}

	public, err := publicConfigSection(runtime, config.SectionPlugins)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		Plugins map[string]struct {
			Settings map[string]any `json:"settings"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	settings, ok := decoded.Plugins["spam_block"].Settings["url"]
	if !ok || settings != "https://model.example.com/v1/predict" {
		t.Fatalf("public plugin settings were not returned: %s", data)
	}
	if _, ok := decoded.Plugins["spam_block"].Settings["password"]; ok {
		t.Fatalf("public plugin settings contain a password field: %s", data)
	}
	if _, ok := decoded.Plugins["spam_block"].Settings["secret"]; ok {
		t.Fatalf("public plugin settings contain a secret field: %s", data)
	}
}
