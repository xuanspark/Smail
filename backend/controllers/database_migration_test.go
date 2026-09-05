package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/dbmigration"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

// 这里只验证控制器契约；真实数据复制由迁移服务集成测试覆盖。
type migrationContractRunner struct{}

func (migrationContractRunner) Run(context.Context, dbmigration.Plan, func(dbmigration.Status) error) error {
	return nil
}

func TestDatabaseMigrationPreviewContract(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"domain":"mail.example.com","webDomain":"mail.example.com","dbType":"sqlite","dbDSN":"./config/source.db","isInit":true,"httpPort":80,"httpsPort":443,"httpsEnabled":1}`)
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
	previousRoot, previousManager, previousService := config.ROOT_PATH, config.CurrentManager(), dbmigration.Current()
	config.ROOT_PATH = root
	config.SetRuntimeManager(manager)
	dbmigration.SetCurrent(dbmigration.NewService(migrationContractRunner{}))
	t.Cleanup(func() {
		config.ROOT_PATH = previousRoot
		config.SetRuntimeManager(previousManager)
		dbmigration.SetCurrent(previousService)
	})
	admin := &appcontext.Context{UserID: 1, Role: access.RoleSuperAdmin, RecentAuth: true}
	for _, test := range []struct {
		name, body string
		ctx        *appcontext.Context
		code       int
	}{
		{"sqlite", `{"target":{"type":"sqlite","localRef":"next.db"},"expectedGeneration":1}`, admin, 200},
		{"mysql", `{"target":{"type":"mysql","host":"localhost","username":"review","name":"next","password":{"plaintext":"private-test-value"}},"expectedGeneration":1}`, admin, 200},
		{"postgresql", `{"target":{"type":"postgresql","host":"localhost","username":"review","name":"next","password":{"plaintext":"private-test-value"}},"expectedGeneration":1}`, admin, 200},
		{"legacy", `{"targetName":"next.db","expectedGeneration":1}`, admin, 200},
		{"both targets", `{"targetName":"next.db","target":{"type":"sqlite","localRef":"next.db"},"expectedGeneration":1}`, admin, 422},
		{"raw dsn", `{"target":{"type":"mysql","dsn":"forbidden"},"expectedGeneration":1}`, admin, 422},
		{"unsafe path", `{"target":{"type":"sqlite","localRef":"../next.db"},"expectedGeneration":1}`, admin, 422},
		{"sqlite password", `{"target":{"type":"sqlite","localRef":"next.db","password":{"plaintext":"private-test-value"}},"expectedGeneration":1}`, admin, 422},
		{"missing password", `{"target":{"type":"mysql","host":"localhost","username":"review","name":"next"},"expectedGeneration":1}`, admin, 422},
		{"generation conflict", `{"targetName":"next.db","expectedGeneration":9}`, admin, 409},
		{"no recent auth", `{"targetName":"next.db","expectedGeneration":1}`, &appcontext.Context{UserID: 1, Role: access.RoleSuperAdmin}, 403},
		{"ordinary admin", `{"targetName":"next.db","expectedGeneration":1}`, &appcontext.Context{UserID: 2, Role: access.RoleAdmin, RecentAuth: true}, 403},
		{"ordinary user", `{"targetName":"next.db","expectedGeneration":1}`, &appcontext.Context{UserID: 3, Role: access.RoleUser, RecentAuth: true}, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			PreviewDatabaseMigration(test.ctx, recorder, httptest.NewRequest(http.MethodPost, "/api/v1/database/migrations/preview", strings.NewReader(test.body)))
			if recorder.Code != test.code {
				t.Fatalf("unexpected status: %d", recorder.Code)
			}
			if strings.Contains(recorder.Body.String(), "private-test-value") || strings.Contains(recorder.Body.String(), "ciphertext") {
				t.Fatal("migration response disclosed secret")
			}
			if test.code == 200 {
				var payload struct {
					ErrorNo int `json:"errorNo"`
					Data    struct {
						Migration dbmigration.Record `json:"migration"`
						Token     string             `json:"token"`
					} `json:"data"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || payload.ErrorNo != 0 || payload.Data.Token == "" || payload.Data.Migration.Status != dbmigration.StatusPreviewed {
					t.Fatal("invalid preview response")
				}
			}
		})
	}
}
