package config

import "testing"

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
