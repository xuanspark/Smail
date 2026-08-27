package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

var (
	ErrChecksumMismatch        = errors.New("migration checksum mismatch")
	ErrUnknownAppliedMigration = errors.New("unknown applied migration")
	ErrLockUnavailable         = errors.New("migration lock unavailable")
	sqliteProcessLock          sync.Mutex
)

const migrationLockID int64 = 0x534d41494c

type Applied struct {
	Version     int64
	Name        string
	Checksum    string
	AppliedAt   time.Time
	ExecutionMS int64
}

type Report struct {
	CurrentVersion int64
	Applied        []Applied
}

type Runner struct {
	DB         *sql.DB
	Dialect    Dialect
	Migrations []Migration
	LockWait   time.Duration
	Now        func() time.Time
}

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r Runner) Run(ctx context.Context) (Report, error) {
	if r.DB == nil {
		return Report{}, errors.New("migration database is nil")
	}
	if _, err := ParseDialect(string(r.Dialect)); err != nil {
		return Report{}, err
	}
	migrations, err := normalize(r.Migrations)
	if err != nil {
		return Report{}, err
	}
	if err = validateDialectMigrations(migrations, r.Dialect); err != nil {
		return Report{}, err
	}
	conn, err := r.DB.Conn(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("open migration connection: %w", err)
	}
	defer conn.Close()
	if r.Dialect == DialectSQLite {
		return r.runSQLite(ctx, conn, migrations)
	}
	if err = r.acquireAdvisoryLock(ctx, conn); err != nil {
		return Report{}, err
	}
	defer r.releaseAdvisoryLock(conn)
	return r.runTransactional(ctx, conn, migrations)
}

func validateDialectMigrations(migrations []Migration, dialect Dialect) error {
	for _, item := range migrations {
		if item.Apply == nil && len(item.StatementsFor(dialect)) == 0 {
			return fmt.Errorf("migration %d has no statements for %s", item.Version, dialect)
		}
	}
	return nil
}

func (r Runner) CurrentVersion(ctx context.Context) (int64, error) {
	if r.DB == nil {
		return 0, errors.New("migration database is nil")
	}
	ddl, err := schemaTableDDL(r.Dialect)
	if err != nil {
		return 0, err
	}
	if _, err = r.DB.ExecContext(ctx, ddl); err != nil {
		return 0, fmt.Errorf("create migration table: %w", err)
	}
	var version sql.NullInt64
	if err = r.DB.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migration").Scan(&version); err != nil {
		return 0, fmt.Errorf("query migration version: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}
	return version.Int64, nil
}

func (r Runner) runSQLite(ctx context.Context, conn *sql.Conn, migrations []Migration) (report Report, resultErr error) {
	sqliteProcessLock.Lock()
	defer sqliteProcessLock.Unlock()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return Report{}, fmt.Errorf("acquire SQLite migration lock: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	report, resultErr = r.applyWithExecutor(ctx, conn, migrations)
	if resultErr != nil {
		return Report{}, resultErr
	}
	if _, resultErr = conn.ExecContext(ctx, "COMMIT"); resultErr != nil {
		return Report{}, fmt.Errorf("commit SQLite migrations: %w", resultErr)
	}
	return report, nil
}

func (r Runner) runTransactional(ctx context.Context, conn *sql.Conn, migrations []Migration) (Report, error) {
	ddl, err := schemaTableDDL(r.Dialect)
	if err != nil {
		return Report{}, err
	}
	if _, err = conn.ExecContext(ctx, ddl); err != nil {
		return Report{}, fmt.Errorf("create migration table: %w", err)
	}
	applied, err := loadApplied(ctx, conn)
	if err != nil {
		return Report{}, err
	}
	if err = verifyApplied(applied, migrations); err != nil {
		return Report{}, err
	}

	report := Report{CurrentVersion: highestVersion(applied)}
	for _, item := range migrations {
		if _, ok := applied[item.Version]; ok {
			continue
		}
		tx, beginErr := conn.BeginTx(ctx, nil)
		if beginErr != nil {
			return Report{}, fmt.Errorf("begin migration %d: %w", item.Version, beginErr)
		}
		entry, applyErr := r.applyOne(ctx, tx, item)
		if applyErr != nil {
			_ = tx.Rollback()
			return Report{}, applyErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return Report{}, fmt.Errorf("commit migration %d: %w", item.Version, commitErr)
		}
		report.Applied = append(report.Applied, entry)
		report.CurrentVersion = item.Version
	}
	return report, nil
}

func (r Runner) applyWithExecutor(ctx context.Context, exec executor, migrations []Migration) (Report, error) {
	ddl, err := schemaTableDDL(r.Dialect)
	if err != nil {
		return Report{}, err
	}
	if _, err = exec.ExecContext(ctx, ddl); err != nil {
		return Report{}, fmt.Errorf("create migration table: %w", err)
	}
	applied, err := loadApplied(ctx, exec)
	if err != nil {
		return Report{}, err
	}
	if err = verifyApplied(applied, migrations); err != nil {
		return Report{}, err
	}
	report := Report{CurrentVersion: highestVersion(applied)}
	for _, item := range migrations {
		if _, ok := applied[item.Version]; ok {
			continue
		}
		entry, applyErr := r.applyOne(ctx, exec, item)
		if applyErr != nil {
			return Report{}, applyErr
		}
		report.Applied = append(report.Applied, entry)
		report.CurrentVersion = item.Version
	}
	return report, nil
}

func (r Runner) applyOne(ctx context.Context, exec executor, item Migration) (Applied, error) {
	started := time.Now()
	if r.Now != nil {
		started = r.Now()
	}
	if item.Apply != nil {
		if err := item.Apply(ctx, exec, r.Dialect); err != nil {
			return Applied{}, fmt.Errorf("execute migration %d (%s): %w", item.Version, item.Name, err)
		}
	} else {
		for _, statement := range item.StatementsFor(r.Dialect) {
			if _, err := exec.ExecContext(ctx, statement); err != nil {
				return Applied{}, fmt.Errorf("execute migration %d (%s): %w", item.Version, item.Name, err)
			}
		}
	}
	finished := time.Now()
	if r.Now != nil {
		finished = r.Now()
	}
	executionMS := int64(math.Max(0, float64(finished.Sub(started).Milliseconds())))
	entry := Applied{Version: item.Version, Name: item.Name, Checksum: item.Checksum(), AppliedAt: finished.UTC(), ExecutionMS: executionMS}
	query, err := insertSQL(r.Dialect)
	if err != nil {
		return Applied{}, err
	}
	if _, err = exec.ExecContext(ctx, query, entry.Version, entry.Name, entry.Checksum, entry.AppliedAt, entry.ExecutionMS); err != nil {
		return Applied{}, fmt.Errorf("record migration %d: %w", item.Version, err)
	}
	return entry, nil
}

func loadApplied(ctx context.Context, exec executor) (map[int64]Applied, error) {
	rows, err := exec.QueryContext(ctx, "SELECT version, name, checksum, applied_at, execution_ms FROM schema_migration ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()
	result := map[int64]Applied{}
	for rows.Next() {
		var item Applied
		if err = rows.Scan(&item.Version, &item.Name, &item.Checksum, &item.AppliedAt, &item.ExecutionMS); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		result[item.Version] = item
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied migrations: %w", err)
	}
	return result, nil
}

func verifyApplied(applied map[int64]Applied, migrations []Migration) error {
	known := make(map[int64]Migration, len(migrations))
	for _, item := range migrations {
		known[item.Version] = item
	}
	for version, stored := range applied {
		item, ok := known[version]
		if !ok {
			return fmt.Errorf("%w: version %d", ErrUnknownAppliedMigration, version)
		}
		if stored.Name != item.Name || stored.Checksum != item.Checksum() {
			return fmt.Errorf("%w: version %d", ErrChecksumMismatch, version)
		}
	}
	return nil
}

func highestVersion(applied map[int64]Applied) int64 {
	var result int64
	for version := range applied {
		if version > result {
			result = version
		}
	}
	return result
}

func (r Runner) acquireAdvisoryLock(ctx context.Context, conn *sql.Conn) error {
	wait := r.LockWait
	if wait <= 0 {
		wait = 30 * time.Second
	}
	switch r.Dialect {
	case DialectMySQL:
		var acquired sql.NullInt64
		if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", "smail_schema_migration", int(math.Ceil(wait.Seconds()))).Scan(&acquired); err != nil {
			return fmt.Errorf("acquire MySQL migration lock: %w", err)
		}
		if !acquired.Valid || acquired.Int64 != 1 {
			return ErrLockUnavailable
		}
	case DialectPostgres:
		deadline := time.Now().Add(wait)
		for {
			var acquired bool
			if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", migrationLockID).Scan(&acquired); err != nil {
				return fmt.Errorf("acquire PostgreSQL migration lock: %w", err)
			}
			if acquired {
				return nil
			}
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return ErrLockUnavailable
			}
			delay := 100 * time.Millisecond
			if remaining < delay {
				delay = remaining
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
	default:
		return fmt.Errorf("unsupported advisory lock dialect %q", r.Dialect)
	}
	return nil
}

func (r Runner) releaseAdvisoryLock(conn *sql.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch r.Dialect {
	case DialectMySQL:
		_, _ = conn.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", "smail_schema_migration")
	case DialectPostgres:
		_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationLockID)
	}
}
