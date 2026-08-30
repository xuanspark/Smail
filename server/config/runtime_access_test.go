package config

import "testing"

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

	if got := CurrentMailPorts(); got != (MailPorts{SMTP: 25, IMAP: 143, POP3: 110}) {
		t.Fatalf("mail ports = %+v, want safe defaults", got)
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

	if got := CurrentMailPorts(); got != (MailPorts{SMTP: 2525, IMAP: 1143, POP3: 1110}) {
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

	if got := CurrentMailPorts(); got != (MailPorts{SMTP: 25, IMAP: 143, POP3: 110}) {
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

	if got := CurrentMailPorts(); got != (MailPorts{SMTP: 25, IMAP: 143, POP3: 110}) {
		t.Fatalf("mail ports = %+v, legacy relay port leaked into listener ports", got)
	}
}

func TestCurrentMailPortsUsesDefaultsForUnavailableSnapshot(t *testing.T) {
	previousManager := CurrentManager()
	defer SetRuntimeManager(previousManager)
	SetRuntimeManager(&ConfigManager{})

	if got := CurrentMailPorts(); got != (MailPorts{SMTP: 25, IMAP: 143, POP3: 110}) {
		t.Fatalf("mail ports = %+v, want defaults for unavailable snapshot", got)
	}
}
