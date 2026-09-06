package config

import (
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/utils/credential"
)

type runtimeAccessSnapshotStore struct {
	runtime RuntimeConfig
}

func (s runtimeAccessSnapshotStore) Validate(RuntimeConfig) error {
	return nil
}

func (s runtimeAccessSnapshotStore) Publish(RuntimeConfig) error {
	return nil
}

func (s runtimeAccessSnapshotStore) Snapshot() (RuntimeConfig, error) {
	return cloneRuntimeConfig(s.runtime), nil
}

func TestCurrentConfigReturnsIndependentSnapshot(t *testing.T) {
	previousInstance := Instance
	defer func() {
		Instance = previousInstance
		PublishConfigSnapshot(previousInstance)
	}()

	Instance = &Config{
		Domain:          "example.com",
		Domains:         []string{"example.com"},
		PluginAllowList: []string{"spam_block"},
		Tables:          map[string]string{"users": "ready"},
		TablesInitData:  map[string]string{"users": "seed"},
	}
	PublishConfigSnapshot(Instance)

	first := CurrentConfig()
	if first == nil {
		t.Fatal("snapshot is nil")
	}
	first.Domains[0] = "changed.example.com"
	first.PluginAllowList[0] = "changed"
	first.Tables["users"] = "changed"
	first.TablesInitData["users"] = "changed"

	second := CurrentConfig()
	if second.Domains[0] != "example.com" || second.PluginAllowList[0] != "spam_block" {
		t.Fatalf("mutable slices leaked through snapshot: %+v", second)
	}
	if second.Tables["users"] != "ready" || second.TablesInitData["users"] != "seed" {
		t.Fatalf("mutable maps leaked through snapshot: %+v", second)
	}
}

func TestPublishConfigSnapshotClearsValue(t *testing.T) {
	PublishConfigSnapshot(&Config{Domain: "example.com"})
	PublishConfigSnapshot(nil)
	if got := CurrentConfig(); got != nil {
		t.Fatalf("expected cleared snapshot, got %+v", got)
	}
}

func TestCurrentMailPortsUsesSafeDefaultsWithoutRuntimeManager(t *testing.T) {
	previousManager := CurrentManager()
	defer SetRuntimeManager(previousManager)
	SetRuntimeManager(nil)

	if got := CurrentMailPorts(); got != (MailPorts{SMTP: 25, SMTPSTARTTLS: 587, SMTPTLS: 465, IMAP: 993, POP3: 110, POP3TLS: 995}) {
		t.Fatalf("mail ports = %+v, want safe defaults", got)
	}
}

func TestCurrentEmailPolicyReadsPartitionSnapshot(t *testing.T) {
	previousManager := CurrentManager()
	defer SetRuntimeManager(previousManager)

	runtime := validRuntimeConfig()
	runtime.Email.Policy = EmailConfig{
		SMTPPort:                2525,
		IMAPPort:                1143,
		POP3Port:                1110,
		MaxMessageBytes:         8 << 20,
		SpamFilterLevel:         2,
		LoginVerificationPolicy: "new_device",
		ExternalRecoveryEnabled: true,
	}
	SetRuntimeManager(&ConfigManager{snapshots: runtimeAccessSnapshotStore{runtime: runtime}})

	got := CurrentEmailPolicy()
	if got != runtime.Email.Policy {
		t.Fatalf("email policy = %+v, want partition snapshot", got)
	}
}

func TestCurrentEmailPolicyFallsBackToLegacyConfig(t *testing.T) {
	previousManager := CurrentManager()
	previousInstance := Instance
	defer func() {
		SetRuntimeManager(previousManager)
		Instance = previousInstance
		PublishConfigSnapshot(previousInstance)
	}()
	SetRuntimeManager(nil)

	Instance = &Config{
		SMTPPort:                2525,
		SpamFilterLevel:         2,
		LoginPolicy:             "new_device",
		ExternalRecoveryEnabled: true,
	}
	PublishConfigSnapshot(Instance)

	got := CurrentEmailPolicy()
	if got.SMTPPort != 2525 || got.IMAPPort != 0 || got.POP3Port != 0 || got.MaxMessageBytes != 0 || got.SpamFilterLevel != 2 || got.LoginVerificationPolicy != "new_device" || !got.ExternalRecoveryEnabled {
		t.Fatalf("legacy email policy = %+v, want mapped fallback", got)
	}
}

func TestCurrentMailPortsReadsEmailPolicySnapshot(t *testing.T) {
	previousManager := CurrentManager()
	defer SetRuntimeManager(previousManager)

	runtime := validRuntimeConfig()
	runtime.Email.Policy.SMTPPort = 2525
	runtime.Email.Policy.IMAPPort = 1143
	runtime.Email.Policy.POP3Port = 1110
	SetRuntimeManager(&ConfigManager{snapshots: runtimeAccessSnapshotStore{runtime: runtime}})

	if got := CurrentMailPorts(); got != (MailPorts{SMTP: 2525, SMTPSTARTTLS: 587, SMTPTLS: 465, IMAP: 993, POP3: 1110, POP3TLS: 995}) {
		t.Fatalf("mail ports = %+v, want policy ports", got)
	}
}

func TestCurrentMailPortsFallsBackForInvalidPolicyValues(t *testing.T) {
	previousManager := CurrentManager()
	defer SetRuntimeManager(previousManager)

	runtime := validRuntimeConfig()
	runtime.Email.Policy.SMTPPort = 0
	runtime.Email.Policy.IMAPPort = 65536
	runtime.Email.Policy.POP3Port = -1
	SetRuntimeManager(&ConfigManager{snapshots: runtimeAccessSnapshotStore{runtime: runtime}})

	if got := CurrentMailPorts(); got != (MailPorts{SMTP: 25, SMTPSTARTTLS: 587, SMTPTLS: 465, IMAP: 993, POP3: 110, POP3TLS: 995}) {
		t.Fatalf("mail ports = %+v, want defaults for invalid policy values", got)
	}
}

func TestCurrentMailPortsDoesNotReadLegacyRelayPort(t *testing.T) {
	previousManager := CurrentManager()
	defer SetRuntimeManager(previousManager)

	runtime := validRuntimeConfig()
	runtime.Root.SMTP.RelayPort = 2465
	runtime.Email.Policy = EmailConfig{}
	SetRuntimeManager(&ConfigManager{snapshots: runtimeAccessSnapshotStore{runtime: runtime}})

	if got := CurrentMailPorts(); got != (MailPorts{SMTP: 25, SMTPSTARTTLS: 587, SMTPTLS: 465, IMAP: 993, POP3: 110, POP3TLS: 995}) {
		t.Fatalf("mail ports = %+v, legacy relay port leaked into listener ports", got)
	}
}

func TestCurrentMailPortsUsesDefaultsForUnavailableSnapshot(t *testing.T) {
	previousManager := CurrentManager()
	defer SetRuntimeManager(previousManager)
	SetRuntimeManager(&ConfigManager{})

	if got := CurrentMailPorts(); got != (MailPorts{SMTP: 25, SMTPSTARTTLS: 587, SMTPTLS: 465, IMAP: 993, POP3: 110, POP3TLS: 995}) {
		t.Fatalf("mail ports = %+v, want defaults for unavailable snapshot", got)
	}
}

func TestCurrentMailDomainsReadsPartitionSnapshot(t *testing.T) {
	previousManager := CurrentManager()
	defer SetRuntimeManager(previousManager)

	runtime := validRuntimeConfig()
	runtime.Root.Domains = DomainConfig{
		Primary:  "partition.example.com",
		Accepted: []string{"partition.example.com", "alias.example.com"},
	}
	SetRuntimeManager(&ConfigManager{snapshots: runtimeAccessSnapshotStore{runtime: runtime}})

	got := CurrentMailDomains()
	if got.Primary != "partition.example.com" || len(got.Accepted) != 2 || got.Accepted[1] != "alias.example.com" {
		t.Fatalf("mail domains = %+v, want partition snapshot", got)
	}

	got.Accepted[0] = "changed.example.com"
	if runtime.Root.Domains.Accepted[0] != "partition.example.com" {
		t.Fatal("mail domain snapshot leaked mutable accepted domains")
	}
}

func TestCurrentMailDomainsFallsBackToLegacyConfig(t *testing.T) {
	previousManager := CurrentManager()
	previousInstance := Instance
	defer func() {
		SetRuntimeManager(previousManager)
		Instance = previousInstance
		PublishConfigSnapshot(previousInstance)
	}()
	SetRuntimeManager(nil)

	Instance = &Config{Domain: "legacy.example.com"}
	PublishConfigSnapshot(Instance)

	got := CurrentMailDomains()
	if got.Primary != "legacy.example.com" || len(got.Accepted) != 1 || got.Accepted[0] != got.Primary {
		t.Fatalf("legacy mail domains = %+v, want normalized fallback", got)
	}
}

func TestCurrentSMTPRuntimeSettingsReadsPartitionSnapshot(t *testing.T) {
	previousManager := CurrentManager()
	defer SetRuntimeManager(previousManager)

	runtime := validRuntimeConfig()
	runtime.SMTP.Relay = SMTPConfig{
		RelayHost:           "smtp.example.com",
		RelayPort:           587,
		Security:            "starttls",
		From:                "no-reply@example.com",
		FromDomain:          "example.com",
		VerificationEnabled: true,
		LoginPolicy:         "all",
	}
	runtime.Email.Policy.ExternalRecoveryEnabled = true
	SetRuntimeManager(&ConfigManager{snapshots: runtimeAccessSnapshotStore{runtime: runtime}})

	got, err := CurrentSMTPRuntimeSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Relay.RelayHost != "smtp.example.com" || got.Relay.RelayPort != 587 || got.Relay.From != "no-reply@example.com" {
		t.Fatalf("SMTP relay settings = %+v, want partition snapshot", got)
	}
	if !got.VerificationEnabled || !got.ExternalRecoveryEnabled {
		t.Fatalf("SMTP security flags = %+v, want enabled", got)
	}
}

func TestCurrentSMTPRuntimeSettingsOpensPartitionPassword(t *testing.T) {
	previousManager := CurrentManager()
	previousRoot := ROOT_PATH
	defer func() {
		SetRuntimeManager(previousManager)
		ROOT_PATH = previousRoot
	}()

	ROOT_PATH = t.TempDir()
	vault, err := credential.NewVault(ROOT_PATH)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := vault.Encrypt("relay-password")
	if err != nil {
		t.Fatal(err)
	}
	runtime := validRuntimeConfig()
	runtime.SMTP.Relay = SMTPConfig{
		RelayHost:           "smtp.example.com",
		RelayPort:           465,
		Username:            "relay-user",
		Password:            StoredSecret{Ciphertext: ciphertext, Version: vault.Version(), UpdatedAt: time.Now().UTC()},
		Security:            "tls",
		From:                "no-reply@example.com",
		FromDomain:          "example.com",
		VerificationEnabled: true,
		LoginPolicy:         "all",
	}
	SetRuntimeManager(&ConfigManager{snapshots: runtimeAccessSnapshotStore{runtime: runtime}})

	got, err := CurrentSMTPRuntimeSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "relay-password" {
		t.Fatalf("SMTP relay password = %q, want decrypted password", got.Password)
	}
}

func TestCurrentSMTPRuntimeSettingsFallsBackToLegacyConfig(t *testing.T) {
	previousManager := CurrentManager()
	previousInstance := Instance
	defer func() {
		SetRuntimeManager(previousManager)
		Instance = previousInstance
		PublishConfigSnapshot(previousInstance)
	}()
	SetRuntimeManager(nil)

	Instance = &Config{
		Domain:                  "example.com",
		SMTPHost:                "smtp.example.com",
		SMTPPort:                587,
		SMTPUsername:            "relay-user",
		SMTPPassword:            "relay-password",
		SMTPSecurity:            "starttls",
		SMTPFrom:                "no-reply@example.com",
		SMTPFromDomain:          "example.com",
		VerificationEnabled:     true,
		ExternalRecoveryEnabled: true,
		LoginPolicy:             "all",
	}
	PublishConfigSnapshot(Instance)

	got, err := CurrentSMTPRuntimeSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "relay-password" || got.Relay.Username != "relay-user" || got.Relay.From != "no-reply@example.com" {
		t.Fatalf("legacy SMTP settings = %+v, password=%q", got.Relay, got.Password)
	}
}
