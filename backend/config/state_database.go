package config

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/internal/configstate"
	"git.pcux.cn/xuans/Smail/utils/credential"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

var ErrStateDatabaseUnavailable = errors.New("配置历史数据库不可用，请检查所选数据库连接；不会回退到独立 SQLite 数据库")

type stateDatabase struct {
	*sql.DB
	driver string
	ctx    context.Context
	cancel context.CancelFunc
}

func (d *stateDatabase) Close() error {
	d.cancel()
	return d.DB.Close()
}

func (d *stateDatabase) Begin() (*sql.Tx, error) {
	return d.DB.BeginTx(d.ctx, nil)
}

func (d *stateDatabase) bind(query string) string {
	if d.driver != "postgres" {
		return query
	}
	var result strings.Builder
	index := 0
	for _, value := range query {
		if value == '?' {
			index++
			fmt.Fprintf(&result, "$%d", index)
		} else {
			result.WriteRune(value)
		}
	}
	return result.String()
}

func (d *stateDatabase) Exec(query string, args ...any) (sql.Result, error) {
	ctx, cancel := context.WithTimeout(d.ctx, 10*time.Second)
	defer cancel()
	return d.DB.ExecContext(ctx, d.bind(query), args...)
}

func (d *stateDatabase) Query(query string, args ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(d.ctx, d.bind(query), args...)
}

func (d *stateDatabase) QueryRow(query string, args ...any) *sql.Row {
	return d.DB.QueryRowContext(d.ctx, d.bind(query), args...)
}

func (d *stateDatabase) upsert() string {
	query := "INSERT INTO config_records(kind,id,payload,digest) VALUES(?,?,?,?) "
	if d.driver == "mysql" {
		return query + "ON DUPLICATE KEY UPDATE payload=VALUES(payload),digest=VALUES(digest)"
	}
	return query + "ON CONFLICT(kind,id) DO UPDATE SET payload=excluded.payload,digest=excluded.digest"
}

func (s *TransactionStore) stateDatabaseConfig() (DatabaseConfig, error) {
	if s.database != nil {
		return *s.database, ValidateDatabaseConfig(*s.database)
	}
	directory, err := secureManagerConfigDirectory(s.root)
	if err != nil {
		return DatabaseConfig{}, err
	}
	data, err := readManagerConfigFile(directory, "config.json")
	if err != nil {
		return DatabaseConfig{}, err
	}
	var root RootConfig
	if err := ParseStrict(data, &root); err != nil {
		return DatabaseConfig{}, err
	}
	database := NormalizeDatabaseConfig(root.Database)
	return database, ValidateDatabaseConfig(database)
}

func (s *TransactionStore) openState() (*stateDatabase, error) {
	var driver, dsn string
	if s.legacyState {
		driver = "sqlite"
		dsn = filepath.Join(s.root, "data", "smail-state.db")
	} else {
		database, err := s.stateDatabaseConfig()
		if err != nil {
			return nil, err
		}
		vault, err := credential.NewVault(s.root)
		if err != nil {
			return nil, err
		}
		driver, dsn, err = compatibilityDatabaseAt(s.root, database, vault)
		if err != nil {
			return nil, err
		}
	}
	filename := ""
	if driver == "sqlite" {
		if _, err := runtimepath.Directory(s.root); err != nil {
			return nil, err
		}
		filename = dsn
		relative, err := filepath.Rel(filepath.Join(s.root, "data"), filepath.Dir(filename))
		if err != nil || relative != "." && !filepath.IsLocal(relative) {
			return nil, ErrManagerStorage
		}
		parent := filepath.Join(s.root, "data")
		if relative != "." {
			for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
				parent, err = runtimepath.SecureDirectory(parent, part)
				if err != nil {
					return nil, err
				}
			}
		}
		info, statErr := os.Lstat(filename)
		if statErr == nil && !info.Mode().IsRegular() {
			return nil, ErrManagerStorage
		}
		if statErr != nil && (!os.IsNotExist(statErr) || s.legacyState) {
			return nil, statErr
		}
		path := filepath.ToSlash(filename)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		uri := url.URL{Scheme: "file", Path: path}
		query := url.Values{"_pragma": {"busy_timeout(5000)", "synchronous(FULL)"}}
		if s.legacyState {
			query.Set("mode", "rw")
		}
		uri.RawQuery = query.Encode()
		dsn = uri.String()
	}
	connection, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, ErrStateDatabaseUnavailable
	}
	operationContext, operationCancel := context.WithTimeout(context.Background(), 30*time.Second)
	database := &stateDatabase{DB: connection, driver: driver, ctx: operationContext, cancel: operationCancel}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		closeConfigResource(database)
		return nil, ErrStateDatabaseUnavailable
	}
	if !s.legacyState {
		if _, err := database.Exec(configstate.Schema(driver)); err != nil {
			closeConfigResource(database)
			return nil, ErrStateDatabaseUnavailable
		}
	}
	if filename != "" {
		if err := os.Chmod(filename, 0600); err != nil {
			return nil, errors.Join(err, database.Close())
		}
	}
	return database, nil
}
