package config

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/services/runtimelog"
	log "github.com/sirupsen/logrus"
)

func TestDefaultConfigManagerUsesRuntimeLogAndPreservesLegacyFile(t *testing.T) {
	for _, withSecurity := range []bool{false, true} {
		t.Run(map[bool]string{false: "默认构造", true: "默认安全配置"}[withSecurity], func(t *testing.T) {
			root := t.TempDir()
			writeRuntimeFixture(t, root, validRuntimeConfig())
			legacy, err := NewFileAuditWriter(root)
			if err != nil {
				t.Fatal(err)
			}
			history := []byte("保留旧审计原文，不解析、不导入、不追加\n")
			if err := os.WriteFile(legacy.path, history, 0600); err != nil {
				t.Fatal(err)
			}
			logger := log.StandardLogger()
			oldHooks, oldOutput, oldFormatter, oldLevel, oldCaller := logger.Hooks, logger.Out, logger.Formatter, logger.GetLevel(), logger.ReportCaller
			var output bytes.Buffer
			logger.ReplaceHooks(make(log.LevelHooks))
			logger.SetOutput(&output)
			logger.SetFormatter(&logFormatter{})
			logger.SetLevel(log.InfoLevel)
			logger.SetReportCaller(true)
			t.Cleanup(func() {
				logger.ReplaceHooks(oldHooks)
				logger.SetOutput(oldOutput)
				logger.SetFormatter(oldFormatter)
				logger.SetLevel(oldLevel)
				logger.SetReportCaller(oldCaller)
			})
			writer, err := runtimelog.Start(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = writer.Close() })
			options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
			var manager *ConfigManager
			if withSecurity {
				manager, err = NewConfigManagerWithSecurity(root, options, ConfigSecurity{})
			} else {
				manager, err = NewConfigManager(root, options)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := manager.security.Audit.(RuntimeConfigLogWriter); !ok {
				t.Fatal("默认写入器仍使用独立配置审计")
			}
			payload, err := json.Marshal(manager.Snapshot().Email)
			if err != nil {
				t.Fatal(err)
			}
			updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
				t.Fatal(err)
			}
			if manager.Status().SetupState != SetupStateReady {
				t.Fatal("取消审计影响了配置保存")
			}
			if _, err := manager.AuditQuery(context.Background(), ConfigAuditQuery{}); err != ErrAuditQueryUnavailable {
				t.Fatal("默认管理器仍提供审计查询")
			}
			event := ConfigAuditEvent{Operation: ConfigOperationRollback, ActorID: "1", ActorRole: "super_admin", ActorName: "不应记录的名称", FromGeneration: 2, ToGeneration: 3, Result: "failure", CreatedAt: time.Now(), Changes: []ConfigAuditChange{{Field: "root.database.password", Before: "不应记录的旧值", After: "不应记录的新值"}}}
			if err := (RuntimeConfigLogWriter{}).WriteConfigAudit(context.Background(), event); err != nil {
				t.Fatal(err)
			}
			page, err := writer.Query(context.Background(), runtimelog.Query{Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(page.Lines, "\n")
			if len(page.Lines) != 2 || !strings.Contains(text, "保存配置成功") || !strings.Contains(text, "回滚配置失败") || !strings.Contains(text, "[warning]") {
				t.Fatalf("运行日志缺少配置结果：%s", text)
			}
			if strings.Contains(text, "不应记录") || strings.Contains(text, "password") || strings.Contains(text, "\"changes\"") {
				t.Fatal("运行日志泄露配置详情")
			}
			unchanged, err := os.ReadFile(legacy.path)
			if err != nil || !bytes.Equal(unchanged, history) {
				t.Fatal("旧审计文件被修改")
			}
			files, err := os.ReadDir(filepath.Join(root, "log"))
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if strings.Contains(file.Name(), "audit") {
					t.Fatal("生成了独立审计文件")
				}
			}
		})
	}
}
