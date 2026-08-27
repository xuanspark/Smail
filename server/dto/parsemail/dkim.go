package parsemail

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/utils/consts"
	"git.pcux.cn/xuans/Smail/utils/context"
	securefile "git.pcux.cn/xuans/Smail/utils/file"
	"github.com/emersion/go-msgauth/dkim"
	log "github.com/sirupsen/logrus"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Dkim struct {
	privateKey crypto.Signer
}

var instance *Dkim

func Init() error {
	cfg := config.CurrentConfig()
	if cfg == nil {
		return fmt.Errorf("runtime configuration is unavailable")
	}
	privateKey, err := loadPrivateKey(cfg.DkimPrivateKeyPath)
	if err != nil {
		return fmt.Errorf("load DKIM private key: %w", err)
	}

	instance = &Dkim{
		privateKey: privateKey,
	}
	return nil
}

func loadPrivateKey(path string) (crypto.Signer, error) {
	basePath := filepath.Join(config.ROOT_PATH, "config", "dkim")
	privateName, err := securefile.ControlledRelativePath(basePath, path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(basePath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(privateName)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<10 {
		return nil, fmt.Errorf("invalid DKIM private key file")
	}
	data, err := root.ReadFile(privateName)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(data)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("invalid DKIM private key PEM")
	}

	var privateKey *rsa.PrivateKey
	switch strings.ToUpper(block.Type) {
	case "PRIVATE KEY":
		parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, parseErr
		}
		var ok bool
		privateKey, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("unsupported DKIM private key type")
		}
	case "RSA PRIVATE KEY":
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported DKIM private key type: %q", block.Type)
	}
	if privateKey.N == nil || privateKey.N.BitLen() < 2048 {
		return nil, fmt.Errorf("DKIM RSA private key is too weak")
	}
	if err := privateKey.Validate(); err != nil {
		return nil, err
	}
	return privateKey, nil
}
func (p *Dkim) Sign(msgData string) ([]byte, error) {
	if p == nil || p.privateKey == nil {
		return nil, fmt.Errorf("DKIM signer is not initialized")
	}
	cfg := config.CurrentConfig()
	if cfg == nil || strings.TrimSpace(cfg.Domain) == "" {
		return nil, fmt.Errorf("DKIM domain is not configured")
	}

	var b bytes.Buffer
	r := strings.NewReader(msgData)

	options := &dkim.SignOptions{
		Domain:   cfg.Domain,
		Selector: "default",
		Signer:   p.privateKey,
	}

	if err := dkim.Sign(&b, r, options); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func Check(ctx *context.Context, mail io.Reader) bool {

	verifications, err := dkim.Verify(mail)
	if err != nil {
		log.WithContext(ctx).Warnf("DKIM Error:%v", err)
	}

	if len(verifications) == 0 {
		return false
	}

	for _, v := range verifications {
		if v.Domain == consts.TEST_DOMAIN {
			return true
		}
		if v.Err == nil {
			log.Println("Valid signature for:", v.Domain)
		} else {
			log.Println("Invalid signature for:", v.Domain, v.Err)
			return false
		}
	}
	return true
}
