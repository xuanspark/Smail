package controllers

import (
	"encoding/json"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/password"
	"github.com/alexedwards/scs/v2"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestModifyPasswordRequiresCurrentPassword(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	oldLoginPairAttempts := loginPairAttempts
	oldLoginAccountAttempts := loginAccountAttempts
	oldLoginIPAttempts := loginIPAttempts
	oldSession := session.Instance
	loginPairAttempts = newLoginLimiter(loginFailureLimit)
	loginAccountAttempts = newLoginLimiter(loginAccountFailureLimit)
	loginIPAttempts = newLoginLimiter(loginIPFailureLimit)
	session.Instance = scs.New()
	testRoot := t.TempDir()
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		config.Instance = oldConfig
		db.Instance = oldDB
		loginPairAttempts = oldLoginPairAttempts
		loginAccountAttempts = oldLoginAccountAttempts
		loginIPAttempts = oldLoginIPAttempts
		session.Instance = oldSession
	})

	config.Instance = &config.Config{
		DbType: config.DBTypeSQLite,
		DbDSN:  filepath.Join(testRoot, "settings.db"),
	}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}

	user := models.User{Account: "password-change-user", Name: "密码修改用户"}
	if err := auth.SetPassword(&user, "CorrectPass123"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}
	originalHash := user.Password
	ctx := &context.Context{UserID: user.ID, UserAccount: user.Account, Lang: "zhCn"}

	request := httptest.NewRequest(http.MethodPost, "/api/settings/modify_password", strings.NewReader(`{"current_password":"WrongPass123","password":"NewPass123"}`))
	recorder := httptest.NewRecorder()
	ModifyPassword(ctx, recorder, request)
	var failed struct {
		ErrorNo int `json:"errorNo"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &failed); err != nil {
		t.Fatal(err)
	}
	if failed.ErrorNo == 0 {
		t.Fatal("password change succeeded with an incorrect current password")
	}

	var unchanged models.User
	unchanged = models.User{}
	if _, err := db.Instance.ID(user.ID).Get(&unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged.Password != originalHash || !password.Verify(unchanged.Password, "CorrectPass123") {
		t.Fatal("password was changed after current password verification failed")
	}

	request = httptest.NewRequest(http.MethodPost, "/api/settings/modify_password", strings.NewReader(`{"current_password":"CorrectPass123","password":"NewPass123"}`))
	loadedContext, err := session.Instance.Load(request.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithContext(loadedContext)
	recorder = httptest.NewRecorder()
	ModifyPassword(ctx, recorder, request)
	var succeeded struct {
		ErrorNo int `json:"errorNo"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &succeeded); err != nil {
		t.Fatal(err)
	}
	if succeeded.ErrorNo != 0 {
		t.Fatalf("password change with the correct current password failed: %s", recorder.Body.String())
	}
	unchanged = models.User{}
	if _, err := db.Instance.ID(user.ID).Get(&unchanged); err != nil {
		t.Fatal(err)
	}
	if !password.Verify(unchanged.Password, "NewPass123") {
		t.Fatal("password change did not persist the new password")
	}
	identity, err := session.GetUser(request.Context())
	if err != nil {
		t.Fatal(err)
	}
	if identity.ID != user.ID || identity.SessionVersion != unchanged.SessionVersion {
		t.Fatalf("current session was not refreshed after password change: %+v", identity)
	}
}
