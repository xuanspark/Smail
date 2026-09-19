package config

import (
	"errors"
	"os"
	"runtime"
	"testing"
)

func TestPreviousSSLReferenceValidation(t *testing.T) {
	const base = "smail-tls-0123456789abcdef0123456789abcdef"
	for _, test := range []struct {
		name        string
		certificate string
		key         string
		valid       bool
	}{
		{"valid", "ssl/" + base + ".crt", "ssl/" + base + ".key", true},
		{"missing_key", "ssl/" + base + ".crt", "", false},
		{"absolute", "/ssl/" + base + ".crt", "/ssl/" + base + ".key", false},
		{"traversal", "ssl/../ssl/" + base + ".crt", "ssl/" + base + ".key", false},
		{"nested", "ssl/old/" + base + ".crt", "ssl/old/" + base + ".key", false},
		{"outside", "data/" + base + ".crt", "data/" + base + ".key", false},
		{"backslash", "ssl\\" + base + ".crt", "ssl\\" + base + ".key", false},
		{"mismatch", "ssl/" + base + ".crt", "ssl/smail-tls-abcdef0123456789abcdef0123456789.key", false},
		{"unmanaged", "ssl/manual.crt", "ssl/manual.key", false},
		{"bad_token", "ssl/smail-tls-short.crt", "ssl/smail-tls-short.key", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ssl := testRootConfig().SSL
			ssl.PreviousCertificateRef = test.certificate
			ssl.PreviousPrivateKeyRef = test.key
			err := validateSSL(ssl)
			if test.valid && err != nil {
				t.Fatalf("validateSSL: %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidSSL) {
				t.Fatalf("validateSSL = %v, want ErrInvalidSSL", err)
			}
		})
	}
}

// 引用格式校验不得依赖调用者的工作目录权限，真实材料由加载器另行校验。
func TestPreviousSSLReferenceWithInaccessibleWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("需要非特权 Unix 用户验证目录权限")
	}
	directory := t.TempDir()
	t.Chdir(directory)
	if err := os.Chmod(directory, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(directory, 0700); err != nil {
			t.Error(err)
		}
	})
	ssl := testRootConfig().SSL
	ssl.PreviousCertificateRef = "ssl/smail-tls-0123456789abcdef0123456789abcdef.crt"
	ssl.PreviousPrivateKeyRef = "ssl/smail-tls-0123456789abcdef0123456789abcdef.key"
	if err := validateSSL(ssl); err != nil {
		t.Fatalf("validateSSL with inaccessible working directory: %v", err)
	}
}
