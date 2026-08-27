package credential

import (
	"errors"
	"strings"
)

const CurrentVersion uint32 = 1

var (
	ErrVaultUnavailable   = errors.New("credential vault unavailable")
	ErrUnsupportedVersion = errors.New("unsupported credential version")
)

type Vault struct {
	root string
}

func NewVault(root string) (*Vault, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, ErrVaultUnavailable
	}
	key, err := loadOrCreateKey(root)
	if err != nil {
		return nil, ErrVaultUnavailable
	}
	for index := range key {
		key[index] = 0
	}
	return &Vault{root: root}, nil
}

func (v *Vault) Version() uint32 {
	if v == nil {
		return 0
	}
	return CurrentVersion
}

func (v *Vault) Encrypt(plaintext string) (string, error) {
	if v == nil {
		return "", ErrVaultUnavailable
	}
	value, err := Encrypt(v.root, plaintext)
	if err != nil {
		return "", ErrVaultUnavailable
	}
	return value, nil
}

func (v *Vault) Decrypt(version uint32, ciphertext string) (string, error) {
	if v == nil {
		return "", ErrVaultUnavailable
	}
	if version != CurrentVersion {
		return "", ErrUnsupportedVersion
	}
	value, err := Decrypt(v.root, ciphertext)
	if err != nil {
		return "", ErrVaultUnavailable
	}
	return value, nil
}
