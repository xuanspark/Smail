package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/utils/credential"
)

func TestApplySecretChangeSemantics(t *testing.T) {
	vault, err := credential.NewVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 26, 2, 0, 0, 0, time.UTC)
	policy := SecretPolicy{Required: false, AllowClear: true, MaxLength: 128}

	stored, err := ApplySecretChange(vault, StoredSecret{}, SecretChange{
		Operation: SecretReplace,
		Value:     &SecretInput{Plaintext: "provider-secret"},
	}, policy, now)
	if err != nil || !stored.Configured() || stored.Version != credential.CurrentVersion || strings.Contains(stored.Ciphertext, "provider-secret") {
		t.Fatalf("replace failed: %#v, %v", stored.Status(), err)
	}
	plaintext, err := OpenStoredSecret(vault, stored)
	if err != nil || plaintext != "provider-secret" {
		t.Fatal("stored secret did not round trip")
	}

	kept, err := ApplySecretChange(vault, stored, SecretChange{Operation: SecretKeep}, policy, now.Add(time.Hour))
	if err != nil || kept != stored {
		t.Fatal("keep changed the stored secret")
	}

	cleared, err := ApplySecretChange(vault, stored, SecretChange{Operation: SecretClear, Confirmed: true}, policy, now.Add(2*time.Hour))
	if err != nil || cleared.Configured() {
		t.Fatal("clear did not remove an optional secret")
	}
}

func TestApplySecretChangeRejectsInvalidTransitions(t *testing.T) {
	vault, err := credential.NewVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input := &SecretInput{Plaintext: "provider-secret"}
	tests := []struct {
		name   string
		change SecretChange
		policy SecretPolicy
		want   error
	}{
		{name: "unknown", change: SecretChange{Operation: "rotate"}, policy: SecretPolicy{MaxLength: 64}, want: ErrInvalidSecretChange},
		{name: "keep with value", change: SecretChange{Operation: SecretKeep, Value: input}, policy: SecretPolicy{MaxLength: 64}, want: ErrInvalidSecretChange},
		{name: "replace without value", change: SecretChange{Operation: SecretReplace}, policy: SecretPolicy{MaxLength: 64}, want: ErrInvalidSecretChange},
		{name: "clear without confirmation", change: SecretChange{Operation: SecretClear}, policy: SecretPolicy{AllowClear: true, MaxLength: 64}, want: ErrInvalidSecretChange},
		{name: "clear required", change: SecretChange{Operation: SecretClear, Confirmed: true}, policy: SecretPolicy{Required: true, AllowClear: true, MaxLength: 64}, want: ErrSecretClearDenied},
		{name: "clear denied", change: SecretChange{Operation: SecretClear, Confirmed: true}, policy: SecretPolicy{MaxLength: 64}, want: ErrSecretClearDenied},
		{name: "required keep missing", change: SecretChange{Operation: SecretKeep}, policy: SecretPolicy{Required: true, MaxLength: 64}, want: ErrSecretUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ApplySecretChange(vault, StoredSecret{}, test.change, test.policy, time.Now()); !errors.Is(err, test.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestStoredSecretRejectsTamperingWrongKeyAndVersion(t *testing.T) {
	vault, err := credential.NewVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := ApplySecretChange(vault, StoredSecret{}, SecretChange{
		Operation: SecretReplace,
		Value:     &SecretInput{Plaintext: "provider-secret"},
	}, SecretPolicy{Required: true, MaxLength: 128}, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	tampered := stored
	replacement := byte('A')
	if tampered.Ciphertext[len(tampered.Ciphertext)-1] == replacement {
		replacement = 'B'
	}
	tampered.Ciphertext = tampered.Ciphertext[:len(tampered.Ciphertext)-1] + string(replacement)
	if _, err := OpenStoredSecret(vault, tampered); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}

	wrongVault, err := credential.NewVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStoredSecret(wrongVault, stored); err == nil {
		t.Fatal("wrong credential key was accepted")
	}

	unsupported := stored
	unsupported.Version++
	if err := ValidateStoredSecret(unsupported, true); err == nil {
		t.Fatal("unsupported credential version was accepted")
	}
	if _, err := OpenStoredSecret(vault, unsupported); err == nil {
		t.Fatal("unsupported credential version was decrypted")
	}
}

func TestSecretJSONOutputIsRedacted(t *testing.T) {
	input := SecretInput{Plaintext: "provider-secret"}
	if fmt.Sprint(input) != "[REDACTED]" || fmt.Sprintf("%#v", input) != "[REDACTED]" {
		t.Fatal("secret formatting exposed plaintext")
	}
	encoded, err := json.Marshal(SecretChange{Operation: SecretReplace, Value: &input})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("provider-secret")) || !bytes.Contains(encoded, []byte("[REDACTED]")) {
		t.Fatal("secret input JSON was not redacted")
	}

	stored := StoredSecret{Ciphertext: "enc:v1:ciphertext", Version: 1, UpdatedAt: time.Now().UTC()}
	status, err := json.Marshal(stored.Status())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(status, []byte("ciphertext")) || bytes.Contains(status, []byte("enc:v1:")) {
		t.Fatal("secret status exposed ciphertext")
	}
}

func TestValidateRuntimeSecretsChecksPluginSecrets(t *testing.T) {
	root := t.TempDir()
	vault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	pluginSecret, err := ApplySecretChange(vault, StoredSecret{}, SecretChange{
		Operation: SecretReplace,
		Value:     &SecretInput{Plaintext: "plugin-secret"},
	}, SecretPolicy{Required: true, MaxLength: 4096}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	runtime := validRuntimeConfig()
	plugin := runtime.Plugins.Plugins["telegram_push"]
	plugin.Secret = pluginSecret
	runtime.Plugins.Plugins["telegram_push"] = plugin
	if err := ValidateRuntimeSecrets(vault, runtime); err != nil {
		t.Fatalf("valid plugin secret was rejected: %v", err)
	}

	tampered := runtime
	tamperedPlugin := tampered.Plugins.Plugins["telegram_push"]
	last := len(tamperedPlugin.Secret.Ciphertext) - 1
	if last < 0 {
		t.Fatal("plugin secret ciphertext is empty")
	}
	replacement := byte('A')
	if tamperedPlugin.Secret.Ciphertext[last] == replacement {
		replacement = 'B'
	}
	tamperedPlugin.Secret.Ciphertext = tamperedPlugin.Secret.Ciphertext[:last] + string(replacement)
	tampered.Plugins.Plugins["telegram_push"] = tamperedPlugin
	if err := ValidateRuntimeSecrets(vault, tampered); !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("tampered plugin secret error = %v, want ErrSecretUnavailable", err)
	}
}
