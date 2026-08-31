package dbmigration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/utils/credential"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

var (
	ErrSQLCopyUnavailable       = errors.New("structured database copier unavailable")
	ErrSQLTargetExists          = errors.New("structured database migration target already exists")
	ErrSQLUnsupportedObject     = errors.New("structured database contains unsupported object")
	ErrSQLConnectionUnavailable = errors.New("structured database connection unavailable")
	ErrSQLDataVerification      = errors.New("structured database data verification failed")
	ErrSQLSourceBusy            = errors.New("structured database source has active connections")
	ErrSwitchOutcomeUnknown     = errors.New("database configuration switch outcome is unknown")
)

type AuthorizedDatabaseSwitch func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error

type StructuredDatabaseCopier interface {
	Copy(context.Context, config.DatabaseConfig, config.DatabaseConfig) error
}

type StructuredDatabaseCleanup interface {
	Cleanup(context.Context, config.DatabaseConfig, config.DatabaseConfig) error
	Complete(config.DatabaseConfig, config.DatabaseConfig)
}

type DatabaseExecutor struct {
	sqlite     Runner
	structured Runner
}

func NewDatabaseExecutor(sqlite, structured Runner) Runner {
	return &DatabaseExecutor{sqlite: sqlite, structured: structured}
}

func (e *DatabaseExecutor) Run(ctx context.Context, plan Plan, update func(Status) error) error {
	if e == nil {
		return ErrExecutorUnavailable
	}
	if plan.sourceConfig.Type == config.DatabaseTypeSQLite {
		if e.sqlite == nil {
			return ErrExecutorUnavailable
		}
		return e.sqlite.Run(ctx, plan, update)
	}
	if e.structured == nil {
		return ErrExecutorUnavailable
	}
	return e.structured.Run(ctx, plan, update)
}

type SQLExecutor struct {
	copier           StructuredDatabaseCopier
	authorizedSwitch AuthorizedDatabaseSwitch
}

func NewSQLExecutor(copier StructuredDatabaseCopier, switchConfig AuthorizedDatabaseSwitch) *SQLExecutor {
	return &SQLExecutor{copier: copier, authorizedSwitch: switchConfig}
}

func (e *SQLExecutor) Run(ctx context.Context, plan Plan, update func(Status) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if e == nil || update == nil {
		return ErrInvalidRequest
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := validateSQLCopyRequest(plan.sourceConfig, plan.targetConfig); err != nil {
		return err
	}
	if e.copier == nil {
		return ErrSQLCopyUnavailable
	}
	if e.authorizedSwitch == nil {
		return ErrSwitchUnavailable
	}
	if err := update(StatusVerifying); err != nil {
		return err
	}
	if err := e.copier.Copy(ctx, plan.sourceConfig, plan.targetConfig); err != nil {
		cleanupStructuredTarget(e.copier, plan.sourceConfig, plan.targetConfig)
		return err
	}
	if err := contextError(ctx); err != nil {
		cleanupStructuredTarget(e.copier, plan.sourceConfig, plan.targetConfig)
		return err
	}
	if err := update(StatusSwitching); err != nil {
		cleanupStructuredTarget(e.copier, plan.sourceConfig, plan.targetConfig)
		return err
	}
	if e.authorizedSwitch == nil {
		cleanupStructuredTarget(e.copier, plan.sourceConfig, plan.targetConfig)
		return ErrSwitchUnavailable
	}
	if err := e.authorizedSwitch(ctx, plan.expectedGeneration, plan.targetConfig, plan.actor); err != nil {
		if isKnownSwitchRejection(err) {
			cleanupStructuredTarget(e.copier, plan.sourceConfig, plan.targetConfig)
			return err
		}
		return errors.Join(ErrSwitchOutcomeUnknown, err)
	}
	if cleanup, ok := e.copier.(StructuredDatabaseCleanup); ok {
		cleanup.Complete(plan.sourceConfig, plan.targetConfig)
	}
	return nil
}

func isKnownSwitchRejection(err error) bool {
	return errors.Is(err, config.ErrAuthorizationRequired) ||
		errors.Is(err, config.ErrDatabaseMigrationTarget) ||
		errors.Is(err, config.ErrGenerationConflict) ||
		errors.Is(err, config.ErrStepUpRequired) ||
		errors.Is(err, config.ErrInvalidRuntimeConfig)
}

func cleanupStructuredTarget(copier StructuredDatabaseCopier, source, target config.DatabaseConfig) {
	cleanup, ok := copier.(StructuredDatabaseCleanup)
	if !ok {
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = cleanup.Cleanup(cleanupContext, source, target)
}

func sameStructuredConnection(source, target config.DatabaseConfig) bool {
	return source.Type == target.Type &&
		source.Host == target.Host &&
		source.Port == target.Port &&
		source.Username == target.Username &&
		source.Password == target.Password &&
		source.LocalRef == target.LocalRef
}

type SQLConnectionFactory func(context.Context, config.DatabaseConfig, string) (*sql.DB, error)

type SQLCopier struct {
	open    SQLConnectionFactory
	mu      sync.Mutex
	created map[string]struct{}
}

func NewSQLCopier(open SQLConnectionFactory) *SQLCopier {
	return &SQLCopier{open: open, created: make(map[string]struct{})}
}

func (c *SQLCopier) Copy(ctx context.Context, source, target config.DatabaseConfig) error {
	if c == nil || c.open == nil {
		return ErrSQLConnectionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := validateSQLCopyRequest(source, target); err != nil {
		return err
	}
	adminDB, err := c.open(ctx, source, "")
	if err != nil || adminDB == nil {
		return ErrSQLConnectionUnavailable
	}
	defer adminDB.Close()
	if err := contextError(ctx); err != nil {
		return err
	}
	switch source.Type {
	case config.DatabaseTypeMySQL:
		sourceDB, err := c.open(ctx, source, source.Name)
		if err != nil || sourceDB == nil {
			return ErrSQLConnectionUnavailable
		}
		defer sourceDB.Close()
		return c.copyMySQL(ctx, sourceDB, adminDB, source, target)
	case config.DatabaseTypePostgreSQL:
		return c.copyPostgreSQL(ctx, adminDB, source, target)
	default:
		return ErrUnsupportedDatabase
	}
}

func (c *SQLCopier) copyMySQL(ctx context.Context, sourceDB, adminDB *sql.DB, source, target config.DatabaseConfig) error {
	sourceName := source.Name
	targetName := target.Name
	if err := c.validateMySQLObjects(ctx, adminDB, sourceName); err != nil {
		return err
	}
	var exists int
	if err := adminDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?", targetName).Scan(&exists); err != nil {
		return fmt.Errorf("check mysql migration target: %w", err)
	}
	if exists != 0 {
		return ErrSQLTargetExists
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+quoteMySQLIdentifier(targetName)); err != nil {
		if isDatabaseAlreadyExistsError(err, config.DatabaseTypeMySQL) {
			return ErrSQLTargetExists
		}
		return fmt.Errorf("create mysql migration target: %w", err)
	}
	c.markCreated(source, target)
	created := true
	defer func() {
		if created {
			cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := adminDB.ExecContext(cleanupContext, "DROP DATABASE "+quoteMySQLIdentifier(targetName)); err == nil {
				c.forgetCreated(source, target)
			}
		}
	}()

	rows, err := sourceDB.QueryContext(ctx, "SHOW FULL TABLES FROM "+quoteMySQLIdentifier(sourceName))
	if err != nil {
		return fmt.Errorf("list mysql tables: %w", err)
	}
	defer rows.Close()
	tableNames := make([]string, 0)
	sourceRowCounts := make(map[string]int64)
	for rows.Next() {
		if err := contextError(ctx); err != nil {
			return err
		}
		var tableName, tableType string
		if err := rows.Scan(&tableName, &tableType); err != nil {
			return fmt.Errorf("read mysql table: %w", err)
		}
		if !strings.EqualFold(tableType, "BASE TABLE") {
			return ErrSQLUnsupportedObject
		}
		tableNames = append(tableNames, tableName)
		sourceTable := quoteMySQLIdentifier(sourceName) + "." + quoteMySQLIdentifier(tableName)
		targetTable := quoteMySQLIdentifier(targetName) + "." + quoteMySQLIdentifier(tableName)
		rowCount, err := countRows(ctx, sourceDB, sourceTable)
		if err != nil {
			return fmt.Errorf("count mysql source rows: %w", err)
		}
		sourceRowCounts[tableName] = rowCount
		if _, err := adminDB.ExecContext(ctx, "CREATE TABLE "+targetTable+" LIKE "+sourceTable); err != nil {
			return fmt.Errorf("copy mysql table structure: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read mysql tables: %w", err)
	}
	for _, tableName := range tableNames {
		if err := contextError(ctx); err != nil {
			return err
		}
		sourceTable := quoteMySQLIdentifier(sourceName) + "." + quoteMySQLIdentifier(tableName)
		targetTable := quoteMySQLIdentifier(targetName) + "." + quoteMySQLIdentifier(tableName)
		if _, err := adminDB.ExecContext(ctx, "INSERT INTO "+targetTable+" SELECT * FROM "+sourceTable); err != nil {
			return fmt.Errorf("copy mysql table data: %w", err)
		}
		targetRowCount, err := countRows(ctx, adminDB, targetTable)
		if err != nil {
			return fmt.Errorf("count mysql target rows: %w", err)
		}
		finalSourceRowCount, err := countRows(ctx, sourceDB, sourceTable)
		if err != nil {
			return fmt.Errorf("verify mysql source rows: %w", err)
		}
		if sourceRowCounts[tableName] != targetRowCount || sourceRowCounts[tableName] != finalSourceRowCount {
			return fmt.Errorf("%w: mysql table %s row count changed during migration", ErrSQLDataVerification, tableName)
		}
	}
	if err := verifyMySQLTableStructure(ctx, adminDB, sourceName, targetName, tableNames); err != nil {
		return err
	}
	if err := c.copyMySQLForeignKeys(ctx, adminDB, sourceName, targetName, tableNames); err != nil {
		return err
	}
	if err := verifyMySQLForeignKeys(ctx, adminDB, sourceName, targetName); err != nil {
		return err
	}
	created = false
	return nil
}

type mysqlForeignKey struct {
	tableName         string
	constraintName    string
	columns           []string
	referencedSchema  string
	referencedTable   string
	referencedColumns []string
	updateRule        string
	deleteRule        string
}

func (c *SQLCopier) validateMySQLObjects(ctx context.Context, database *sql.DB, sourceName string) error {
	checks := []struct {
		query string
		args  []any
	}{
		{
			query: "SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema = ?",
			args:  []any{sourceName},
		},
		{
			query: "SELECT COUNT(*) FROM information_schema.events WHERE event_schema = ?",
			args:  []any{sourceName},
		},
		{
			query: "SELECT COUNT(*) FROM information_schema.routines WHERE routine_schema = ?",
			args:  []any{sourceName},
		},
	}
	for _, check := range checks {
		var count int
		if err := database.QueryRowContext(ctx, check.query, check.args...).Scan(&count); err != nil {
			return fmt.Errorf("%w: inspect mysql database objects", ErrSQLConnectionUnavailable)
		}
		if count > 0 {
			return ErrSQLUnsupportedObject
		}
	}
	return nil
}

func (c *SQLCopier) copyMySQLForeignKeys(ctx context.Context, database *sql.DB, sourceName, targetName string, tableNames []string) error {
	if len(tableNames) == 0 {
		return nil
	}
	foreignKeys, err := listMySQLForeignKeys(ctx, database, sourceName)
	if err != nil {
		return fmt.Errorf("%w: inspect mysql foreign keys", ErrSQLConnectionUnavailable)
	}
	for _, foreignKey := range foreignKeys {
		if err := contextError(ctx); err != nil {
			return err
		}
		referencedSchema := foreignKey.referencedSchema
		if referencedSchema == sourceName {
			referencedSchema = targetName
		}
		columns := quoteMySQLIdentifiers(foreignKey.columns)
		referencedColumns := quoteMySQLIdentifiers(foreignKey.referencedColumns)
		query := "ALTER TABLE " + quoteMySQLIdentifier(targetName) + "." + quoteMySQLIdentifier(foreignKey.tableName) + " ADD CONSTRAINT " + quoteMySQLIdentifier(foreignKey.constraintName) + " FOREIGN KEY (" + strings.Join(columns, ", ") + ") REFERENCES " + quoteMySQLIdentifier(referencedSchema) + "." + quoteMySQLIdentifier(foreignKey.referencedTable) + " (" + strings.Join(referencedColumns, ", ") + ") ON UPDATE " + foreignKey.updateRule + " ON DELETE " + foreignKey.deleteRule
		if _, err := database.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("copy mysql foreign key: %w", err)
		}
	}
	return nil
}

func verifyMySQLForeignKeys(ctx context.Context, database *sql.DB, sourceName, targetName string) error {
	source, err := listMySQLForeignKeys(ctx, database, sourceName)
	if err != nil {
		return fmt.Errorf("%w: list mysql source foreign keys for verification", ErrSQLConnectionUnavailable)
	}
	target, err := listMySQLForeignKeys(ctx, database, targetName)
	if err != nil {
		return fmt.Errorf("%w: list mysql target foreign keys for verification", ErrSQLConnectionUnavailable)
	}
	if !sameMySQLForeignKeys(source, target, sourceName, targetName) {
		return fmt.Errorf("%w: mysql foreign key structure changed during migration", ErrSQLDataVerification)
	}
	return nil
}

func listMySQLForeignKeys(ctx context.Context, database *sql.DB, schemaName string) ([]mysqlForeignKey, error) {
	rows, err := database.QueryContext(ctx, "SELECT kcu.TABLE_NAME, kcu.CONSTRAINT_NAME, kcu.COLUMN_NAME, kcu.REFERENCED_TABLE_SCHEMA, kcu.REFERENCED_TABLE_NAME, kcu.REFERENCED_COLUMN_NAME, rc.UPDATE_RULE, rc.DELETE_RULE FROM information_schema.KEY_COLUMN_USAGE kcu JOIN information_schema.TABLE_CONSTRAINTS tc ON tc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA AND tc.TABLE_NAME = kcu.TABLE_NAME AND tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME JOIN information_schema.REFERENTIAL_CONSTRAINTS rc ON rc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA AND rc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME AND rc.TABLE_NAME = kcu.TABLE_NAME WHERE tc.CONSTRAINT_SCHEMA = ? AND tc.CONSTRAINT_TYPE = 'FOREIGN KEY' AND kcu.REFERENCED_TABLE_NAME IS NOT NULL ORDER BY kcu.TABLE_NAME, kcu.CONSTRAINT_NAME, kcu.ORDINAL_POSITION", schemaName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	foreignKeys := make([]mysqlForeignKey, 0)
	indexes := make(map[string]int)
	for rows.Next() {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		var item mysqlForeignKey
		var column, referencedColumn string
		if err := rows.Scan(&item.tableName, &item.constraintName, &column, &item.referencedSchema, &item.referencedTable, &referencedColumn, &item.updateRule, &item.deleteRule); err != nil {
			return nil, err
		}
		if !validMySQLReferenceRule(item.updateRule) || !validMySQLReferenceRule(item.deleteRule) {
			return nil, ErrSQLUnsupportedObject
		}
		key := item.tableName + "\x00" + item.constraintName
		index, ok := indexes[key]
		if !ok {
			indexes[key] = len(foreignKeys)
			foreignKeys = append(foreignKeys, item)
			index = len(foreignKeys) - 1
		}
		foreignKeys[index].columns = append(foreignKeys[index].columns, column)
		foreignKeys[index].referencedColumns = append(foreignKeys[index].referencedColumns, referencedColumn)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return foreignKeys, nil
}

func sameMySQLForeignKeys(source, target []mysqlForeignKey, sourceName, targetName string) bool {
	if len(source) != len(target) {
		return false
	}
	sourceSignatures := make([]string, len(source))
	for index, item := range source {
		sourceSignatures[index] = mysqlForeignKeySignature(item, sourceName, targetName)
	}
	targetSignatures := make([]string, len(target))
	for index, item := range target {
		targetSignatures[index] = mysqlForeignKeySignature(item, sourceName, targetName)
	}
	sort.Strings(sourceSignatures)
	sort.Strings(targetSignatures)
	return sameStringSet(sourceSignatures, targetSignatures)
}

func mysqlForeignKeySignature(item mysqlForeignKey, sourceName, targetName string) string {
	referencedSchema := item.referencedSchema
	if referencedSchema == sourceName {
		referencedSchema = targetName
	}
	return strings.Join([]string{
		item.tableName,
		item.constraintName,
		strings.Join(item.columns, "\x00"),
		referencedSchema,
		item.referencedTable,
		strings.Join(item.referencedColumns, "\x00"),
		strings.ToUpper(strings.TrimSpace(item.updateRule)),
		strings.ToUpper(strings.TrimSpace(item.deleteRule)),
	}, "\x00")
}

func validMySQLReferenceRule(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "CASCADE", "SET NULL", "SET DEFAULT", "RESTRICT", "NO ACTION":
		return true
	default:
		return false
	}
}

func quoteMySQLIdentifiers(values []string) []string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = quoteMySQLIdentifier(value)
	}
	return quoted
}

func (c *SQLCopier) copyPostgreSQL(ctx context.Context, adminDB *sql.DB, source, target config.DatabaseConfig) error {
	sourceName := source.Name
	targetName := target.Name
	var exists bool
	if err := adminDB.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", targetName).Scan(&exists); err != nil {
		return fmt.Errorf("check postgresql migration target: %w", err)
	}
	if exists {
		return ErrSQLTargetExists
	}
	var activeConnections int
	if err := adminDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", sourceName).Scan(&activeConnections); err != nil {
		return fmt.Errorf("inspect postgresql source connections: %w", err)
	}
	if activeConnections > 0 {
		return ErrSQLSourceBusy
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+quotePostgreSQLIdentifier(targetName)+" TEMPLATE "+quotePostgreSQLIdentifier(sourceName)); err != nil {
		if isDatabaseAlreadyExistsError(err, config.DatabaseTypePostgreSQL) {
			return ErrSQLTargetExists
		}
		return fmt.Errorf("copy postgresql database: %w", err)
	}
	c.markCreated(source, target)
	created := true
	defer func() {
		if created {
			cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := c.Cleanup(cleanupContext, source, target); err == nil {
				created = false
			}
		}
	}()
	if err := c.verifyPostgreSQLClone(ctx, source, target); err != nil {
		return err
	}
	created = false
	return nil
}

func (c *SQLCopier) verifyPostgreSQLClone(ctx context.Context, source, target config.DatabaseConfig) error {
	if c == nil || c.open == nil {
		return ErrSQLConnectionUnavailable
	}
	sourceDB, err := c.open(ctx, source, source.Name)
	if err != nil {
		return ErrSQLConnectionUnavailable
	}
	defer sourceDB.Close()
	targetDB, err := c.open(ctx, target, target.Name)
	if err != nil {
		return ErrSQLConnectionUnavailable
	}
	defer targetDB.Close()

	tables, err := listPostgreSQLTables(ctx, sourceDB)
	if err != nil {
		return fmt.Errorf("%w: list postgresql source tables", ErrSQLConnectionUnavailable)
	}
	targetTables, err := listPostgreSQLTables(ctx, targetDB)
	if err != nil {
		return fmt.Errorf("%w: list postgresql target tables", ErrSQLConnectionUnavailable)
	}
	if !samePostgreSQLTables(tables, targetTables) {
		return fmt.Errorf("%w: postgresql table structure changed during migration", ErrSQLDataVerification)
	}
	for _, table := range tables {
		sourceColumns, err := listPostgreSQLColumns(ctx, sourceDB, table)
		if err != nil {
			return fmt.Errorf("%w: list postgresql source columns", ErrSQLConnectionUnavailable)
		}
		targetColumns, err := listPostgreSQLColumns(ctx, targetDB, table)
		if err != nil {
			return fmt.Errorf("%w: list postgresql target columns", ErrSQLConnectionUnavailable)
		}
		if !sameStringSet(sourceColumns, targetColumns) {
			return fmt.Errorf("%w: postgresql table %s columns changed during migration", ErrSQLDataVerification, table.name)
		}
		sourceCount, err := countRows(ctx, sourceDB, quotePostgreSQLIdentifier(table.schema)+"."+quotePostgreSQLIdentifier(table.name))
		if err != nil {
			return fmt.Errorf("%w: count postgresql source rows", ErrSQLConnectionUnavailable)
		}
		targetCount, err := countRows(ctx, targetDB, quotePostgreSQLIdentifier(table.schema)+"."+quotePostgreSQLIdentifier(table.name))
		if err != nil {
			return fmt.Errorf("%w: count postgresql target rows", ErrSQLConnectionUnavailable)
		}
		if sourceCount != targetCount {
			return fmt.Errorf("%w: postgresql table %s row count changed during migration", ErrSQLDataVerification, table.name)
		}
	}
	return nil
}

func validateSQLCopyRequest(source, target config.DatabaseConfig) error {
	source = config.NormalizeDatabaseConfig(source)
	target = config.NormalizeDatabaseConfig(target)
	if source.Type != target.Type ||
		(source.Type != config.DatabaseTypeMySQL && source.Type != config.DatabaseTypePostgreSQL) {
		return ErrUnsupportedDatabase
	}
	if err := config.ValidateDatabaseConfig(source); err != nil {
		return ErrSourceDatabase
	}
	if err := config.ValidateDatabaseConfig(target); err != nil {
		return ErrInvalidTargetName
	}
	if !validStructuredDatabaseName(source.Name) {
		return ErrSourceDatabase
	}
	if !validStructuredDatabaseName(target.Name) {
		return ErrInvalidTargetName
	}
	if source.Name == target.Name ||
		!sameStructuredConnection(source, target) {
		return ErrImmutableSetting
	}
	return nil
}

func verifyMySQLTableStructure(ctx context.Context, database *sql.DB, sourceName, targetName string, tableNames []string) error {
	sourceTables, err := listMySQLTables(ctx, database, sourceName)
	if err != nil {
		return fmt.Errorf("%w: list mysql source tables for verification", ErrSQLConnectionUnavailable)
	}
	targetTables, err := listMySQLTables(ctx, database, targetName)
	if err != nil {
		return fmt.Errorf("%w: list mysql target tables for verification", ErrSQLConnectionUnavailable)
	}
	if !sameStringSet(sourceTables, targetTables) || !sameStringSet(sourceTables, tableNames) {
		return fmt.Errorf("%w: mysql table structure changed during migration", ErrSQLDataVerification)
	}
	for _, tableName := range sourceTables {
		sourceColumns, err := listMySQLColumns(ctx, database, sourceName, tableName)
		if err != nil {
			return fmt.Errorf("%w: list mysql source columns", ErrSQLConnectionUnavailable)
		}
		targetColumns, err := listMySQLColumns(ctx, database, targetName, tableName)
		if err != nil {
			return fmt.Errorf("%w: list mysql target columns", ErrSQLConnectionUnavailable)
		}
		if !sameStringSet(sourceColumns, targetColumns) {
			return fmt.Errorf("%w: mysql table %s columns changed during migration", ErrSQLDataVerification, tableName)
		}
	}
	return nil
}

func listMySQLTables(ctx context.Context, database *sql.DB, databaseName string) ([]string, error) {
	rows, err := database.QueryContext(ctx, "SELECT TABLE_NAME FROM information_schema.tables WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME", databaseName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		result = append(result, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func listMySQLColumns(ctx context.Context, database *sql.DB, databaseName, tableName string) ([]string, error) {
	rows, err := database.QueryContext(ctx, "SELECT ORDINAL_POSITION, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COALESCE(COLUMN_DEFAULT, ''), EXTRA, COLUMN_KEY FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION", databaseName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		var ordinal int
		var name, columnType, nullable, defaultValue, extra, key string
		if err := rows.Scan(&ordinal, &name, &columnType, &nullable, &defaultValue, &extra, &key); err != nil {
			return nil, err
		}
		result = append(result, fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", ordinal, name, columnType, nullable, defaultValue, extra, key))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func listPostgreSQLColumns(ctx context.Context, database *sql.DB, table postgreSQLTable) ([]string, error) {
	rows, err := database.QueryContext(ctx, "SELECT ordinal_position, column_name, data_type, udt_name, is_nullable, COALESCE(column_default, ''), COALESCE(character_maximum_length, -1), COALESCE(numeric_precision, -1), COALESCE(numeric_scale, -1) FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position", table.schema, table.name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		var ordinal, characterLength, precision, scale int
		var name, dataType, udtName, nullable, defaultValue string
		if err := rows.Scan(&ordinal, &name, &dataType, &udtName, &nullable, &defaultValue, &characterLength, &precision, &scale); err != nil {
			return nil, err
		}
		result = append(result, fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%d", ordinal, name, dataType, udtName, nullable, defaultValue, characterLength, precision, scale))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
func samePostgreSQLTables(source, target []postgreSQLTable) bool {
	if len(source) != len(target) {
		return false
	}
	seen := make(map[string]struct{}, len(source))
	for _, table := range source {
		seen[table.schema+"\x00"+table.name] = struct{}{}
	}
	for _, table := range target {
		if _, ok := seen[table.schema+"\x00"+table.name]; !ok {
			return false
		}
	}
	return true
}

func sameStringSet(source, target []string) bool {
	if len(source) != len(target) {
		return false
	}
	seen := make(map[string]struct{}, len(source))
	for _, value := range source {
		seen[value] = struct{}{}
	}
	for _, value := range target {
		if _, ok := seen[value]; !ok {
			return false
		}
	}
	return true
}

type postgreSQLTable struct {
	schema string
	name   string
}

func listPostgreSQLTables(ctx context.Context, database *sql.DB) ([]postgreSQLTable, error) {
	rows, err := database.QueryContext(ctx, "SELECT table_schema, table_name FROM information_schema.tables WHERE table_type = 'BASE TABLE' AND table_schema NOT IN ('pg_catalog', 'information_schema') ORDER BY table_schema, table_name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := make([]postgreSQLTable, 0)
	for rows.Next() {
		var table postgreSQLTable
		if err := rows.Scan(&table.schema, &table.name); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tables, nil
}

func isDatabaseAlreadyExistsError(err error, databaseType config.DatabaseType) bool {
	if err == nil {
		return false
	}
	switch databaseType {
	case config.DatabaseTypeMySQL:
		var mysqlErr *mysql.MySQLError
		return errors.As(err, &mysqlErr) && mysqlErr.Number == 1007
	case config.DatabaseTypePostgreSQL:
		var postgresErr *pq.Error
		return errors.As(err, &postgresErr) && string(postgresErr.Code) == "42P04"
	default:
		return false
	}
}

func (c *SQLCopier) markCreated(source, target config.DatabaseConfig) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.created == nil {
		c.created = make(map[string]struct{})
	}
	c.created[databaseTargetKey(source, target)] = struct{}{}
}

func (c *SQLCopier) hasCreated(source, target config.DatabaseConfig) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.created[databaseTargetKey(source, target)]
	return ok
}

func (c *SQLCopier) forgetCreated(source, target config.DatabaseConfig) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.created, databaseTargetKey(source, target))
	c.mu.Unlock()
}

func (c *SQLCopier) Complete(source, target config.DatabaseConfig) {
	c.forgetCreated(source, target)
}

func (c *SQLCopier) Cleanup(ctx context.Context, source, target config.DatabaseConfig) error {
	if !c.hasCreated(source, target) {
		return nil
	}
	if c.open == nil {
		return ErrSQLConnectionUnavailable
	}
	adminDB, err := c.open(ctx, source, "")
	if err != nil {
		return ErrSQLConnectionUnavailable
	}
	defer adminDB.Close()
	var query string
	switch target.Type {
	case config.DatabaseTypeMySQL:
		query = "DROP DATABASE " + quoteMySQLIdentifier(target.Name)
	case config.DatabaseTypePostgreSQL:
		query = "DROP DATABASE " + quotePostgreSQLIdentifier(target.Name)
	default:
		return ErrUnsupportedDatabase
	}
	if _, err := adminDB.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("cleanup structured migration target: %w", err)
	}
	c.forgetCreated(source, target)
	return nil
}

func countRows(ctx context.Context, database *sql.DB, table string) (int64, error) {
	var count int64
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func databaseTargetKey(source, target config.DatabaseConfig) string {
	return strings.Join([]string{
		string(target.Type),
		target.Host,
		strconv.Itoa(int(target.Port)),
		target.Username,
		target.Name,
		source.Name,
	}, "\x00")
}

func quoteMySQLIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func quotePostgreSQLIdentifier(value string) string {
	return "\"" + strings.ReplaceAll(value, "\"", "\"\"") + "\""
}

func NewRuntimeSQLConnectionFactory(root string) (SQLConnectionFactory, error) {
	vault, err := credential.NewVault(root)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, database config.DatabaseConfig, databaseName string) (*sql.DB, error) {
		password, err := config.OpenStoredSecret(vault, database.Password)
		if err != nil {
			return nil, ErrSQLConnectionUnavailable
		}
		driverName, dsn, err := runtimeSQLDSN(database, password, databaseName)
		if err != nil {
			return nil, err
		}
		connection, err := sql.Open(driverName, dsn)
		if err != nil {
			return nil, ErrSQLConnectionUnavailable
		}
		if err := connection.PingContext(ctx); err != nil {
			_ = connection.Close()
			return nil, ErrSQLConnectionUnavailable
		}
		return connection, nil
	}, nil
}

func runtimeSQLDSN(database config.DatabaseConfig, password, databaseName string) (string, string, error) {
	if database.Host == "" || database.Port == 0 || database.Username == "" || password == "" {
		return "", "", ErrSQLConnectionUnavailable
	}
	address := net.JoinHostPort(database.Host, strconv.Itoa(int(database.Port)))
	switch database.Type {
	case config.DatabaseTypeMySQL:
		settings := mysql.Config{
			User:      database.Username,
			Passwd:    password,
			Net:       "tcp",
			Addr:      address,
			DBName:    databaseName,
			ParseTime: true,
			Params:    map[string]string{"charset": "utf8mb4"},
		}
		return "mysql", settings.FormatDSN(), nil
	case config.DatabaseTypePostgreSQL:
		maintenanceDatabase := databaseName
		if maintenanceDatabase == "" {
			maintenanceDatabase = "postgres"
		}
		databaseURL := url.URL{
			Scheme:   "postgres",
			User:     url.UserPassword(database.Username, password),
			Host:     address,
			Path:     "/" + maintenanceDatabase,
			RawQuery: "sslmode=prefer",
		}
		return "postgres", databaseURL.String(), nil
	default:
		return "", "", ErrUnsupportedDatabase
	}
}

var structuredDatabaseNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,62}$`)
var mysqlOptionIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func validStructuredDatabaseName(value string) bool {
	return structuredDatabaseNamePattern.MatchString(strings.TrimSpace(value))
}
