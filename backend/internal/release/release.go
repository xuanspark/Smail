package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/internal/buildinfo"
)

var (
	ErrInvalidPair           = errors.New("invalid release pair")
	ErrUpgradeRollback       = errors.New("release upgrade rolled back")
	ErrUpgradeRollbackFailed = errors.New("release rollback incomplete; preserve backups for manual recovery")
	ErrUpgradeBackupConflict = errors.New("release recovery backup already exists")
	ErrUpgradeDirectory      = errors.New("release target directory is invalid")
)

type Manifest struct {
	Version         string `json:"version"`
	GitHash         string `json:"gitHash"`
	ProtocolVersion int    `json:"protocolVersion"`
	CreatedAt       string `json:"createdAt"`
}

type Pair struct {
	Main  buildinfo.Info
	Admin buildinfo.Info
}

func NewManifest(pair Pair, now time.Time) (Manifest, error) {
	if err := buildinfo.ValidatePair(pair.Main, pair.Admin); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrInvalidPair, err)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return Manifest{
		Version:         pair.Main.Version,
		GitHash:         pair.Main.GitHash,
		ProtocolVersion: pair.Main.ProtocolVersion,
		CreatedAt:       now.UTC().Format(time.RFC3339),
	}, nil
}

func ValidateManifest(manifest Manifest, pair Pair) error {
	if err := buildinfo.ValidatePair(pair.Main, pair.Admin); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPair, err)
	}
	if strings.TrimSpace(manifest.Version) == "" || manifest.ProtocolVersion != buildinfo.ProtocolVersion {
		return ErrInvalidPair
	}
	if _, err := time.Parse(time.RFC3339, manifest.CreatedAt); err != nil {
		return ErrInvalidPair
	}
	if manifest.Version != pair.Main.Version || manifest.ProtocolVersion != pair.Main.ProtocolVersion {
		return ErrInvalidPair
	}
	if known(manifest.GitHash) && known(pair.Main.GitHash) && manifest.GitHash != pair.Main.GitHash {
		return ErrInvalidPair
	}
	return nil
}

func InstallPair(mainSource, adminSource, targetDir string, pair Pair) error {
	if strings.TrimSpace(targetDir) == "" || filepath.Clean(targetDir) == "." {
		return ErrUpgradeDirectory
	}
	manifest, err := NewManifest(pair, time.Now().UTC())
	if err != nil {
		return err
	}
	mainName, adminName := executableNames()
	if err := validateExecutableSource(mainSource); err != nil {
		return fmt.Errorf("validate main executable: %w", err)
	}
	if err := validateExecutableSource(adminSource); err != nil {
		return fmt.Errorf("validate admin executable: %w", err)
	}

	parent := filepath.Dir(filepath.Clean(targetDir))
	if err := os.MkdirAll(parent, 0750); err != nil {
		return fmt.Errorf("create release parent: %w", err)
	}
	stage, err := os.MkdirTemp(parent, ".smail-release-")
	if err != nil {
		return fmt.Errorf("create release staging directory: %w", err)
	}
	defer os.RemoveAll(stage)
	if err := copyExecutable(mainSource, filepath.Join(stage, mainName), 0755); err != nil {
		return fmt.Errorf("stage main executable: %w", err)
	}
	if err := copyExecutable(adminSource, filepath.Join(stage, adminName), 0750); err != nil {
		return fmt.Errorf("stage admin executable: %w", err)
	}
	manifestPath := filepath.Join(stage, "release.json")
	if err := writeManifest(manifestPath, manifest); err != nil {
		return err
	}
	if err := syncDirectory(stage); err != nil {
		return err
	}
	if err := os.MkdirAll(targetDir, 0750); err != nil {
		return fmt.Errorf("create release target: %w", err)
	}

	return replacePair(stage, targetDir, []string{mainName, adminName, "release.json"})
}

var (
	renamePath           = os.Rename
	syncReleaseDirectory = syncDirectory
)

func replacePair(stage, targetDir string, names []string) error {
	// 先检查所有备份，不能因升级新版本而删除上次未完成恢复的证据。
	for _, name := range names {
		target := filepath.Join(targetDir, name)
		if _, err := os.Lstat(target + ".previous"); err == nil {
			return fmt.Errorf("%w: %s", ErrUpgradeBackupConflict, name)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect recovery backup %s: %w", name, err)
		}
		if info, err := os.Lstat(target); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("%w: %s", ErrUpgradeDirectory, name)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect release target %s: %w", name, err)
		}
	}
	committed := make(map[string]bool, len(names))
	backups := make(map[string]string, len(names))
	rollback := func(cause error) error {
		var failures []error
		for index := len(names) - 1; index >= 0; index-- {
			name := names[index]
			target := filepath.Join(targetDir, name)
			if committed[name] {
				if err := os.Remove(target); err != nil {
					failures = append(failures, fmt.Errorf("remove replacement %s: %w", name, err))
					continue
				}
			}
			if backup, ok := backups[name]; ok {
				if err := renamePath(backup, target); err != nil {
					failures = append(failures, fmt.Errorf("restore %s: %w", name, err))
				}
			}
		}
		if err := syncReleaseDirectory(targetDir); err != nil {
			failures = append(failures, fmt.Errorf("sync restored release: %w", err))
		}
		if len(failures) != 0 {
			return errors.Join(cause, ErrUpgradeRollbackFailed, errors.Join(failures...))
		}
		return errors.Join(cause, ErrUpgradeRollback)
	}

	for _, name := range names {
		target := filepath.Join(targetDir, name)
		if _, err := os.Stat(target); err == nil {
			backup := target + ".previous"
			if err := renamePath(target, backup); err != nil {
				return rollback(fmt.Errorf("backup %s: %w", name, err))
			}
			backups[name] = backup
		} else if !os.IsNotExist(err) {
			return rollback(fmt.Errorf("inspect %s: %w", name, err))
		}
		if err := renamePath(filepath.Join(stage, name), target); err != nil {
			return rollback(fmt.Errorf("install %s: %w", name, err))
		}
		committed[name] = true
	}

	if err := syncReleaseDirectory(targetDir); err != nil {
		return rollback(fmt.Errorf("sync release target: %w", err))
	}
	for _, backup := range backups {
		// 备份清理失败不回滚已经持久化的新版本，避免把升级结果置于不一致状态。
		_ = os.Remove(backup)
	}
	return nil
}

func ReadManifest(targetDir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(targetDir, "release.json"))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	return manifest, nil
}

func executableNames() (string, string) {
	if runtime.GOOS == "windows" {
		return "smail.exe", "smail-admin.exe"
	}
	return "smail", "smail-admin"
}

func validateExecutableSource(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() == 0 {
		return ErrInvalidPair
	}
	return nil
}

func copyExecutable(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func writeManifest(path string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode release manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0640); err != nil {
		return fmt.Errorf("write release manifest: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func known(value string) bool {
	return strings.TrimSpace(value) != "" && value != "unknown" && value != "dev"
}
