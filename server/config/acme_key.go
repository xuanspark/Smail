package config

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
)

const maxACMEPrivateKeySize = 16 << 10

func loadOrCreateACMEPrivateKey(directory, name string) (*ecdsa.PrivateKey, bool, error) {
	if filepath.Base(name) != name || name == "." || name == "" {
		return nil, false, errors.New("invalid ACME private key path")
	}
	if err := ensureSecureACMEDirectory(directory); err != nil {
		return nil, false, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()

	info, statErr := root.Lstat(name)
	if statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, false, errors.New("invalid ACME private key file")
		}
		if info.Size() > 0 && info.Size() <= maxACMEPrivateKeySize {
			data, readErr := root.ReadFile(name)
			if readErr != nil {
				return nil, false, readErr
			}
			if privateKey, parseErr := parseACMEPrivateKey(data); parseErr == nil {
				if err := root.Chmod(name, 0600); err != nil {
					return nil, false, err
				}
				return privateKey, false, nil
			}
		}
	} else if !os.IsNotExist(statErr) {
		return nil, false, statErr
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, false, err
	}
	encoded, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, false, err
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encoded})
	if len(privatePEM) == 0 {
		return nil, false, errors.New("ACME private key encoding failed")
	}
	if err := atomicfile.WriteRoot(root, name, privatePEM, 0600); err != nil {
		return nil, false, err
	}
	return privateKey, true, nil
}

func ensureSecureACMEDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("invalid ACME private key directory")
	}
	// #nosec G302 -- ACME 账户私钥目录只允许当前服务账户访问。
	return os.Chmod(directory, 0700)
}

func parseACMEPrivateKey(data []byte) (*ecdsa.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "EC PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("invalid ACME private key")
	}
	privateKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	if privateKey.Curve == nil || privateKey.Curve.Params().Name != elliptic.P256().Params().Name {
		return nil, errors.New("invalid ACME private key curve")
	}
	return privateKey, nil
}
