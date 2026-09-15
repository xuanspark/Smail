package setup

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func prepareUniqueAdminDatabase(t *testing.T) {
	t.Helper()
	oldConfig, oldSnapshot := config.Instance, config.CurrentConfig()
	oldManager, oldDB := config.CurrentManager(), db.Instance
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "setup.db")}
	config.PublishConfigSnapshot(config.Instance)
	config.SetRuntimeManager(nil)
	db.Instance = nil
	t.Setenv("SMAIL_ENABLE_LEGACY_SYNC2", "0")
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		db.Instance = oldDB
		config.Instance = oldConfig
		config.PublishConfigSnapshot(oldSnapshot)
		config.SetRuntimeManager(oldManager)
	})
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentAdministratorInitialization(t *testing.T) {
	prepareUniqueAdminDatabase(t)
	var group sync.WaitGroup
	results := make(chan error, 4)
	for _, account := range []string{"first", "second", "third", "fourth"} {
		group.Add(1)
		go func(account string) {
			defer group.Done()
			results <- SetAdminPassword(&appcontext.Context{}, account, "StrongP12")
		}(account)
	}
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("初始化成功次数：%d", successes)
	}
	if err := db.ValidateSuperAdmin(db.Instance); err != nil {
		t.Fatal(err)
	}
}

func TestSetupRejectsMissingMigrationLock(t *testing.T) {
	prepareUniqueAdminDatabase(t)
	// 仅破坏本测试的临时迁移记录，不读取或清空运行数据库。
	if _, err := db.Instance.Exec("DELETE FROM schema_migration WHERE version = 1"); err != nil {
		t.Fatal(err)
	}
	if err := SetAdminPassword(&appcontext.Context{}, "root", "StrongP12"); err == nil {
		t.Fatal("缺少锁记录仍创建了账号")
	}
	count, err := db.Instance.Count(new(models.User))
	if err != nil || count != 0 {
		t.Fatalf("失败初始化写入了账号：%d %v", count, err)
	}
}

func TestSetupDoesNotPromoteSoleOrdinaryAccount(t *testing.T) {
	prepareUniqueAdminDatabase(t)
	user := models.User{Account: "ordinary", Name: "ordinary", Password: "unused", Role: "user"}
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.Instance.Close(); err != nil {
		t.Fatal(err)
	}
	db.Instance = nil
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	account, err := GetAdminPassword(&appcontext.Context{})
	if err != nil || account != "" {
		t.Fatalf("把普通用户当作超级管理员：%q %v", account, err)
	}
	if err := db.ValidateSuperAdmin(db.Instance); !errors.Is(err, db.ErrSuperAdminInvariant) {
		t.Fatal("普通用户被自动提权")
	}
	if err := SetAdminPassword(&appcontext.Context{}, "root", "StrongP12"); err == nil {
		t.Fatal("非空库重新初始化")
	}
	for _, account := range []string{"root", "duplicate"} {
		if _, err := db.Instance.Insert(&models.User{Account: account, Name: account, Password: "unused", Role: "super_admin"}); err != nil {
			t.Fatal(err)
		}
		got, err := GetAdminPassword(&appcontext.Context{})
		if account == "root" && (err != nil || got != "root") {
			t.Fatalf("超级管理员查询错误：%q %v", got, err)
		}
		if account == "duplicate" && !errors.Is(err, db.ErrSuperAdminInvariant) {
			t.Fatal("重复超级管理员没有被拒绝")
		}
	}
}

func TestFinishRejectsDuplicateSuperAdminBeforeWritingConfiguration(t *testing.T) {
	prepareFinishConfigTest(t)
	if err := config.WriteConfig(&config.Config{SetupState: config.SetupStateBootstrap}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&models.User{Account: "duplicate", Name: "test", Password: "unused", Role: "super_admin"}); err != nil {
		t.Fatal(err)
	}
	if err := Finish(); !errors.Is(err, db.ErrSuperAdminInvariant) {
		t.Fatal("初始化完成时未验证唯一超级管理员")
	}
	cfg, err := config.ReadConfig()
	if err != nil || cfg.IsInit || cfg.SetupState != config.SetupStateBootstrap {
		t.Fatal("非法账号状态仍完成了初始化")
	}
}
