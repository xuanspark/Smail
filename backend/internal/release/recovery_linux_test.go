//go:build linux

package release

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallPairPreservesDanglingRecoveryLink(t *testing.T) {
	f := newReleaseFixture(t)
	backup := filepath.Join(f.target, "release.json.previous")
	missing := filepath.Join(filepath.Dir(f.target), "missing")
	if err := os.Symlink(missing, backup); err != nil {
		t.Fatal(err)
	}
	if err := InstallPair(f.main, f.admin, f.target, f.pair); !errors.Is(err, ErrUpgradeBackupConflict) {
		t.Fatal("悬空备份链接未在升级前拒绝")
	}
	assertOldRelease(t, f)
	if target, err := os.Readlink(backup); err != nil || target != missing {
		t.Fatal("悬空备份链接被修改")
	}
}

func TestInstallPairRejectsLinkedTargetEntry(t *testing.T) {
	f := newReleaseFixture(t)
	f.target = filepath.Join(filepath.Dir(f.target), "linked-bin")
	if err := os.Mkdir(f.target, 0700); err != nil {
		t.Fatal(err)
	}
	mainName, _ := executableNames()
	if err := os.Symlink(f.main, filepath.Join(f.target, mainName)); err != nil {
		t.Fatal(err)
	}
	if err := InstallPair(f.main, f.admin, f.target, f.pair); !errors.Is(err, ErrUpgradeDirectory) {
		t.Fatal("符号链接形式的目标程序未被拒绝")
	}
	if data, err := os.ReadFile(f.main); err != nil || string(data) != "new" {
		t.Fatal("链接指向的外部文件被改写")
	}
	if target, err := os.Readlink(filepath.Join(f.target, mainName)); err != nil || target != f.main {
		t.Fatal("原有目标链接被改写")
	}
}
