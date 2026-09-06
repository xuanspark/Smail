package res_init

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func TestWebAuthnStartsWithMigratedSiteDefaults(t *testing.T) {
	root := t.TempDir()
	previousRoot, previousDevelopment, previousDB := config.ROOT_PATH, config.DevelopmentMode, db.Instance
	previousManager, previousService := config.CurrentManager(), webAuthnRuntimeService
	config.ROOT_PATH, config.DevelopmentMode = root, false
	t.Cleanup(func() {
		stopConfigManager()
		config.SetRuntimeManager(previousManager)
		config.ROOT_PATH, config.DevelopmentMode, db.Instance = previousRoot, previousDevelopment, previousDB
		webAuthnRuntimeService = previousService
		controllers.ConfigureWebAuthnRuntime(previousService)
	})
	if err := os.MkdirAll(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"domain": "example.com", "webDomain": "mail.example.com", "dbType": "sqlite",
		"dbDSN": "./config/existing.db", "isInit": true, "httpsEnabled": 1, "httpsPort": 443,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.MigrateLegacyConfig(root, config.RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err != nil {
		t.Fatal(err)
	}
	if err := startConfigManager(); err != nil {
		t.Fatal(err)
	}
	engine, err := xorm.NewEngine("sqlite", filepath.Join(root, "config", "existing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	db.Instance = engine
	if err := engine.Sync2(&models.User{}, &models.WebAuthnCredential{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Account: "test", Name: "测试用户", Password: "unused", Role: "user"}
	if _, err := engine.Insert(&user); err != nil {
		t.Fatal(err)
	}
	if err := startWebAuthnRuntime(); err != nil {
		t.Fatal(err)
	}
	if webAuthnRuntimeService == nil {
		t.Fatal("未设置旧全局开关时安全密钥服务仍应可用")
	}
	options, _, err := webAuthnRuntimeService.BeginRegistration(user.ID)
	if err != nil || options.Response.RelyingParty.ID != "mail.example.com" {
		t.Fatalf("安全密钥注册未使用受控站点标识：%v", err)
	}
}
