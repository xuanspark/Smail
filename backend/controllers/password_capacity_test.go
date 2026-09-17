package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/internal/passwordtest"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/session"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/password"
	"github.com/alexedwards/scs/v2"
)

func TestPasswordBusyHTTPDoesNotLockAccounts(t *testing.T) {
	oldConfig, oldDB, oldSession := config.Instance, db.Instance, session.Instance
	oldPair, oldAccount, oldIP := loginPairAttempts, loginAccountAttempts, loginIPAttempts
	oldReauthUser, oldReauthIP := reauthUserAttempts, reauthIPAttempts
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "capacity.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Instance.Close()
		config.Instance, db.Instance, session.Instance = oldConfig, oldDB, oldSession
		loginPairAttempts, loginAccountAttempts, loginIPAttempts = oldPair, oldAccount, oldIP
		reauthUserAttempts, reauthIPAttempts = oldReauthUser, oldReauthIP
	})
	loginPairAttempts, loginAccountAttempts, loginIPAttempts = newLoginLimiter(1), newLoginLimiter(1), newLoginLimiter(1)
	reauthUserAttempts, reauthIPAttempts = newLoginLimiter(1), newLoginLimiter(1)
	session.Instance = scs.New()
	user := models.User{Account: "capacity-user", SessionVersion: 1}
	if err := auth.SetPassword(&user, "CorrectP12"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}
	release := passwordtest.Hold(t)
	cases := []struct {
		handler func(*appcontext.Context, http.ResponseWriter, *http.Request)
		body    string
	}{
		{Login, `{"account":"capacity-user","password":"CorrectP12"}`},
		{Login, `{"account":"missing-user","password":"CorrectP12"}`},
		{ReauthenticateCurrentUser, `{"password":"CorrectP12"}`},
		{ModifyPassword, `{"current_password":"CorrectP12","password":"NewPass123"}`},
	}
	for index := 0; index < 12; index++ {
		for _, item := range cases {
			request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/test", strings.NewReader(item.body))
			request.RemoteAddr = "192.0.2.71:1234"
			recorder := httptest.NewRecorder()
			ctx := &appcontext.Context{Context: request.Context(), UserID: user.ID, UserAccount: user.Account}
			item.handler(ctx, recorder, request)
			if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" || !strings.Contains(recorder.Body.String(), "AUTH_BUSY") {
				t.Fatalf("认证繁忙响应不正确：%d %s", recorder.Code, recorder.Body.String())
			}
		}
	}
	for _, limiter := range []*loginLimiter{loginPairAttempts, loginAccountAttempts, loginIPAttempts, reauthUserAttempts, reauthIPAttempts} {
		if len(limiter.attempts) != 0 {
			t.Fatal("认证繁忙增加了登录失败计数")
		}
	}
	release()
	if _, err := auth.Authenticate(user.Account, "CorrectP12"); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordBusyResponsePreservesOtherErrors(t *testing.T) {
	if writePasswordBusy(httptest.NewRecorder(), fmt.Errorf("wrapped: %w", password.ErrInvalidLength)) {
		t.Fatal("普通错误被错误标记为资源繁忙")
	}
	if !writePasswordBusy(httptest.NewRecorder(), fmt.Errorf("wrapped: %w", password.ErrBusy)) {
		t.Fatal("包装后的繁忙错误未被识别")
	}
}

func TestPublicRecoveryBusyHTTPAllowsRetry(t *testing.T) {
	enablePublicRecoveryForTest(t)
	recoveryService, _, _, user, _ := newPublicSecurityFixture(t)
	var token string
	recoveryService.SetPublicLinkDelivery(func(_ string, issued string) error { token = issued; return nil })
	if err := recoveryService.BeginPublicLink(user.Account); err != nil {
		t.Fatal(err)
	}
	previous := currentSecurityRuntime()
	ConfigureSecurityRuntime(previous.verification, previous.external, recoveryService, previous.credentials)
	t.Cleanup(func() {
		ConfigureSecurityRuntime(previous.verification, previous.external, previous.recovery, previous.credentials)
	})
	body, err := json.Marshal(map[string]string{"token": token, "new_password": "NewPass123"})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "https://example.com/rest/api/recovery/complete", strings.NewReader(string(body)))
		recorder := httptest.NewRecorder()
		CompletePublicLinkPasswordRecovery(recorder, request)
		return recorder
	}
	release := passwordtest.Hold(t)
	for index := 0; index < 12; index++ {
		recorder := invoke()
		var result map[string]bool
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || len(result) != 1 || result["reset"] {
			t.Fatal("匿名恢复繁忙响应泄露额外状态或错误报告成功")
		}
		if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("匿名恢复繁忙响应不可重试：%d", recorder.Code)
		}
	}
	release()
	if recorder := invoke(); recorder.Code != http.StatusOK {
		t.Fatalf("繁忙结束后链接无法重试：%d", recorder.Code)
	}
	if recorder := invoke(); recorder.Code != http.StatusBadRequest {
		t.Fatalf("恢复链接可被重复使用：%d", recorder.Code)
	}
}
