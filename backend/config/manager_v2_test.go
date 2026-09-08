package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
	"git.pcux.cn/xuans/Smail/utils/credential"
)

func writeRuntimeFixture(t *testing.T, root string, runtime RuntimeConfig) {
	t.Helper()
	directory, err := secureManagerConfigDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := marshalRuntimeFilesWithPluginSecrets(runtime, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := atomicfile.Write(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func validMySQLRuntimeConfig(t *testing.T, root string) RuntimeConfig {
	t.Helper()
	runtime := validRuntimeConfig()
	vault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := vault.Encrypt("initial-database-secret")
	if err != nil {
		t.Fatal(err)
	}
	runtime.Root.Database = DatabaseConfig{
		Type:     DatabaseTypeMySQL,
		Host:     "127.0.0.1",
		Port:     DefaultMySQLPort,
		Username: "smail",
		Password: StoredSecret{Ciphertext: ciphertext, Version: vault.Version(), UpdatedAt: time.Now().UTC()},
		Name:     "smail",
	}
	return runtime
}

type cancelAfterFirstContext struct {
	done  chan struct{}
	calls int
}

func newCancelAfterFirstContext() *cancelAfterFirstContext {
	return &cancelAfterFirstContext{done: make(chan struct{})}
}

func (c *cancelAfterFirstContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (c *cancelAfterFirstContext) Done() <-chan struct{} {
	c.calls++
	if c.calls > 1 {
		select {
		case <-c.done:
		default:
			close(c.done)
		}
	}
	return c.done
}

func (c *cancelAfterFirstContext) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}

func (c *cancelAfterFirstContext) Value(any) any {
	return nil
}

func authorizedConfigActor() AuditActor {
	return AuditActor{UserID: "1", Role: "super_admin", SessionID: "test-session", RecentAuth: true, RecoveryCodeConfigured: true}
}

func mailSectionFromRuntime(runtime RuntimeConfig) MailSectionConfig {
	return MailSectionConfig{
		Meta:     runtime.SMTP.Meta,
		Listener: runtime.SMTP.Listener,
		Relay:    runtime.SMTP.Relay,
		IMAP:     runtime.Email.IMAP,
		POP3:     runtime.Email.POP3,
		Policy:   runtime.Email.Policy,
	}
}

type rejectingRuntimeSnapshotStore struct {
	current RuntimeConfig
	err     error
}

func (s *rejectingRuntimeSnapshotStore) Validate(RuntimeConfig) error {
	return s.err
}

func (s *rejectingRuntimeSnapshotStore) Publish(RuntimeConfig) error {
	return s.err
}

func (s *rejectingRuntimeSnapshotStore) Snapshot() (RuntimeConfig, error) {
	return cloneRuntimeConfig(s.current), nil
}

func TestConfigLoaderLoadsFixedPartitionsAndRejectsDigestMismatch(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	loader := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	runtime, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Generation != 1 || runtime.SMTP.Meta.Generation != 1 || runtime.Email.Meta.Generation != 1 {
		t.Fatalf("unexpected runtime generation: %+v", runtime)
	}
	sectionPath := filepath.Join(root, "config", "email.json")
	data, err := os.ReadFile(sectionPath)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] = ' '
	if err := os.WriteFile(sectionPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Load(); !errors.Is(err, ErrConfigDigestMismatch) {
		t.Fatalf("load error = %v, want digest mismatch", err)
	}
}

func TestConfigLoaderRejectsMissingPartitionAndGenerationMismatch(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	if err := os.Remove(filepath.Join(root, "config", "ssl.json")); err != nil {
		t.Fatal(err)
	}
	loader := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if _, err := loader.Load(); !errors.Is(err, ErrConfigSectionMissing) {
		t.Fatalf("load error = %v, want missing section", err)
	}

	root = t.TempDir()
	runtime := validRuntimeConfig()
	runtime.Generation = 2
	runtime.Root.Generation = 2
	runtime.SMTP.Meta.Generation = 2
	runtime.Email.Meta.Generation = 2
	runtime.SSL.Meta.Generation = 2
	runtime.Plugins.Meta.Generation = 2
	writeRuntimeFixture(t, root, runtime)
	data, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rootConfig RootConfig
	if err := ParseStrict(data, &rootConfig); err != nil {
		t.Fatal(err)
	}
	rootConfig.Sections[SectionEmail] = SectionRef{File: "email.json", Schema: CurrentSectionSchema, Generation: 1, SHA256: rootConfig.Sections[SectionEmail].SHA256}
	data, err = json.Marshal(rootConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load(); !errors.Is(err, ErrSectionGenerationMismatch) {
		t.Fatalf("load error = %v, want generation mismatch", err)
	}
}

func TestConfigManagerRejectsUnavailableRuntimeSecretsAtStartup(t *testing.T) {
	root := t.TempDir()
	runtime := validMySQLRuntimeConfig(t, root)
	otherVault, err := credential.NewVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreignCiphertext, err := otherVault.Encrypt("foreign-database-secret")
	if err != nil {
		t.Fatal(err)
	}
	runtime.Root.Database.Password.Ciphertext = foreignCiphertext
	writeRuntimeFixture(t, root, runtime)

	_, err = NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("manager construction error = %v, want unavailable secret", err)
	}
}

func TestConfigManagerLoadRejectsUnavailableRuntimeSecretsWithoutChangingSnapshot(t *testing.T) {
	root := t.TempDir()
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	writeRuntimeFixture(t, root, validMySQLRuntimeConfig(t, root))
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	current := manager.Snapshot()
	otherVault, err := credential.NewVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreignCiphertext, err := otherVault.Encrypt("foreign-database-secret")
	if err != nil {
		t.Fatal(err)
	}
	current.Root.Database.Password.Ciphertext = foreignCiphertext
	writeRuntimeFixture(t, root, current)

	if err := manager.Load(context.Background()); !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("load error = %v, want unavailable secret", err)
	}
	loaded := manager.Snapshot()
	if loaded.Root.Database.Password.Ciphertext == foreignCiphertext {
		t.Fatal("rejected load changed the active snapshot")
	}
}

func TestConfigManagerLoadHonorsCancellationAfterReadingConfiguration(t *testing.T) {
	root := t.TempDir()
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	current := manager.Snapshot()
	ctx := newCancelAfterFirstContext()

	if err := manager.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("load error = %v, want context canceled", err)
	}
	loaded := manager.Snapshot()
	if loaded.Generation != current.Generation {
		t.Fatalf("canceled load changed active generation: got %d, want %d", loaded.Generation, current.Generation)
	}
}

func TestConfigManagerUpdateHonorsCancellationBeforeCreatingPendingState(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(manager.Snapshot().Email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Update(newCancelAfterFirstContext(), UpdateRequest{
		Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor(),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled update error = %v, want context canceled", err)
	}
	status := manager.Status()
	if len(status.PendingRestart) != 0 {
		t.Fatalf("canceled update left pending restart state: %+v", status.PendingRestart)
	}
	if manager.Snapshot().Generation != 1 {
		t.Fatalf("canceled update changed active generation: %d", manager.Snapshot().Generation)
	}
}

func TestConfigManagerApplyHonorsCancellationBeforePersistence(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(manager.Snapshot().Email)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{
		Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(newCancelAfterFirstContext(), updated.Generation); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled apply error = %v, want context canceled", err)
	}
	if manager.Snapshot().Generation != 1 {
		t.Fatalf("canceled apply changed active generation: %d", manager.Snapshot().Generation)
	}
	status := manager.Status()
	if len(status.PendingRestart) != 1 || status.PendingRestart[0] != ServiceEmail {
		t.Fatalf("canceled apply lost pending state: %+v", status.PendingRestart)
	}
}

func TestConfigManagerApplyPreflightsSnapshotBeforePersisting(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	current := manager.Snapshot()
	manager.snapshots = &rejectingRuntimeSnapshotStore{current: current, err: errors.New("snapshot validation rejected")}
	email := current.Email
	email.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(email)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{
		Section: SectionEmail, ExpectedGeneration: current.Generation, Payload: payload, Actor: authorizedConfigActor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); !errors.Is(err, manager.snapshots.(*rejectingRuntimeSnapshotStore).err) {
		t.Fatalf("apply error = %v, want snapshot validation error", err)
	}
	loaded, err := NewConfigLoader(root, options).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation != current.Generation || loaded.Email.Policy.MaxMessageBytes == 8<<20 {
		t.Fatalf("candidate was persisted before snapshot validation: %+v", loaded)
	}
}

func TestConfigManagerUpdateApplyUsesPendingGeneration(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	current := manager.Snapshot()
	email := current.Email
	email.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(email)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: current.Generation, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Pending || updated.Generation != current.Generation+1 {
		t.Fatalf("unexpected update result: %+v", updated)
	}
	if _, err := manager.Validate(context.Background(), ValidateRequest{Section: SectionEmail, ExpectedGeneration: current.Generation, Payload: payload}); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("second update error = %v, want generation conflict", err)
	}
	applied, err := manager.Apply(context.Background(), updated.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Generation != 2 || manager.Snapshot().Email.Policy.MaxMessageBytes != 8<<20 {
		t.Fatalf("unexpected applied result: %+v", applied)
	}
	if _, err := manager.Apply(context.Background(), applied.Generation); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("second apply error = %v, want generation conflict", err)
	}
}

func TestConfigManagerMailSectionRejectsNonStrictPayloadsAndDirectSecrets(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	mail := mailSectionFromRuntime(manager.Snapshot())
	payload, err := json.Marshal(mail)
	if err != nil {
		t.Fatal(err)
	}

	unknown := append([]byte(nil), payload[:len(payload)-1]...)
	unknown = append(unknown, []byte(`,"unknown":true}`)...)
	if _, err := manager.Validate(context.Background(), ValidateRequest{Section: SectionMail, Payload: unknown}); err == nil {
		t.Fatal("mail section accepted an unknown JSON field")
	}

	meta, err := json.Marshal(mail.Meta)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := append([]byte(`{"meta":`), meta...)
	duplicate = append(duplicate, ',')
	duplicate = append(duplicate, payload[1:]...)
	if _, err := manager.Validate(context.Background(), ValidateRequest{Section: SectionMail, Payload: duplicate}); err == nil {
		t.Fatal("mail section accepted a duplicate JSON field")
	}

	mail.Relay.Password = StoredSecret{Ciphertext: "enc:v1:direct-secret", Version: credential.CurrentVersion, UpdatedAt: time.Now().UTC()}
	directSecret, err := json.Marshal(mail)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Validate(context.Background(), ValidateRequest{Section: SectionMail, Payload: directSecret}); !errors.Is(err, ErrInvalidSecretChange) {
		t.Fatalf("direct relay secret error = %v, want invalid secret change", err)
	}
}

func TestConfigManagerMailSectionAppliesBothPartitionsWithoutMailFile(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	vault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := ApplySecretChange(vault, StoredSecret{}, SecretChange{
		Operation: SecretReplace,
		Value:     &SecretInput{Plaintext: "existing-relay-secret"},
	}, SecretPolicy{Required: true, MaxLength: 4096}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	runtime.SMTP.Relay = SMTPConfig{
		RelayHost:           "smtp.example.com",
		RelayPort:           587,
		Username:            "relay-user",
		Password:            stored,
		Security:            "starttls",
		From:                "no-reply@example.com",
		FromDomain:          "example.com",
		VerificationEnabled: true,
		LoginPolicy:         "new_device",
	}
	runtime.Email.Policy.LoginVerificationPolicy = "new_device"
	runtime.Email.Policy.ExternalRecoveryEnabled = true
	writeRuntimeFixture(t, root, runtime)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}

	mail := mailSectionFromRuntime(manager.Snapshot())
	mail.Relay.Password = StoredSecret{}
	mail.Listener = SMTPListenerConfig{Port: 2525, STARTTLSPort: 2587, TLSPort: 2465}
	mail.IMAP.TLSPort = 2993
	mail.POP3 = POP3ListenerConfig{Port: 2110, TLSPort: 2995}
	mail.Policy.MaxMessageBytes = 8 << 20
	mail.Policy.SpamFilterLevel = 2
	payload, err := json.Marshal(mail)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{
		Section: SectionMail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Candidate.SMTP.Relay.Password != stored {
		t.Fatal("mail candidate did not preserve the existing relay secret")
	}
	status := manager.Status()
	if len(status.PendingRestart) != 2 || status.PendingRestart[0] != ServiceSMTP || status.PendingRestart[1] != ServiceEmail {
		t.Fatalf("mail pending restart services = %+v, want smtp and email", status.PendingRestart)
	}
	if _, err := manager.ApplyPendingAuthorized(context.Background(), updated.Generation, "wrong-candidate", SectionMail, authorizedConfigActor()); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("wrong candidate ID error = %v, want generation conflict", err)
	}
	if _, err := manager.ApplyPendingAuthorized(context.Background(), updated.Generation, updated.CandidateID, SectionSMTP, authorizedConfigActor()); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("wrong section error = %v, want generation conflict", err)
	}
	if _, err := manager.ApplyPendingAuthorized(context.Background(), updated.Generation+1, updated.CandidateID, SectionMail, authorizedConfigActor()); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("wrong generation error = %v, want generation conflict", err)
	}
	if _, err := manager.ApplyPendingAuthorized(context.Background(), updated.Generation, updated.CandidateID, SectionMail, authorizedConfigActor()); err != nil {
		t.Fatal(err)
	}

	configDirectory := filepath.Join(root, "config")
	if _, err := os.Stat(filepath.Join(configDirectory, "mail.json")); !os.IsNotExist(err) {
		t.Fatalf("combined mail view was persisted as mail.json: %v", err)
	}
	for _, name := range []string{"smtp.json", "email.json"} {
		data, err := os.ReadFile(filepath.Join(configDirectory, name))
		if err != nil {
			t.Fatal(err)
		}
		var saved struct {
			Meta SectionMeta `json:"meta"`
		}
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Meta.Generation != 2 {
			t.Fatalf("%s 未更新至第二代配置", name)
		}
	}
	reloaded, err := NewConfigLoader(root, options).Load()
	if err != nil {
		t.Fatal(err)
	}
	wantPorts := MailPorts{SMTP: 2525, SMTPSTARTTLS: 2587, SMTPTLS: 2465, IMAP: 2993, POP3: 2110, POP3TLS: 2995}
	if got := ResolveMailPorts(reloaded.SMTP, reloaded.Email); got != wantPorts {
		t.Fatalf("reloaded mail ports = %+v, want %+v", got, wantPorts)
	}
	if reloaded.Email.Policy.MaxMessageBytes != 8<<20 || reloaded.Email.Policy.SpamFilterLevel != 2 {
		t.Fatalf("reloaded mail policy is incomplete: %+v", reloaded.Email.Policy)
	}
	if reloaded.SMTP.Relay.Password != stored {
		t.Fatal("reloaded relay secret changed without an explicit secret operation")
	}
}

func TestConfigManagerMailSectionAcceptsUnifiedSecurityModes(t *testing.T) {
	tests := []struct {
		name          string
		security      string
		relaySecurity string
		listener      SMTPListenerConfig
		imap          IMAPListenerConfig
		pop3          POP3ListenerConfig
		relayPort     int
		want          MailPorts
	}{
		{
			name:          "both",
			security:      MailSecurityBoth,
			relaySecurity: MailSecuritySTARTTLS,
			listener:      SMTPListenerConfig{Port: 25, STARTTLSPort: 587, TLSPort: 465, Security: MailSecurityBoth},
			imap:          IMAPListenerConfig{TLSPort: 993},
			pop3:          POP3ListenerConfig{Port: 110, TLSPort: 995},
			relayPort:     587,
			want:          MailPorts{SMTP: 25, SMTPSTARTTLS: 587, SMTPTLS: 465, IMAP: 993, POP3: 110, POP3TLS: 995},
		},
		{
			name:          "starttls",
			security:      MailSecuritySTARTTLS,
			relaySecurity: MailSecuritySTARTTLS,
			listener:      SMTPListenerConfig{Port: 25, STARTTLSPort: 587, Security: MailSecuritySTARTTLS},
			imap:          IMAPListenerConfig{TLSPort: 143},
			pop3:          POP3ListenerConfig{Port: 110},
			relayPort:     587,
			want:          MailPorts{SMTP: 25, SMTPSTARTTLS: 587, IMAP: 143, POP3: 110},
		},
		{
			name:          "implicit_tls",
			security:      MailSecurityImplicitTLS,
			relaySecurity: MailSecurityImplicitTLS,
			listener:      SMTPListenerConfig{Port: 25, TLSPort: 465, Security: MailSecurityImplicitTLS},
			imap:          IMAPListenerConfig{TLSPort: 993},
			pop3:          POP3ListenerConfig{TLSPort: 995},
			relayPort:     465,
			want:          MailPorts{SMTP: 25, SMTPTLS: 465, IMAP: 993, POP3TLS: 995},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeRuntimeFixture(t, root, validRuntimeConfig())
			options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
			manager, err := NewConfigManager(root, options)
			if err != nil {
				t.Fatal(err)
			}

			mail := mailSectionFromRuntime(manager.Snapshot())
			mail.Listener = test.listener
			mail.IMAP = test.imap
			mail.POP3 = test.pop3
			mail.Relay.RelayPort = test.relayPort
			mail.Relay.Security = test.relaySecurity
			mail.Relay.FromDomain = "example.com"
			mail.Relay.VerificationEnabled = true
			payload, err := json.Marshal(mail)
			if err != nil {
				t.Fatal(err)
			}

			updated, err := manager.Update(context.Background(), UpdateRequest{
				Section: SectionMail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor(),
			})
			if err != nil {
				t.Fatalf("update unified %s mail configuration: %v", test.security, err)
			}
			if got := ResolveMailPorts(updated.Candidate.SMTP, updated.Candidate.Email); got != test.want {
				t.Fatalf("candidate ports = %+v, want %+v", got, test.want)
			}
			if smtpRelayConfigured(updated.Candidate.SMTP.Relay) {
				t.Fatal("recommended relay defaults enabled the optional SMTP relay")
			}
			if _, err := manager.ApplyPendingAuthorized(context.Background(), updated.Generation, updated.CandidateID, SectionMail, authorizedConfigActor()); err != nil {
				t.Fatal(err)
			}

			reloaded, err := NewConfigLoader(root, options).Load()
			if err != nil {
				t.Fatal(err)
			}
			if got := ResolveMailPorts(reloaded.SMTP, reloaded.Email); got != test.want {
				t.Fatalf("reloaded ports = %+v, want %+v", got, test.want)
			}
			if ResolveMailSecurity(reloaded.SMTP.Listener) != test.security || NormalizeMailSecurity(reloaded.SMTP.Relay.Security) != test.relaySecurity {
				t.Fatalf("reloaded security modes are not unified: listener=%q relay=%q", reloaded.SMTP.Listener.Security, reloaded.SMTP.Relay.Security)
			}
		})
	}
}

func TestConfigManagerMailSectionSecretReplacementUsesVault(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	mail := mailSectionFromRuntime(manager.Snapshot())
	mail.Relay = SMTPConfig{
		RelayHost:  "smtp.example.com",
		RelayPort:  587,
		Username:   "relay-user",
		Security:   "starttls",
		From:       "no-reply@example.com",
		FromDomain: "example.com",
	}
	payload, err := json.Marshal(mail)
	if err != nil {
		t.Fatal(err)
	}
	request := UpdateRequest{
		Section: SectionMail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor(),
		SecretChanges: map[string]SecretChange{
			"relay.password": {Operation: SecretReplace, Value: &SecretInput{Plaintext: "new-relay-secret"}},
		},
	}
	if _, err := manager.Update(context.Background(), request); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("mail secret replacement without step-up error = %v, want step-up required", err)
	}
	request.Confirmation = StepUpConfirmation{Confirmed: true, Method: "recent_auth"}
	updated, err := manager.Update(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	stored := updated.Candidate.SMTP.Relay.Password
	if !stored.Configured() || stored.Ciphertext == "new-relay-secret" || !credential.IsEncrypted(stored.Ciphertext) {
		t.Fatalf("relay secret was not stored as managed ciphertext: %+v", stored)
	}
	plaintext, err := manager.vault.Decrypt(stored.Version, stored.Ciphertext)
	if err != nil || plaintext != "new-relay-secret" {
		t.Fatalf("relay secret cannot be recovered from the managed vault: value=%q error=%v", plaintext, err)
	}
}

func TestConfigManagerMailSectionPreservesFieldsAcrossPageSpecificEdits(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	runtime.SMTP.Listener = SMTPListenerConfig{Port: 2525, STARTTLSPort: 2587, TLSPort: 2465}
	runtime.Email.IMAP.TLSPort = 2993
	runtime.Email.POP3 = POP3ListenerConfig{Port: 2110, TLSPort: 2995}
	runtime.Email.Policy.MaxMessageBytes = 4 << 20
	writeRuntimeFixture(t, root, runtime)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}

	securityEdit := mailSectionFromRuntime(manager.Snapshot())
	securityEdit.Relay.VerificationEnabled = true
	securityEdit.Relay.LoginPolicy = "new_device"
	securityEdit.Policy.ExternalRecoveryEnabled = true
	securityPayload, err := json.Marshal(securityEdit)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{
		Section: SectionMail, ExpectedGeneration: 1, Payload: securityPayload, Actor: authorizedConfigActor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ApplyPendingAuthorized(context.Background(), updated.Generation, updated.CandidateID, SectionMail, authorizedConfigActor()); err != nil {
		t.Fatal(err)
	}
	afterSecurity := manager.Snapshot()
	wantPorts := MailPorts{SMTP: 2525, SMTPSTARTTLS: 2587, SMTPTLS: 2465, IMAP: 2993, POP3: 2110, POP3TLS: 2995}
	if got := ResolveMailPorts(afterSecurity.SMTP, afterSecurity.Email); got != wantPorts || afterSecurity.Email.Policy.MaxMessageBytes != 4<<20 {
		t.Fatal("security policy edit overwrote mail listener or policy fields")
	}

	mailEdit := mailSectionFromRuntime(afterSecurity)
	mailEdit.Policy.MaxMessageBytes = 12 << 20
	mailPayload, err := json.Marshal(mailEdit)
	if err != nil {
		t.Fatal(err)
	}
	updated, err = manager.Update(context.Background(), UpdateRequest{
		Section: SectionMail, ExpectedGeneration: 2, Payload: mailPayload, Actor: authorizedConfigActor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ApplyPendingAuthorized(context.Background(), updated.Generation, updated.CandidateID, SectionMail, authorizedConfigActor()); err != nil {
		t.Fatal(err)
	}
	afterMail := manager.Snapshot()
	if !afterMail.SMTP.Relay.VerificationEnabled || afterMail.SMTP.Relay.LoginPolicy != "new_device" ||
		afterMail.Email.Policy.LoginVerificationPolicy != "new_device" || !afterMail.Email.Policy.ExternalRecoveryEnabled {
		t.Fatal("mail page edit overwrote security policy fields")
	}
}

func TestTransactionRecoveryRestoresOldGenerationAfterSectionFailure(t *testing.T) {
	root := t.TempDir()
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	candidate := cloneRuntimeConfig(current)
	candidate.Generation = 2
	candidate.Root.Generation = 2
	candidate.SetupState = candidate.Root.SetupState
	candidate.SMTP.Meta.Generation = 2
	candidate.Email.Meta.Generation = 2
	candidate.SSL.Meta.Generation = 2
	candidate.Plugins.Meta.Generation = 2
	candidate.Email.Policy.MaxMessageBytes = 9 << 20
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).WithHooks(TransactionHooks{
		BeforePhase: func(phase TransactionPhase) error {
			if phase == TransactionSectionsCommitted {
				return errors.New("injected interruption")
			}
			return nil
		},
	})
	if err := store.Commit(current, candidate); err == nil {
		t.Fatal("transaction unexpectedly succeeded")
	}
	if _, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation != current.Generation || loaded.Email.Policy.MaxMessageBytes == 9<<20 {
		t.Fatalf("recovery did not restore old generation: %+v", loaded)
	}
}

func TestConfigManagerRollbackRestoresBackupAsNewGeneration(t *testing.T) {
	root := t.TempDir()
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	updatedEmail := manager.Snapshot().Email
	updatedEmail.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(updatedEmail)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	if manager.Snapshot().Email.Policy.MaxMessageBytes != 8<<20 {
		t.Fatal("updated configuration was not published")
	}
	rolledBack, err := manager.RollbackAuthorized(context.Background(), 1, authorizedConfigActor(), StepUpConfirmation{Confirmed: true, Method: "recent_auth"})
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Generation != 3 {
		t.Fatalf("rollback generation = %d, want 3", rolledBack.Generation)
	}
	if rolledBack.Snapshot.Email.Policy.MaxMessageBytes == 8<<20 {
		t.Fatal("rollback did not restore the selected backup")
	}
	loaded, err := NewConfigLoader(root, options).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation != 3 || loaded.Email.Policy.MaxMessageBytes == 8<<20 {
		t.Fatalf("persisted rollback is invalid: %+v", loaded)
	}
}

func TestConfigBackupRejectsTamperedPartition(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	writeRuntimeFixture(t, root, runtime)
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if _, err := store.CreateBackup(runtime); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "data", "backups-v2"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("backup entries = %v, err = %v", entries, err)
	}
	path := filepath.Join(root, "data", "backups-v2", entries[0].Name(), "files", "email.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadBackup(1); !errors.Is(err, ErrConfigDigestMismatch) {
		t.Fatalf("tampered backup error = %v, want digest mismatch", err)
	}
}

func TestConfigManagerRejectsUndecryptableRollbackBackup(t *testing.T) {
	root := t.TempDir()
	current := validMySQLRuntimeConfig(t, root)
	writeRuntimeFixture(t, root, current)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	updatedEmail := manager.Snapshot().Email
	updatedEmail.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(updatedEmail)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{
		Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(root, "data", "backups-v2"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("backup entries = %v, err = %v", entries, err)
	}
	backupDirectory := filepath.Join(root, "data", "backups-v2", entries[0].Name())
	rootPath := filepath.Join(backupDirectory, "files", "config.json")
	rootData, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	var backupRoot RootConfig
	if err := ParseStrict(rootData, &backupRoot); err != nil {
		t.Fatal(err)
	}
	foreignRoot := t.TempDir()
	foreignVault, err := credential.NewVault(foreignRoot)
	if err != nil {
		t.Fatal(err)
	}
	foreignCiphertext, err := foreignVault.Encrypt("foreign-database-secret")
	if err != nil {
		t.Fatal(err)
	}
	backupRoot.Database.Password.Ciphertext = foreignCiphertext
	rootData, err = json.Marshal(backupRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootPath, rootData, 0600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(backupDirectory, "backup.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest ConfigBackupManifest
	if err := ParseStrict(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Files["config.json"] = SectionDigest(rootData)
	manifestData, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifestData, 0600); err != nil {
		t.Fatal(err)
	}

	actor := authorizedConfigActor()
	if _, err := manager.PreviewRollback(context.Background(), 1, actor); !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("undecryptable backup preview error = %v, want secret unavailable", err)
	}
	if _, err := manager.RollbackAuthorized(context.Background(), 1, actor, StepUpConfirmation{Confirmed: true, Method: "webauthn"}); !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("undecryptable backup rollback error = %v, want secret unavailable", err)
	}
	if manager.Snapshot().Generation != 2 {
		t.Fatalf("failed rollback changed active generation to %d", manager.Snapshot().Generation)
	}
}
func TestConfigBackupRetentionKeepsFourRecentBackups(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	writeRuntimeFixture(t, root, runtime)
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	for generation := uint64(1); generation <= 5; generation++ {
		runtime.Generation = generation
		setCandidateGeneration(&runtime, generation)
		if _, err := store.CreateBackup(runtime); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "data", "backups-v2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("backup count = %d, want 4", len(entries))
	}
}

func TestTransactionRecoveryKeepsOrRestoresByPhase(t *testing.T) {
	phases := []struct {
		name    string
		phase   TransactionPhase
		wantNew bool
	}{
		{name: "prepared", phase: TransactionPrepared, wantNew: false},
		{name: "sections_committed", phase: TransactionSectionsCommitted, wantNew: false},
		{name: "root_committed", phase: TransactionRootCommitted, wantNew: true},
		{name: "verified", phase: TransactionVerified, wantNew: true},
	}
	for _, test := range phases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			current := validRuntimeConfig()
			writeRuntimeFixture(t, root, current)
			candidate := cloneRuntimeConfig(current)
			setCandidateGeneration(&candidate, 2)
			candidate.Email.Policy.MaxMessageBytes = 9 << 20
			store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).WithHooks(TransactionHooks{
				BeforePhase: func(phase TransactionPhase) error {
					if phase == test.phase {
						return errors.New("injected interruption")
					}
					return nil
				},
			})
			if err := store.Commit(current, candidate); err == nil {
				t.Fatal("transaction unexpectedly succeeded")
			}
			if _, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}); err != nil {
				t.Fatal(err)
			}
			loaded, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load()
			if err != nil {
				t.Fatal(err)
			}
			gotNew := loaded.Email.Policy.MaxMessageBytes == 9<<20
			if gotNew != test.wantNew {
				t.Fatalf("recovery new=%v, want %v; generation=%d", gotNew, test.wantNew, loaded.Generation)
			}
		})
	}
}

func TestConfigManagerSecretChangesRequireStepUpAndPreserveStoredSecret(t *testing.T) {
	root := t.TempDir()
	runtime := validMySQLRuntimeConfig(t, root)
	writeRuntimeFixture(t, root, runtime)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	rootValue := manager.Snapshot().Root
	rootValue.Database.Password = StoredSecret{}
	rootPayload, err := json.Marshal(rootValue)
	if err != nil {
		t.Fatal(err)
	}
	request := UpdateRequest{
		Section:            SectionRoot,
		ExpectedGeneration: 1,
		Payload:            rootPayload,
		SecretChanges: map[string]SecretChange{
			"database.password": {Operation: SecretReplace, Value: &SecretInput{Plaintext: "database-secret"}},
		},
		Actor: authorizedConfigActor(),
	}
	if _, err := manager.Update(context.Background(), request); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("replace without step-up error = %v, want step-up required", err)
	}
	request.Confirmation = StepUpConfirmation{Confirmed: true, Method: "recent_auth"}
	updated, err := manager.Update(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Candidate.Root.Database.Password.Configured() {
		t.Fatal("replacement did not configure database secret")
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	stored := manager.Snapshot().Root.Database.Password
	rootValue = manager.Snapshot().Root
	rootValue.Database.Password = StoredSecret{}
	rootValue.Database.Name = "next"
	payload, err := json.Marshal(rootValue)
	if err != nil {
		t.Fatal(err)
	}
	request = UpdateRequest{Section: SectionRoot, ExpectedGeneration: 2, Payload: payload, Actor: authorizedConfigActor()}
	updated, err = manager.Update(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Candidate.Root.Database.Password != stored {
		t.Fatalf("stored secret changed without explicit secret operation: got %+v want %+v", updated.Candidate.Root.Database.Password, stored)
	}
}

func TestConfigManagerSecretClearRequiresExplicitConfirmation(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	runtime.SMTP.Relay = SMTPConfig{
		RelayHost:  "smtp.example.com",
		RelayPort:  587,
		Security:   "starttls",
		From:       "no-reply@example.com",
		FromDomain: "example.com",
	}
	writeRuntimeFixture(t, root, runtime)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	smtp := manager.Snapshot().SMTP
	smtp.Relay.Username = "relay-user"
	payload, err := json.Marshal(smtp)
	if err != nil {
		t.Fatal(err)
	}
	replaceRequest := UpdateRequest{
		Section: SectionSMTP, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor(),
		SecretChanges: map[string]SecretChange{"relay.password": {Operation: SecretReplace, Value: &SecretInput{Plaintext: "relay-secret"}}},
		Confirmation:  StepUpConfirmation{Confirmed: true, Method: "recent_auth"},
	}
	updated, err := manager.Update(context.Background(), replaceRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	smtp = manager.Snapshot().SMTP
	smtp.Relay.Username = ""
	smtp.Relay.Password = StoredSecret{}
	payload, err = json.Marshal(smtp)
	if err != nil {
		t.Fatal(err)
	}
	clearRequest := UpdateRequest{
		Section: SectionSMTP, ExpectedGeneration: 2, Payload: payload, Actor: authorizedConfigActor(),
		SecretChanges: map[string]SecretChange{"relay.password": {Operation: SecretClear, Confirmed: true}},
	}
	if _, err := manager.Update(context.Background(), clearRequest); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("clear without step-up error = %v, want step-up required", err)
	}
	clearRequest.Confirmation = StepUpConfirmation{Confirmed: true, Method: "webauthn"}
	updated, err = manager.Update(context.Background(), clearRequest)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Candidate.SMTP.Relay.Password.Configured() {
		t.Fatal("explicit clear left the relay secret configured")
	}
}

func TestConfigManagerSecretChangesUseVaultAndRejectUnknownFields(t *testing.T) {
	root := t.TempDir()
	runtime := validMySQLRuntimeConfig(t, root)
	writeRuntimeFixture(t, root, runtime)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	rootValue := manager.Snapshot().Root
	rootValue.Database.Password = StoredSecret{}
	rootPayload, err := json.Marshal(rootValue)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Update(context.Background(), UpdateRequest{
		Section:            SectionRoot,
		ExpectedGeneration: 1,
		Payload:            rootPayload,
		SecretChanges: map[string]SecretChange{
			"database.password": {Operation: SecretReplace, Value: &SecretInput{Plaintext: "database-secret"}},
		},
		Actor:        authorizedConfigActor(),
		Confirmation: StepUpConfirmation{Confirmed: true, Method: "recent_auth"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored := result.Candidate.Root.Database.Password
	if !stored.Configured() || stored.Ciphertext == "database-secret" {
		t.Fatalf("secret was not encrypted: %+v", stored)
	}
	if _, err := manager.Validate(context.Background(), ValidateRequest{
		Section:       SectionRoot,
		Payload:       rootPayload,
		SecretChanges: map[string]SecretChange{"database.unknown": {Operation: SecretKeep}},
	}); !errors.Is(err, ErrInvalidSecretChange) {
		t.Fatalf("unknown secret field error = %v, want invalid secret change", err)
	}
}

func TestConfigManagerRejectsPluginSecretWrites(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	writeRuntimeFixture(t, root, runtime)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}

	plugins := manager.Snapshot().Plugins
	payload, err := json.Marshal(plugins)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Validate(context.Background(), ValidateRequest{
		Section:       SectionPlugins,
		Payload:       payload,
		SecretChanges: map[string]SecretChange{"plugins.telegram_push.secret": {Operation: SecretReplace, Value: &SecretInput{Plaintext: "plugin-secret"}}},
	}); !errors.Is(err, ErrInvalidSecretChange) {
		t.Fatalf("plugin secret update error = %v, want invalid secret change", err)
	}

	plugin := plugins.Plugins["telegram_push"]
	plugin.Secret = StoredSecret{Ciphertext: "smail:v1:encrypted", Version: credential.CurrentVersion, UpdatedAt: time.Now().UTC()}
	plugins.Plugins["telegram_push"] = plugin
	payload, err = json.Marshal(plugins)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Validate(context.Background(), ValidateRequest{Section: SectionPlugins, Payload: payload}); !errors.Is(err, ErrInvalidSecretChange) {
		t.Fatalf("direct plugin secret error = %v, want invalid secret change", err)
	}
}

func TestConfigManagerMigratesLegacyPluginSecrets(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	vault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := ApplySecretChange(vault, StoredSecret{}, SecretChange{Operation: SecretReplace, Value: &SecretInput{Plaintext: "legacy-token"}}, SecretPolicy{Required: true, MaxLength: 4096}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plugin := runtime.Plugins.Plugins["telegram_push"]
	plugin.Secret = stored
	runtime.Plugins.Plugins["telegram_push"] = plugin
	writeRuntimeFixture(t, root, runtime)

	writer := &MemoryAuditWriter{}
	manager, err := NewConfigManagerWithSecurity(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}, ConfigSecurity{Audit: writer})
	if err != nil {
		t.Fatal(err)
	}
	var imported []LegacyPluginSecret
	migrated, err := manager.MigrateLegacyPluginSecrets(context.Background(), func(_ context.Context, values []LegacyPluginSecret) error {
		imported = append([]LegacyPluginSecret(nil), values...)
		return nil
	})
	if err != nil || !migrated {
		t.Fatalf("migrate legacy plugin secrets = %v, %v", migrated, err)
	}
	if len(imported) != 1 || imported[0].PluginID != "telegram_push" || imported[0].FieldName != "botToken" || imported[0].Plaintext != "legacy-token" {
		t.Fatalf("imported legacy values = %+v", imported)
	}
	if manager.Snapshot().Generation != 2 || manager.Snapshot().Plugins.Plugins["telegram_push"].Secret.Configured() {
		t.Fatalf("migrated snapshot = %+v", manager.Snapshot())
	}
	pluginData, err := os.ReadFile(filepath.Join(root, "config", "plugins.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(pluginData) == "" || containsAny(string(pluginData), "legacy-token", "ciphertext", "\"secret\"") {
		t.Fatalf("plugins.json retained legacy secret material: %s", pluginData)
	}
	events := writer.Events()
	if len(events) != 1 || events[0].Operation != ConfigOperationMigration || events[0].Section != SectionPlugins {
		t.Fatalf("migration audit events = %+v", events)
	}
	migrated, err = manager.MigrateLegacyPluginSecrets(context.Background(), func(_ context.Context, values []LegacyPluginSecret) error {
		t.Fatalf("unexpected repeated importer invocation: %+v", values)
		return nil
	})
	if err != nil || migrated {
		t.Fatalf("repeat migration = %v, %v", migrated, err)
	}
}

func TestConfigManagerRetainsLegacySecretWhenImportFails(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	vault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := ApplySecretChange(vault, StoredSecret{}, SecretChange{Operation: SecretReplace, Value: &SecretInput{Plaintext: "legacy-token"}}, SecretPolicy{Required: true, MaxLength: 4096}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plugin := runtime.Plugins.Plugins["telegram_push"]
	plugin.Secret = stored
	runtime.Plugins.Plugins["telegram_push"] = plugin
	writeRuntimeFixture(t, root, runtime)
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.MigrateLegacyPluginSecrets(context.Background(), func(_ context.Context, _ []LegacyPluginSecret) error {
		return errors.New("database unavailable")
	}); err == nil {
		t.Fatal("legacy import failure unexpectedly succeeded")
	}
	if manager.Snapshot().Generation != 1 || !manager.Snapshot().Plugins.Plugins["telegram_push"].Secret.Configured() {
		t.Fatal("failed import altered the in-memory legacy configuration")
	}
	pluginData, readErr := os.ReadFile(filepath.Join(root, "config", "plugins.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !containsAny(string(pluginData), "ciphertext", "\"secret\"") {
		t.Fatal("failed import removed compatibility secret from plugins.json")
	}
}

func TestConfigManagerLegacySecretMigrationHonorsCancellationAfterImport(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	vault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := ApplySecretChange(vault, StoredSecret{}, SecretChange{Operation: SecretReplace, Value: &SecretInput{Plaintext: "legacy-token"}}, SecretPolicy{Required: true, MaxLength: 4096}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plugin := runtime.Plugins.Plugins["telegram_push"]
	plugin.Secret = stored
	runtime.Plugins.Plugins["telegram_push"] = plugin
	writeRuntimeFixture(t, root, runtime)
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if migrated, err := manager.MigrateLegacyPluginSecrets(ctx, func(context.Context, []LegacyPluginSecret) error {
		cancel()
		return nil
	}); !errors.Is(err, context.Canceled) || migrated {
		t.Fatalf("canceled legacy migration = %v, %v", migrated, err)
	}
	if manager.Snapshot().Generation != 1 || !manager.Snapshot().Plugins.Plugins["telegram_push"].Secret.Configured() {
		t.Fatal("canceled legacy migration altered the active configuration")
	}
}

func containsAny(value string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func findTransactionDirectory(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "data", "transaction"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("transaction entries = %+v", entries)
	}
	return filepath.Join(root, "data", "transaction", entries[0].Name())
}

func TestTransactionRecoveryRejectsTamperedArtifacts(t *testing.T) {
	for _, test := range []struct {
		name string
		dir  string
	}{
		{name: "candidate", dir: "candidate"},
		{name: "original", dir: "original"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			current := validRuntimeConfig()
			writeRuntimeFixture(t, root, current)
			candidate := cloneRuntimeConfig(current)
			setCandidateGeneration(&candidate, 2)
			store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).WithHooks(TransactionHooks{
				BeforePhase: func(phase TransactionPhase) error {
					if phase == TransactionPrepared {
						return errors.New("injected interruption")
					}
					return nil
				},
			})
			if err := store.Commit(current, candidate); err == nil {
				t.Fatal("transaction unexpectedly succeeded")
			}
			transactionDirectory := findTransactionDirectory(t, root)
			path := filepath.Join(transactionDirectory, test.dir, "email.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data[len(data)-1] ^= 1
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).Recover(); !errors.Is(err, ErrConfigDigestMismatch) {
				t.Fatalf("tampered %s recovery error = %v, want digest mismatch", test.dir, err)
			}
		})
	}
}

func TestTransactionRecoveryRejectsArtifactSymlink(t *testing.T) {
	root := t.TempDir()
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	candidate := cloneRuntimeConfig(current)
	setCandidateGeneration(&candidate, 2)
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).WithHooks(TransactionHooks{
		BeforePhase: func(phase TransactionPhase) error {
			if phase == TransactionPrepared {
				return errors.New("injected interruption")
			}
			return nil
		},
	})
	if err := store.Commit(current, candidate); err == nil {
		t.Fatal("transaction unexpectedly succeeded")
	}
	transactionDirectory := findTransactionDirectory(t, root)
	candidateDirectory := filepath.Join(transactionDirectory, "candidate")
	originalDirectory := filepath.Join(transactionDirectory, "original")
	if err := os.RemoveAll(candidateDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(originalDirectory, candidateDirectory); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if err := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).Recover(); !errors.Is(err, ErrTransactionInvalid) {
		t.Fatalf("symlink recovery error = %v, want invalid transaction", err)
	}
}

func TestTransactionRecoveryRestoresWhenCommittedRootIsDamaged(t *testing.T) {
	root := t.TempDir()
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	candidate := cloneRuntimeConfig(current)
	setCandidateGeneration(&candidate, 2)
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).WithHooks(TransactionHooks{
		BeforePhase: func(phase TransactionPhase) error {
			if phase == TransactionRootCommitted {
				return errors.New("injected interruption")
			}
			return nil
		},
	})
	if err := store.Commit(current, candidate); err == nil {
		t.Fatal("transaction unexpectedly succeeded")
	}
	path := filepath.Join(root, "config", "email.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).Recover(); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation != current.Generation || loaded.Email.Policy.MaxMessageBytes != current.Email.Policy.MaxMessageBytes {
		t.Fatalf("recovered runtime = %+v, want generation %d", loaded, current.Generation)
	}
}

func TestTransactionRecoveryRejectsInvalidTransactionID(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	directory, err := secureManagerConfigDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	transactions, err := secureTransactionDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(transactions, "invalid-id"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).Recover(); !errors.Is(err, ErrTransactionInvalid) {
		t.Fatalf("invalid transaction id error = %v, want invalid transaction", err)
	}
}

func TestConfigBackupScheduleUsesSevenDayInterval(t *testing.T) {
	root := t.TempDir()
	first := validRuntimeConfig()
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	firstAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path, created, err := store.CreateBackupIfDue(first, firstAt)
	if err != nil || !created || path == "" {
		t.Fatalf("first backup path=%q created=%v err=%v", path, created, err)
	}
	second := cloneRuntimeConfig(first)
	setCandidateGeneration(&second, 2)
	beforeDue, created, err := store.CreateBackupIfDue(second, firstAt.Add(7*24*time.Hour-time.Nanosecond))
	if err != nil || created || beforeDue != path {
		t.Fatalf("before due path=%q created=%v err=%v, want existing backup", beforeDue, created, err)
	}
	atDue, created, err := store.CreateBackupIfDue(second, firstAt.Add(7*24*time.Hour))
	if err != nil || !created || atDue == path {
		t.Fatalf("at due path=%q created=%v err=%v, want new backup", atDue, created, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "data", "backups-v2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("scheduled backup count=%d, want 2", len(entries))
	}
}

func TestConfigBackupScheduleCreatesSameGenerationBackupWhenDue(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	firstAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	firstPath, created, err := store.CreateBackupIfDue(runtime, firstAt)
	if err != nil || !created || firstPath == "" {
		t.Fatalf("first backup path=%q created=%v err=%v", firstPath, created, err)
	}
	secondPath, created, err := store.CreateBackupIfDue(runtime, firstAt.Add(configBackupInterval))
	if err != nil || !created || secondPath == "" || secondPath == firstPath {
		t.Fatalf("due backup path=%q created=%v err=%v, want a new backup", secondPath, created, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "data", "backups-v2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("same generation backup count=%d, want 2", len(entries))
	}
}

func TestConfigBackupListingAndLoadingIgnoreTemporaryDirectories(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if _, err := store.CreateBackup(runtime); err != nil {
		t.Fatal(err)
	}
	backupDirectory := filepath.Join(root, "data", "backups-v2")
	if err := os.Mkdir(filepath.Join(backupDirectory, ".interrupted.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Generation != runtime.Generation {
		t.Fatalf("backup list = %+v, want generation %d", items, runtime.Generation)
	}
	loaded, err := store.LoadBackup(runtime.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation != runtime.Generation {
		t.Fatalf("loaded generation=%d, want %d", loaded.Generation, runtime.Generation)
	}
}

func TestConfigBackupCleanupKeepsOnlyExpiredValidBackup(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := store.createBackupAt(runtime, createdAt); err != nil {
		t.Fatal(err)
	}
	backupDirectory := filepath.Join(root, "data", "backups-v2")
	if err := cleanupConfigBackups(backupDirectory, createdAt.Add(configBackupMaxAge+time.Nanosecond), store.validation); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(backupDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("backup count=%d, want the only valid backup retained", len(entries))
	}
}
func TestConfigBackupRetentionHonorsThirtyDayBoundary(t *testing.T) {
	root := t.TempDir()
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := validRuntimeConfig()
	if _, err := store.createBackupAt(first, base); err != nil {
		t.Fatal(err)
	}
	second := cloneRuntimeConfig(first)
	setCandidateGeneration(&second, 2)
	if _, err := store.createBackupAt(second, base.Add(configBackupMaxAge)); err != nil {
		t.Fatal(err)
	}
	backupDirectory := filepath.Join(root, "data", "backups-v2")
	entries, err := os.ReadDir(backupDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("backup count at exact age=%d, want 2", len(entries))
	}
	third := cloneRuntimeConfig(second)
	setCandidateGeneration(&third, 3)
	if _, err := store.createBackupAt(third, base.Add(configBackupMaxAge+time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(backupDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("backup count after expiration=%d, want 2", len(entries))
	}
	if _, err := store.LoadBackup(1); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("expired generation load error=%v, want no backup", err)
	}
	if _, err := store.LoadBackup(2); err != nil {
		t.Fatalf("boundary generation was removed: %v", err)
	}
}

func TestConfigBackupFailureDoesNotDeleteExistingValidBackup(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if _, err := store.CreateBackup(runtime); err != nil {
		t.Fatal(err)
	}
	invalid := cloneRuntimeConfig(runtime)
	invalid.Generation = 0
	if _, err := store.CreateBackup(invalid); err == nil {
		t.Fatal("invalid backup unexpectedly succeeded")
	}
	if _, err := store.LoadBackup(runtime.Generation); err != nil {
		t.Fatalf("existing valid backup was removed after failure: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "data", "backups-v2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("backup count after failed creation=%d, want 1", len(entries))
	}
}
func TestConfigManagerStatusReportsPendingRestartServices(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	current := manager.Snapshot()
	smtp := current.SMTP
	smtp.Listener.Port++
	payload, err := json.Marshal(smtp)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionSMTP, ExpectedGeneration: current.Generation, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	status := manager.Status()
	if len(status.PendingRestart) != 1 || status.PendingRestart[0] != ServiceSMTP {
		t.Fatalf("pending restart services = %+v, want smtp", status.PendingRestart)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	status = manager.Status()
	if len(status.PendingRestart) != 1 || status.PendingRestart[0] != ServiceSMTP {
		t.Fatalf("applied pending restart services = %+v, want smtp", status.PendingRestart)
	}
	if err := manager.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status = manager.Status(); len(status.PendingRestart) != 0 {
		t.Fatalf("loaded pending restart services = %+v, want empty", status.PendingRestart)
	}
}

type failingConfigAuditWriter struct{}

func (failingConfigAuditWriter) WriteConfigAudit(context.Context, ConfigAuditEvent) error {
	return errors.New("audit backend unavailable")
}

func TestConfigManagerRequiresActorForWrites(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(manager.Snapshot().Email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload}); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("missing actor error = %v, want authorization required", err)
	}
}

func TestConfigManagerRollbackTicketIsBoundAndSingleUse(t *testing.T) {
	root := t.TempDir()
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	updatedEmail := manager.Snapshot().Email
	updatedEmail.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(updatedEmail)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	actor := authorizedConfigActor()
	preview, err := manager.PreviewRollback(context.Background(), 1, actor)
	if err != nil {
		t.Fatal(err)
	}
	if preview.CurrentGeneration != 2 || preview.TargetGeneration != 1 || preview.CandidateGeneration != 3 {
		t.Fatalf("unexpected rollback preview: %+v", preview)
	}
	ticket, err := manager.IssueRollbackTicket(context.Background(), 1, actor, StepUpConfirmation{Confirmed: true, Method: "webauthn"})
	if err != nil {
		t.Fatal(err)
	}
	wrongActor := actor
	wrongActor.SessionID = "different-session"
	if _, err := manager.RollbackWithTicket(context.Background(), ticket, wrongActor); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("wrong session error = %v, want authorization required", err)
	}
	rolledBack, err := manager.RollbackWithTicket(context.Background(), ticket, actor)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Generation != 3 || rolledBack.Snapshot.Email.Policy.MaxMessageBytes == 8<<20 {
		t.Fatalf("rollback result = %+v", rolledBack)
	}
	if _, err := manager.RollbackWithTicket(context.Background(), ticket, actor); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("replayed ticket error = %v, want authorization required", err)
	}
}

func TestConfigManagerRollbackRequiresStepUpButNotRecoveryCodeForSuperAdmin(t *testing.T) {
	root := t.TempDir()
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	updatedEmail := manager.Snapshot().Email
	updatedEmail.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(updatedEmail)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	actor := authorizedConfigActor()
	actor.RecoveryCodeConfigured = false
	if _, err := manager.RollbackAuthorized(context.Background(), 1, actor, StepUpConfirmation{}); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("missing step-up error = %v, want step-up required", err)
	}
	if _, err := manager.RollbackAuthorized(context.Background(), 1, actor, StepUpConfirmation{Confirmed: true, Method: "webauthn"}); err != nil {
		t.Fatalf("rollback with WebAuthn confirmation failed: %v", err)
	}
}

func TestConfigManagerWritesRedactedAuditAndDegradesOnAuditFailure(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	writer := &MemoryAuditWriter{}
	manager, err := NewConfigManagerWithSecurity(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}, ConfigSecurity{Audit: writer})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(manager.Snapshot().Email)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	events := writer.Events()
	// 原样保存只记录操作，不能伪造业务字段发生变更。
	if len(events) != 1 || events[0].Section != SectionEmail || len(events[0].ChangedFields) != 0 || len(events[0].Changes) != 0 {
		t.Fatalf("unexpected audit events: %+v", events)
	}
	if events[0].Result != "success" || events[0].ToGeneration != 2 {
		t.Fatalf("unexpected audit event: %+v", events[0])
	}

	root = t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err = NewConfigManagerWithSecurity(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}, ConfigSecurity{Audit: failingConfigAuditWriter{}})
	if err != nil {
		t.Fatal(err)
	}
	payload, err = json.Marshal(manager.Snapshot().Email)
	if err != nil {
		t.Fatal(err)
	}
	updated, err = manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); !errors.Is(err, ErrAuditWrite) {
		t.Fatalf("audit failure error = %v, want audit write", err)
	}
	if manager.Status().SetupState != SetupStateDegraded {
		t.Fatalf("status after audit failure = %+v, want degraded", manager.Status())
	}
}

func TestConfigManagerSnapshotResultsAreIndependentCopies(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}

	current := manager.Snapshot()
	current.Root.Domains.Accepted[0] = "changed.example.com"
	current.Plugins.Plugins["telegram_push"].Settings[0] = '['
	if got := manager.Snapshot(); got.Root.Domains.Accepted[0] != "mail.example.com" || !json.Valid(got.Plugins.Plugins["telegram_push"].Settings) {
		t.Fatal("manager snapshot exposed active mutable data")
	}

	email := manager.Snapshot().Email
	email.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(email)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{
		Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated.Candidate.Email.Policy.MaxMessageBytes = 64 << 20
	if got := manager.Snapshot(); got.Email.Policy.MaxMessageBytes == 64<<20 {
		t.Fatal("update result exposed manager-owned pending data")
	}

	applied, err := manager.Apply(context.Background(), updated.Generation)
	if err != nil {
		t.Fatal(err)
	}
	applied.Snapshot.Email.Policy.MaxMessageBytes = 128 << 20
	if got := manager.Snapshot(); got.Email.Policy.MaxMessageBytes == 128<<20 {
		t.Fatal("apply result exposed manager-owned active data")
	}
}

type flakyRuntimeSnapshotStore struct {
	mu       sync.Mutex
	current  RuntimeConfig
	failures int
}

func (s *flakyRuntimeSnapshotStore) Validate(RuntimeConfig) error {
	return nil
}

func (s *flakyRuntimeSnapshotStore) Publish(candidate RuntimeConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures > 0 {
		s.failures--
		return ErrRuntimeSnapshotUnavailable
	}
	s.current = cloneRuntimeConfig(candidate)
	return nil
}

func (s *flakyRuntimeSnapshotStore) Snapshot() (RuntimeConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneRuntimeConfig(s.current), nil
}

func TestConfigManagerApplyCanRetryAfterSnapshotFailure(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	updatedEmail := manager.Snapshot().Email
	updatedEmail.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(updatedEmail)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{
		Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor(),
	})
	if err != nil {
		t.Fatal(err)
	}

	manager.snapshots = &flakyRuntimeSnapshotStore{current: manager.Snapshot(), failures: 1}
	if _, err := manager.Apply(context.Background(), updated.Generation); !errors.Is(err, ErrRuntimeSnapshotUnavailable) {
		t.Fatalf("first apply error = %v, want snapshot unavailable", err)
	}
	status := manager.Status()
	if status.SetupState != SetupStateDegraded || len(status.DegradedReasons) != 1 || status.DegradedReasons[0].Code != "RUNTIME_SNAPSHOT_UNAVAILABLE" {
		t.Fatalf("degraded status after snapshot failure = %+v", status)
	}
	if manager.Snapshot().Generation != 1 {
		t.Fatalf("runtime snapshot generation after failed publish = %d, want 1", manager.Snapshot().Generation)
	}
	persisted, err := NewConfigLoader(root, options).Load()
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Generation != 2 || persisted.Email.Policy.MaxMessageBytes != 8<<20 {
		t.Fatalf("persisted candidate after snapshot failure = %+v", persisted)
	}

	applied, err := manager.Apply(context.Background(), updated.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Generation != 2 || manager.Snapshot().Generation != 2 || manager.Snapshot().Email.Policy.MaxMessageBytes != 8<<20 {
		t.Fatalf("retried apply result = %+v, runtime = %+v", applied, manager.Snapshot())
	}
	if manager.Status().SetupState == SetupStateDegraded || len(manager.Status().DegradedReasons) != 0 {
		t.Fatalf("status was not recovered after retry: %+v", manager.Status())
	}
}

func TestConfigManagerRollbackTicketCanRetryAfterSnapshotFailure(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	updatedEmail := manager.Snapshot().Email
	updatedEmail.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(updatedEmail)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	manager.snapshots = &flakyRuntimeSnapshotStore{current: manager.Snapshot(), failures: 1}
	actor := authorizedConfigActor()
	ticket, err := manager.IssueRollbackTicket(context.Background(), 1, actor, StepUpConfirmation{Confirmed: true, Method: "webauthn"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RollbackWithTicket(context.Background(), ticket, actor); !errors.Is(err, ErrRuntimeSnapshotUnavailable) {
		t.Fatalf("first rollback error = %v, want snapshot unavailable", err)
	}
	stored, exists := manager.rollbackTickets[ticket]
	if !exists || stored.InFlight {
		t.Fatalf("rollback ticket state after retryable failure = %+v, exists=%v", stored, exists)
	}
	result, err := manager.RollbackWithTicket(context.Background(), ticket, actor)
	if err != nil {
		t.Fatal(err)
	}
	if result.Generation != 3 || result.Snapshot.Email.Policy.MaxMessageBytes == 8<<20 {
		t.Fatalf("retry rollback result = %+v", result)
	}
	if _, err := manager.RollbackWithTicket(context.Background(), ticket, actor); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("replayed retry ticket error = %v, want authorization required", err)
	}
}
