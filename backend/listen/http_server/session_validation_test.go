package http_server

import (
	stdcontext "context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		config.SetInitState(oldInit)
		db.Instance = oldDB
		session.Instance = oldSession
	})

	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(testRoot, "session-validation.db")}
	config.SetInitState(true)
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
			called = ctx.UserID == user.ID && ctx.UserAccount == user.Account && ctx.IsAdmin && ctx.RecentAuth
			response.NewSuccessResponse("ok").FPrint(w)
		}))
		request := httptest.NewRequest(http.MethodGet, "/api/user/info", nil).WithContext(loaded)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if !called {
			t.Fatalf("current user session was not accepted: %s", recorder.Body.String())
		}
	})

	t.Run("required security setup blocks business routes", func(t *testing.T) {
		manager, loaded := sessionContext(t, &user)
		session.Instance = manager
		called := false
		handler := contextIterceptor(controllers.HandlerFunc(func(_ *appcontext.Context, w http.ResponseWriter, _ *http.Request) {
			called = true
			response.NewSuccessResponse(nil).FPrint(w)
		}))
		for _, path := range []string{"/api/config/status", "/api/v1/config/status", "/api/v1/user/list"} {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil).WithContext(loaded))
			if called || !strings.Contains(recorder.Body.String(), "SECURITY_SETUP_REQUIRED") {
				t.Fatalf("business route escaped gate: %s", recorder.Body.String())
			}
		}
		for _, path := range []string{"/api/v1/security/totp", "/api/v1/security/external-email/list", "/api/v1/security/credentials/recovery-codes"} {
			called = false
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil).WithContext(loaded))
			if !called {
				t.Fatalf("setup route blocked: %s", path)
			}
		}
		code := models.RecoveryCode{UserID: user.ID, CodeHash: "fixture"}
		if _, err := db.Instance.Insert(&code); err != nil {
			t.Fatal(err)
		}
		called = false
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/config/status", nil).WithContext(loaded))
		if !called {
			t.Fatal("configured account blocked")
		}
		now := time.Now()
		if _, err := db.Instance.ID(code.ID).Cols("used_at").Update(&models.RecoveryCode{UsedAt: &now}); err != nil {
			t.Fatal(err)
		}
		called = false
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/config/status", nil).WithContext(loaded))
		if called {
			t.Fatal("revoked last method did not restore gate")
		}
	})

	t.Run("refresh does not elevate or renew authentication", func(t *testing.T) {
		for _, authenticatedAt := range []int64{0, time.Now().Add(-11 * time.Minute).Unix(), time.Now().Add(time.Hour).Unix()} {
			manager, loaded := sessionContext(t, &user)
			session.Instance = manager
			identity, err := session.GetUser(loaded)
			if err != nil {
				t.Fatal(err)
			}
			identity.AuthenticatedAt = authenticatedAt
			encoded, err := json.Marshal(identity)
			if err != nil {
				t.Fatal(err)
			}
			manager.Put(loaded, "user", string(encoded))
			called := false
			handler := contextIterceptor(controllers.HandlerFunc(func(ctx *appcontext.Context, w http.ResponseWriter, r *http.Request) {
				called = true
				if ctx.UserID != user.ID || ctx.RecentAuth {
					t.Fatalf("refresh changed authentication authority: user=%d recent=%v", ctx.UserID, ctx.RecentAuth)
				}
			}))
			for range 2 {
				handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/user/info", nil).WithContext(loaded))
			}
			stored, err := session.GetUser(loaded)
			if err != nil || !called || stored.AuthenticatedAt != authenticatedAt {
				t.Fatalf("refresh renewed authentication: called=%v identity=%+v err=%v", called, stored, err)
			}
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
