package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"git.pcux.cn/xuans/Smail/utils/atomicfile"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const encryptedPrefix = "enc:v1:"

var credentialAAD = []byte("smail-config-v1")

func Encrypt(root string, plaintext string) (string, error) {
	if plaintext == "" {
		return plaintext, nil
	}
	key, err := loadOrCreateKey(root)
	if err != nil {
		return "", errors.New("credential protection failed")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", errors.New("credential protection failed")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.New("credential protection failed")
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", errors.New("credential protection failed")
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), credentialAAD)
	return encryptedPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func Decrypt(root string, value string) (string, error) {
	if value == "" {
		return value, nil
	}
	if !IsEncrypted(value) {
		return "", errors.New("credential decryption failed")
	}
	encoded := strings.TrimPrefix(value, encryptedPrefix)
	sealed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || base64.RawURLEncoding.EncodeToString(sealed) != encoded {
		return "", errors.New("credential decryption failed")
	}
	key, err := readKey(root)
	if err != nil {
		return "", errors.New("credential decryption failed")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", errors.New("credential decryption failed")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(sealed) < gcm.NonceSize() {
		return "", errors.New("credential decryption failed")
	}
	plaintext, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], credentialAAD)
	if err != nil {
		return "", errors.New("credential decryption failed")
	}
	return string(plaintext), nil
}

func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, encryptedPrefix)
}

func loadOrCreateKey(root string) ([]byte, error) {
	key, err := readKey(root)
	if err == nil {
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	directory := filepath.Join(root, "data")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	if err := validateKeyDirectory(directory); err != nil {
		return nil, err
	}
	rootHandle, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer rootHandle.Close()
	key = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	temporaryName, err := newCredentialKeyTemporaryName()
	if err != nil {
		return nil, err
	}
	file, err := rootHandle.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		_ = rootHandle.Remove(temporaryName)
	}()
	written, err := file.Write(key)
	if err != nil {
		return nil, err
	}
	if written != len(key) {
		return nil, io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		closed = true
		return nil, err
	}
	closed = true
	if err := rootHandle.Chmod(temporaryName, 0600); err != nil {
		return nil, err
	}
	if err := rootHandle.Link(temporaryName, "credential.key"); err != nil {
		if os.IsExist(err) {
			return readKey(root)
		}
		return nil, err
	}
	if err := rootHandle.Chmod("credential.key", 0600); err != nil {
		return nil, err
	}
	if err := atomicfile.SyncDirectory(directory); err != nil {
		return nil, err
	}
	return key, nil
}

func newCredentialKeyTemporaryName() (string, error) {
	token := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, token); err != nil {
		return "", err
	}
	return ".credential.key-" + base64.RawURLEncoding.EncodeToString(token), nil
}

func readKey(root string) ([]byte, error) {
	// 旧密钥尚未迁移时拒绝生成新密钥，避免现有密文永久失去读取能力。
	if _, err := os.Lstat(filepath.Join(root, "config", "credential.key")); err == nil {
		return nil, errors.New("凭据密钥尚未迁移到 data")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	directory := filepath.Join(root, "data")
	if err := validateKeyDirectory(directory); err != nil {
		return nil, err
	}
	keyInfo, err := os.Lstat(filepath.Join(directory, "credential.key"))
	if err != nil {
		return nil, err
	}
	if keyInfo.Mode()&os.ModeSymlink != 0 || !keyInfo.Mode().IsRegular() {
		return nil, errors.New("invalid credential key")
	}
	if runtime.GOOS != "windows" && keyInfo.Mode().Perm()&0077 != 0 {
		return nil, errors.New("invalid credential key")
	}

	rootHandle, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer rootHandle.Close()
	key, err := rootHandle.ReadFile("credential.key")
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid credential key")
	}
	return key, nil
}

func validateKeyDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("invalid credential directory")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return errors.New("invalid credential directory")
	}
	return nil
}
