package config

import (
	"encoding/json"
	"errors"
	"git.pcux.cn/xuans/Smail/plugins/catalog"
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
				Executable: currentPluginExecutable("telegram_push"),
				Permissions: PluginPermissions{
					Capabilities: []string{"network"},
					Network:      []PluginNetworkTarget{{Scheme: "https", Host: "api.telegram.org", Port: 443}},
				},
				Settings: json.RawMessage(`{"displayName":"Telegram"}`),
			},
		}},
	}
}

func currentPluginExecutable(logicalID string) string {
	executable, err := catalog.CurrentExecutableName(logicalID)
	if err != nil {
		panic(err)
	}
	return executable
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
		{name: "port_conflict", mutate: func(runtime *RuntimeConfig, _ *RuntimeValidationOptions) { runtime.Email.IMAP.TLSPort = 443 }, want: ErrPortConflict},
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
		{name: "plugin_executable_mismatch", mutate: func(runtime *RuntimeConfig, _ *RuntimeValidationOptions) {
			plugin := runtime.Plugins.Plugins["telegram_push"]
			plugin.Executable = "untrusted_plugin.exe"
			runtime.Plugins.Plugins["telegram_push"] = plugin
		}, want: ErrInvalidPlugin},
		{name: "plugin_capability_escalation", mutate: func(runtime *RuntimeConfig, _ *RuntimeValidationOptions) {
			plugin := runtime.Plugins.Plugins["telegram_push"]
			plugin.Permissions.Capabilities = append(plugin.Permissions.Capabilities, "filesystem")
			runtime.Plugins.Plugins["telegram_push"] = plugin
		}, want: ErrPluginCapability},
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

func TestValidateHTTPProxyMode(t *testing.T) {
	runtime := validRuntimeConfig()
	runtime.Root.HTTP = HTTPConfig{
		Port:              15184,
		TLSMode:           HTTPSTLSModeProxy,
		TrustedProxyCIDRs: []string{"127.0.0.1/32", "::1/128"},
	}
	if err := ValidateRuntimeConfig(runtime, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err != nil {
		t.Fatalf("proxy mode was rejected: %v", err)
	}

	invalid := runtime
	invalid.Root.HTTP.HTTPSPort = 443
	if err := ValidateRuntimeConfig(invalid, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); !errors.Is(err, ErrInvalidHTTP) {
		t.Fatalf("proxy mode with HTTPS port returned %v, want %v", err, ErrInvalidHTTP)
	}
}

func TestValidateHTTPRejectsInvalidProxySettings(t *testing.T) {
	cases := []HTTPConfig{
		{Port: 15184, TLSMode: "unsupported"},
		{Port: 15184, TLSMode: HTTPSTLSModeProxy, HTTPSEnabled: true},
		{Port: 15184, TLSMode: HTTPSTLSModeProxy, TrustedProxyCIDRs: []string{"0.0.0.0"}},
		{Port: 15184, TLSMode: HTTPSTLSModeProxy, TrustedProxyCIDRs: []string{" 127.0.0.1/32"}},
	}
	for index, candidate := range cases {
		if err := validateHTTP(candidate); !errors.Is(err, ErrInvalidHTTP) {
			t.Fatalf("case %d returned %v, want %v", index, err, ErrInvalidHTTP)
		}
	}
}
