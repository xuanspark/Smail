// Package runtimepath 统一运行数据目录；配置文件仍固定保存在 config 中。
package runtimepath

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrMigrationRequired = errors.New("运行数据仍在 config 中，请停止服务并运行 smail-config --runtime-data --apply")

const migrationLock = ".runtime-migration.lock"

// Directory 创建受保护的数据目录，拒绝符号链接及普通文件。
func Directory(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("运行根目录为空")
	}
	directory := filepath.Join(root, "data")
	if err := os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("运行数据目录无效")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return "", err
	}
	return directory, nil
}

// LegacyReference 只转换旧运行目录的相对引用，不放宽路径安全检查。
func LegacyReference(value string) string {
	valueWithoutDot := strings.TrimPrefix(value, "./")
	for _, prefix := range []string{"config/ssl", "config/dkim"} {
		if valueWithoutDot == prefix || strings.HasPrefix(valueWithoutDot, prefix+"/") {
			return "data/" + strings.TrimPrefix(valueWithoutDot, "config/")
		}
	}
	return value
}

var configurationFiles = map[string]bool{
	"config.json": true, "smtp.json": true, "email.json": true,
	"ssl.json": true, "plugins.json": true,
}

var runtimeEntries = map[string]bool{
	"credential.key": true, "external-email-encryption.key": true,
	"external-email-fingerprint.key": true, "config-audit-v2.jsonl": true,
	"ssl": true, "dkim": true, "backups": true, "backups-v2": true,
	"migration-backups": true, "transaction": true,
}

var obsoleteFiles = map[string]bool{
	"config.dev.json": true, "config_mysql.json": true, "config.json.legacy": true,
}

type Move struct {
	Source string
	Target string
}

// Plan 检查整个迁移集合，未知文件、目标冲突或链接均拒绝自动处理。
func Plan(root string) ([]Move, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	configDirectory := filepath.Join(absolute, "config")
	if err := safeTree(configDirectory); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	dataDirectory := filepath.Join(absolute, "data")
	// 只检查目标父目录，不在每次启动时遍历全部邮件备份和证书历史。
	if err := safeDirectory(dataDirectory); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	entries, err := os.ReadDir(configDirectory)
	if err != nil {
		return nil, err
	}
	var moves []Move
	for _, entry := range entries {
		name := entry.Name()
		if configurationFiles[name] && !entry.IsDir() {
			continue
		}
		target := filepath.Join(absolute, "data", name)
		switch {
		case obsoleteFiles[name] && !entry.IsDir():
			target = filepath.Join(absolute, "data", "legacy-config", name)
		case runtimeEntries[name], !entry.IsDir() && databaseFile(name):
		default:
			return nil, fmt.Errorf("config 中存在未识别项目，请人工核对：%s", name)
		}
		if err := safeDirectory(filepath.Dir(target)); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if _, err := os.Lstat(target); err == nil {
			return nil, fmt.Errorf("迁移目标已存在，拒绝覆盖：%s", name)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		moves = append(moves, Move{Source: filepath.Join(configDirectory, name), Target: target})
	}
	return moves, nil
}

func databaseFile(name string) bool {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		name = strings.TrimSuffix(name, suffix)
	}
	switch filepath.Ext(name) {
	case ".db", ".sqlite", ".sqlite3":
		return true
	}
	return false
}

func safeTree(path string) error {
	return filepath.WalkDir(path, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || (!entry.IsDir() && !entry.Type().IsRegular()) {
			return errors.New("迁移目录中存在链接或特殊文件")
		}
		return nil
	})
}

func safeDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("运行目录必须是实际目录，不能是链接或普通文件")
	}
	return nil
}

// CheckReady 防止遗漏迁移时创建空账号库或重新生成加密密钥。
func CheckReady(root string) error {
	if err := CheckKeyLayout(root); err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(root, "data", migrationLock)); err == nil || !os.IsNotExist(err) {
		return errors.New("运行目录迁移尚未结束，请保留现场并核对迁移锁")
	}
	moves, err := Plan(root)
	if err != nil {
		return err
	}
	if len(moves) != 0 {
		return ErrMigrationRequired
	}
	// 已初始化部署缺失账号库或加密密钥时必须报错，不能按首次安装处理。
	raw, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var config struct {
		SetupState string `json:"setupState"`
		Database   struct {
			Type     string `json:"type"`
			LocalRef string `json:"localRef"`
		} `json:"database"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	if config.SetupState != "ready" {
		return nil
	}
	required := BusinessKeyNames()
	if config.Database.Type == "sqlite" {
		if !filepath.IsLocal(config.Database.LocalRef) {
			return errors.New("SQLite 本地引用无效")
		}
		required = append(required, config.Database.LocalRef)
	}
	for _, name := range required {
		base := filepath.Join(root, "data")
		if strings.HasSuffix(name, ".key") {
			base = filepath.Dir(KeyPath(root, name))
			if DeploymentLayout(root) {
				if err := safeDirectory(filepath.Join(root, "ssl")); err != nil {
					return err
				}
			}
		}
		if err := safeDirectory(base); err != nil {
			return err
		}
		parent := filepath.Dir(name)
		for parent != "." {
			if err := safeDirectory(filepath.Join(base, parent)); err != nil {
				return err
			}
			parent = filepath.Dir(parent)
		}
		info, err := os.Lstat(filepath.Join(base, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("已初始化部署缺少必要运行文件，拒绝重新创建：%s", name)
		}
	}
	return nil
}

// Migrate 必须停服调用；同卷重命名保留文件内容，失败逆序回滚，重试保持幂等。
func Migrate(root string) ([]Move, error) {
	return migrate(root, os.Rename)
}

func migrate(root string, rename func(string, string) error) ([]Move, error) {
	moves, err := Plan(root)
	if err != nil {
		return moves, err
	}
	directory, err := Directory(root)
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(directory, migrationLock)
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, errors.New("无法取得迁移锁；请确认没有其他迁移进程，并人工核对中断现场")
	}
	if err := lock.Close(); err != nil {
		return nil, err
	}
	keepLock := false
	defer func() {
		if !keepLock {
			_ = os.Remove(lockPath)
		}
	}()
	// 取得独占锁后重新检查，避免两个迁移进程使用同一份旧计划。
	moves, err = Plan(root)
	if err != nil {
		return nil, err
	}
	var completed []Move
	for _, move := range moves {
		if err = os.MkdirAll(filepath.Dir(move.Target), 0700); err == nil {
			err = rename(move.Source, move.Target)
		}
		if err != nil {
			for index := len(completed) - 1; index >= 0; index-- {
				item := completed[index]
				rollbackErr := rename(item.Target, item.Source)
				keepLock = keepLock || rollbackErr != nil
				err = errors.Join(err, rollbackErr)
			}
			return nil, fmt.Errorf("运行数据迁移失败，已尝试回滚：%w", err)
		}
		completed = append(completed, move)
	}
	return completed, nil
}

// CleanCallerPath 清洗调用者代码路径，去除本地盘符和宿主机绝对目录，只保留模块相对路径。
func CleanCallerPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	normalized := strings.ReplaceAll(path, string(filepath.Separator), "/")
	normalized = strings.ReplaceAll(normalized, "\\", "/")
	for _, target := range []string{"backend/", "frontend/", "plugins/"} {
		if index := strings.Index(normalized, target); index != -1 {
			return normalized[index:]
		}
	}
	marker := "smail/"
	lower := strings.ToLower(normalized)
	if index := strings.Index(lower, marker); index != -1 {
		return normalized[index+len(marker):]
	}
	if len(normalized) >= 2 && normalized[1] == ':' {
		return strings.TrimLeft(normalized[2:], "/")
	}
	return normalized
}
