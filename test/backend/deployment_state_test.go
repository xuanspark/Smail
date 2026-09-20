package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/utils/credential"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
)

func TestDeploymentStateRecoversEveryCommitPhase(t *testing.T) {
	for _, phase := range []TransactionPhase{TransactionPrepared, TransactionSectionsCommitted, TransactionRootCommitted, TransactionVerified} {
		t.Run(string(phase), func(t *testing.T) {
			root := t.TempDir()
			if err := InitializeDeployment(root); err != nil {
				t.Fatal(err)
			}
			current := validRuntimeConfig()
			writeRuntimeFixture(t, root, current)
			candidate := cloneRuntimeConfig(current)
			setCandidateGeneration(&candidate, 2)
			candidate.Email.Policy.MaxMessageBytes = 9 << 20
			validation := RuntimeValidationOptions{HasSuperAdmin: true, Production: true}
			store := NewTransactionStore(root, validation).WithHooks(TransactionHooks{BeforePhase: func(actual TransactionPhase) error {
				if actual == phase {
					return errors.New("模拟进程中断")
				}
				return nil
			}})
			if err := store.Commit(current, candidate); err == nil {
				t.Fatal("未注入中断")
			}
			if err := NewTransactionStore(root, validation).Recover(); err != nil {
				t.Fatal(err)
			}
			loaded, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: validation}).Load()
			if err != nil {
				t.Fatal(err)
			}
			want := uint64(1)
			if phase == TransactionRootCommitted || phase == TransactionVerified {
				want = 2
			}
			if loaded.Generation != want {
				t.Fatalf("恢复代次错误：%d", loaded.Generation)
			}
			if err := store.Recover(); err != nil {
				t.Fatal(err)
			}
			entries, _ := os.ReadDir(filepath.Join(root, "config"))
			if len(entries) != 5 {
				t.Fatal("config 生成了额外文件")
			}
			entries, _ = os.ReadDir(filepath.Join(root, "data"))
			for _, entry := range entries {
				if entry.Name() != ".config-state.lock" && entry.Name() != "transaction" {
					t.Fatalf("data 中产生额外文件：%s", entry.Name())
				}
			}
		})
	}
}

func TestDeploymentStateImportsHistoryWithoutChangingSecret(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	current := validMySQLRuntimeConfig(t, source)
	writeRuntimeFixture(t, source, current)
	validation := RuntimeValidationOptions{HasSuperAdmin: true}
	old := NewTransactionStore(source, validation)
	backup, err := old.CreateBackup(current)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(backup, "files", "config.json"))
	if err := InitializeDeployment(target); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimepath.SecureDirectory(target, "ssl"); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(source, "data", "credential.key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "ssl", "credential.key"), key, 0600); err != nil {
		t.Fatal(err)
	}
	targetConfig := cloneRuntimeConfig(current)
	targetConfig.Root.Database = DatabaseConfig{Type: DatabaseTypeSQLite, LocalRef: "smail.db"}
	writeRuntimeFixture(t, target, targetConfig)
	if err := ImportDeploymentHistory(source, target, validation); err != nil {
		t.Fatal(err)
	}
	store := NewTransactionStore(target, validation)
	loaded, err := store.LoadBackup(current.Generation)
	if err != nil {
		t.Fatal(err)
	}
	vault, err := credential.NewVault(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStoredSecret(vault, loaded.Root.Database.Password); err != nil {
		t.Fatal("迁移后密文无法解密")
	}
	items, err := store.stateBackups()
	if err != nil || len(items) != 1 {
		t.Fatalf("历史导入失败：%v", err)
	}
	if !bytes.Equal(before, items[0].Original["config.json"]) {
		t.Fatal("历史原始内容被改写")
	}
	for index := 0; index < 6; index++ {
		if _, err := store.createBackupAt(current, time.Now().Add(time.Duration(index)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	items, err = store.stateBackups()
	if err != nil || len(items) != 4 {
		t.Fatalf("历史保留策略错误：%v", err)
	}
}

func TestDeploymentStateRejectsTamperAndConcurrentWriter(t *testing.T) {
	root := t.TempDir()
	if err := InitializeDeployment(root); err != nil {
		t.Fatal(err)
	}
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true})
	if _, err := store.CreateBackup(current); err != nil {
		t.Fatal(err)
	}
	unlock, err := store.lockState()
	if err != nil {
		t.Fatal(err)
	}
	if another, err := store.lockState(); err == nil {
		another()
		t.Fatal("并行写入没有被拒绝")
	}
	unlock()
	db, err := store.openState()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("UPDATE config_records SET payload=? WHERE kind='backup'", []byte("{}"))
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadBackup(1); !errors.Is(err, ErrConfigDigestMismatch) {
		t.Fatalf("篡改未被拒绝：%v", err)
	}
}

func TestDeploymentManagedReferenceUsesArgumentRoot(t *testing.T) {
	root := t.TempDir()
	if err := InitializeDeployment(root); err != nil {
		t.Fatal(err)
	}
	private, certificate := testManagedCertificate(t, "example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048)
	ref, err := importManagedSSLAtRoot(root, ManagedSSLModeUpload, private, certificate, nil, []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if ref.CertificateRef != "ssl/managed/upload/current/public.crt" {
		t.Fatalf("引用错误：%s", ref.CertificateRef)
	}
}

func TestDeploymentAutomaticallyCreatesBusinessDirectories(t *testing.T) {
	root := t.TempDir()
	if err := InitializeDeployment(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config", "ssl", "data", "log"} {
		if info, err := os.Lstat(filepath.Join(root, name)); err != nil || !info.IsDir() {
			t.Fatalf("正式启动没有创建部署目录：%s", name)
		}
	}
	for _, name := range []string{"credentials", "external-email-encryption", "external-email-fingerprint", "dkim"} {
		info, err := os.Lstat(filepath.Join(root, "ssl", name))
		if err != nil || !info.IsDir() {
			t.Fatalf("程序启动没有创建业务目录：%s", name)
		}
	}
	if _, err := credential.Encrypt(root, "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := externalemail.NewVault(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range runtimepath.BusinessKeyNames() {
		if filepath.Dir(runtimepath.KeyPath(root, name)) == filepath.Join(root, "ssl") {
			t.Fatal("新密钥仍在 ssl 根目录生成")
		}
		if info, err := os.Stat(runtimepath.KeyPath(root, name)); err != nil || info.Size() != 32 {
			t.Fatal("业务密钥未正确生成")
		}
	}
}

func TestDeploymentAutomaticallyMigratesOriginalKeyBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "ssl"), 0700); err != nil {
		t.Fatal(err)
	}
	for index, name := range runtimepath.BusinessKeyNames() {
		if err := os.WriteFile(filepath.Join(root, "ssl", name), bytes.Repeat([]byte{byte(index + 1)}, 32), 0600); err != nil {
			t.Fatal(err)
		}
	}
	secret, err := credential.Encrypt(root, "preserved-secret")
	if err != nil {
		t.Fatal(err)
	}
	oldVault, err := externalemail.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	email, _ := oldVault.Encrypt("user@example.com")
	fingerprint, _ := oldVault.Fingerprint("user@example.com")
	for attempt := 0; attempt < 3; attempt++ {
		if err := InitializeDeployment(root); err != nil {
			t.Fatal(err)
		}
		for index, name := range runtimepath.BusinessKeyNames() {
			actual, err := os.ReadFile(runtimepath.KeyPath(root, name))
			if err != nil || !bytes.Equal(actual, bytes.Repeat([]byte{byte(index + 1)}, 32)) {
				t.Fatal("迁移或重复启动改变了原密钥")
			}
			if _, err := os.Lstat(filepath.Join(root, "ssl", name)); !os.IsNotExist(err) {
				t.Fatal("旧位置未迁移")
			}
		}
	}
	if value, err := credential.Decrypt(root, secret); err != nil || value != "preserved-secret" {
		t.Fatal("旧凭据密文无法解密")
	}
	vault, err := externalemail.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := vault.Decrypt(email); err != nil || value != "user@example.com" {
		t.Fatal("外部邮箱旧密文无法解密")
	}
	if value, err := vault.Fingerprint("user@example.com"); err != nil || value != fingerprint {
		t.Fatal("邮箱索引指纹发生变化")
	}
}
