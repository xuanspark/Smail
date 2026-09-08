package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
	fileutil "git.pcux.cn/xuans/Smail/utils/file"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
)

var (
	ErrGenerationConflict = errors.New("configuration generation conflict")
	ErrRollbackInProgress = errors.New("configuration rollback is already in progress")
	ErrNoBackup           = errors.New("configuration backup not found")
	ErrManagerStorage     = errors.New("configuration manager storage unavailable")
)

type RuntimeSnapshot struct {
	Config     RootConfig
	Generation uint64
}

type Manager struct {
	mu         sync.RWMutex
	current    RootConfig
	generation uint64
	validator  func(RootConfig) error
}

func NewManager(initial RootConfig) (*Manager, error) {
	initial = normalizeRootConfig(initial)
	if err := ValidateRootConfig(initial); err != nil {
		return nil, err
	}
	return &Manager{current: cloneRootConfig(initial), generation: initial.Generation, validator: ValidateRootConfig}, nil
}

func LoadManager(root string, fallback RootConfig) (*Manager, error) {
	if root == "" {
		return nil, ErrManagerStorage
	}
	directory, err := secureManagerConfigDirectory(root)
	if err != nil {
		return nil, err
	}
	data, err := readManagerConfigFile(directory, "config.json")
	if err != nil {
		if os.IsNotExist(err) {
			return NewManager(fallback)
		}
		return nil, ErrManagerStorage
	}
	var loaded RootConfig
	if err := ParseStrict(data, &loaded); err != nil {
		return nil, err
	}
	requiresRewrite := loaded.SchemaVersion == 0 || loaded.Generation == 0 || loaded.Version != 0
	manager, err := NewManager(loaded)
	if err != nil {
		return nil, err
	}
	if requiresRewrite {
		if err := writeRootConfig(root, manager.current); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

func (m *Manager) Snapshot() RuntimeSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	current := cloneRootConfig(m.current)
	current.Generation = m.generation
	return RuntimeSnapshot{Config: current, Generation: m.generation}
}

func (m *Manager) Preview(candidate RootConfig, expected uint64) error {
	candidate = normalizeRootConfig(candidate)
	m.mu.RLock()
	generation := m.generation
	m.mu.RUnlock()
	if expected != generation {
		return ErrGenerationConflict
	}
	candidate.SchemaVersion = CurrentSchemaVersion
	candidate.Generation = generation + 1
	candidate.Version = 0
	return m.validator(candidate)
}

func (m *Manager) Apply(candidate RootConfig, expected uint64) (RuntimeSnapshot, error) {
	candidate = normalizeRootConfig(candidate)
	m.mu.Lock()
	defer m.mu.Unlock()
	if expected != m.generation {
		return RuntimeSnapshot{}, ErrGenerationConflict
	}
	candidate.SchemaVersion = CurrentSchemaVersion
	candidate.Generation = m.generation + 1
	candidate.Version = 0
	candidate.UpdatedAt = time.Now().UTC()
	if err := m.validator(candidate); err != nil {
		return RuntimeSnapshot{}, err
	}
	m.current = cloneRootConfig(candidate)
	m.generation = candidate.Generation
	return RuntimeSnapshot{Config: cloneRootConfig(m.current), Generation: m.generation}, nil
}

func cloneRootConfig(source RootConfig) RootConfig {
	clone := source
	clone.Security.WebAuthnRPOrigins = append([]string(nil), source.Security.WebAuthnRPOrigins...)
	clone.Domains.Accepted = append([]string(nil), source.Domains.Accepted...)
	clone.Domains.AccessHosts = append([]string(nil), source.Domains.AccessHosts...)
	clone.HTTP.TrustedProxyCIDRs = append([]string(nil), source.HTTP.TrustedProxyCIDRs...)
	if source.Sections != nil {
		clone.Sections = make(map[Section]SectionRef, len(source.Sections))
		for section, reference := range source.Sections {
			clone.Sections[section] = reference
		}
	}
	if source.Plugins != nil {
		clone.Plugins = make([]PluginConfig, len(source.Plugins))
		for i, plugin := range source.Plugins {
			clone.Plugins[i] = plugin
			if plugin.NetworkAllowlist != nil {
				clone.Plugins[i].NetworkAllowlist = append([]string(nil), plugin.NetworkAllowlist...)
			}
		}
	}
	return clone
}

func BackupConfig(root string, snapshot RuntimeSnapshot, now time.Time) (string, error) {
	if root == "" || snapshot.Generation == 0 {
		return "", errors.New("invalid backup request")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if snapshot.Config.Generation != snapshot.Generation {
		return "", errors.New("inconsistent backup generation")
	}
	directory, err := secureBackupDirectory(root)
	if err != nil {
		return "", err
	}
	if err := pruneBackups(directory, now); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("config-%020d-%s.json", snapshot.Generation, now.UTC().Format("20060102T150405.000000000Z"))
	path := filepath.Join(directory, name)
	if _, err := os.Lstat(path); err == nil {
		return "", ErrManagerStorage
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := atomicfile.Write(path, data, 0600); err != nil {
		return "", err
	}
	if err := pruneBackups(directory, now); err != nil {
		return "", err
	}
	return path, nil
}

func LoadBackup(path string) (RuntimeSnapshot, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > maxConfigFileSize {
		return RuntimeSnapshot{}, ErrNoBackup
	}
	data, err := fileutil.ReadRegularFile(path, maxConfigFileSize)
	if err != nil {
		return RuntimeSnapshot{}, ErrNoBackup
	}
	var snapshot RuntimeSnapshot
	if err := ParseStrict(data, &snapshot); err != nil || snapshot.Generation == 0 || snapshot.Config.Generation != snapshot.Generation {
		return RuntimeSnapshot{}, ErrNoBackup
	}
	if err := ValidateRootConfig(snapshot.Config); err != nil {
		return RuntimeSnapshot{}, err
	}
	return snapshot, nil
}

func pruneBackups(directory string, now time.Time) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	type item struct {
		path string
		mod  time.Time
	}
	items := make([]item, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrManagerStorage
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || !strings.HasPrefix(entry.Name(), "config-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return ErrManagerStorage
		}
		if now.Sub(info.ModTime()) > 30*24*time.Hour {
			if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
				return err
			}
			continue
		}
		items = append(items, item{path: filepath.Join(directory, entry.Name()), mod: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].mod.Equal(items[j].mod) {
			return items[i].path > items[j].path
		}
		return items[i].mod.After(items[j].mod)
	})
	if len(items) > 4 {
		for _, old := range items[4:] {
			if err := os.Remove(old.path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) ApplyPersisted(root string, candidate RootConfig, expected uint64, now time.Time) (RuntimeSnapshot, string, error) {
	if m == nil || root == "" {
		return RuntimeSnapshot{}, "", ErrManagerStorage
	}
	if now.IsZero() {
		now = time.Now()
	}
	candidate = normalizeRootConfig(candidate)
	if err := m.validator(candidate); err != nil {
		return RuntimeSnapshot{}, "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if expected != m.generation {
		return RuntimeSnapshot{}, "", ErrGenerationConflict
	}
	previous := RuntimeSnapshot{Config: cloneRootConfig(m.current), Generation: m.generation}
	backupPath, err := BackupConfig(root, previous, now)
	if err != nil {
		return RuntimeSnapshot{}, "", err
	}
	next := cloneRootConfig(candidate)
	next.SchemaVersion = CurrentSchemaVersion
	next.Generation = m.generation + 1
	next.Version = 0
	next.UpdatedAt = now.UTC()
	if err := writeRootConfig(root, next); err != nil {
		return RuntimeSnapshot{}, "", err
	}
	m.current = next
	m.generation = next.Generation
	return RuntimeSnapshot{Config: cloneRootConfig(next), Generation: m.generation}, backupPath, nil
}

func (m *Manager) RollbackPersisted(root, backupPath string, expected uint64, now time.Time) (RuntimeSnapshot, string, error) {
	directory, err := secureBackupDirectory(root)
	if err != nil {
		return RuntimeSnapshot{}, "", err
	}
	cleanPath, err := filepath.Abs(filepath.Clean(backupPath))
	if err != nil {
		return RuntimeSnapshot{}, "", ErrNoBackup
	}
	cleanDirectory, err := filepath.Abs(directory)
	if err != nil || filepath.Dir(cleanPath) != cleanDirectory {
		return RuntimeSnapshot{}, "", ErrNoBackup
	}
	snapshot, err := LoadBackup(cleanPath)
	if err != nil {
		return RuntimeSnapshot{}, "", err
	}
	return m.ApplyPersisted(root, snapshot.Config, expected, now)
}

func writeRootConfig(root string, value RootConfig) error {
	directory, err := secureManagerConfigDirectory(root)
	if err != nil {
		return err
	}
	value = normalizeRootConfig(value)
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if info, statErr := os.Lstat(filepath.Join(directory, "config.json")); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ErrManagerStorage
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	return atomicfile.Write(filepath.Join(directory, "config.json"), data, 0600)
}

func secureManagerConfigDirectory(root string) (string, error) {
	if root == "" {
		return "", ErrManagerStorage
	}
	directory := filepath.Join(root, "config")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", ErrManagerStorage
	}
	// 目录权限 0700 是配置目录的最低要求；G302 将目录误判为文件。
	// #nosec G302
	if err := os.Chmod(directory, 0700); err != nil {
		return "", err
	}
	return directory, nil
}

func secureBackupDirectory(root string) (string, error) {
	configDirectory, err := runtimepath.Directory(root)
	if err != nil {
		return "", err
	}
	directory := filepath.Join(configDirectory, "backups")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", ErrManagerStorage
	}
	// 备份目录只允许服务账户访问；G302 将目录误判为文件。
	// #nosec G302
	if err := os.Chmod(directory, 0700); err != nil {
		return "", err
	}
	return directory, nil
}

func readManagerConfigFile(directory, name string) ([]byte, error) {
	if filepath.Base(name) != name || name == "" {
		return nil, ErrManagerStorage
	}
	path := filepath.Join(directory, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxConfigFileSize {
		return nil, ErrManagerStorage
	}
	return fileutil.ReadRegularFile(path, maxConfigFileSize)
}
