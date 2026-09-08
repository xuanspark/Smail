package runtimepath

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func put(t *testing.T, root, name string, data []byte) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeMigrationPreservesFilesAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	for name := range configurationFiles {
		put(t, root, "config/"+name, []byte(`{"generation":21}`))
	}
	for _, name := range []string{"credential.key", "external-email-encryption.key", "external-email-fingerprint.key", "smail.db", "smail.db-wal", "smail.db-shm", "ssl/managed/upload/current/private.key", "dkim/dkim.priv", "backups-v2/old/files/config.json", "transaction/pending/manifest.json", "config-audit-v2.jsonl", "config.dev.json"} {
		put(t, root, "config/"+name, []byte("preserved:"+name))
	}
	if !errors.Is(CheckReady(root), ErrMigrationRequired) {
		t.Fatal("旧目录应阻止启动")
	}
	plan, err := Plan(root)
	if err != nil {
		t.Fatal(err)
	}
	original := map[string][]byte{}
	for _, item := range plan {
		if info, _ := os.Stat(item.Source); info.Mode().IsRegular() {
			original[item.Target], _ = os.ReadFile(item.Source)
		}
	}
	if _, err := Migrate(root); err != nil {
		t.Fatal(err)
	}
	for path, expected := range original {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatal("迁移改变了文件内容")
		}
	}
	entries, _ := os.ReadDir(filepath.Join(root, "config"))
	if len(entries) != 5 {
		t.Fatal("config 必须恰好保留五个文件")
	}
	for _, entry := range entries {
		data, _ := os.ReadFile(filepath.Join(root, "config", entry.Name()))
		if string(data) != `{"generation":21}` {
			t.Fatal("当前配置被覆盖")
		}
	}
	if moves, err := Migrate(root); err != nil || len(moves) != 0 {
		t.Fatal("重复迁移不应改动文件")
	}
}

func TestRuntimeMigrationRejectsConflictAndUnknownFilesBeforeMoving(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		root := t.TempDir()
		put(t, root, "config/credential.key", []byte("old"))
		if conflict {
			put(t, root, "data/credential.key", []byte("new"))
		} else {
			put(t, root, "config/unknown", []byte("unknown"))
		}
		if _, err := Migrate(root); err == nil {
			t.Fatal("冲突或未知文件应拒绝自动迁移")
		}
		data, _ := os.ReadFile(filepath.Join(root, "config", "credential.key"))
		if string(data) != "old" {
			t.Fatal("检查失败不应移动密钥")
		}
	}
}

func TestRuntimeMigrationRejectsLinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "config")); err != nil {
		t.Skipf("当前环境不支持符号链接：%v", err)
	}
	if _, err := Migrate(root); err == nil {
		t.Fatal("符号链接必须被拒绝")
	}
}

func TestReadyDeploymentCannotCreateNewDatabaseOrKeys(t *testing.T) {
	root := t.TempDir()
	put(t, root, "config/config.json", []byte(`{"setupState":"ready","database":{"type":"sqlite","localRef":"mail.db"}}`))
	if err := CheckReady(root); err == nil {
		t.Fatal("必要文件缺失必须阻止启动")
	}
	if _, err := os.Stat(filepath.Join(root, "data")); !os.IsNotExist(err) {
		t.Fatal("只读启动检查不应生成空数据库或密钥")
	}
}

func TestRuntimeMigrationRollsBackOnMoveFailure(t *testing.T) {
	root := t.TempDir()
	put(t, root, "config/credential.key", []byte("original-key"))
	put(t, root, "config/smail.db", []byte("original-database"))
	calls := 0
	_, err := migrate(root, func(source, target string) error {
		calls++
		if calls == 2 {
			return errors.New("模拟迁移中途失败")
		}
		return os.Rename(source, target)
	})
	if err == nil || calls != 3 {
		t.Fatal("应执行逆序回滚")
	}
	for name, expected := range map[string]string{"credential.key": "original-key", "smail.db": "original-database"} {
		data, err := os.ReadFile(filepath.Join(root, "config", name))
		if err != nil || string(data) != expected {
			t.Fatal("回滚后原文件应完整保留")
		}
	}
	if _, err := Migrate(root); err != nil {
		t.Fatal("正常回滚后应能重新迁移", err)
	}
}

func TestRuntimeMigrationLockBlocksConcurrentMigrationAndStartup(t *testing.T) {
	root := t.TempDir()
	put(t, root, "data/"+migrationLock, nil)
	if _, err := Migrate(root); err == nil {
		t.Fatal("并发迁移或中断现场必须拒绝继续")
	}
	if err := CheckReady(root); err == nil {
		t.Fatal("迁移锁必须阻止主服务启动")
	}
}

func TestLegacyReferenceOnlyChangesKnownRelativeDirectories(t *testing.T) {
	for input, expected := range map[string]string{
		"./config/ssl/public.crt": "data/ssl/public.crt",
		"config/dkim/dkim.priv":   "data/dkim/dkim.priv",
		"/config/ssl/public.crt":  "/config/ssl/public.crt",
		"config/ssl-other/key":    "config/ssl-other/key",
		"../config/ssl/key":       "../config/ssl/key",
	} {
		if actual := LegacyReference(input); actual != expected {
			t.Fatalf("相对目录转换错误：%q", input)
		}
	}
}
