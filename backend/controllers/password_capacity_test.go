package controllers

import (
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
