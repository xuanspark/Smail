package ssl

import (
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/utils/atomicfile"
	securefile "git.pcux.cn/xuans/Smail/utils/file"
)

func writeCertificateBundle(cfg *config.Config, privateKey, certificate, issuer []byte) error {
	if cfg == nil {
		return errors.New("SSL configuration is required")
	}
	if len(privateKey) == 0 || len(certificate) == 0 || len(privateKey) > 4<<20 || len(certificate) > 4<<20 || len(issuer) > 4<<20 {
		return errors.New("invalid SSL certificate data")
	}
	if _, err := tls.X509KeyPair(certificate, privateKey); err != nil {
		return errors.New("SSL certificate and private key do not match")
	}
	rootPath, err := filepath.Abs(filepath.Clean(config.ROOT_PATH))
	if err != nil {
		return err
	}
	workspaceRoot, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer workspaceRoot.Close()
	baseRelative := filepath.Join("config", "ssl")
	if err := ensureSecureRootDirectory(workspaceRoot, baseRelative); err != nil {
		return err
	}
	sslRoot, err := workspaceRoot.OpenRoot(baseRelative)
	if err != nil {
		return err
	}
	defer sslRoot.Close()
	basePath := filepath.Join(rootPath, baseRelative)

	files := []struct {
		path string
		data []byte
	}{
		{path: cfg.SSLPrivateKeyPath, data: privateKey},
		{path: cfg.SSLPublicKeyPath, data: certificate},
		{path: filepath.Join(filepath.Dir(cfg.SSLPublicKeyPath), "issuerCert.crt"), data: issuer},
	}
	seen := make(map[string]struct{}, len(files))
	for _, item := range files {
		if strings.TrimSpace(item.path) == "" {
			return errors.New("SSL file path is empty")
		}
		relativePath, err := securefile.ControlledRelativePath(basePath, item.path)
		if err != nil {
			return errors.New("SSL file path must stay inside config/ssl")
		}
		if _, exists := seen[relativePath]; exists {
			return errors.New("SSL file paths must be unique")
		}
		seen[relativePath] = struct{}{}
		if err := ensureSecureRootDirectory(sslRoot, filepath.Dir(relativePath)); err != nil {
			return err
		}
		if info, statErr := sslRoot.Lstat(relativePath); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return errors.New("invalid SSL file")
			}
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		if err := atomicfile.WriteRoot(sslRoot, relativePath, item.data, 0600); err != nil {
			return err
		}
	}
	return nil
}

func ensureSecureRootDirectory(root *os.Root, name string) error {
	if name == "." || name == "" {
		return nil
	}
	current := ""
	for _, part := range strings.Split(filepath.Clean(name), string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid secure directory")
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
			return errors.New("invalid secure directory")
		}
		if err := root.Chmod(current, 0700); err != nil {
			return err
		}
	}
	return nil
}
