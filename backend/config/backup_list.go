package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListBackups 枚举合法备份的代次和时间，不返回内部路径。
func (s *TransactionStore) ListBackups() ([]ConfigBackupInfo, error) {
	if s == nil || s.root == "" {
		return nil, ErrBackupListingUnavailable
	}
	configDirectory, err := secureManagerConfigDirectory(s.root)
	if err != nil {
		return nil, err
	}
	backupsDirectory, err := secureConfigBackupDirectory(configDirectory)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(backupsDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return []ConfigBackupInfo{}, nil
		}
		return nil, err
	}
	result := make([]ConfigBackupInfo, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			return nil, ErrTransactionInvalid
		}
		runtime, manifest, loadErr := loadConfigBackup(filepath.Join(backupsDirectory, entry.Name()), s.validation)
		if loadErr != nil {
			return nil, loadErr
		}
		_ = runtime
		result = append(result, ConfigBackupInfo{Generation: manifest.Generation, CreatedAt: manifest.CreatedAt})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].Generation > result[j].Generation
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}
