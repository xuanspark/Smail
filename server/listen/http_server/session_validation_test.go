package http_server

import (
	stdcontext "context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/session"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"github.com/alexedwards/scs/v2"
)

func TestContextInterceptorValidatesCurrentUserState(t *testing.T) {
	oldConfig := config.Instance
	oldInit := config.IsInit
	oldDB := db.Instance
	oldSession := session.Instance
	testRoot := t.TempDir()
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		config.Instance = oldConfig
		config.IsInit = oldInit
		db.Instance = oldDB
		session.Instance = oldSession
	})

	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(testRoot, "session-validation.db")}
	config.IsInit = true
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	user := models.User{Account: "session-admin", Name: "Admin", IsAdmin: 1, SessionVersion: 1}
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}

	t.Run("current identity", func(t *testing.T) {
		manager, loaded := sessionContext(t, &user)
		session.Instance = manager
		called := false
		handler := contextIterceptor(controllers.HandlerFunc(func(ctx *appcontext.Context, w http.ResponseWriter, r *http.Request) {
			called = ctx.UserID == user.ID && ctx.UserAccount == user.Account && ctx.IsAdmin
			response.NewSuccessResponse("ok").FPrint(w)
		}))
		request := httptest.NewRequest(http.MethodGet, "/api/user/info", nil).WithContext(loaded)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if !called {
			t.Fatalf("current user session was not accepted: %s", recorder.Body.String())
		}
	})

	t.Run("disabled identity", func(t *testing.T) {
		manager, loaded := sessionContext(t, &user)
		session.Instance = manager
		if _, err := db.Instance.ID(user.ID).Cols("disabled").Update(&models.User{Disabled: 1}); err != nil {
			t.Fatal(err)
		}
		assertSessionRejected(t, loaded)
		if _, err := db.Instance.ID(user.ID).Cols("disabled").Update(&models.User{Disabled: 0}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("stale version", func(t *testing.T) {
		manager, loaded := sessionContext(t, &user)
		session.Instance = manager
		if _, err := db.Instance.ID(user.ID).Cols("session_version").Update(&models.User{SessionVersion: 2}); err != nil {
			t.Fatal(err)
		}
		assertSessionRejected(t, loaded)
	})
}

func sessionContext(t *testing.T, user *models.User) (*scs.SessionManager, stdcontext.Context) {
	t.Helper()
	manager := scs.New()
	loaded, err := manager.Load(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	session.Instance = manager
	if err := session.PutUser(loaded, user); err != nil {
		t.Fatal(err)
	}
	return manager, loaded
}

func assertSessionRejected(t *testing.T, loaded stdcontext.Context) {
	t.Helper()
	called := false
	handler := contextIterceptor(controllers.HandlerFunc(func(ctx *appcontext.Context, w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/user/info", nil).WithContext(loaded)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var result response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if called || result.ErrorNo != response.NeedLogin {
		t.Fatalf("revoked session was accepted: called=%v result=%+v", called, result)
	}
}
