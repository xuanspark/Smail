package release

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"git.pcux.cn/xuans/Smail/internal/buildinfo"
)

type releaseFixture struct {
	target string
	main   string
	admin  string
	names  []string
	pair   Pair
}

func newReleaseFixture(t *testing.T) releaseFixture {
	t.Helper()
	root := t.TempDir()
	f := releaseFixture{target: filepath.Join(root, "bin"), main: filepath.Join(root, "main-source"), admin: filepath.Join(root, "admin-source")}
	mainName, adminName := executableNames()
	f.names = []string{mainName, adminName, "release.json"}
	info := buildinfo.Info{Version: "v2", GitHash: "abc", ProtocolVersion: buildinfo.ProtocolVersion}
	f.pair = Pair{Main: info, Admin: info}
	if err := os.Mkdir(f.target, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{f.main, f.admin} {
		if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range f.names {
		if err := os.WriteFile(filepath.Join(f.target, name), []byte("old-"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func assertOldRelease(t *testing.T, f releaseFixture) {
	t.Helper()
	for _, name := range f.names {
		data, err := os.ReadFile(filepath.Join(f.target, name))
		if err != nil || string(data) != "old-"+name {
			t.Fatalf("旧版本未完整保留：%s，错误：%v", name, err)
		}
	}
}

func TestInstallPairPreservesExistingRecoveryBackup(t *testing.T) {
	mainName, adminName := executableNames()
	for _, name := range []string{mainName, adminName, "release.json"} {
		t.Run(name, func(t *testing.T) {
			f := newReleaseFixture(t)
			backup := filepath.Join(f.target, name+".previous")
			if err := os.WriteFile(backup, []byte("retained-evidence"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := InstallPair(f.main, f.admin, f.target, f.pair); !errors.Is(err, ErrUpgradeBackupConflict) {
				t.Fatal("已有恢复备份时仍执行了升级")
			}
			assertOldRelease(t, f)
			data, err := os.ReadFile(backup)
			if err != nil || string(data) != "retained-evidence" {
				t.Fatal("已有恢复备份被删除或改写")
			}
		})
	}
}

func TestInstallPairRestoresEveryRenameFailure(t *testing.T) {
	for failure := 1; failure <= 6; failure++ {
		t.Run(strconv.Itoa(failure), func(t *testing.T) {
			f := newReleaseFixture(t)
			original := renamePath
			t.Cleanup(func() { renamePath = original })
			calls := 0
			renamePath = func(source, target string) error {
				calls++
				if calls == failure {
					return errors.New("simulated rename failure")
				}
				return original(source, target)
			}
			if err := InstallPair(f.main, f.admin, f.target, f.pair); !errors.Is(err, ErrUpgradeRollback) || errors.Is(err, ErrUpgradeRollbackFailed) {
				t.Fatal("替换失败没有返回完整回滚状态")
			}
			assertOldRelease(t, f)
		})
	}
}

func TestInstallPairRollsBackFirstInstallation(t *testing.T) {
	f := newReleaseFixture(t)
	f.target = filepath.Join(filepath.Dir(f.target), "empty-bin")
	original := renamePath
	t.Cleanup(func() { renamePath = original })
	_, adminName := executableNames()
	renamePath = func(source, target string) error {
		if filepath.Base(source) == adminName {
			return errors.New("simulated first installation failure")
		}
		return original(source, target)
	}
	if err := InstallPair(f.main, f.admin, f.target, f.pair); !errors.Is(err, ErrUpgradeRollback) {
		t.Fatal("首次安装失败没有回滚")
	}
	entries, err := os.ReadDir(f.target)
	if err != nil || len(entries) != 0 {
		t.Fatal("首次安装失败后留下不完整程序或备份")
	}
}

func TestInstallPairRejectsDirectoryRecoveryBackup(t *testing.T) {
	f := newReleaseFixture(t)
	backup := filepath.Join(f.target, "release.json.previous")
	if err := os.Mkdir(backup, 0700); err != nil {
		t.Fatal(err)
	}
	if err := InstallPair(f.main, f.admin, f.target, f.pair); !errors.Is(err, ErrUpgradeBackupConflict) {
		t.Fatal("目录形式的已有备份未被保护")
	}
	assertOldRelease(t, f)
	if info, err := os.Stat(backup); err != nil || !info.IsDir() {
		t.Fatal("备份目录被删除或改写")
	}
}

func TestInstallPairDoesNotClaimSuccessfulRollbackWhenRestoreFails(t *testing.T) {
	f := newReleaseFixture(t)
	original := renamePath
	t.Cleanup(func() { renamePath = original })
	mainName, adminName := executableNames()
	renamePath = func(source, target string) error {
		if (filepath.Base(source) == adminName && filepath.Dir(source) != f.target) || filepath.Base(source) == mainName+".previous" {
			return errors.New("simulated install and restore failure")
		}
		return original(source, target)
	}
	err := InstallPair(f.main, f.admin, f.target, f.pair)
	if !errors.Is(err, ErrUpgradeRollbackFailed) || errors.Is(err, ErrUpgradeRollback) {
		t.Fatal("恢复失败仍被标记为回滚成功")
	}
	data, readErr := os.ReadFile(filepath.Join(f.target, mainName+".previous"))
	if readErr != nil || string(data) != "old-"+mainName {
		t.Fatal("恢复失败后未保留旧版本备份")
	}
}

func TestInstallPairReportsRollbackDurabilityFailure(t *testing.T) {
	f := newReleaseFixture(t)
	original := syncReleaseDirectory
	t.Cleanup(func() { syncReleaseDirectory = original })
	syncReleaseDirectory = func(string) error { return errors.New("simulated directory sync failure") }
	err := InstallPair(f.main, f.admin, f.target, f.pair)
	if !errors.Is(err, ErrUpgradeRollbackFailed) || errors.Is(err, ErrUpgradeRollback) {
		t.Fatal("回滚持久化失败没有报告恢复未完成")
	}
	assertOldRelease(t, f)
}

func TestInstallPairRestoresAfterCommitSyncFailure(t *testing.T) {
	f := newReleaseFixture(t)
	original := syncReleaseDirectory
	t.Cleanup(func() { syncReleaseDirectory = original })
	calls := 0
	syncReleaseDirectory = func(path string) error {
		calls++
		if calls == 1 {
			return errors.New("simulated commit sync failure")
		}
		return original(path)
	}
	err := InstallPair(f.main, f.admin, f.target, f.pair)
	if !errors.Is(err, ErrUpgradeRollback) || errors.Is(err, ErrUpgradeRollbackFailed) {
		t.Fatal("持久化失败后的完整回滚状态错误")
	}
	assertOldRelease(t, f)
}
