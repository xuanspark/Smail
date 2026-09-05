package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// 审查专用：模拟配置落盘中断，只操作测试创建的临时配置根目录。
func TestAuditSuccessfulConfigRetryMustSurviveRestart(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	current := manager.Snapshot()
	email := current.Email
	email.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(email)
	if err != nil {
		t.Fatal(err)
	}
	actor := authorizedConfigActor()
	updated, err := manager.Update(context.Background(), UpdateRequest{
		Section: SectionEmail, ExpectedGeneration: current.Generation, Payload: payload, Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("审查注入的一次性分区提交失败")
	manager.transactions.WithHooks(TransactionHooks{BeforePhase: func(phase TransactionPhase) error {
		if phase == TransactionSectionsCommitted {
			return interrupted
		}
		return nil
	}})
	if _, err := manager.ApplyAuthorized(context.Background(), updated.Generation, actor); !errors.Is(err, interrupted) {
		t.Fatalf("中断前提不成立: %v", err)
	}
	manager.transactions.WithHooks(TransactionHooks{})
	if _, err := manager.ApplyAuthorized(context.Background(), updated.Generation, actor); err != nil {
		t.Fatalf("重试没有成功: %v", err)
	}
	before, err := NewConfigLoader(root, options).Load()
	if err != nil {
		t.Fatal(err)
	}
	if before.Generation != updated.Generation || before.Email.Policy.MaxMessageBytes != email.Policy.MaxMessageBytes {
		t.Fatal("重试结果未实际持久化")
	}
	restarted, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	after := restarted.Snapshot()
	t.Logf("successful_generation=%d before_restart=%d after_restart=%d expected_bytes=%d actual_bytes=%d", updated.Generation, before.Generation, after.Generation, email.Policy.MaxMessageBytes, after.Email.Policy.MaxMessageBytes)
	if after.Generation != before.Generation || after.Email.Policy.MaxMessageBytes != before.Email.Policy.MaxMessageBytes {
		t.Fatal("旧失败事务在启动恢复时撤销了后续已成功落盘的配置")
	}
}

// 记录真实后端候选状态约束，供前端丢失候选代次的复现使用。
func TestAuditPendingConfigBlocksFurtherUpdates(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	current := manager.Snapshot()
	email := current.Email
	email.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(email)
	if err != nil {
		t.Fatal(err)
	}
	actor := authorizedConfigActor()
	request := UpdateRequest{Section: SectionEmail, ExpectedGeneration: current.Generation, Payload: payload, Actor: actor}
	updated, err := manager.Update(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	_, retryErr := manager.Update(context.Background(), request)
	if !errors.Is(retryErr, ErrGenerationConflict) {
		t.Fatalf("原会话再次保存的后端前提变化: %v", retryErr)
	}
	newActor := actor
	newActor.SessionID = "audit-new-session"
	request.Actor = newActor
	_, newSaveErr := manager.Update(context.Background(), request)
	_, newApplyErr := manager.ApplyAuthorized(context.Background(), updated.Generation, newActor)
	if !errors.Is(newSaveErr, ErrGenerationConflict) || !errors.Is(newApplyErr, ErrAuthorizationRequired) {
		t.Fatalf("重新登录后的后端前提变化: save=%v apply=%v", newSaveErr, newApplyErr)
	}
	t.Logf("active=%d pending=%d retry_conflict=%t new_session_save_conflict=%t new_session_apply_denied=%t", manager.Status().ActiveGeneration, updated.Generation, errors.Is(retryErr, ErrGenerationConflict), errors.Is(newSaveErr, ErrGenerationConflict), errors.Is(newApplyErr, ErrAuthorizationRequired))
	// 原会话保留代次时可以应用，问题不是所有应用请求都无条件失败。
	if _, err := manager.ApplyAuthorized(context.Background(), updated.Generation, actor); err != nil {
		t.Fatalf("原会话应用的对照组失败: %v", err)
	}
}

func TestAuditLegacyMigrationMustPreserveSQLiteLocation(t *testing.T) {
	root := t.TempDir()
	oldRoot := ROOT_PATH
	ROOT_PATH = root
	t.Cleanup(func() { ROOT_PATH = oldRoot })
	sourcePath := filepath.Join(root, "config", "archive", "review.db")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0700); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec("CREATE TABLE audit_marker (value TEXT); INSERT INTO audit_marker VALUES ('original')"); err != nil {
		t.Fatal(err)
	}
	writeLegacyMigrationFixture(t, root, map[string]any{
		"logLevel": "info", "domain": "mail.example.test", "webDomain": "mail.example.test",
		"dbType": "sqlite", "dbDSN": "./config/archive/review.db", "isInit": true,
		"sslType": "1", "SSLPublicKeyPath": "./config/ssl/public.crt", "SSLPrivateKeyPath": "./config/ssl/private.key",
		"httpPort": 80, "httpsPort": 443, "httpsEnabled": 1,
	})
	before, err := ReadConfig()
	if err != nil || before.DbDSN != sourcePath {
		t.Fatalf("原配置读取路径前提不成立: %v", err)
	}
	if _, err := MigrateLegacyConfig(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err != nil {
		t.Fatal(err)
	}
	after, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	_, sourceErr := os.Stat(sourcePath)
	_, targetErr := os.Stat(after.DbDSN)
	target, err := sql.Open("sqlite", after.DbDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	var marker string
	queryErr := target.QueryRow("SELECT value FROM audit_marker").Scan(&marker)
	t.Logf("source_exists=%t target_missing_before_open=%t same_path=%t marker=%q query_error=%v",
		sourceErr == nil, os.IsNotExist(targetErr), before.DbDSN == after.DbDSN, marker, queryErr)
	if before.DbDSN != after.DbDSN || queryErr != nil || marker != "original" {
		t.Fatal("仅迁移配置不应把已有 SQLite 指向一个新建的空库")
	}
}
