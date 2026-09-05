package session

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	"git.pcux.cn/xuans/Smail/services/dbmigration"
	"github.com/alexedwards/scs/v2"
	log "github.com/sirupsen/logrus"
)

var errMigrationSessionWrite = errors.New("session changes are paused during database migration")

// migrationStore 只跳过冻结期间未改变内容的会话续期。
// 不能新增会话、修改权限、延长有效期或伪称已保存安全状态变更。
type migrationStore struct {
	scs.Store
	codec  scs.Codec
	paused func() bool
}

func (s *migrationStore) Commit(token string, data []byte, expiry time.Time) error {
	if !s.paused() {
		return s.Store.Commit(token, data, expiry)
	}
	previous, found, err := s.Store.Find(token)
	if err != nil {
		return err
	}
	if !found {
		return errMigrationSessionWrite
	}
	beforeDeadline, before, err := s.codec.Decode(previous)
	if err != nil {
		return err
	}
	afterDeadline, after, err := s.codec.Decode(data)
	if err != nil {
		return err
	}
	if !beforeDeadline.Equal(afterDeadline) || !reflect.DeepEqual(before, after) {
		return errMigrationSessionWrite
	}
	return nil
}

func (s *migrationStore) Delete(token string) error {
	if s.paused() {
		return errMigrationSessionWrite
	}
	return s.Store.Delete(token)
}

func (s *migrationStore) All() (map[string][]byte, error) {
	store, ok := s.Store.(scs.IterableStore)
	if !ok {
		return nil, errors.New("session store does not support iteration")
	}
	return store.All()
}

var stopCleanup context.CancelFunc

// Stop 在内部重启时停止旧会话清理器，防止它继续访问旧库。
func Stop() {
	if stopCleanup != nil {
		stopCleanup()
		stopCleanup = nil
	}
}

func startCleanup(database *sql.DB, sqlite bool) {
	Stop()
	ctx, cancel := context.WithCancel(context.Background())
	stopCleanup = cancel
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := dbmigration.WhileWritable(func() error {
					operation, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					query := "DELETE FROM sessions WHERE expiry < CURRENT_TIMESTAMP"
					if sqlite {
						query = "DELETE FROM sessions WHERE expiry < julianday('now')"
					}
					_, err := database.ExecContext(operation, query)
					return err
				})
				if err != nil && ctx.Err() == nil {
					log.Warn("清理过期会话失败")
				}
			}
		}
	}()
}
