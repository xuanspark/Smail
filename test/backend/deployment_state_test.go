package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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
				if entry.Name() != "smail-state.db" {
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
	writeRuntimeFixture(t, target, current)
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
