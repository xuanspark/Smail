package parsemail

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"github.com/emersion/go-msgauth/dkim"
)

func TestCheckForDomainRequiresValidAlignedSignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	options := &dkim.VerifyOptions{LookupTXT: func(domain string) ([]string, error) {
		return []string{"v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(publicKey)}, nil
	}}
	message := "From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: authentication\r\n\r\noriginal body\r\n"
	for _, tc := range []struct {
		name, signer, expected string
		tampered               bool
		want                   bool
	}{
		{name: "aligned", signer: "example.com", expected: "example.com", want: true},
		{name: "case_insensitive", signer: "example.com", expected: "EXAMPLE.COM", want: true},
		{name: "foreign_domain", signer: "other.example", expected: "example.com"},
		{name: "subdomain", signer: "sub.example.com", expected: "example.com"},
		{name: "suffix_attack", signer: "example.com.other.example", expected: "example.com"},
		{name: "invalid_signature", signer: "example.com", expected: "example.com", tampered: true},
		{name: "generic_verification", signer: "other.example", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var signed bytes.Buffer
			if err := dkim.Sign(&signed, strings.NewReader(message), &dkim.SignOptions{
				Domain: tc.signer, Selector: "default", Signer: key,
			}); err != nil {
				t.Fatal(err)
			}
			data := signed.String()
			if tc.tampered {
				data += "tampered body\r\n"
			}
			if got := checkForDomain(nil, strings.NewReader(data), tc.expected, options); got != tc.want {
				t.Fatalf("签名认证结果为 %v，期望 %v", got, tc.want)
			}
		})
	}
	if checkForDomain(nil, strings.NewReader(message), "example.com", options) {
		t.Fatal("未签名邮件不能通过认证")
	}
}

func TestLoadPrivateKeyAcceptsStrongRSAKey(t *testing.T) {
	path := writeDKIMTestKey(t, 2048)
	signer, err := loadPrivateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, ok := signer.(*rsa.PrivateKey)
	if !ok || privateKey.N.BitLen() < 2048 {
		t.Fatal("应加载至少 2048 位的 RSA DKIM 私钥")
	}
}

func TestLoadPrivateKeyRejectsWeakRSAKey(t *testing.T) {
	path := writeDKIMTestKey(t, 1024)
	if _, err := loadPrivateKey(path); err == nil {
		t.Fatal("弱 RSA DKIM 私钥应被拒绝")
	}
}

func TestLoadPrivateKeyRejectsUnexpectedPKCS8Type(t *testing.T) {
	root, directory := prepareDKIMTestDirectory(t)
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "dkim.priv")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		t.Fatal(err)
	}
	config.ROOT_PATH = root + string(os.PathSeparator)
	if _, err := loadPrivateKey(path); err == nil {
		t.Fatal("非 RSA PKCS8 私钥应被安全拒绝")
	}
}

func TestLoadPrivateKeyRejectsSymlink(t *testing.T) {
	root, directory := prepareDKIMTestDirectory(t)
	outside := filepath.Join(t.TempDir(), "outside.key")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "dkim.priv")
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("当前环境不支持符号链接测试: %v", err)
	}
	config.ROOT_PATH = root + string(os.PathSeparator)
	if _, err := loadPrivateKey(path); err == nil {
		t.Fatal("DKIM 私钥符号链接应被拒绝")
	}
}

func writeDKIMTestKey(t *testing.T, bits int) string {
	t.Helper()
	root, directory := prepareDKIMTestDirectory(t)
	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "dkim.priv")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		t.Fatal(err)
	}
	config.ROOT_PATH = root + string(os.PathSeparator)
	return path
}

func prepareDKIMTestDirectory(t *testing.T) (string, string) {
	t.Helper()
	oldRoot := config.ROOT_PATH
	root := t.TempDir()
	t.Cleanup(func() { config.ROOT_PATH = oldRoot })
	directory := filepath.Join(root, "data", "dkim")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return root, directory
}
