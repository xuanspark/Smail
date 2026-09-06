package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/services/access"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func TestAuditConfigStatusIncludesTLSMode(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"domain":"mail.example.test","webDomain":"mail.example.test","dbType":"sqlite","dbDSN":"./config/test.db","isInit":true,"httpPort":8080,"httpsEnabled":2,"tlsMode":"proxy","trustedProxyCidrs":["127.0.0.1/32"]}`)
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
	for _, role := range []access.Role{access.RoleSuperAdmin, access.RoleUser} {
		recorder := httptest.NewRecorder()
		ConfigStatus(&appcontext.Context{UserID: 1, Role: role}, recorder, httptest.NewRequest(http.MethodGet, "/api/v1/config/status", nil))
		if role == access.RoleUser {
			if recorder.Code != http.StatusForbidden {
				t.Fatal("增加证书状态字段不能放宽配置读取权限")
			}
			continue
		}
		var payload struct {
			ErrorNo int `json:"errorNo"`
			Data    struct {
				TLSMode string `json:"tlsMode"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusOK || payload.ErrorNo != 0 || payload.Data.TLSMode != config.HTTPSTLSModeProxy {
			t.Fatalf("状态响应缺少生效反代模式: status=%d error=%d mode=%q", recorder.Code, payload.ErrorNo, payload.Data.TLSMode)
		}
	}
}
