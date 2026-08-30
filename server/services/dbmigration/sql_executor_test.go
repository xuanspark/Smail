package dbmigration

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

type recordingStructuredCopier struct {
	called bool
	source config.DatabaseConfig
	target config.DatabaseConfig
	err    error
}

func (c *recordingStructuredCopier) Copy(_ context.Context, source, target config.DatabaseConfig) error {
	c.called = true
	c.source = source
	c.target = target
	return c.err
}

func validStructuredPlan(t *testing.T) Plan {
	t.Helper()
	database := testDatabase(t)
	database.Name = "smail"
	target := database
	target.Name = "smail_next"
	return Plan{
		sourceConfig: database,
		targetConfig: target,
		actor:        config.AuditActor{UserID: "admin-1", Role: "super_admin", SessionID: "session-1"},
	}
}

func TestSQLExecutorCopiesAndSwitchesWithOrderedStatuses(t *testing.T) {
	copier := &recordingStructuredCopier{}
	var actor config.AuditActor
	var generation uint64
	executor := NewSQLExecutor(copier, func(_ context.Context, receivedGeneration uint64, _ config.DatabaseConfig, received config.AuditActor) error {
		generation = receivedGeneration
		actor = received
		return nil
	})
	statuses := make([]Status, 0, 2)
	plan := validStructuredPlan(t)
	plan.expectedGeneration = 42
	if err := executor.Run(context.Background(), plan, func(status Status) error {
		statuses = append(statuses, status)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0] != StatusVerifying || statuses[1] != StatusSwitching {
		t.Fatalf("statuses = %v, want verifying then switching", statuses)
	}
	if !copier.called || copier.source.Name != "smail" || copier.target.Name != "smail_next" {
		t.Fatalf("copier call = %+v", copier)
	}
	if actor != plan.actor {
		t.Fatalf("switch actor = %+v, want %+v", actor, plan.actor)
	}
	if generation != plan.expectedGeneration {
		t.Fatalf("switch generation = %d, want %d", generation, plan.expectedGeneration)
	}
}

func TestSQLExecutorRejectsImmutablePlanBeforeCopy(t *testing.T) {
	copier := &recordingStructuredCopier{}
	executor := NewSQLExecutor(copier, func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error { return nil })
	plan := validStructuredPlan(t)
	plan.targetConfig.Host = "other.example"
	if err := executor.Run(context.Background(), plan, func(Status) error { return nil }); !errors.Is(err, ErrImmutableSetting) {
		t.Fatalf("immutable plan error = %v, want immutable setting", err)
	}
	if copier.called {
		t.Fatal("copier ran for immutable plan")
	}
}

func TestSQLExecutorPreservesCopierError(t *testing.T) {
	copyErr := errors.New("复制失败")
	copier := &recordingStructuredCopier{err: copyErr}
	executor := NewSQLExecutor(copier, func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error { return nil })
	if err := executor.Run(context.Background(), validStructuredPlan(t), func(Status) error { return nil }); !errors.Is(err, copyErr) {
		t.Fatalf("copy error = %v, want copier error", err)
	}
}

func TestSQLExecutorCleansStructuredTargetWhenCopyFails(t *testing.T) {
	copier := &cleanupRecordingCopier{copyErr: errors.New("复制失败")}
	executor := NewSQLExecutor(copier, func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error {
		return nil
	})
	if err := executor.Run(context.Background(), validStructuredPlan(t), func(Status) error { return nil }); !errors.Is(err, copier.copyErr) {
		t.Fatalf("copy error = %v, want %v", err, copier.copyErr)
	}
	if !copier.cleanupCalled {
		t.Fatal("structured target cleanup was not requested after copy failure")
	}
}

func TestStructuredDatabaseNameValidation(t *testing.T) {
	for _, value := range []string{"smail", "Smail_2026", "mail-prod"} {
		if !validStructuredDatabaseName(value) {
			t.Fatalf("valid database name rejected: %q", value)
		}
	}
	for _, value := range []string{"", "smail;drop", "smail name", "../smail"} {
		if validStructuredDatabaseName(value) {
			t.Fatalf("unsafe database name accepted: %q", value)
		}
	}
}

func TestDatabaseExecutorRoutesByDatabaseType(t *testing.T) {
	var sqliteCalled, structuredCalled bool
	sqlite := runnerFunc(func(context.Context, Plan, func(Status) error) error {
		sqliteCalled = true
		return nil
	})
	structured := runnerFunc(func(context.Context, Plan, func(Status) error) error {
		structuredCalled = true
		return nil
	})
	router := NewDatabaseExecutor(sqlite, structured)
	if err := router.Run(context.Background(), Plan{sourceConfig: config.DatabaseConfig{Type: config.DatabaseTypeSQLite}}, func(Status) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := router.Run(context.Background(), Plan{sourceConfig: config.DatabaseConfig{Type: config.DatabaseTypeMySQL}}, func(Status) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !sqliteCalled || !structuredCalled {
		t.Fatalf("router calls: sqlite=%v structured=%v", sqliteCalled, structuredCalled)
	}
}

type runnerFunc func(context.Context, Plan, func(Status) error) error

func (f runnerFunc) Run(ctx context.Context, plan Plan, update func(Status) error) error {
	return f(ctx, plan, update)
}
func TestSQLExecutorRejectsMissingConfigurationSwitchBeforeCopy(t *testing.T) {
	copier := &recordingStructuredCopier{}
	executor := NewSQLExecutor(copier, nil)
	if err := executor.Run(context.Background(), validStructuredPlan(t), func(Status) error { return nil }); !errors.Is(err, ErrSwitchUnavailable) {
		t.Fatalf("missing switch error = %v, want switch unavailable", err)
	}
	if copier.called {
		t.Fatal("copier ran without a configuration switch")
	}
}

type cleanupRecordingCopier struct {
	cleanupCalled  bool
	completeCalled bool
	copyErr        error
}

func (c *cleanupRecordingCopier) Copy(context.Context, config.DatabaseConfig, config.DatabaseConfig) error {
	return c.copyErr
}

func TestDatabaseAlreadyExistsErrorClassification(t *testing.T) {
	mysqlError := &mysql.MySQLError{Number: 1007}
	if !isDatabaseAlreadyExistsError(mysqlError, config.DatabaseTypeMySQL) {
		t.Fatal("mysql duplicate database error was not classified")
	}
	postgresError := &pq.Error{Code: "42P04"}
	if !isDatabaseAlreadyExistsError(postgresError, config.DatabaseTypePostgreSQL) {
		t.Fatal("postgresql duplicate database error was not classified")
	}
	if isDatabaseAlreadyExistsError(errors.New("duplicate database"), config.DatabaseTypeMySQL) {
		t.Fatal("generic mysql error was incorrectly classified")
	}
}

func (c *cleanupRecordingCopier) Cleanup(context.Context, config.DatabaseConfig, config.DatabaseConfig) error {
	c.cleanupCalled = true
	return nil
}

func (c *cleanupRecordingCopier) Complete(config.DatabaseConfig, config.DatabaseConfig) {
	c.completeCalled = true
}

func TestSQLExecutorPreservesStructuredTargetWhenConfigurationSwitchOutcomeIsUnknown(t *testing.T) {
	copier := &cleanupRecordingCopier{}
	executor := NewSQLExecutor(copier, func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error {
		return errors.New("配置切换失败")
	})
	if err := executor.Run(context.Background(), validStructuredPlan(t), func(Status) error { return nil }); !errors.Is(err, ErrSwitchOutcomeUnknown) {
		t.Fatal("configuration switch failure was not returned")
	}
	if copier.cleanupCalled {
		t.Fatal("structured target was cleaned up even though switch outcome was unknown")
	}
	if copier.completeCalled {
		t.Fatal("structured target was finalized after a failed switch")
	}
}

func TestSQLExecutorCleansStructuredTargetWhenStatusUpdateFails(t *testing.T) {
	copier := &cleanupRecordingCopier{}
	executor := NewSQLExecutor(copier, func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error {
		return nil
	})
	statusError := errors.New("状态写入失败")
	if err := executor.Run(context.Background(), validStructuredPlan(t), func(status Status) error {
		if status == StatusSwitching {
			return statusError
		}
		return nil
	}); !errors.Is(err, statusError) {
		t.Fatalf("status update error = %v, want %v", err, statusError)
	}
	if !copier.cleanupCalled {
		t.Fatal("structured target cleanup was not requested after status update failure")
	}
}

func TestSQLExecutorCleansStructuredTargetWhenContextIsCanceledAfterCopy(t *testing.T) {
	copier := &cleanupRecordingCopier{}
	executor := NewSQLExecutor(copier, func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error {
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := executor.Run(ctx, validStructuredPlan(t), func(status Status) error {
		if status == StatusVerifying {
			cancel()
		}
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled execution error = %v, want context canceled", err)
	}
	if !copier.cleanupCalled {
		t.Fatal("structured target cleanup was not requested after cancellation")
	}
}

func TestRuntimeSQLDSNSupportsIPv6AndEscapesCredentials(t *testing.T) {
	database := config.DatabaseConfig{
		Type:     config.DatabaseTypeMySQL,
		Host:     "2001:db8::3",
		Port:     3306,
		Username: "mail user",
	}
	driver, dsn, err := runtimeSQLDSN(database, "p@ss/word", "smail_next")
	if err != nil {
		t.Fatal(err)
	}
	if driver != "mysql" {
		t.Fatalf("mysql driver = %q", driver)
	}
	parsedMySQL, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsedMySQL.Addr != "[2001:db8::3]:3306" || parsedMySQL.User != "mail user" || parsedMySQL.Passwd != "p@ss/word" || parsedMySQL.DBName != "smail_next" {
		t.Fatalf("mysql dsn config = %+v", parsedMySQL)
	}

	database.Type = config.DatabaseTypePostgreSQL
	driver, dsn, err = runtimeSQLDSN(database, "p@ss/word", "smail_next")
	if err != nil {
		t.Fatal(err)
	}
	if driver != "postgres" {
		t.Fatalf("postgres driver = %q", driver)
	}
	parsedPostgres, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	password, ok := parsedPostgres.User.Password()
	if !ok || parsedPostgres.Host != "[2001:db8::3]:3306" || parsedPostgres.User.Username() != "mail user" || password != "p@ss/word" || parsedPostgres.Path != "/smail_next" {
		t.Fatalf("postgres dsn = %s", dsn)
	}
}

func TestMySQLReferenceRulesAndIdentifiersAreRestrictedSafely(t *testing.T) {
	for _, rule := range []string{"CASCADE", "SET NULL", "SET DEFAULT", "RESTRICT", "NO ACTION"} {
		if !validMySQLReferenceRule(rule) {
			t.Fatalf("valid mysql reference rule rejected: %q", rule)
		}
	}
	for _, rule := range []string{"DROP DATABASE", "CASCADE; DROP TABLE users"} {
		if validMySQLReferenceRule(rule) {
			t.Fatalf("unsafe mysql reference rule accepted: %q", rule)
		}
	}
	if got := quoteMySQLIdentifier("mail`prod"); got != "`mail``prod`" {
		t.Fatalf("quoted mysql identifier = %q", got)
	}
	if got := strings.Join(quoteMySQLIdentifiers([]string{"user", "tenant"}), ", "); got != "`user`, `tenant`" {
		t.Fatalf("quoted mysql identifiers = %q", got)
	}
}
