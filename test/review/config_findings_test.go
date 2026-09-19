package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/utils/runtimepath"
	_ "modernc.org/sqlite"
)

func TestAuditAutomaticSSLPolicy(t *testing.T) {
	var empty *Config
	if empty.UsesProxyHTTPS() || empty.AutomaticSSLEnabled() {
		t.Fatal("空配置不能启用自动证书")
	}
	for _, mode := range []string{"", HTTPSTLSModeDirect, HTTPSTLSModeProxy, " PROXY "} {
		for _, sslType := range []string{"", SSLTypeUser, SSLTypeAutoHTTP, SSLTypeAutoDNS} {
			cfg := &Config{TLSMode: mode, SSLType: sslType}
			want := (mode == "" || mode == HTTPSTLSModeDirect) && (sslType == SSLTypeAutoHTTP || sslType == SSLTypeAutoDNS)
			if cfg.AutomaticSSLEnabled() != want {
				t.Fatalf("自动证书策略错误: mode=%q type=%q", mode, sslType)
			}
		}
	}
}

func TestAuditProxySwitchRejectsStaleSSLCommit(t *testing.T) {
	previousRoot, previousSnapshot := ROOT_PATH, CurrentConfig()
	ROOT_PATH = t.TempDir()
	t.Cleanup(func() {
		ROOT_PATH = previousRoot
		PublishConfigSnapshot(previousSnapshot)
	})
	expected := &Config{TLSMode: HTTPSTLSModeDirect, SSLType: SSLTypeAutoDNS, HttpsEnabled: 2}
	if err := WriteConfig(expected); err != nil {
		t.Fatal(err)
	}
	live := *expected
	// 只修改 TLSMode，确保拒绝不是由 HTTPS 开关或域名变化引起。
	live.TLSMode = HTTPSTLSModeProxy
	if err := WriteConfig(&live); err != nil {
		t.Fatal(err)
	}
	if err := WriteSSLConfig(expected, expected); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("切换反代后未拒绝旧签发结果: %v", err)
	}
	stored, err := ReadConfig()
	if err != nil || !stored.UsesProxyHTTPS() {
		t.Fatal("过期签发改变了反代策略")
	}
}

func TestAuditTLSStatusTracksAppliedPolicy(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	runtime.Root.HTTP.TLSMode = HTTPSTLSModeDirect
	writeRuntimeFixture(t, root, runtime)
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	actor := authorizedConfigActor()
	for _, mode := range []string{HTTPSTLSModeProxy, HTTPSTLSModeDirect} {
		current := manager.Snapshot()
		next := current.Root
		next.HTTP.TLSMode = mode
		next.HTTP.HTTPSEnabled = mode == HTTPSTLSModeDirect
		next.HTTP.HTTPSPort = 0
		if next.HTTP.HTTPSEnabled {
			next.HTTP.HTTPSPort = 443
		}
		next.HTTP.TrustedProxyCIDRs = []string{"127.0.0.1/32"}
		payload, err := json.Marshal(next)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := manager.Update(context.Background(), UpdateRequest{
			Section: SectionRoot, ExpectedGeneration: current.Generation, Payload: payload, Actor: actor,
			Confirmation: StepUpConfirmation{Confirmed: true, Method: "recent_auth"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if manager.Status().TLSMode != current.Root.HTTP.TLSMode {
			t.Fatal("未应用的候选不能改变生效模式提示")
		}
		if _, err := manager.ApplyAuthorized(context.Background(), updated.Generation, actor); err != nil {
			t.Fatal(err)
		}
		if manager.Status().TLSMode != mode {
			t.Fatal("状态没有跟随已应用的 HTTPS 模式")
		}
	}
}

func TestAuditPendingIdentityAndRecovery(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	actor := authorizedConfigActor()
	payload, _ := json.Marshal(manager.Snapshot().Email)
	req := UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: actor}
	first, err := manager.Update(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	confirmation := StepUpConfirmation{Confirmed: true, Method: "recent_auth"}
	if err := manager.ManagePending(context.Background(), first.Generation, first.CandidateID, actor, confirmation, true); err != nil {
		t.Fatal(err)
	}
	second, err := manager.Update(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.CandidateID == second.CandidateID {
		t.Fatal("不同候选不能复用标识")
	}
	if _, err := manager.ApplyPendingAuthorized(context.Background(), first.Generation, first.CandidateID, SectionEmail, actor); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("旧候选应用未拒绝: %v", err)
	}
	if err := manager.ManagePending(context.Background(), first.Generation, first.CandidateID, actor, confirmation, true); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("旧候选取消未拒绝: %v", err)
	}
	actor.SessionID = "renewed-session"
	actor.RecentAuth = false
	if err := manager.ManagePending(context.Background(), second.Generation, second.CandidateID, actor, confirmation, false); err == nil {
		t.Fatal("跨会话恢复缺少近期认证")
	}
	actor.RecentAuth = true
	if err := manager.ManagePending(context.Background(), second.Generation, second.CandidateID, actor, confirmation, false); err != nil {
		t.Fatal(err)
	}
	if !manager.Pending(actor).SameSession {
		t.Fatal("恢复没有绑定新会话")
	}
	manager.snapshots = &flakyRuntimeSnapshotStore{current: manager.Snapshot(), failures: 1}
	if _, err := manager.ApplyPendingAuthorized(context.Background(), second.Generation, second.CandidateID, SectionEmail, actor); !errors.Is(err, ErrRuntimeSnapshotUnavailable) {
		t.Fatal(err)
	}
	if err := manager.ManagePending(context.Background(), second.Generation, second.CandidateID, actor, confirmation, true); !errors.Is(err, ErrRuntimeSnapshotUnavailable) {
		t.Fatalf("已落盘候选不应取消: %v", err)
	}
	manager.transactions.WithHooks(TransactionHooks{BeforePhase: func(TransactionPhase) error { return errors.New("不能再次提交") }})
	if _, err := manager.ApplyPendingAuthorized(context.Background(), second.Generation, second.CandidateID, SectionEmail, actor); err != nil {
		t.Fatalf("快照重试重新写盘: %v", err)
	}
}

func TestAuditStagedSSLDoesNotReplaceCurrent(t *testing.T) {
	previous := ROOT_PATH
	ROOT_PATH = t.TempDir()
	t.Cleanup(func() { ROOT_PATH = previous })
	key, cert := testManagedCertificate(t, "mail.example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048)
	active, err := ImportManagedSSL(ManagedSSLModeDefault, key, cert, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	nextKey, nextCert := testManagedCertificate(t, "mail.example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048)
	staged, err := StageManagedSSL(ManagedSSLModeDefault, nextKey, nextCert, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(active.CertificatePath)
	if err != nil || string(actual) != string(cert) {
		t.Fatal("暂存替换了现用证书")
	}
	if _, err := ResolveManagedSSLReferences(SSLConfig{Mode: staged.Mode, CertificateRef: staged.CertificateRef, PrivateKeyRef: active.PrivateKeyRef}); !errors.Is(err, ErrInvalidSSLReference) {
		t.Fatalf("跨版本配对未拒绝: %v", err)
	}
	if _, err := ResolveManagedSSLReferences(SSLConfig{Mode: staged.Mode, CertificateRef: staged.CertificateRef, PrivateKeyRef: staged.PrivateKeyRef}); err != nil {
		t.Fatal(err)
	}
}

func TestAuditLegacySSLCommitPreservesLatestUnrelatedSettings(t *testing.T) {
	previousRoot, previousSnapshot := ROOT_PATH, CurrentConfig()
	ROOT_PATH = t.TempDir()
	t.Cleanup(func() {
		ROOT_PATH = previousRoot
		PublishConfigSnapshot(previousSnapshot)
	})
	writeLegacyMigrationFixture(t, ROOT_PATH, map[string]any{
		"domain": "example.test", "webDomain": "mail.example.test",
		"sslType": SSLTypeAutoDNS, "httpsEnabled": 1, "httpPort": 8080,
	})
	expected, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	next := *expected
	next.SSL = SSLConfig{Mode: ManagedSSLModeDefault, Challenge: SSLChallengeDNS, DefaultDirectory: "config/ssl"}
	// 模拟签发期间另一项设置先保存成功，续期不得用旧快照覆盖它。
	live := *expected
	live.HttpPort = 9080
	if err := WriteConfig(&live); err != nil {
		t.Fatal(err)
	}
	if err := WriteSSLConfig(expected, &next); err != nil {
		t.Fatal(err)
	}
	stored, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if stored.HttpPort != 9080 || next.HttpPort != 9080 || CurrentConfig().HttpPort != 9080 || stored.SSL != next.SSL {
		t.Fatalf("SSL 提交覆盖了其他设置：disk=%d next=%d runtime=%d", stored.HttpPort, next.HttpPort, CurrentConfig().HttpPort)
	}
	stale := *stored
	live = *stored
	live.WebDomain = "changed.example.test"
	if err := WriteConfig(&live); err != nil {
		t.Fatal(err)
	}
	if err := WriteSSLConfig(&stale, &stale); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("签发期间域名变化必须拒绝旧结果：%v", err)
	}
}

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

func TestAuditLegacyMigrationMustPreserveSQLiteData(t *testing.T) {
	root := t.TempDir()
	oldRoot := ROOT_PATH
	ROOT_PATH = root
	t.Cleanup(func() { ROOT_PATH = oldRoot })
	sourcePath := filepath.Join(root, "config", "review.db")
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
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	writeLegacyMigrationFixture(t, root, map[string]any{
		"logLevel": "info", "domain": "mail.example.test", "webDomain": "mail.example.test",
		"dbType": "sqlite", "dbDSN": "./config/review.db", "isInit": true,
		"sslType": "1", "SSLPublicKeyPath": "./config/ssl/public.crt", "SSLPrivateKeyPath": "./config/ssl/private.key",
		"httpPort": 80, "httpsPort": 443, "httpsEnabled": 1,
	})
	before, err := ReadConfig()
	if err != nil || before.DbDSN != sourcePath {
		t.Fatalf("原配置读取路径前提不成立: %v", err)
	}
	if err := runtimepath.CheckReady(root); !errors.Is(err, runtimepath.ErrMigrationRequired) {
		t.Fatalf("未迁移旧账号库时必须拒绝启动: %v", err)
	}
	if _, err := runtimepath.Migrate(root); err != nil {
		t.Fatal(err)
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
	if after.DbDSN != filepath.Join(root, "data", "review.db") || queryErr != nil || marker != "original" {
		t.Fatal("运行数据与配置迁移必须保留已有 SQLite 记录")
	}
}
