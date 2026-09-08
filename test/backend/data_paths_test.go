package config

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/utils/credential"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
)

func TestDataMigrationKeepsSecretsCertificatesAndBackupRestorable(t *testing.T) {
	root := t.TempDir()
	runtime := validMySQLRuntimeConfig(t, root)
	vault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	external, err := externalemail.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	emailCipher, _ := external.Encrypt("test@example.com")
	fingerprint, _ := external.Fingerprint("test@example.com")
	private, certificate := testManagedCertificate(t, "example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048)
	reference, err := stageManagedSSLAtRoot(root, ManagedSSLModeUpload, private, certificate, nil, []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.SSL.TLS = SSLConfig{
		Mode: ManagedSSLModeUpload, Management: SSLManagementCustom,
		DefaultDirectory: "config/ssl", CustomDirectory: "config/ssl",
		CertificateRef: strings.Replace(reference.CertificateRef, "data/", "config/", 1),
		PrivateKeyRef:  strings.Replace(reference.PrivateKeyRef, "data/", "config/", 1),
	}
	writeRuntimeFixture(t, root, runtime)
	validation := RuntimeValidationOptions{HasSuperAdmin: true}
	store := NewTransactionStore(root, validation)
	if _, err := store.CreateBackup(runtime); err != nil {
		t.Fatal(err)
	}
	writer, err := NewFileAuditWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteConfigAudit(context.Background(), ConfigAuditEvent{
		Operation: ConfigOperationUpdate, Section: SectionSMTP, ActorID: "1", ActorRole: "super_admin",
		FromGeneration: 1, ToGeneration: 2, Result: "success", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	// 构造旧版磁盘布局；文件内容及密文均来自真实实现，而不是手写假密钥。
	// 模拟停服时释放暂存租约，真实进程退出后操作系统也会释放文件锁。
	releaseManagedSSL(root, SSLConfig{CertificateRef: reference.CertificateRef})
	entries, _ := os.ReadDir(filepath.Join(root, "data"))
	for _, entry := range entries {
		if err := os.Rename(filepath.Join(root, "data", entry.Name()), filepath.Join(root, "config", entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(filepath.Join(root, "config", "ssl.json"))
	if _, err := credential.NewVault(root); err == nil {
		t.Fatal("未迁移的旧密钥必须阻止生成新密钥")
	}
	if _, err := runtimepath.Migrate(root); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "config", "ssl.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("目录迁移不得篡改当前配置代次或摘要")
	}
	newVault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := OpenStoredSecret(newVault, runtime.Root.Database.Password)
	expected, _ := OpenStoredSecret(vault, runtime.Root.Database.Password)
	if err != nil || secret != expected {
		t.Fatal("配置密文无法继续解密")
	}
	newExternal, err := externalemail.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := newExternal.Decrypt(emailCipher)
	newFingerprint, _ := newExternal.Fingerprint("test@example.com")
	if err != nil || plain != "test@example.com" || newFingerprint != fingerprint {
		t.Fatal("外部邮箱密文或指纹发生改变")
	}
	loaded, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: validation}).Load()
	if err != nil || !strings.HasPrefix(loaded.SSL.TLS.CertificateRef, "data/") {
		t.Fatalf("旧配置未兼容新目录：%v", err)
	}
	resolved, err := resolveManagedSSLReferencesAtRoot(root, loaded.SSL.TLS)
	if err != nil {
		t.Fatal(err)
	}
	material, _ := os.ReadFile(resolved.CertificatePath)
	if !bytes.Equal(material, certificate) {
		t.Fatal("证书发生改变")
	}
	backup, err := store.LoadBackup(runtime.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolveManagedSSLReferencesAtRoot(root, backup.SSL.TLS); err != nil {
		t.Fatal("历史备份中的旧证书引用无法使用")
	}
	setCandidateGeneration(&backup, loaded.Generation+1)
	if err := store.Commit(loaded, backup); err != nil {
		t.Fatal(err)
	}
	writer, err = NewFileAuditWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	items, err := writer.Query(context.Background(), ConfigAuditQuery{})
	if err != nil || len(items) != 1 {
		t.Fatal("历史审计丢失")
	}
	entries, _ = os.ReadDir(filepath.Join(root, "config"))
	if len(entries) != 5 {
		t.Fatal("保存或备份恢复又向 config 写入了运行文件")
	}
}
