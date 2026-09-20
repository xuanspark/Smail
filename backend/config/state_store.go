package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
)

// 配置历史保存在所选业务数据库；事务字段仅用于读取旧版独立状态库。
type stateRecord struct {
	Transaction *TransactionManifest  `json:"transaction,omitempty"`
	Backup      *ConfigBackupManifest `json:"backup,omitempty"`
	Original    map[string][]byte     `json:"original,omitempty"`
	Candidate   map[string][]byte     `json:"candidate,omitempty"`
}

func (s *TransactionStore) lockState() (func(), error) {
	directory, err := runtimepath.Directory(s.root)
	if err != nil {
		return nil, err
	}
	filename := filepath.Join(directory, ".config-state.lock")
	if info, err := os.Lstat(filename); err == nil && !info.Mode().IsRegular() {
		return nil, ErrManagerStorage
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	unlock, err := lockStateFile(file)
	if err != nil {
		file.Close()
		return nil, errors.New("配置状态正在被其他进程使用，请稍后重试")
	}
	return func() { unlock(); file.Close() }, nil
}

func (s *TransactionStore) saveState(kind, id string, record stateRecord) error {
	if !transactionIDPattern.MatchString(id) {
		return ErrTransactionInvalid
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	db, err := s.openState()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(db.upsert(), kind, id, data, SectionDigest(data))
	return err
}

func validateStateFiles(files map[string][]byte, digests map[string]string) error {
	if !validFileDigestSet(digests) || len(files) != len(digests) {
		return ErrTransactionInvalid
	}
	for name, digest := range digests {
		if len(files[name]) == 0 || len(files[name]) > maxConfigFileSize || SectionDigest(files[name]) != digest {
			return ErrConfigDigestMismatch
		}
	}
	return nil
}

func (s *TransactionStore) stateRecords(kind string) ([]stateRecord, error) {
	db, err := s.openState()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var oversized int
	if err := db.QueryRow("SELECT count(*) FROM config_records WHERE kind=? AND length(payload)>?", kind, 16*maxConfigFileSize).Scan(&oversized); err != nil || oversized != 0 {
		return nil, errors.Join(ErrTransactionInvalid, err)
	}
	rows, err := db.Query("SELECT id,payload,digest FROM config_records WHERE kind=? ORDER BY id", kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []stateRecord
	for rows.Next() {
		var id, digest string
		var data []byte
		if err := rows.Scan(&id, &data, &digest); err != nil {
			return nil, err
		}
		if !transactionIDPattern.MatchString(id) || len(data) > 16*maxConfigFileSize || SectionDigest(data) != digest {
			return nil, ErrConfigDigestMismatch
		}
		var record stateRecord
		if err := ParseStrict(data, &record); err != nil {
			return nil, err
		}
		switch kind {
		case "transaction":
			if record.Transaction == nil || record.Transaction.ID != id || record.Backup != nil {
				return nil, ErrTransactionInvalid
			}
			if err := validateTransactionManifest(*record.Transaction); err != nil {
				return nil, err
			}
			if err := validateStateFiles(record.Original, record.Transaction.OriginalFiles); err != nil {
				return nil, err
			}
			if err := validateStateFiles(record.Candidate, record.Transaction.CandidateFiles); err != nil {
				return nil, err
			}
			for _, item := range []struct {
				files      map[string][]byte
				generation uint64
			}{
				{record.Original, record.Transaction.FromGeneration},
				{record.Candidate, record.Transaction.ToGeneration},
			} {
				config, err := parseRuntimeFiles(item.files, s.validation)
				if err != nil || config.Generation != item.generation {
					return nil, errors.Join(ErrTransactionInvalid, err)
				}
			}
		case "backup":
			if record.Backup == nil || record.Backup.ID != id || record.Transaction != nil || record.Backup.CreatedAt.IsZero() {
				return nil, ErrTransactionInvalid
			}
			if err := validateStateFiles(record.Original, record.Backup.Files); err != nil {
				return nil, err
			}
			config, err := parseRuntimeFiles(record.Original, s.validation)
			if err != nil {
				return nil, err
			}
			if config.Generation != record.Backup.Generation {
				return nil, ErrConfigGenerationMismatch
			}
		default:
			return nil, ErrTransactionInvalid
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

func (s *TransactionStore) removeState(kind, id string) error {
	db, err := s.openState()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("DELETE FROM config_records WHERE kind=? AND id=?", kind, id)
	return err
}

func writeStateLive(directory string, files map[string][]byte, rootOnly bool) error {
	for _, name := range transactionFileNames() {
		if (name == "config.json") != rootOnly {
			continue
		}
		target := filepath.Join(directory, name)
		if info, err := os.Lstat(target); err == nil && !info.Mode().IsRegular() {
			return ErrManagerStorage
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := atomicfile.Write(target, files[name], 0600); err != nil {
			return err
		}
	}
	return atomicfile.SyncDirectory(directory)
}

func (s *TransactionStore) commitState(current, candidate RuntimeConfig) error {
	unlock, err := s.lockState()
	if err != nil {
		return err
	}
	defer unlock()
	if current.Generation == 0 || candidate.Generation != current.Generation+1 {
		return ErrGenerationConflict
	}
	if err := ValidateRuntimeConfig(candidate, s.validation); err != nil {
		return err
	}
	if records, err := s.stateRecords("transaction"); err != nil || len(records) != 0 {
		return errors.Join(ErrConfigRecoveryRequired, err)
	}
	live, err := NewConfigLoader(s.root, ConfigLoaderOptions{Validation: s.validation}).Load()
	if err != nil || live.Generation != current.Generation {
		return errors.Join(ErrGenerationConflict, err)
	}
	original, err := marshalRuntimeFiles(current)
	if err != nil {
		return err
	}
	files, err := marshalRuntimeFiles(candidate)
	if err != nil {
		return err
	}
	directory, err := secureManagerConfigDirectory(s.root)
	if err != nil {
		return err
	}
	id, err := newTransactionID()
	if err != nil {
		return err
	}
	manifest := TransactionManifest{
		ID: id, FromGeneration: current.Generation, ToGeneration: candidate.Generation,
		Sections: append([]Section(nil), requiredPartitionSections...), OriginalFiles: digestFiles(original),
		CandidateFiles: digestFiles(files), Phase: TransactionPrepared, CreatedAt: time.Now().UTC(),
	}
	record := stateRecord{Transaction: &manifest, Original: original, Candidate: files}
	persist := func(phase TransactionPhase) error {
		manifest.Phase = phase
		if err := s.saveState("transaction", id, record); err != nil {
			return err
		}
		return s.beforePhase(phase)
	}
	if err := persist(TransactionPrepared); err != nil {
		return err
	}
	if err := writeStateLive(directory, files, false); err != nil {
		return err
	}
	if err := persist(TransactionSectionsCommitted); err != nil {
		return err
	}
	if err := writeStateLive(directory, files, true); err != nil {
		return err
	}
	if err := persist(TransactionRootCommitted); err != nil {
		return err
	}
	loaded, err := NewConfigLoader(s.root, ConfigLoaderOptions{Validation: s.validation}).Load()
	if err != nil || loaded.Generation != candidate.Generation {
		return errors.Join(ErrConfigGenerationMismatch, err)
	}
	if err := persist(TransactionVerified); err != nil {
		return err
	}
	return s.removeState("transaction", id)
}

func (s *TransactionStore) recoverState() error {
	unlock, err := s.lockState()
	if err != nil {
		return err
	}
	defer unlock()
	return s.recoverStateLocked()
}

func (s *TransactionStore) recoverStateLocked() error {
	if entries, err := os.ReadDir(filepath.Join(s.root, "data", "transaction")); err == nil && len(entries) != 0 {
		return ErrConfigRecoveryRequired
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	records, err := s.stateRecords("transaction")
	if err != nil {
		return err
	}
	if len(records) > 1 {
		return ErrTransactionInvalid
	}
	for _, record := range records {
		manifest := record.Transaction
		loaded, err := NewConfigLoader(s.root, ConfigLoaderOptions{Validation: s.validation}).Load()
		if err == nil && loaded.Generation > manifest.ToGeneration {
			return ErrConfigGenerationMismatch
		}
		if err != nil || loaded.Generation < manifest.ToGeneration {
			if _, err := parseRuntimeFiles(record.Original, s.validation); err != nil {
				return err
			}
			directory, err := secureManagerConfigDirectory(s.root)
			if err != nil {
				return err
			}
			if err := writeStateLive(directory, record.Original, false); err != nil {
				return err
			}
			if err := writeStateLive(directory, record.Original, true); err != nil {
				return err
			}
			if _, err := NewConfigLoader(s.root, ConfigLoaderOptions{Validation: s.validation}).Load(); err != nil {
				return err
			}
		}
		if err := s.removeState("transaction", manifest.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *TransactionStore) stateBackups() ([]stateRecord, error) {
	items, err := s.stateRecords("backup")
	sort.Slice(items, func(i, j int) bool {
		if items[i].Backup.CreatedAt.Equal(items[j].Backup.CreatedAt) {
			return items[i].Backup.ID > items[j].Backup.ID
		}
		return items[i].Backup.CreatedAt.After(items[j].Backup.CreatedAt)
	})
	return items, err
}

func (s *TransactionStore) createStateBackup(config RuntimeConfig, now time.Time) (string, error) {
	unlock, err := s.lockState()
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := ValidateRuntimeConfig(config, s.validation); err != nil {
		return "", err
	}
	files, err := marshalRuntimeFiles(config)
	if err != nil {
		return "", err
	}
	id, err := newTransactionID()
	if err != nil {
		return "", err
	}
	manifest := ConfigBackupManifest{ID: id, Generation: config.Generation, CreatedAt: now, Files: digestFiles(files)}
	if err := s.saveState("backup", id, stateRecord{Backup: &manifest, Original: files}); err != nil {
		return "", err
	}
	items, err := s.stateBackups()
	if err != nil {
		return "", err
	}
	for index, item := range items {
		if index > 0 && (index >= configBackupLimit || now.Sub(item.Backup.CreatedAt) > configBackupMaxAge) {
			if err := s.removeState("backup", item.Backup.ID); err != nil {
				return "", err
			}
		}
	}
	return id, nil
}
