package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/services/access"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func TestConfigBackupReadDoesNotRequireRecentAuth(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"domain":"mail.example.com","webDomain":"mail.example.com","dbType":"sqlite","dbDSN":"./config/test.db","isInit":true,"httpPort":80,"httpsPort":443,"httpsEnabled":1}`)
	if err := os.WriteFile(filepath.Join(root, "config", "config.json"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	validation := config.RuntimeValidationOptions{HasSuperAdmin: true, Production: true}
	if _, err := config.MigrateLegacyConfig(root, validation); err != nil {
		t.Fatal(err)
	}
	manager, err := config.NewConfigManager(root, config.ConfigLoaderOptions{Validation: validation})
	if err != nil {
		t.Fatal(err)
	}
	previous := config.CurrentManager()
	config.SetRuntimeManager(manager)
	t.Cleanup(func() { config.SetRuntimeManager(previous) })

	for _, role := range []access.Role{access.RoleSuperAdmin, access.RoleAdmin, access.RoleUser} {
		t.Run(string(role), func(t *testing.T) {
			ctx := &appcontext.Context{UserID: 1, Role: role, RecentAuth: false}
			recorder := httptest.NewRecorder()
			ListConfigBackups(ctx, recorder, httptest.NewRequest(http.MethodGet, "/api/v1/config/backups", nil))
			if role != access.RoleSuperAdmin {
				if recorder.Code != http.StatusForbidden {
					t.Fatalf("非超级管理员不应读取配置备份：%d", recorder.Code)
				}
				return
			}
			var payload struct {
				ErrorNo int `json:"errorNo"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusOK || payload.ErrorNo != 0 {
				t.Fatalf("超级管理员读取脱敏备份不应要求近期认证：%d", recorder.Code)
			}
		})
	}

	// 列表读取放宽不能改变实际回滚链路的近期认证要求。
	for name, handler := range map[string]func(*appcontext.Context, http.ResponseWriter, *http.Request){
		"preview": PreviewConfigRollback,
		"ticket":  IssueConfigRollbackTicket,
		"execute": RollbackConfig,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler(&appcontext.Context{UserID: 1, Role: access.RoleSuperAdmin}, recorder,
				httptest.NewRequest(http.MethodPost, "/api/v1/config/rollback", strings.NewReader(`{"targetGeneration":1}`)))
			if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "RECENT_AUTH_REQUIRED") {
				t.Fatalf("回滚仍应要求近期认证：%d", recorder.Code)
			}
		})
	}
}
