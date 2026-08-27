package config

import (
	"encoding/json"
	"errors"
	"testing"
)

func validRuntimeConfig() RuntimeConfig {
	root := testRootConfig()
	root.SetupState = SetupStateReady
	root.Domains = DomainConfig{Primary: "mail.example.com", Accepted: []string{"mail.example.com", "example.com"}, Web: "mail.example.com"}
	root.HTTP = HTTPConfig{Port: 80, HTTPSPort: 443, HTTPSEnabled: true}
	root.Sections = validSectionReferences(root.Generation)
	meta := SectionMeta{Schema: CurrentSectionSchema, Generation: root.Generation}
	return RuntimeConfig{
		Generation: root.Generation,
		SetupState: root.SetupState,
		Root:       root,
		SMTP:       SMTPSectionConfig{Meta: meta, Relay: SMTPConfig{}},
		Email: EmailSectionConfig{Meta: meta, Policy: EmailConfig{
			SMTPPort: 25,
			IMAPPort: 143,
			POP3Port: 110,
		}},
		SSL: SSLSectionConfig{Meta: meta, TLS: root.SSL},
		Plugins: PluginsSectionConfig{Meta: meta, Plugins: map[string]PluginInstance{
			"telegram_push": {
				Enabled:    true,
				Executable: "telegram_push",
				Permissions: PluginPermissions{
					Capabilities: []string{"network"},
					Network:      []PluginNetworkTarget{{Scheme: "https", Host: "api.telegram.org", Port: 443}},
				},
				Settings: json.RawMessage(`{"displayName":"Telegram"}`),
			},
		}},
	}
}

func TestValidateRuntimeConfig(t *testing.T) {
	runtime := validRuntimeConfig()
	runtime.Root.Domains.AccessHosts = []string{"192.0.2.10", "reset.example.com"}
	if err := ValidateRuntimeConfig(runtime, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRuntimeConfigRejectsCrossSectionViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RuntimeConfig, *RuntimeValidationOptions)
		want   error
	}{
		{name: "port_conflict", mutate: func(runtime *RuntimeConfig, _ *RuntimeValidationOptions) { runtime.Email.Policy.IMAPPort = 443 }, want: ErrPortConflict},
		{name: "primary_domain_missing", mutate: func(runtime *RuntimeConfig, _ *RuntimeValidationOptions) {
			runtime.Root.Domains.Accepted = []string{"example.com"}
		}, want: ErrInvalidDomainConfig},
		{name: "generation_mismatch", mutate: func(runtime *RuntimeConfig, _ *RuntimeValidationOptions) { runtime.Email.Meta.Generation++ }, want: ErrSectionGenerationMismatch},
		{name: "missing_super_admin", mutate: func(_ *RuntimeConfig, options *RuntimeValidationOptions) { options.HasSuperAdmin = false }, want: ErrInvalidRuntimeConfig},
		{name: "network_without_capability", mutate: func(runtime *RuntimeConfig, _ *RuntimeValidationOptions) {
			plugin := runtime.Plugins.Plugins["telegram_push"]
			plugin.Permissions.Capabilities = nil
			runtime.Plugins.Plugins["telegram_push"] = plugin
		}, want: ErrPluginCapability},
		{name: "insecure_plugin_scheme", mutate: func(runtime *RuntimeConfig, _ *RuntimeValidationOptions) {
			plugin := runtime.Plugins.Plugins["telegram_push"]
			plugin.Permissions.Network[0].Scheme = "http"
			runtime.Plugins.Plugins["telegram_push"] = plugin
		}, want: ErrPluginCapability},
		{name: "plugin_secret_in_settings", mutate: func(runtime *RuntimeConfig, _ *RuntimeValidationOptions) {
			plugin := runtime.Plugins.Plugins["telegram_push"]
			plugin.Settings = json.RawMessage(`{"botToken":"secret"}`)
			runtime.Plugins.Plugins["telegram_push"] = plugin
		}, want: ErrInvalidPlugin},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := validRuntimeConfig()
			options := RuntimeValidationOptions{HasSuperAdmin: true, Production: true}
			test.mutate(&runtime, &options)
			if err := ValidateRuntimeConfig(runtime, options); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestProductionDegradedRuntimeRequiresHTTPS(t *testing.T) {
	runtime := validRuntimeConfig()
	runtime.SetupState = SetupStateDegraded
	runtime.Root.SetupState = SetupStateDegraded
	runtime.Root.HTTP.HTTPSEnabled = false
	runtime.Root.HTTP.HTTPSPort = 0
	if err := ValidateRuntimeConfig(runtime, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); !errors.Is(err, ErrInvalidRuntimeConfig) {
		t.Fatalf("error = %v", err)
	}
	if err := ValidateRuntimeConfig(runtime, RuntimeValidationOptions{HasSuperAdmin: true, Production: false}); err != nil {
		t.Fatal(err)
	}
}
