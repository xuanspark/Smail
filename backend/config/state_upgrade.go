package config

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
)

// 旧独立状态库只用于一次性升级；校验和导入成功后退出运行路径，不覆盖冲突历史。
func (s *TransactionStore) migrateLegacyState() error {
	unlock, err := s.lockState()
	if err != nil {
		return err
	}
	defer unlock()
	filename := filepath.Join(s.root, "data", "smail-state.db")
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return errors.Join(ErrManagerStorage, err)
	}
	legacy := *s
	legacy.legacyState = true
	if err := legacy.recoverStateLocked(); err != nil {
		return err
	}
	records, err := legacy.stateBackups()
	if err != nil {
		return err
	}
	database, err := s.openState()
	if err != nil {
		return err
	}
	defer database.Close()
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		var digest string
		var payload []byte
		err = tx.QueryRow(database.bind("SELECT digest,payload FROM config_records WHERE kind=? AND id=?"), "backup", record.Backup.ID).Scan(&digest, &payload)
		if err == nil {
			if digest != SectionDigest(data) || SectionDigest(payload) != digest {
				return ErrConfigDigestMismatch
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(database.bind("INSERT INTO config_records(kind,id,payload,digest) VALUES(?,?,?,?)"), "backup", record.Backup.ID, data, SectionDigest(data)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	archive, err := runtimepath.SecureDirectory(filepath.Join(s.root, "data"), "migration-history")
	if err != nil {
		return err
	}
	target := filepath.Join(archive, "smail-state.retired")
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		return ErrManagerStorage
	}
	if err := os.Rename(filename, target); err != nil {
		return err
	}
	return errors.Join(atomicfile.SyncDirectory(archive), atomicfile.SyncDirectory(filepath.Dir(filename)))
}
