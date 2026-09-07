package config

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestMailListenerConfigurationOverridesLegacyPolicyAndIgnoresRelay(t *testing.T) {
	previous := CurrentManager()
	defer SetRuntimeManager(previous)
	runtime := validRuntimeConfig()
	runtime.SMTP.Listener = SMTPListenerConfig{Port: 2525, STARTTLSPort: 2587, TLSPort: 2465}
	runtime.SMTP.Relay.RelayPort = 1993
	runtime.Email.IMAP = IMAPListenerConfig{TLSPort: 1993}
	runtime.Email.POP3 = POP3ListenerConfig{Port: 1110, TLSPort: 1995}
	SetRuntimeManager(&ConfigManager{snapshots: runtimeAccessSnapshotStore{runtime: runtime}})
	want := MailPorts{SMTP: 2525, SMTPSTARTTLS: 2587, SMTPTLS: 2465, IMAP: 1993, POP3: 1110, POP3TLS: 1995}
	if got := CurrentMailPorts(); got != want {
		t.Fatalf("listeners = %+v, want %+v", got, want)
	}
}

func TestMailListenerValidationCoversEveryActualListener(t *testing.T) {
	for name, mutate := range map[string]func(*RuntimeConfig){
		"smtp_http":           func(r *RuntimeConfig) { r.SMTP.Listener.Port = 80 },
		"smtp_starttls_https": func(r *RuntimeConfig) { r.SMTP.Listener.STARTTLSPort = 443 },
		"smtp_tls_imap":       func(r *RuntimeConfig) { r.SMTP.Listener.TLSPort = 993 },
		"imap_pop3_tls":       func(r *RuntimeConfig) { r.Email.IMAP.TLSPort = 995 },
		"pop3_smtp":           func(r *RuntimeConfig) { r.Email.POP3.Port = 25 },
		"pop3_tls_starttls":   func(r *RuntimeConfig) { r.Email.POP3.TLSPort = 587 },
		"http_default":        func(r *RuntimeConfig) { r.Root.HTTP.Port = 0; r.Email.IMAP.TLSPort = 80 },
		"smtp_negative":       func(r *RuntimeConfig) { r.SMTP.Listener.Port = -1 },
		"starttls_overflow":   func(r *RuntimeConfig) { r.SMTP.Listener.STARTTLSPort = 65536 },
		"tls_negative":        func(r *RuntimeConfig) { r.SMTP.Listener.TLSPort = -1 },
		"imap_overflow":       func(r *RuntimeConfig) { r.Email.IMAP.TLSPort = 65536 },
		"pop3_negative":       func(r *RuntimeConfig) { r.Email.POP3.Port = -1 },
		"pop3_tls_overflow":   func(r *RuntimeConfig) { r.Email.POP3.TLSPort = 65536 },
	} {
		t.Run(name, func(t *testing.T) {
			runtime := validRuntimeConfig()
			mutate(&runtime)
			if err := ValidateRuntimeConfig(runtime, RuntimeValidationOptions{HasSuperAdmin: true}); !errors.Is(err, ErrPortConflict) {
				t.Fatalf("validation = %v, want port conflict", err)
			}
		})
	}
}

func TestMailListenerCandidateApplyAndReloadPreservesAllSections(t *testing.T) {
	root := t.TempDir()
	initial := validRuntimeConfig()
	writeRuntimeFixture(t, root, initial)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []Section{SectionSMTP, SectionEmail} {
		current := manager.Snapshot()
		var value any
		if section == SectionSMTP {
			current.SMTP.Listener = SMTPListenerConfig{Port: 2525, STARTTLSPort: 2587, TLSPort: 2465}
			value = current.SMTP
		} else {
			current.Email.IMAP.TLSPort = 1993
			current.Email.POP3 = POP3ListenerConfig{Port: 1110, TLSPort: 1995}
			value = current.Email
		}
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := manager.Update(context.Background(), UpdateRequest{
			Section: section, ExpectedGeneration: current.Generation, Payload: payload, Actor: authorizedConfigActor(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if manager.Snapshot().Generation != current.Generation {
			t.Fatal("saving candidate published it prematurely")
		}
		if _, err := manager.ApplyPendingAuthorized(context.Background(), updated.Generation, updated.CandidateID, section, authorizedConfigActor()); err != nil {
			t.Fatal(err)
		}
	}
	reloaded, err := NewConfigLoader(root, options).Load()
	if err != nil {
		t.Fatal(err)
	}
	want := MailPorts{SMTP: 2525, SMTPSTARTTLS: 2587, SMTPTLS: 2465, IMAP: 1993, POP3: 1110, POP3TLS: 1995}
	if got := ResolveMailPorts(reloaded.SMTP, reloaded.Email); got != want {
		t.Fatalf("reloaded listeners = %+v, want %+v", got, want)
	}
	if reloaded.Email.Policy != initial.Email.Policy || reloaded.SMTP.Relay != initial.SMTP.Relay {
		t.Fatal("listener change overwrote policy or relay")
	}
}

func TestMailListenerConflictsUseEffectiveWebPorts(t *testing.T) {
	previous := RuntimeSetupPort()
	t.Cleanup(func() { SetRuntimeSetupPort(previous) })
	for _, tc := range []struct {
		name      string
		http      HTTPConfig
		envHTTP   string
		envHTTPS  string
		setupPort int
		mailPort  int
		conflict  bool
	}{
		{"environment_http", HTTPConfig{}, "9080", "", 8080, 9080, true},
		{"setup_http", HTTPConfig{}, "", "", 8080, 8080, true},
		{"configured_http", HTTPConfig{Port: 7080}, "9080", "", 8080, 7080, true},
		{"unused_environment_http", HTTPConfig{Port: 7080}, "9080", "", 8080, 9080, false},
		{"environment_https", HTTPConfig{HTTPSEnabled: true}, "", "9443", 8080, 9443, true},
		{"default_https", HTTPConfig{HTTPSEnabled: true}, "", "", 8080, 443, true},
		{"proxy_no_internal_https", HTTPConfig{TLSMode: HTTPSTLSModeProxy}, "", "9443", 8080, 9443, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SMAIL_HTTP_PORT", tc.envHTTP)
			t.Setenv("SMAIL_HTTPS_PORT", tc.envHTTPS)
			SetRuntimeSetupPort(tc.setupPort)
			ports := ResolveMailPorts(SMTPSectionConfig{}, EmailSectionConfig{})
			ports.IMAP = tc.mailPort
			err := validateRuntimePorts(tc.http, ports)
			if errors.Is(err, ErrPortConflict) != tc.conflict {
				t.Fatalf("validation = %v, want conflict %v", err, tc.conflict)
			}
		})
	}
}

func TestOldPartitionPayloadsKeepExistingListenerDefaults(t *testing.T) {
	var smtp SMTPSectionConfig
	var email EmailSectionConfig
	if err := ParseStrict([]byte(`{"meta":{"schema":1,"generation":1},"relay":{"relayPort":2465}}`), &smtp); err != nil {
		t.Fatal(err)
	}
	if err := ParseStrict([]byte(`{"meta":{"schema":1,"generation":1},"policy":{"smtpPort":2525,"imapPort":1143,"pop3Port":1110}}`), &email); err != nil {
		t.Fatal(err)
	}
	want := MailPorts{SMTP: 2525, SMTPSTARTTLS: 587, SMTPTLS: 465, IMAP: 993, POP3: 1110, POP3TLS: 995}
	if got := ResolveMailPorts(smtp, email); got != want {
		t.Fatalf("legacy listeners changed: %+v", got)
	}
}

func TestUnifiedMailSecurityResolvesOnlySelectedClientListeners(t *testing.T) {
	for _, test := range []struct {
		security string
		want     MailPorts
	}{
		{MailSecuritySTARTTLS, MailPorts{SMTP: 25, SMTPSTARTTLS: 587, IMAP: 143, POP3: 110}},
		{MailSecurityImplicitTLS, MailPorts{SMTP: 25, SMTPTLS: 465, IMAP: 993, POP3TLS: 995}},
	} {
		smtp := SMTPSectionConfig{Listener: SMTPListenerConfig{Security: test.security}}
		if got := ResolveMailPorts(smtp, EmailSectionConfig{}); got != test.want {
			t.Fatalf("%s ports = %+v, want %+v", test.security, got, test.want)
		}
	}
}

func TestLegacyMailSecurityKeepsExistingListeners(t *testing.T) {
	smtp := SMTPSectionConfig{Listener: SMTPListenerConfig{Port: 2525, STARTTLSPort: 2587, TLSPort: 2465}}
	email := EmailSectionConfig{IMAP: IMAPListenerConfig{TLSPort: 2993}, POP3: POP3ListenerConfig{Port: 2110, TLSPort: 2995}}
	want := MailPorts{SMTP: 2525, SMTPSTARTTLS: 2587, SMTPTLS: 2465, IMAP: 2993, POP3: 2110, POP3TLS: 2995}
	if got := ResolveMailPorts(smtp, email); got != want {
		t.Fatalf("legacy ports = %+v, want %+v", got, want)
	}
	if got := ResolveMailSecurity(smtp.Listener); got != MailSecurityBoth {
		t.Fatalf("legacy security = %q, want %q", got, MailSecurityBoth)
	}
	if got := PublicMailSecurity(smtp.Listener); got != "" {
		t.Fatalf("legacy public security = %q, want empty", got)
	}
	if got := PublicMailSecurity(SMTPListenerConfig{Security: "tls"}); got != MailSecurityImplicitTLS {
		t.Fatalf("legacy TLS public security = %q, want %q", got, MailSecurityImplicitTLS)
	}
	if got := NormalizeMailSecurity("tls"); got != MailSecurityImplicitTLS {
		t.Fatalf("legacy relay security = %q, want %q", got, MailSecurityImplicitTLS)
	}
}

func TestMailListenerValidationAcceptsMatchingLegacyTLSNames(t *testing.T) {
	runtime := validRuntimeConfig()
	runtime.SMTP.Listener.Security = "tls"
	runtime.SMTP.Relay.Security = "tls"
	if err := ValidateRuntimeConfig(runtime, RuntimeValidationOptions{HasSuperAdmin: true}); err != nil {
		t.Fatalf("matching legacy TLS validation = %v", err)
	}
}

func TestMailListenerValidationRejectsUnknownSecurity(t *testing.T) {
	runtime := validRuntimeConfig()
	runtime.SMTP.Listener.Security = "unknown"
	if err := ValidateRuntimeConfig(runtime, RuntimeValidationOptions{HasSuperAdmin: true}); !errors.Is(err, ErrInvalidRuntimeConfig) {
		t.Fatalf("validation = %v, want invalid runtime configuration", err)
	}
}

func TestMailListenerValidationAllowsLegacyEmptySecurityButRejectsExplicitBoth(t *testing.T) {
	runtime := validRuntimeConfig()
	if err := ValidateRuntimeConfig(runtime, RuntimeValidationOptions{HasSuperAdmin: true}); err != nil {
		t.Fatalf("legacy empty security validation = %v", err)
	}
	runtime.SMTP.Listener.Security = MailSecurityBoth
	if err := ValidateRuntimeConfig(runtime, RuntimeValidationOptions{HasSuperAdmin: true}); !errors.Is(err, ErrInvalidRuntimeConfig) {
		t.Fatalf("explicit both validation = %v, want invalid runtime configuration", err)
	}
}
