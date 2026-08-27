package main

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/utils/context"
)

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
	if result != "success" {
		t.Fatalf("save result = %q", result)
	}
	s.mu.RLock()
	config := s.cfg
	client := s.hc
	s.mu.RUnlock()
	if config.ApiURL != "https://model.example.com" || config.ApiTimeout != 3000 || config.Threshold != 25 || client == nil {
		t.Fatalf("unexpected applied configuration: %+v", config)
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
	result := s.SettingsHtml(&context.Context{}, "SpamBlock/save", `{"url":"https://model.example.com/predict"}`)
	if result != "No Access Privileges" {
		t.Fatalf("non-admin save result = %q, want access denial", result)
	}
	if s.cfg.ApiURL != "" {
		t.Fatal("non-admin save changed the configuration")
	}
}
