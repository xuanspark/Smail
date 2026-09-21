package dbmigration

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db/migration"
	securefile "git.pcux.cn/xuans/Smail/utils/file"
)

// PortableExecutor 使用版本化目标结构复制业务数据，不拼接用户提供的 SQL。
// 成功切换后保留源库写锁直到进程重启，避免旧连接继续接收邮件造成数据丢失。
type PortableExecutor struct {
	root          string
	open          SQLConnectionFactory
	switchConfig  AuthorizedDatabaseSwitch
	mu            sync.Mutex
	pending       bool
	paused        atomic.Bool
	releaseSource func()
}

func NewPortableDatabaseExecutor(root string, open SQLConnectionFactory, switchConfig AuthorizedDatabaseSwitch) Runner {
	return &DatabaseExecutor{portable: &PortableExecutor{root: root, open: open, switchConfig: switchConfig}}
}

func (e *PortableExecutor) connect(ctx context.Context, database config.DatabaseConfig) (*sql.DB, error) {
	if database.Type != config.DatabaseTypeSQLite {
		if e.open == nil {
			return nil, ErrSQLConnectionUnavailable
		}
		return e.open(ctx, database, database.Name)
	}
	path, err := NewSQLiteExecutor(e.root, nil).resolveReference(database.LocalRef)
	if err != nil {
		return nil, err
	}
	if err := checkRegularFile(path, false); err != nil {
		return nil, ErrSourceDatabase
	}
	connection, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, ErrSQLConnectionUnavailable
	}
	connection.SetMaxOpenConns(1)
	return connection, nil
}

func (e *PortableExecutor) Preview(ctx context.Context, plan Plan) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := buildTargetPlan(plan.sourceConfig, plan.targetConfig); err != nil {
		return err
	}
	source, err := e.connect(ctx, plan.sourceConfig)
	if err != nil {
		return ErrSQLConnectionUnavailable
	}
	defer source.Close()
	if err := source.PingContext(ctx); err != nil {
		return ErrSQLConnectionUnavailable
	}
	return e.checkTarget(ctx, plan.targetConfig)
}

func (e *PortableExecutor) checkTarget(ctx context.Context, target config.DatabaseConfig) error {
	if target.Type == config.DatabaseTypeSQLite {
		path, err := NewSQLiteExecutor(e.root, nil).resolveReference(target.LocalRef)
		if err != nil {
			return err
		}
		return checkRegularFile(path, true)
	}
	if e.open == nil {
		return ErrSQLConnectionUnavailable
	}
	admin, err := e.open(ctx, target, "")
	if err != nil {
		return ErrSQLConnectionUnavailable
	}
	defer admin.Close()
	query := "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?"
	if target.Type == config.DatabaseTypePostgreSQL {
		query = "SELECT COUNT(*) FROM pg_database WHERE datname = $1"
	}
	var count int
	if err := admin.QueryRowContext(ctx, query, target.Name).Scan(&count); err != nil {
		return ErrSQLConnectionUnavailable
	}
	if count != 0 {
		return ErrSQLTargetExists
	}
	return nil
}

func (e *PortableExecutor) createTarget(ctx context.Context, target config.DatabaseConfig) (*sql.DB, error) {
	if err := e.checkTarget(ctx, target); err != nil {
		return nil, err
	}
	if target.Type == config.DatabaseTypeSQLite {
		path, err := NewSQLiteExecutor(e.root, nil).resolveReference(target.LocalRef)
		if err != nil {
			return nil, err
		}
		file, err := securefile.CreateExclusiveFile(path, 0600)
		if err != nil {
			return nil, ErrTargetExists
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
	} else {
		admin, err := e.open(ctx, target, "")
		if err != nil {
			return nil, ErrSQLConnectionUnavailable
		}
		defer admin.Close()
		query := "CREATE DATABASE " + quoteDatabaseIdentifier(target.Type, target.Name)
		if target.Type == config.DatabaseTypeMySQL {
			query += " CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"
		}
		if target.Type == config.DatabaseTypePostgreSQL {
			query += " TEMPLATE template0 ENCODING 'UTF8'"
		}
		if _, err := admin.ExecContext(ctx, query); err != nil {
			if isDatabaseAlreadyExistsError(err, target.Type) {
				return nil, ErrSQLTargetExists
			}
			return nil, ErrSQLConnectionUnavailable
		}
	}
	// 失败目标保留供检查，后续重试必须换用未占用的名称，绝不删除旧库。
	return e.connect(ctx, target)
}

func (e *PortableExecutor) Run(ctx context.Context, plan Plan, update func(Status) error) error {
	if !e.mu.TryLock() {
		return ErrSQLSourceBusy
	}
	defer e.mu.Unlock()
	if e.pending {
		return ErrSQLSourceBusy
	}
	if e.switchConfig == nil || update == nil {
		return ErrSwitchUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := e.Preview(ctx, plan); err != nil {
		return err
	}
	source, err := e.connect(ctx, plan.sourceConfig)
	if err != nil {
		return ErrSQLConnectionUnavailable
	}
	keepLock := false
	defer func() {
		if !keepLock {
			_ = source.Close()
		}
	}()
	connection, err := source.Conn(ctx)
	if err != nil {
		return ErrSQLConnectionUnavailable
	}
	defer func() {
		if !keepLock {
			_ = connection.Close()
		}
	}()
	tables, err := portableTables(ctx, connection, plan.Source.Type)
	if err != nil {
		return err
	}
	if err := validatePortableObjects(ctx, connection, plan.Source.Type); err != nil {
		return err
	}
	e.paused.Store(true)
	defer func() {
		if !keepLock {
			e.paused.Store(false)
		}
	}()
	release, err := lockPortableSource(ctx, connection, plan.Source.Type, tables)
	if err != nil {
		return err
	}
	defer func() {
		if !keepLock {
			release()
		}
	}()
	target, err := e.createTarget(ctx, plan.targetConfig)
	if err != nil {
		return err
	}
	defer target.Close()
	dialect := migration.Dialect(plan.Target.Type)
	if plan.Target.Type == config.DatabaseTypePostgreSQL {
		dialect = migration.DialectPostgres
	}
	if _, err := (migration.Runner{DB: target, Dialect: dialect, Migrations: migration.DefaultMigrations()}).Run(ctx); err != nil {
		return ErrSQLUnsupportedObject
	}
	if err := copyPortableTables(ctx, connection, target, plan, tables); err != nil {
		return err
	}
	if err := update(StatusVerifying); err != nil {
		return err
	}
	if err := verifyPortableTables(ctx, connection, target, plan, tables); err != nil {
		return err
	}
	if err := update(StatusSwitching); err != nil {
		return err
	}
	// 配置切换结果只写运行日志，不再依赖源库或目标库保存独立审计。
	err = e.switchConfig(ctx, plan.expectedGeneration, plan.targetConfig, plan.actor)
	if err != nil && isKnownSwitchRejection(err) {
		return err
	}
	// 切换结果未知也保留写锁和目标，避免在不确定状态下继续写入旧库。
	keepLock = true
	e.pending = true
	e.releaseSource = func() { release(); _ = connection.Close(); _ = source.Close() }
	if err != nil {
		return errors.Join(ErrSwitchOutcomeUnknown, err)
	}
	return nil
}

// Close 只能在请求和邮件监听器停止后调用，不能提前解除旧库保护。
func (e *PortableExecutor) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.releaseSource != nil {
		e.releaseSource()
		e.releaseSource = nil
	}
}

func quoteDatabaseIdentifier(kind config.DatabaseType, name string) string {
	if kind == config.DatabaseTypeMySQL {
		return quoteMySQLIdentifier(name)
	}
	return quotePostgreSQLIdentifier(name)
}

func lockPortableSource(ctx context.Context, connection *sql.Conn, kind config.DatabaseType, tables []string) (func(), error) {
	query, unlock := "BEGIN IMMEDIATE", "ROLLBACK"
	if kind == config.DatabaseTypeMySQL {
		parts := make([]string, len(tables))
		for i, table := range tables {
			parts[i] = quoteMySQLIdentifier(table) + " READ"
		}
		query, unlock = "LOCK TABLES "+strings.Join(parts, ","), "UNLOCK TABLES"
	} else if kind == config.DatabaseTypePostgreSQL {
		if _, err := connection.ExecContext(ctx, "BEGIN"); err != nil {
			return nil, ErrSQLSourceBusy
		}
		parts := make([]string, len(tables))
		for i, table := range tables {
			parts[i] = quotePostgreSQLIdentifier(table)
		}
		query = "LOCK TABLE " + strings.Join(parts, ",") + " IN SHARE MODE NOWAIT"
	}
	release := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = connection.ExecContext(cleanup, unlock)
	}
	if _, err := connection.ExecContext(ctx, query); err != nil {
		release()
		return nil, ErrSQLSourceBusy
	}
	return release, nil
}
