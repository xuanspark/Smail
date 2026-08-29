package credential

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// Fingerprint 使用数据库外主密钥计算固定长度校验值，不能用于恢复原文。
func (v *Vault) Fingerprint(value string) (string, error) {
	if v == nil {
		return "", ErrVaultUnavailable
	}
	key, err := readKey(v.root)
	if err != nil {
		return "", ErrVaultUnavailable
	}
	defer func() {
		for index := range key {
			key[index] = 0
		}
	}()
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("smail-plugin-setting-v1\x00"))
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
