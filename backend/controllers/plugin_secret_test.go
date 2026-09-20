package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func callPluginSecret(ctx *appcontext.Context, method, pluginID, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	PluginSecret(ctx, recorder, httptest.NewRequest(method, "/api/v1/plugin/secrets/"+pluginID, strings.NewReader(body)))
	return recorder
}

func TestPluginSecretAuthorization(t *testing.T) {
	service, engine := newTelegramControllerConfigService(t)
	previous := hooks.PluginConfigService()
	hooks.SetPluginConfigService(service)
	t.Cleanup(func() { hooks.SetPluginConfigService(previous) })
	for _, tc := range []struct {
		name string
		ctx  *appcontext.Context
		code string
	}{
		{"anonymous", nil, "ACCESS_DENIED"},
		{"no session user", &appcontext.Context{Role: access.RoleSuperAdmin, RecentAuth: true, RecoveryCodeConfigured: true}, "ACCESS_DENIED"},
		{"ordinary user", &appcontext.Context{UserID: 3, Role: access.RoleUser, RecentAuth: true, RecoveryCodeConfigured: true}, "ACCESS_DENIED"},
		{"missing recent authentication", &appcontext.Context{UserID: 2, Role: access.RoleAdmin, RecoveryCodeConfigured: true}, "RECENT_AUTH_REQUIRED"},
		{"missing recovery code", &appcontext.Context{UserID: 2, Role: access.RoleAdmin, RecentAuth: true}, "RECOVERY_CODE_REQUIRED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := callPluginSecret(tc.ctx, http.MethodPut, "telegram_push", `{"version":0,"change":{"operation":"replace","value":{"plaintext":"test-only-secret"}}}`)
			if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), tc.code) {
				t.Fatalf("status=%d response=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	count, err := engine.Count(new(models.PluginSecret))
	if err != nil || count != 0 {
		t.Fatalf("denied writes changed secrets: count=%d err=%v", count, err)
	}
	for _, role := range []access.Role{access.RoleAdmin, access.RoleSuperAdmin} {
		recorder := callPluginSecret(&appcontext.Context{UserID: 2, Role: role}, http.MethodGet, "telegram_push", "")
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"configured":false`) {
			t.Fatalf("read role=%s status=%d body=%s", role, recorder.Code, recorder.Body.String())
		}
	}
	recorder := callPluginSecret(&appcontext.Context{UserID: 3, Role: access.RoleUser}, http.MethodGet, "telegram_push", "")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("ordinary user read status=%d", recorder.Code)
	}
}

func TestPluginSecretEncryptedLifecycleAndValidation(t *testing.T) {
	service, engine := newTelegramControllerConfigService(t)
	previous := hooks.PluginConfigService()
	hooks.SetPluginConfigService(service)
	t.Cleanup(func() { hooks.SetPluginConfigService(previous) })
	ctx := &appcontext.Context{UserID: 2, Role: access.RoleAdmin, RecentAuth: true, RecoveryCodeConfigured: true}
	for _, id := range []string{"telegram_push", "wechat_push"} {
		t.Run(id, func(t *testing.T) {
			for _, body := range []string{
				`{"version":0,"change":{"operation":"replace","value":{"plaintext":"test-only-secret"}},"userId":99}`,
				`{"version":0,"change":{"operation":"replace","value":{"plaintext":"test-only-secret","extra":true}}}`,
				`{"version":0,"change":{"operation":"replace","value":{"plaintext":"test-only-secret"}}} {}`,
				`{"version":0,"change":{"operation":"replace","value":{"plaintext":""}}}`,
				`{"version":0,"change":{"operation":"replace","value":{"plaintext":"` + strings.Repeat("x", 17000) + `"}}}`,
			} {
				recorder := callPluginSecret(ctx, http.MethodPut, id, body)
				if strings.Contains(recorder.Body.String(), `"errorNo":0`) {
					t.Fatal("invalid request accepted")
				}
			}
			body := `{"version":0,"change":{"operation":"replace","value":{"plaintext":"test-only-secret"}}}`
			recorder := callPluginSecret(ctx, http.MethodPut, id, body)
			if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"version":1`) {
				t.Fatalf("save status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if recorder.Header().Get("Cache-Control") != "no-store" || strings.Contains(recorder.Body.String(), "test-only-secret") {
				t.Fatal("unsafe secret response")
			}
			record := models.PluginSecret{}
			found, err := engine.Where("plugin_id = ?", id).Get(&record)
			if err != nil || !found || record.Ciphertext == "" || record.Ciphertext == "test-only-secret" || record.KeyVersion == 0 {
				t.Fatalf("secret was not encrypted: found=%v err=%v", found, err)
			}
			recorder = callPluginSecret(ctx, http.MethodGet, id, "")
			var payload struct {
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || len(payload.Data) != 4 || payload.Data["configured"] != true || payload.Data["version"] != float64(1) {
				t.Fatalf("unexpected public status: %s", recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), record.Ciphertext) || strings.Contains(recorder.Body.String(), "test-only-secret") {
				t.Fatal("secret leaked in status")
			}
			recorder = callPluginSecret(ctx, http.MethodPut, id, body)
			if recorder.Code != http.StatusConflict {
				t.Fatalf("stale version accepted: %s", recorder.Body.String())
			}
			recorder = callPluginSecret(ctx, http.MethodPut, id, `{"version":1,"change":{"operation":"clear"}}`)
			if strings.Contains(recorder.Body.String(), `"errorNo":0`) {
				t.Fatal("unconfirmed clear accepted")
			}
			recorder = callPluginSecret(ctx, http.MethodPut, id, `{"version":1,"change":{"operation":"clear","confirmed":true}}`)
			if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"configured":false`) {
				t.Fatalf("clear failed: %s", recorder.Body.String())
			}
		})
	}
	if recorder := callPluginSecret(ctx, http.MethodDelete, "telegram_push", ""); recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unsupported method status=%d", recorder.Code)
	}
	if recorder := callPluginSecret(ctx, http.MethodPut, "unknown", `{}`); recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown plugin status=%d", recorder.Code)
	}
}
