package controllers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/session"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"github.com/alexedwards/scs/v2"
)

func TestCompletedLoginSetsAuditActor(t *testing.T) {
	previousConfig, previousSession := config.Instance, session.Instance
	t.Cleanup(func() { config.Instance, session.Instance = previousConfig, previousSession })
	config.Instance = &config.Config{Domain: "example.test"}
	session.Instance = scs.New()
	loaded, err := session.Instance.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &appcontext.Context{Context: loaded}
	request := httptest.NewRequest("POST", "https://example.test/api/security/login/verify", nil).WithContext(loaded)
	writer := httptest.NewRecorder()
	completeLoginSession(ctx, writer, request, &models.User{ID: 42, Account: "audit-probe", Role: string(access.RoleSuperAdmin)})
	var result response.Response
	if err := json.Unmarshal(writer.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ErrorNo != 0 || ctx.UserID != 42 || ctx.UserAccount != "audit-probe" || ctx.Role != access.RoleSuperAdmin {
		t.Fatalf("completed login lost audit actor: code=%d id=%d role=%s", result.ErrorNo, ctx.UserID, ctx.Role)
	}
}
