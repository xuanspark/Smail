package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/utils/credential"
)

type SecretOperation string

const (
	SecretKeep    SecretOperation = "keep"
	SecretReplace SecretOperation = "replace"
	SecretClear   SecretOperation = "clear"
)

var (
	ErrInvalidSecretChange = errors.New("invalid secret change")
	ErrSecretClearDenied   = errors.New("secret clear is not allowed")
	ErrSecretUnavailable   = errors.New("secret is unavailable")
)

type SecretInput struct {
	Plaintext string `json:"plaintext"`
}

func (s SecretInput) Reveal() string {
	return s.Plaintext
}

func (s SecretInput) String() string {
	return "[REDACTED]"
}

func (s SecretInput) GoString() string {
	return "[REDACTED]"
}

func (s SecretInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Plaintext string `json:"plaintext"`
	}{Plaintext: "[REDACTED]"})
}

type SecretChange struct {
	Operation SecretOperation `json:"operation"`
	Value     *SecretInput    `json:"value,omitempty"`
	Confirmed bool            `json:"confirmed,omitempty"`
}

type StoredSecret struct {
	Ciphertext string    `json:"ciphertext,omitempty"`
	Version    uint32    `json:"version,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt,omitempty"`
}

func (s StoredSecret) Configured() bool {
	return s.Ciphertext != "" && s.Version != 0 && !s.UpdatedAt.IsZero()
}

type SecretStatus struct {
	Configured bool      `json:"configured"`
	UpdatedAt  time.Time `json:"updatedAt,omitempty"`
	Hint       string    `json:"hint,omitempty"`
	Version    uint32    `json:"version,omitempty"`
}

type SecretPolicy struct {
	Required   bool
	AllowClear bool
	MaxLength  int
}

func (s StoredSecret) Status() SecretStatus {
	if !s.Configured() {
		return SecretStatus{}
	}
	return SecretStatus{
		Configured: true,
		UpdatedAt:  s.UpdatedAt,
		Hint:       "******",
		Version:    s.Version,
	}
}

func ApplySecretChange(vault *credential.Vault, current StoredSecret, change SecretChange, policy SecretPolicy, now time.Time) (StoredSecret, error) {
	if vault == nil || policy.MaxLength <= 0 {
		return StoredSecret{}, ErrInvalidSecretChange
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	switch change.Operation {
	case SecretKeep:
		if change.Value != nil || change.Confirmed {
			return StoredSecret{}, ErrInvalidSecretChange
		}
		if err := ValidateStoredSecret(current, policy.Required); err != nil {
			return StoredSecret{}, ErrSecretUnavailable
		}
		return current, nil
	case SecretReplace:
		if change.Value == nil || change.Confirmed {
			return StoredSecret{}, ErrInvalidSecretChange
		}
		plaintext, err := credential.ValidateSecret(change.Value.Reveal(), policy.MaxLength)
		if err != nil {
			return StoredSecret{}, ErrInvalidSecretChange
		}
		ciphertext, err := vault.Encrypt(plaintext)
		if err != nil {
			return StoredSecret{}, ErrSecretUnavailable
		}
		return StoredSecret{Ciphertext: ciphertext, Version: vault.Version(), UpdatedAt: now}, nil
	case SecretClear:
		if change.Value != nil || !change.Confirmed {
			return StoredSecret{}, ErrInvalidSecretChange
		}
		if policy.Required || !policy.AllowClear {
			return StoredSecret{}, ErrSecretClearDenied
		}
		return StoredSecret{}, nil
	default:
		return StoredSecret{}, ErrInvalidSecretChange
	}
}

func OpenStoredSecret(vault *credential.Vault, stored StoredSecret) (string, error) {
	if vault == nil || !stored.Configured() || strings.TrimSpace(stored.Ciphertext) != stored.Ciphertext {
		return "", ErrSecretUnavailable
	}
	plaintext, err := vault.Decrypt(stored.Version, stored.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSecretUnavailable, err)
	}
	return plaintext, nil
}

// ValidateRuntimeSecrets 验证运行时配置中的持久化 Secret 可以由当前密钥解密。
func ValidateRuntimeSecrets(vault *credential.Vault, runtime RuntimeConfig) error {
	for _, stored := range []StoredSecret{runtime.Root.Database.Password, runtime.SMTP.Relay.Password, runtime.SSL.TLS.DNSAccessKey, runtime.SSL.TLS.DNSSecretKey} {
		if !stored.Configured() {
			continue
		}
		if _, err := OpenStoredSecret(vault, stored); err != nil {
			return err
		}
	}
	for _, plugin := range runtime.Plugins.Plugins {
		if !plugin.Secret.Configured() {
			continue
		}
		if _, err := OpenStoredSecret(vault, plugin.Secret); err != nil {
			return err
		}
	}
	return nil
}

func ValidateStoredSecret(stored StoredSecret, required bool) error {
	if !stored.Configured() {
		if stored.Ciphertext != "" || stored.Version != 0 || !stored.UpdatedAt.IsZero() || required {
			return ErrSecretUnavailable
		}
		return nil
	}
	if stored.Version != credential.CurrentVersion || !credential.IsEncrypted(stored.Ciphertext) {
		return ErrSecretUnavailable
	}
	return nil
}
