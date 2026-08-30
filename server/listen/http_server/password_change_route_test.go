package http_server

import (
	stdcontext "context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/utils/password"
	"github.com/alexedwards/scs/v2"
)

func TestModifyPasswordRouteRejectsUnauthenticatedRequest(t *testing.T) {
	oldConfig := config.Instance
	oldInit := config.IsInit
	oldSession := session.Instance
	t.Cleanup(func() {
		config.Instance = oldConfig
		config.IsInit = oldInit
		session.Instance = oldSession
	})

	config.Instance = &config.Config{}
	config.IsInit = true
	session.Instance = scs.New()

	mux := http.NewServeMux()
	if err := router(mux); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/settings/modify_password", strings.NewReader(`{"current_password":"CurrentP12","password":"NewPass123"}`))
	loaded, err := session.Instance.Load(request.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithContext(loaded)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	var result response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ErrorNo != response.NeedLogin {
		t.Fatalf("unauthenticated password change was not rejected: %+v", result)
	}
}

func TestSuperAdminCannotChangeOwnPasswordAfterLogin(t *testing.T) {
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

	config.Instance = &config.Config{
		DbType: config.DBTypeSQLite,
		DbDSN:  filepath.Join(testRoot, "super-admin-password.db"),
	}
	config.IsInit = true
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}

	superAdmin := models.User{Account: "super-admin", Name: "root", IsAdmin: 1, SessionVersion: 1}
	if err := auth.SetPassword(&superAdmin, "CurrentP12"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&superAdmin); err != nil {
		t.Fatal(err)
	}

	manager := scs.New()
	session.Instance = manager
	currentSession := passwordChangeSession(t, manager, &superAdmin)

	mux := http.NewServeMux()
	if err := router(mux); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/settings/modify_password", strings.NewReader(`{"current_password":"CurrentP12","password":"NewPass123"}`)).WithContext(currentSession)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	var result response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ErrorNo != response.NoAccessPrivileges {
		t.Fatalf("super admin web password change was not rejected: %s", recorder.Body.String())
	}

	var unchanged models.User
	found, err := db.Instance.ID(superAdmin.ID).Get(&unchanged)
	if err != nil {
		t.Fatal(err)
	}
	if !found || !password.Verify(unchanged.Password, "CurrentP12") {
		t.Fatal("super admin password changed through the web route")
	}
	if password.Verify(unchanged.Password, "NewPass123") {
		t.Fatal("super admin new password was accepted through the web route")
	}
}
func passwordChangeSession(t *testing.T, manager *scs.SessionManager, user *models.User) stdcontext.Context {
	t.Helper()
	loaded, err := manager.Load(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := session.PutUser(loaded, user); err != nil {
		t.Fatal(err)
	}
	return loaded
}
