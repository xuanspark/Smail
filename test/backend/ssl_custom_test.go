package config

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func customSSLFixture(t *testing.T) (string, *ConfigManager, UpdateRequest, []byte) {
	t.Helper()
	root := t.TempDir()
	runtime := validRuntimeConfig()
	runtime.Root.Domains = DomainConfig{Primary: "example.com", Web: "example.com", Accepted: []string{"example.com"}}
	writeRuntimeFixture(t, root, runtime)
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{"example.com", "*.example.com"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	external := filepath.Join(root, "external")
	if err := os.Mkdir(external, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"public.crt": certPEM, "private.key": keyPEM} {
		if err := os.WriteFile(filepath.Join(external, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	section := runtime.SSL
	section.TLS = SSLConfig{
		Management: SSLManagementCustom, Mode: ManagedSSLModeUpload,
		DefaultDirectory: "config/ssl", CustomDirectory: "config/ssl",
		CertificateRef: filepath.Join(external, "public.crt"), PrivateKeyRef: filepath.Join(external, "private.key"),
	}
	payload, err := json.Marshal(section)
	if err != nil {
		t.Fatal(err)
	}
	return root, manager, UpdateRequest{Section: SectionSSL, ExpectedGeneration: runtime.Generation, Payload: payload, Actor: authorizedConfigActor()}, certPEM
}

func assertNoCustomSSLVersions(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, "data", "ssl", "managed", "upload")); !os.IsNotExist(err) {
		t.Fatalf("失败或只读操作产生了证书存储副作用: %v", err)
	}
}

func TestCustomSSLManagerValidateSaveApplyReload(t *testing.T) {
	for _, relative := range []bool{false, true} {
		t.Run(map[bool]string{false: "absolute", true: "relative"}[relative], func(t *testing.T) {
			root, manager, request, certificate := customSSLFixture(t)
			if relative {
				var section SSLSectionConfig
				if err := json.Unmarshal(request.Payload, &section); err != nil {
					t.Fatal(err)
				}
				section.TLS.CertificateRef = "external/public.crt"
				section.TLS.PrivateKeyRef = "external/private.key"
				request.Payload, _ = json.Marshal(section)
			}
			result, err := manager.Validate(context.Background(), ValidateRequest{Section: SectionSSL, ExpectedGeneration: request.ExpectedGeneration, Payload: request.Payload})
			if err != nil || !result.Valid {
				t.Fatalf("只读校验失败: %+v, %v", result, err)
			}
			assertNoCustomSSLVersions(t, root)
			oldKey, oldCertificate := testManagedCertificate(t, "old.example.org", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048)
			oldReference, err := importManagedSSLAtRoot(root, ManagedSSLModeUpload, oldKey, oldCertificate, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			updated, err := manager.Update(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if manager.Snapshot().Generation != request.ExpectedGeneration {
				t.Fatal("暂存提前切换了当前配置")
			}
			if !strings.HasPrefix(updated.Candidate.SSL.TLS.CertificateRef, "data/ssl/managed/upload/version-") {
				t.Fatal("候选未使用独立受管引用")
			}
			currentBytes, err := os.ReadFile(oldReference.CertificatePath)
			if err != nil || string(currentBytes) != string(oldCertificate) {
				t.Fatal("暂存不应覆盖原有 current 证书")
			}
			if _, err := manager.ApplyPendingAuthorized(context.Background(), updated.Generation, updated.CandidateID, SectionSSL, request.Actor); err != nil {
				t.Fatal(err)
			}
			if err := manager.Load(context.Background()); err != nil {
				t.Fatal(err)
			}
			reference, err := resolveManagedSSLReferencesAtRoot(root, manager.Snapshot().SSL.TLS)
			if err != nil {
				t.Fatal(err)
			}
			pair, err := tls.LoadX509KeyPair(reference.CertificatePath, reference.PrivateKeyPath)
			if err != nil || len(pair.Certificate) == 0 {
				t.Fatalf("重新加载 TLS 证书失败: %v", err)
			}
			stored, err := os.ReadFile(reference.CertificatePath)
			if err != nil || string(stored) != string(certificate) {
				t.Fatal("导入改变了证书内容")
			}
			// 再次保存受管引用不应重复导入。
			payload, _ := json.Marshal(manager.Snapshot().SSL)
			again, err := manager.Update(context.Background(), UpdateRequest{Section: SectionSSL, ExpectedGeneration: updated.Generation, Payload: payload, Actor: request.Actor})
			if err != nil || again.Candidate.SSL.TLS.CertificateRef != reference.CertificateRef {
				t.Fatalf("受管引用回填无法再次保存: %v", err)
			}
		})
	}
}

func TestCustomSSLManagerRejectsBeforeWriting(t *testing.T) {
	for _, name := range []string{"unauthorized", "confirmation", "invalid_config", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			root, manager, request, _ := customSSLFixture(t)
			ctx := context.Background()
			switch name {
			case "unauthorized":
				request.Actor = AuditActor{}
			case "confirmation":
				manager.security.Confirm = func(context.Context, AuditActor, StepUpConfirmation, ConfigOperation, Section) error {
					return ErrStepUpRequired
				}
			case "invalid_config":
				manager.options.HasSuperAdmin = false
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			if _, err := manager.Update(ctx, request); err == nil {
				t.Fatal("无效操作意外成功")
			}
			assertNoCustomSSLVersions(t, root)
			if manager.Pending(request.Actor) != nil {
				t.Fatal("无效操作留下候选")
			}
		})
	}
}

func TestCustomSSLApplyRechecksFilesAndCancelPreservesActive(t *testing.T) {
	root, manager, request, _ := customSSLFixture(t)
	updated, err := manager.Update(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := resolveManagedSSLReferencesAtRoot(root, updated.Candidate.SSL.TLS)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reference.CertificatePath, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ApplyPendingAuthorized(context.Background(), updated.Generation, updated.CandidateID, SectionSSL, request.Actor); err == nil {
		t.Fatal("损坏候选证书不应应用成功")
	}
	if manager.Snapshot().Generation != request.ExpectedGeneration {
		t.Fatal("失败应用改变了当前代次")
	}
	if err := manager.ManagePending(context.Background(), updated.Generation, updated.CandidateID, request.Actor, StepUpConfirmation{}, true); err != nil {
		t.Fatal(err)
	}
	if manager.Pending(request.Actor) != nil || manager.Snapshot().Generation != request.ExpectedGeneration {
		t.Fatal("取消候选影响了现用配置")
	}
}

func TestCustomSSLRejectsInvalidMaterialsAndPaths(t *testing.T) {
	root, manager, request, _ := customSSLFixture(t)
	var original SSLSectionConfig
	if err := json.Unmarshal(request.Payload, &original); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing", "traversal", "mismatch", "domain", "expired", "weak"} {
		t.Run(name, func(t *testing.T) {
			section := original
			want := ErrSSLFileUnavailable
			switch name {
			case "missing":
				section.TLS.CertificateRef = filepath.Join(root, "missing.crt")
			case "traversal":
				section.TLS.CertificateRef = "external/../external/public.crt"
				want = ErrInvalidSSLReference
			default:
				bits, expires := 2048, time.Now().Add(time.Hour)
				want = ErrSSLCertificateDomain
				if name == "expired" {
					expires = time.Now().Add(-time.Minute)
					want = ErrSSLCertificateExpired
				}
				if name == "weak" {
					bits = 1024
					want = ErrInvalidSSLMaterial
				}
				key, cert := testManagedCertificate(t, "other.example.org", time.Now().Add(-time.Hour), expires, bits)
				section.TLS.CertificateRef = filepath.Join(root, name+".crt")
				if err := os.WriteFile(section.TLS.CertificateRef, cert, 0600); err != nil {
					t.Fatal(err)
				}
				if name == "mismatch" {
					want = ErrSSLCertificateMismatch
				} else {
					section.TLS.PrivateKeyRef = filepath.Join(root, name+".key")
					if err := os.WriteFile(section.TLS.PrivateKeyRef, key, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			request.Payload, _ = json.Marshal(section)
			if _, err := manager.Update(context.Background(), request); !errors.Is(err, want) {
				t.Fatalf("证书错误类型不符: %v, want %v", err, want)
			}
			result, err := manager.Validate(context.Background(), ValidateRequest{Section: SectionSSL, Payload: request.Payload})
			if err != nil || result.Valid || len(result.Issues) != 1 || strings.Contains(result.Issues[0].Message, root) {
				t.Fatalf("校验接口应返回安全的具体错误: %+v, %v", result, err)
			}
			assertNoCustomSSLVersions(t, root)
		})
	}
}
