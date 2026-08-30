package dbmigration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	_ "modernc.org/sqlite"
)

var ErrSQLiteIntegrity = errors.New("sqlite database integrity check failed")

type SQLiteSwitch func(context.Context, config.DatabaseConfig) error
type AuthorizedSQLiteSwitch func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error

type SQLiteExecutor struct {
	root             string
	switchConfig     SQLiteSwitch
	authorizedSwitch AuthorizedSQLiteSwitch
}

func NewSQLiteExecutor(root string, switchConfig SQLiteSwitch) *SQLiteExecutor {
	return &SQLiteExecutor{root: root, switchConfig: switchConfig}
}

func NewAuthorizedSQLiteExecutor(root string, switchConfig AuthorizedSQLiteSwitch) *SQLiteExecutor {
	return &SQLiteExecutor{root: root, authorizedSwitch: switchConfig}
}

func (e *SQLiteExecutor) Run(ctx context.Context, plan Plan, update func(Status) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if e == nil || update == nil {
		return ErrInvalidRequest
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if plan.sourceConfig.Type != config.DatabaseTypeSQLite || plan.targetConfig.Type != config.DatabaseTypeSQLite {
		return ErrUnsupportedDatabase
	}
	if err := config.ValidateDatabaseConfig(plan.sourceConfig); err != nil {
		return ErrSourceDatabase
	}
	if err := config.ValidateDatabaseConfig(plan.targetConfig); err != nil {
		return ErrInvalidTargetName
	}
	if plan.sourceConfig.LocalRef == plan.targetConfig.LocalRef {
		return ErrInvalidTargetName
	}

	sourcePath, err := e.resolveReference(plan.sourceConfig.LocalRef)
	if err != nil {
		return err
	}
	targetPath, err := e.resolveReference(plan.targetConfig.LocalRef)
	if err != nil {
		return err
	}
	if sourcePath == targetPath {
		return ErrInvalidTargetName
	}
	if err := checkRegularFile(sourcePath, false); err != nil {
		return fmt.Errorf("open source database: %w", err)
	}
	if err := checkRegularFile(targetPath, true); err != nil {
		return err
	}
	if err := update(StatusVerifying); err != nil {
		return err
	}
	if err := verifySQLite(ctx, sourcePath); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}

	temporary, err := e.temporaryPath(targetPath)
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	activated := false
	switched := false
	switchStarted := false
	defer func() {
		if activated && !switched && !switchStarted {
			_ = os.Remove(targetPath)
		}
	}()
	if err := vacuumSQLite(ctx, sourcePath, temporary); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0600); err != nil {
		return fmt.Errorf("set migrated database permissions: %w", err)
	}
	if err := verifySQLite(ctx, temporary); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := update(StatusSwitching); err != nil {
		return err
	}
	if err := os.Link(temporary, targetPath); err != nil {
		if os.IsExist(err) {
			return ErrTargetExists
		}
		return fmt.Errorf("activate migrated database file: %w", err)
	}
	activated = true
	if err := os.Remove(temporary); err != nil {
		return fmt.Errorf("finalize migrated database file: %w", err)
	}
	if err := verifySQLite(ctx, targetPath); err != nil {
		return err
	}
	if e.authorizedSwitch == nil && e.switchConfig == nil {
		return ErrSwitchUnavailable
	}
	var switchErr error
	switchStarted = true
	if e.authorizedSwitch != nil {
		switchErr = e.authorizedSwitch(ctx, plan.expectedGeneration, plan.targetConfig, plan.actor)
	} else {
		switchErr = e.switchConfig(ctx, plan.targetConfig)
	}
	if switchErr != nil {
		if isKnownSwitchRejection(switchErr) {
			_ = os.Remove(targetPath)
			return switchErr
		}
		return errors.Join(ErrSwitchOutcomeUnknown, switchErr)
	}
	switched = true
	return nil
}

func (e *SQLiteExecutor) resolveReference(reference string) (string, error) {
	if e == nil || strings.TrimSpace(e.root) == "" || !sqliteTargetNamePattern.MatchString(reference) {
		return "", ErrInvalidTargetName
	}
	root, err := filepath.Abs(filepath.Clean(e.root))
	if err != nil {
		return "", ErrInvalidTargetName
	}
	configDirectory := filepath.Join(root, "config")
	info, err := os.Lstat(configDirectory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", ErrInvalidTargetName
	}
	path := filepath.Join(configDirectory, reference)
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil || filepath.Dir(absolute) != configDirectory {
		return "", ErrInvalidTargetName
	}
	return absolute, nil
}

func (e *SQLiteExecutor) temporaryPath(target string) (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		id, err := randomID(".migration-")
		if err != nil {
			return "", err
		}
		candidate := filepath.Join(filepath.Dir(target), id+".tmp")
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", ErrTargetExists
}

func checkRegularFile(path string, target bool) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if target {
			return nil
		}
		return fmt.Errorf("source database does not exist: %w", err)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		if target {
			return ErrTargetExists
		}
		return errors.New("source database is not a regular file")
	}
	if target {
		return ErrTargetExists
	}
	return nil
}

func verifySQLite(ctx context.Context, path string) error {
	database, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return fmt.Errorf("open sqlite database: %w", err)
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: ping sqlite database: %v", ErrSQLiteIntegrity, err)
	}
	var result string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: check sqlite database integrity: %v", ErrSQLiteIntegrity, err)
	}
	if !strings.EqualFold(strings.TrimSpace(result), "ok") {
		return fmt.Errorf("%w: %s", ErrSQLiteIntegrity, result)
	}
	return nil
}

func vacuumSQLite(ctx context.Context, source, target string) error {
	database, err := sql.Open("sqlite", source+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return fmt.Errorf("open sqlite source: %w", err)
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite source: %w", err)
	}
	query := "VACUUM INTO '" + strings.ReplaceAll(target, "'", "''") + "'"
	if _, err := database.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("copy sqlite database: %w", err)
	}
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
