package config

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/internal/configstate"
	"git.pcux.cn/xuans/Smail/utils/credential"
)

func newTestStateVault(root, password string) (StoredSecret, error) {
	vault, err := credential.NewVault(root)
	if err != nil {
		return StoredSecret{}, err
	}
	ciphertext, err := vault.Encrypt(password)
	return StoredSecret{Ciphertext: ciphertext, Version: vault.Version(), UpdatedAt: time.Now().UTC()}, err
}

func TestSelectedSQLiteContainsConfigurationHistory(t *testing.T) {
	root := t.TempDir()
	if err := InitializeDeployment(root); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != ".config-state.lock" {
			t.Fatalf("选库前产生额外文件：%s", entry.Name())
		}
	}
	current := validRuntimeConfig()
	current.Root.Database.LocalRef = "selected.db"
	writeRuntimeFixture(t, root, current)
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true})
	if _, err := store.CreateBackup(current); err != nil {
		t.Fatal(err)
	}
	database, err := store.openState()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM config_records WHERE kind='backup'").Scan(&count)
	database.Close()
	if err != nil || count != 1 {
		t.Fatalf("所选数据库没有配置历史：count=%d, err=%v", count, err)
	}
	entries, err = os.ReadDir(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != ".config-state.lock" && entry.Name() != "selected.db" {
			t.Fatalf("出现额外数据库或文件：%s", entry.Name())
		}
	}
}

func TestDatabaseSwitchBacksUpToTargetWhileSourceLocked(t *testing.T) {
	root := t.TempDir()
	if err := InitializeDeployment(root); err != nil {
		t.Fatal(err)
	}
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManagerWithSecurity(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true}}, ConfigSecurity{Audit: &MemoryAuditWriter{}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := manager.transactions.openState()
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := source.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer source.Exec("ROLLBACK")
	target := manager.Snapshot().Root.Database
	target.LocalRef = "next.db"
	if err := manager.SwitchDatabase(context.Background(), 1, target, authorizedConfigActor()); err != nil {
		t.Fatalf("源库锁定阻止了已完成复制后的配置切换：%v", err)
	}
	if _, err := manager.transactions.LoadBackup(1); err != nil {
		t.Fatalf("切换前历史未写入目标库：%v", err)
	}
	var count int
	if err := source.QueryRow("SELECT COUNT(*) FROM config_records").Scan(&count); err != nil || count != 0 {
		t.Fatalf("切换阶段写入了源库：%d，%v", count, err)
	}
}

func TestUnavailableSelectedDatabaseDoesNotCreateSQLite(t *testing.T) {
	for _, kind := range []DatabaseType{DatabaseTypeMySQL, DatabaseTypePostgreSQL} {
		t.Run(string(kind), func(t *testing.T) {
			root := t.TempDir()
			if err := InitializeDeployment(root); err != nil {
				t.Fatal(err)
			}
			current := validMySQLRuntimeConfig(t, root)
			current.Root.Database.Type = kind
			current.Root.Database.Port = 1
			writeRuntimeFixture(t, root, current)
			store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true})
			if _, err := store.CreateBackup(current); !errors.Is(err, ErrStateDatabaseUnavailable) {
				t.Fatalf("未拒绝不可用数据库：%v", err)
			}
			candidate := cloneRuntimeConfig(current)
			setCandidateGeneration(&candidate, 2)
			store.WithHooks(TransactionHooks{BeforePhase: func(phase TransactionPhase) error {
				if phase == TransactionSectionsCommitted {
					return errors.New("模拟进程中断")
				}
				return nil
			}})
			if err := store.Commit(current, candidate); err == nil {
				t.Fatal("未注入配置提交故障")
			}
			if err := store.Recover(); err != nil {
				t.Fatalf("数据库离线时无法恢复配置：%v", err)
			}
			entries, err := os.ReadDir(filepath.Join(root, "data"))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name() != ".config-state.lock" && entry.Name() != "transaction" {
					t.Fatalf("远程数据库失败后创建了额外文件：%s", entry.Name())
				}
			}
		})
	}
}

func TestLegacyStateImportsAndRecoversIntoSelectedDatabase(t *testing.T) {
	for _, phase := range []TransactionPhase{TransactionPrepared, TransactionSectionsCommitted, TransactionRootCommitted, TransactionVerified} {
		t.Run(string(phase), func(t *testing.T) {
			root := t.TempDir()
			if err := InitializeDeployment(root); err != nil {
				t.Fatal(err)
			}
			current := validRuntimeConfig()
			writeRuntimeFixture(t, root, current)
			filename := filepath.Join(root, "data", "smail-state.db")
			db, err := sql.Open("sqlite", filename)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(configstate.Schema("sqlite"))
			db.Close()
			if err != nil {
				t.Fatal(err)
			}
			legacy := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true})
			legacy.legacyState = true
			if _, err := legacy.CreateBackup(current); err != nil {
				t.Fatal(err)
			}
			candidate := cloneRuntimeConfig(current)
			setCandidateGeneration(&candidate, 2)
			legacy.WithHooks(TransactionHooks{BeforePhase: func(actual TransactionPhase) error {
				if actual == phase {
					return errors.New("模拟旧版中断")
				}
				return nil
			}})
			if err := legacy.commitState(current, candidate); err == nil {
				t.Fatal("未注入旧版故障")
			}
			store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true})
			for i := 0; i < 2; i++ {
				if err := store.Recover(); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(filename); !os.IsNotExist(err) {
				t.Fatal("旧状态库仍在运行路径中")
			}
			if _, err := store.LoadBackup(1); err != nil {
				t.Fatalf("升级后历史丢失：%v", err)
			}
			loaded, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: store.validation}).Load()
			want := uint64(1)
			if phase == TransactionRootCommitted || phase == TransactionVerified {
				want = 2
			}
			if err != nil || loaded.Generation != want {
				t.Fatalf("旧事务恢复代次错误：%d，%v", loaded.Generation, err)
			}
		})
	}
}

// 云端测试必须使用预先清空的专用测试库；凭据只从进程环境读取。
func TestSelectedDatabaseLiveIntegration(t *testing.T) {
	kind := DatabaseType(os.Getenv("SMAIL_TEST_STATE_DATABASE"))
	if kind == "" {
		t.Skip("未配置云端专用数据库")
	}
	if kind != DatabaseTypeMySQL && kind != DatabaseTypePostgreSQL && kind != DatabaseTypeSQLite {
		t.Fatal("测试数据库类型无效")
	}
	root := t.TempDir()
	if err := InitializeDeployment(root); err != nil {
		t.Fatal(err)
	}
	current := validRuntimeConfig()
	if kind != DatabaseTypeSQLite {
		current = validMySQLRuntimeConfig(t, root)
		current.Root.Database.Type = kind
		current.Root.Database.Username = os.Getenv("SMAIL_TEST_STATE_USER")
		current.Root.Database.Name = os.Getenv("SMAIL_TEST_STATE_NAME")
		if kind == DatabaseTypePostgreSQL {
			current.Root.Database.Port = DefaultPostgreSQLPort
		}
		vault, err := newTestStateVault(root, os.Getenv("SMAIL_TEST_STATE_PASSWORD"))
		if err != nil {
			t.Fatal("测试凭据初始化失败")
		}
		current.Root.Database.Password = vault
	}
	writeRuntimeFixture(t, root, current)
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true})
	database, err := store.openState()
	if err != nil {
		t.Fatal(err)
	}
	dialect, err := migration.ParseDialect(database.driver)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (migration.Runner{DB: database.DB, Dialect: dialect, Migrations: migration.DefaultMigrations()}).Run(context.Background())
	database.Close()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 6; index++ {
		if _, err := store.createBackupAt(current, time.Now().Add(time.Duration(index)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	store = NewTransactionStore(root, store.validation)
	if err := store.Recover(); err != nil {
		t.Fatal(err)
	}
	items, err := store.stateBackups()
	if err != nil || len(items) != 4 {
		t.Fatalf("真实数据库历史读写或保留策略失败：%d，%v", len(items), err)
	}
	if _, err := store.LoadBackup(1); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == ".config-state.lock" || entry.Name() == "transaction" || kind == DatabaseTypeSQLite && entry.Name() == "smail.db" {
			continue
		}
		t.Fatalf("真实数据库模式产生额外数据库：%s", entry.Name())
	}
}
