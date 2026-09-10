package main

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/utils/context"
)

func validSpamBlockRuntimePayload() []byte {
	payload := spamBlockRuntimeConfig{
		PluginID:     "spam_block",
		Generation:   1,
		Settings:     json.RawMessage(`{"url":"https://model.example.com/v1/predict","timeout":3000,"threshold":20}`),
		Capabilities: []string{"network"},
		Network:      []framework.PluginNetworkTarget{{Scheme: "https", Host: "model.example.com", Port: 443}},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return data
}

func TestSpamBlockRuntimeConfigAcceptsAuthorizedPayload(t *testing.T) {
	hook := NewSpamBlockHook()
	if err := hook.ValidateConfig(validSpamBlockRuntimePayload()); err != nil {
		t.Fatal(err)
	}
	if err := hook.Configure(validSpamBlockRuntimePayload()); err != nil {
		t.Fatal(err)
	}
	if err := hook.Health(); err != nil {
		t.Fatal(err)
	}
}

func TestSpamBlockRuntimeConfigRejectsZeroGeneration(t *testing.T) {
	var payload spamBlockRuntimeConfig
	if err := json.Unmarshal(validSpamBlockRuntimePayload(), &payload); err != nil {
		t.Fatal(err)
	}
	payload.Generation = 0
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewSpamBlockHook().ValidateConfig(data); err == nil {
		t.Fatal("zero plugin configuration generation was accepted")
	}
}

func TestSpamBlockRuntimeConfigRejectsUnapprovedNetwork(t *testing.T) {
	var payload spamBlockRuntimeConfig
	if err := json.Unmarshal(validSpamBlockRuntimePayload(), &payload); err != nil {
		t.Fatal(err)
	}
	payload.Network = []framework.PluginNetworkTarget{{Scheme: "https", Host: "other.example.com", Port: 443}}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewSpamBlockHook().ValidateConfig(data); err == nil {
		t.Fatal("unapproved network target was accepted")
	}
}

func TestSpamBlockRuntimeConfigRejectsCapabilityAndExtraNetwork(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*spamBlockRuntimeConfig)
	}{
		{name: "missing capability", mutate: func(payload *spamBlockRuntimeConfig) { payload.Capabilities = nil }},
		{name: "extra capability", mutate: func(payload *spamBlockRuntimeConfig) { payload.Capabilities = []string{"network", "filesystem"} }},
		{name: "extra network", mutate: func(payload *spamBlockRuntimeConfig) {
			payload.Network = append(payload.Network, framework.PluginNetworkTarget{Scheme: "https", Host: "other.example.com", Port: 443})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var payload spamBlockRuntimeConfig
			if err := json.Unmarshal(validSpamBlockRuntimePayload(), &payload); err != nil {
				t.Fatal(err)
			}
			test.mutate(&payload)
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if err := NewSpamBlockHook().ValidateConfig(data); err == nil {
				t.Fatal("invalid runtime configuration was accepted")
			}
		})
	}
}

func TestSpamBlockRuntimeConfigRejectsNetworkWithoutModelURL(t *testing.T) {
	var payload spamBlockRuntimeConfig
	if err := json.Unmarshal(validSpamBlockRuntimePayload(), &payload); err != nil {
		t.Fatal(err)
	}
	payload.Settings = json.RawMessage(`{"timeout":3000,"threshold":20}`)
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewSpamBlockHook().ValidateConfig(data); err == nil {
		t.Fatal("network permission without model URL was accepted")
	}
}

func TestSpamBlockRuntimeConfigRejectsTrailingJSON(t *testing.T) {
	data := append(validSpamBlockRuntimePayload(), []byte(` {}`)...)
	if err := NewSpamBlockHook().ValidateConfig(data); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}

func TestSpamBlockConfigJSONRoundTrip(t *testing.T) {
	input := []byte(`{"url":"https://model.example.com","timeout":3000,"threshold":20}`)
	var config SpamBlockConfig
	if err := json.Unmarshal(input, &config); err != nil {
		t.Fatal(err)
	}
	if config.ApiURL != "https://model.example.com" || config.ApiTimeout != 3000 || config.Threshold != 20 {
		t.Fatalf("unexpected configuration: %+v", config)
	}

	output, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(output, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"url", "timeout", "threshold"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing configuration field %q", key)
		}
	}
	for _, key := range []string{"apiURL", "apiTimeout"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("legacy configuration field %q was emitted", key)
		}
	}
}

func TestDecodeSpamBlockConfigUsesUnifiedValidation(t *testing.T) {
	for _, input := range []string{
		`{"url":"https://model.example.com","timeout":3000,"threshold":20}`,
		`{"url":"https://model.example.com","timeout":"3000","threshold":"20"}`,
	} {
		config, err := decodeSpamBlockConfig([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if config.ApiURL != "https://model.example.com" || config.ApiTimeout != 3000 || config.Threshold != 20 {
			t.Fatalf("unexpected configuration: %+v", config)
		}
	}

	for _, input := range []string{
		`{"url":"https://127.0.0.1","timeout":3000,"threshold":20}`,
		`{"url":"https://model.example.com","timeout":"invalid","threshold":20}`,
		`{"url":"https://model.example.com","timeout":3000,"threshold":101}`,
		`{"url":"https://model.example.com","timeout":3000,"threshold":20,"extra":true}`,
	} {
		if _, err := decodeSpamBlockConfig([]byte(input)); err == nil {
			t.Fatalf("invalid configuration was accepted: %s", input)
		}
	}
}

func TestSettingsHtmlAppliesValidatedConfiguration(t *testing.T) {
	s := &SpamBlock{}
	result := s.SettingsHtml(&context.Context{IsAdmin: true}, "SpamBlock/save", `{"url":"https://model.example.com","timeout":"3000","threshold":"25"}`)
	if result != "旧版插件配置保存接口已停用，请使用新版插件配置接口" {
		t.Fatalf("save result = %q", result)
	}
	s.mu.RLock()
	config := s.cfg
	client := s.hc
	s.mu.RUnlock()
	if config != (SpamBlockConfig{}) || client != nil {
		t.Fatalf("legacy save changed configuration: %+v", config)
	}
}

func TestSettingsHtmlEscapesConfiguredURL(t *testing.T) {
	s := &SpamBlock{cfg: SpamBlockConfig{ApiURL: `"><script>alert(1)</script>`}}
	result := s.SettingsHtml(&context.Context{IsAdmin: true}, "SpamBlock/index.html", "")
	if strings.Contains(result, `<script>alert(1)</script>`) || !strings.Contains(result, "&lt;script&gt;") {
		t.Fatal("configured URL was not HTML escaped")
	}
}

func TestBuildSpamBlockModelTextLimitsInput(t *testing.T) {
	email := &parsemail.Email{
		Subject: strings.Repeat("\u754c", maxSpamBlockModelTextBytes),
		HTML:    []byte(strings.Repeat("<p>body</p>", maxSpamBlockSourceBytes)),
	}
	text := buildSpamBlockModelText(email)
	if len(text) > maxSpamBlockModelTextBytes {
		t.Fatalf("model text length = %d, want at most %d", len(text), maxSpamBlockModelTextBytes)
	}
	if !utf8.ValidString(text) {
		t.Fatal("model text is not valid UTF-8")
	}
}

func TestBuildSpamBlockModelTextHandlesNilAndLimitsSource(t *testing.T) {
	if text := buildSpamBlockModelText(nil); text != "" {
		t.Fatalf("nil email text = %q", text)
	}
	source := []byte(strings.Repeat("a", maxSpamBlockSourceBytes+1))
	if limited := limitSpamBlockBytes(source); len(limited) != maxSpamBlockSourceBytes {
		t.Fatalf("limited source length = %d", len(limited))
	}
}

func TestSettingsHtmlRejectsNonAdminSave(t *testing.T) {
	s := &SpamBlock{}
	result := s.SettingsHtml(&context.Context{}, "SpamBlock/index.html", "")
	if !strings.Contains(result, "请联系管理员") {
		t.Fatalf("non-admin page result = %q, want access denial", result)
	}
	if s.cfg.ApiURL != "" {
		t.Fatal("non-admin save changed the configuration")
	}
}
