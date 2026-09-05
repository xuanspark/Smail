package externalemail

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	keySize           = 32
	ciphertextPrefix  = "email:v1:"
	fingerprintPrefix = "email-fp:v1:"
)

var (
	ErrInvalidEmail      = errors.New("invalid external email")
	ErrVaultUnavailable  = errors.New("external email vault unavailable")
	ErrCiphertextInvalid = errors.New("external email ciphertext invalid")
)

type Vault struct {
	encryptionKey  [keySize]byte
	fingerprintKey [keySize]byte
	version        string
	mu             sync.RWMutex
}

func NewVault(root string) (*Vault, error) {
	if strings.TrimSpace(root) == "" {
		return nil, ErrVaultUnavailable
	}
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, ErrVaultUnavailable
	}
	encryptionKey, err := loadKey(filepath.Join(configDir, "external-email-encryption.key"))
	if err != nil {
		return nil, ErrVaultUnavailable
	}
	fingerprintKey, err := loadKey(filepath.Join(configDir, "external-email-fingerprint.key"))
	if err != nil {
		return nil, ErrVaultUnavailable
	}
	return NewVaultWithKeys(encryptionKey, fingerprintKey), nil
}

func NewVaultWithKeys(encryptionKey, fingerprintKey []byte) *Vault {
	vault := &Vault{version: "1"}
	copy(vault.encryptionKey[:], encryptionKey)
	copy(vault.fingerprintKey[:], fingerprintKey)
	return vault
}

func NormalizeEmail(value string) (string, error) {
	normalized := strings.TrimSpace(strings.ToLower(value))
	if normalized == "" || strings.ContainsAny(normalized, "\r\n") {
		return "", ErrInvalidEmail
	}
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized || !strings.Contains(normalized, "@") {
		return "", ErrInvalidEmail
	}
	return normalized, nil
}

func (v *Vault) Fingerprint(email string) (string, error) {
	if v == nil {
		return "", ErrVaultUnavailable
	}
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return "", err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	mac := hmac.New(sha256.New, v.fingerprintKey[:])
	_, _ = mac.Write([]byte(normalized))
	return fingerprintPrefix + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (v *Vault) Encrypt(email string) (string, error) {
	if v == nil {
		return "", ErrVaultUnavailable
	}
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return "", err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	block, err := aes.NewCipher(v.encryptionKey[:])
	if err != nil {
		return "", ErrVaultUnavailable
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", ErrVaultUnavailable
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", ErrVaultUnavailable
	}
	sealed := gcm.Seal(nonce, nonce, []byte(normalized), []byte("smail-external-email-v1"))
	return ciphertextPrefix + v.version + ":" + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (v *Vault) Decrypt(value string) (string, error) {
	if v == nil || !strings.HasPrefix(value, ciphertextPrefix) {
		return "", ErrCiphertextInvalid
	}
	parts := strings.SplitN(strings.TrimPrefix(value, ciphertextPrefix), ":", 2)
	if len(parts) != 2 || parts[0] != v.version {
		return "", ErrCiphertextInvalid
	}
	sealed, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(sealed) != parts[1] {
		return "", ErrCiphertextInvalid
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	block, err := aes.NewCipher(v.encryptionKey[:])
	if err != nil {
		return "", ErrCiphertextInvalid
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(sealed) < gcm.NonceSize() {
		return "", ErrCiphertextInvalid
	}
	plaintext, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte("smail-external-email-v1"))
	if err != nil {
		return "", ErrCiphertextInvalid
	}
	result, err := NormalizeEmail(string(plaintext))
	for index := range plaintext {
		plaintext[index] = 0
	}
	if err != nil {
		return "", ErrCiphertextInvalid
	}
	return result, nil
}

func loadKey(path string) ([]byte, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, ErrVaultUnavailable
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, ErrVaultUnavailable
	}
	defer root.Close()

	name := filepath.Base(absolute)
	info, err := root.Lstat(name)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && (info.Mode().Perm()&0077) != 0) || info.Size() != keySize {
			return nil, ErrVaultUnavailable
		}
		key, err := root.ReadFile(name)
		if err != nil || len(key) != keySize {
			return nil, ErrVaultUnavailable
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, ErrVaultUnavailable
	}
	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, ErrVaultUnavailable
	}
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return loadKey(path)
		}
		return nil, ErrVaultUnavailable
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		return nil, ErrVaultUnavailable
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, ErrVaultUnavailable
	}
	if err := file.Close(); err != nil {
		return nil, ErrVaultUnavailable
	}
	return key, nil
}
func EqualFingerprint(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
