package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRootConfigRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RootConfig)
		want   error
	}{
		{
			name: "invalid_log_level",
			mutate: func(root *RootConfig) {
				root.LogLevel = "trace"
			},
			want: ErrInvalidLogLevel,
		},
		{
			name: "invalid_http_port",
			mutate: func(root *RootConfig) {
				root.HTTP.Port = 70000
			},
			want: ErrInvalidHTTP,
		},
		{
			name: "https_requires_port",
			mutate: func(root *RootConfig) {
				root.HTTP.HTTPSEnabled = true
				root.HTTP.HTTPSPort = 0
			},
			want: ErrInvalidHTTP,
		},
		{
			name: "invalid_email_policy",
			mutate: func(root *RootConfig) {
				root.Email.LoginVerificationPolicy = "sometimes"
			},
			want: ErrInvalidEmail,
		},
		{
			name: "invalid_smtp_host",
			mutate: func(root *RootConfig) {
				root.SMTP = SMTPConfig{
					RelayHost:           "smtp.example.com/relay",
					RelayPort:           587,
					Security:            "starttls",
					FromDomain:          "mail.example.com",
					VerificationEnabled: true,
				}
			},
			want: ErrInvalidSMTP,
		},
		{
			name: "remote_plain_smtp_relay",
			mutate: func(root *RootConfig) {
				root.SMTP = SMTPConfig{
					RelayHost:           "smtp.example.com",
					RelayPort:           25,
					Security:            "plain",
					FromDomain:          "mail.example.com",
					VerificationEnabled: true,
				}
			},
			want: ErrInvalidSMTP,
		},
		{
			name: "inconsistent_disabled_smtp_secret",
			mutate: func(root *RootConfig) {
				root.SMTP.Password = StoredSecret{Ciphertext: "ENC[v1]invalid", Version: 1}
			},
			want: ErrInvalidSMTP,
		},
		{
			name: "ssl_path_escape",
			mutate: func(root *RootConfig) {
				root.SSL.CertificateRef = "config/ssl/../outside.pem"
				root.SSL.PrivateKeyRef = "config/ssl/key.pem"
			},
			want: ErrInvalidSSL,
		},
		{
			name: "ssl_certificate_key_mismatch",
			mutate: func(root *RootConfig) {
				root.SSL.CertificateRef = "config/ssl/certificate.pem"
			},
			want: ErrInvalidSSL,
		},
		{
			name: "duplicate_plugin_allowlist_target",
			mutate: func(root *RootConfig) {
				root.Plugins = []PluginConfig{{
					LogicalID:        "telegram_push",
					FileName:         "telegram_push",
					NetworkAllowlist: []string{"api.example.com", "api.example.com"},
				}}
			},
			want: ErrInvalidPlugin,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := testRootConfig()
			test.mutate(&root)
			if err := ValidateRootConfig(root); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateRuntimeConfigRejectsUnsafeCrossSectionValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RuntimeConfig)
		want   error
	}{
		{
			name: "duplicate_accepted_domain",
			mutate: func(runtime *RuntimeConfig) {
				runtime.Root.Domains.Accepted = []string{"mail.example.com", "mail.example.com"}
			},
			want: ErrInvalidDomainConfig,
		},
		{
			name: "ready_requires_web_domain",
			mutate: func(runtime *RuntimeConfig) {
				runtime.Root.Domains.Web = ""
			},
			want: ErrInvalidDomainConfig,
		},
		{
			name: "duplicate_plugin_network_target",
			mutate: func(runtime *RuntimeConfig) {
				plugin := runtime.Plugins.Plugins["telegram_push"]
				plugin.Permissions.Network = append(plugin.Permissions.Network, plugin.Permissions.Network[0])
				runtime.Plugins.Plugins["telegram_push"] = plugin
			},
			want: ErrInvalidPlugin,
		},
		{
			name: "plugin_network_ip_target",
			mutate: func(runtime *RuntimeConfig) {
				plugin := runtime.Plugins.Plugins["telegram_push"]
				plugin.Permissions.Network[0].Host = "127.0.0.1"
				runtime.Plugins.Plugins["telegram_push"] = plugin
			},
			want: ErrInvalidPlugin,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := validRuntimeConfig()
			test.mutate(&runtime)
			if err := ValidateRuntimeConfig(runtime, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateSMTPKeepsLoginVerificationIndependentFromRelay(t *testing.T) {
	tests := []struct {
		name  string
		smtp  SMTPConfig
		valid bool
	}{
		{
			name: "verification enabled without relay",
			smtp: SMTPConfig{
				VerificationEnabled: true,
				LoginPolicy:         "new_device",
				FromDomain:          "example.com",
			},
			valid: true,
		},
		{
			name: "partially configured relay",
			smtp: SMTPConfig{
				RelayHost:   "smtp.example.com",
				RelayPort:   587,
				Security:    "starttls",
				LoginPolicy: "new_device",
			},
		},
		{
			name: "complete relay",
			smtp: SMTPConfig{
				RelayHost:   "smtp.example.com",
				RelayPort:   587,
				Security:    "starttls",
				From:        "no-reply@example.com",
				FromDomain:  "example.com",
				LoginPolicy: "new_device",
			},
			valid: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSMTP(test.smtp)
			if test.valid && err != nil {
				t.Fatalf("validateSMTP() error = %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidSMTP) {
				t.Fatalf("validateSMTP() error = %v, want %v", err, ErrInvalidSMTP)
			}
		})
	}
}

func TestSnapshotStoreRejectsInvalidCandidateWithoutChangingSnapshot(t *testing.T) {
	initial := validRuntimeConfig()
	store, err := NewSnapshotStore(initial, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	invalid := cloneRuntimeConfig(initial)
	invalid.Email.IMAP.TLSPort = invalid.Root.HTTP.HTTPSPort
	if err := store.Publish(invalid); !errors.Is(err, ErrPortConflict) {
		t.Fatalf("publish error = %v, want %v", err, ErrPortConflict)
	}
	if store.Generation() != before.Generation {
		t.Fatalf("generation = %d, want %d", store.Generation(), before.Generation)
	}
	after, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if after.Email.IMAP.TLSPort != before.Email.IMAP.TLSPort {
		t.Fatalf("snapshot changed after rejection: before=%d after=%d", before.Email.IMAP.TLSPort, after.Email.IMAP.TLSPort)
	}
}

func TestConfigManagerRejectsInvalidUpdateWithoutDiskOrStatusMutation(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}

	beforeFiles := make(map[string][]byte)
	for _, name := range []string{"config.json", "smtp.json", "email.json", "ssl.json", "plugins.json"} {
		data, readErr := os.ReadFile(filepath.Join(root, "config", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		beforeFiles[name] = data
	}
	beforeStatus := manager.Status()
	current := manager.Snapshot()
	invalidEmail := current.Email
	invalidEmail.IMAP.TLSPort = current.Root.HTTP.HTTPSPort
	payload, err := json.Marshal(invalidEmail)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Update(context.Background(), UpdateRequest{
		Section:            SectionEmail,
		ExpectedGeneration: current.Generation,
		Payload:            payload,
		Actor:              authorizedConfigActor(),
	}); !errors.Is(err, ErrPortConflict) {
		t.Fatalf("update error = %v, want %v", err, ErrPortConflict)
	}

	if got := manager.Snapshot().Generation; got != beforeStatus.ActiveGeneration {
		t.Fatalf("active generation = %d, want %d", got, beforeStatus.ActiveGeneration)
	}
	afterStatus := manager.Status()
	if afterStatus.ActiveGeneration != beforeStatus.ActiveGeneration || len(afterStatus.PendingRestart) != 0 {
		t.Fatalf("status changed after rejected update: before=%+v after=%+v", beforeStatus, afterStatus)
	}
	for name, before := range beforeFiles {
		after, readErr := os.ReadFile(filepath.Join(root, "config", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(after, before) {
			t.Fatalf("configuration file %s changed after rejected update", name)
		}
	}
}
