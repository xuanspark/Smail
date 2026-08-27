package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
	securefile "git.pcux.cn/xuans/Smail/utils/file"
)

const (
	maxDKIMPrivateKeySize = 64 << 10
	maxDKIMPublicKeySize  = 16 << 10
)

func ensureDKIMKeyPair(basePath, privatePath string) (string, error) {
	if err := ensureSecureDKIMDirectory(basePath); err != nil {
		return "", err
	}
	privateName, err := securefile.ControlledRelativePath(basePath, privatePath)
	if err != nil {
		return "", err
	}
	publicName := filepath.Join(filepath.Dir(privateName), "dkim.public")

	root, err := os.OpenRoot(basePath)
	if err != nil {
		return "", err
	}
	defer root.Close()

	if err := ensureSecureDKIMParent(root, filepath.Dir(privateName)); err != nil {
		return "", err
	}

	privateData, privateExists, err := readDKIMFile(root, privateName, maxDKIMPrivateKeySize)
	if err != nil {
		return "", err
	}
	if privateExists {
		if privateKey, parseErr := parseDKIMPrivateKey(privateData); parseErr == nil {
			publicKey, buildErr := buildDKIMPublicKey(&privateKey.PublicKey)
			if buildErr != nil {
				return "", buildErr
			}
			publicData, publicExists, readErr := readDKIMFile(root, publicName, maxDKIMPublicKeySize)
			if readErr != nil {
				return "", readErr
			}
			if !publicExists || !bytes.Equal(publicData, []byte(publicKey)) {
				if err := atomicfile.WriteRoot(root, publicName, []byte(publicKey), 0600); err != nil {
					return "", err
				}
			}
			if err := root.Chmod(privateName, 0600); err != nil {
				return "", err
			}
			return publicKey, nil
		}
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}
	privateBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return "", err
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateBytes})
	if len(privatePEM) == 0 {
		return "", errors.New("DKIM private key encoding failed")
	}
	publicKey, err := buildDKIMPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", err
	}

	// 先发布私钥；若公钥写入中断，下次启动会从私钥重新生成并修复公钥文件。
	if err := atomicfile.WriteRoot(root, privateName, privatePEM, 0600); err != nil {
		return "", err
	}
	if err := atomicfile.WriteRoot(root, publicName, []byte(publicKey), 0600); err != nil {
		return "", err
	}
	return publicKey, nil
}

func ensureSecureDKIMDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("invalid DKIM directory")
	}
	// #nosec G302 -- DKIM 目录只允许当前服务账户访问。
	return os.Chmod(path, 0700)
}

func ensureSecureDKIMParent(root *os.Root, name string) error {
	if name == "." || name == "" {
		return nil
	}
	current := ""
	for _, part := range strings.Split(filepath.Clean(name), string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid DKIM key directory")
		}
		current = filepath.Join(current, part)
		if err := root.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := root.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("invalid DKIM key directory")
		}
		if err := root.Chmod(current, 0700); err != nil {
			return err
		}
	}
	return nil
}

func readDKIMFile(root *os.Root, name string, maxSize int64) ([]byte, bool, error) {
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, false, errors.New("invalid DKIM key file")
	}
	if info.Size() <= 0 || info.Size() > maxSize {
		return nil, true, nil
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func parseDKIMPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("invalid DKIM private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if !ok || privateKey.N == nil || privateKey.N.BitLen() < 2048 {
		return nil, errors.New("invalid DKIM private key")
	}
	if err := privateKey.Validate(); err != nil {
		return nil, err
	}
	return privateKey, nil
}

func buildDKIMPublicKey(publicKey *rsa.PublicKey) (string, error) {
	publicBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", err
	}
	if len(publicBytes) == 0 {
		return "", fmt.Errorf("DKIM public key encoding failed")
	}
	return strings.Join([]string{
		"v=DKIM1",
		"k=rsa",
		"p=" + base64.StdEncoding.EncodeToString(publicBytes),
	}, "; "), nil
}
