package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/internal/passwordtest"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/credentials"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"xorm.io/xorm"
)

func TestRecoveryGenerationHandlersForwardRequestCancellation(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	oldDB, oldSecurity := db.Instance, securityRuntime
	t.Cleanup(func() { db.Instance = oldDB; securityRuntime = oldSecurity; _ = engine.Close() })
	if err := engine.Sync2(&models.User{}, &models.RecoveryCode{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Account: "cancel-user", Role: string(access.RoleUser)}
	if _, err := engine.Insert(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Insert(&models.RecoveryCode{UserID: user.ID, CodeHash: "original-hash", CodeVersion: 1}); err != nil {
		t.Fatal(err)
	}
	service, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	db.Instance = engine
	ConfigureSecurityRuntime(nil, nil, nil, service)
	actor := &appcontext.Context{UserID: user.ID, Role: access.RoleSuperAdmin, RecentAuth: true, RecoveryCodeConfigured: true}
	for _, item := range []struct {
		name    string
		handler func(*appcontext.Context, http.ResponseWriter, *http.Request)
	}{{"self", GenerateRecoveryCodes}, {"managed", GenerateManagedRecoveryCodes}} {
		t.Run(item.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			request := httptest.NewRequest(http.MethodPost, "/api/security", strings.NewReader(`{"user_id":`+strconv.Itoa(user.ID)+`,"count":10}`)).WithContext(ctx)
			recorder := httptest.NewRecorder()
			item.handler(actor, recorder, request)
			var result response.Response
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.ErrorNo == 0 {
				t.Fatal("已取消的请求返回了成功")
			}
			var rows []models.RecoveryCode
			if err := engine.Find(&rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].CodeHash != "original-hash" || rows[0].UsedAt != nil {
				t.Fatal("已取消请求轮换了恢复码")
			}
		})
	}
	// 资源繁忙必须返回可重试状态，不能把它当作凭据错误。
	release := passwordtest.Hold(t)
	defer release()
	request := httptest.NewRequest(http.MethodPost, "/api/security", strings.NewReader(`{"user_id":`+strconv.Itoa(user.ID)+`,"count":10}`))
	recorder := httptest.NewRecorder()
	GenerateManagedRecoveryCodes(actor, recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" || !strings.Contains(recorder.Body.String(), "AUTH_BUSY") {
		t.Fatal("管理恢复码入口没有传递可重试繁忙状态")
	}
}

func TestRecoveryRequestContextPreservesDeadline(t *testing.T) {
	if recoveryRequestContext(nil).Err() != nil {
		t.Fatal("无请求的兼容上下文不可用")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx)
	if recoveryRequestContext(request) != ctx {
		t.Fatal("HTTP 上下文被替换")
	}
}
