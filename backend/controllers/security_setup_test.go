package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/session"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/csrf"
	"github.com/alexedwards/scs/v2"
	"xorm.io/xorm"
)

func TestLogoutDestroysSessionAndCSRFCookies(t *testing.T) {
	previous := session.Instance
	session.Instance = scs.New()
	t.Cleanup(func() { session.Instance = previous })
	request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/v1/logout", nil)
	loaded, err := session.Instance.Load(request.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	session.Instance.Put(loaded, csrf.SessionKey, "test-csrf")
	token, _, err := session.Instance.Commit(loaded)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: session.Instance.Cookie.Name, Value: token})
	recorder := httptest.NewRecorder()
	session.Instance.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Logout(&appcontext.Context{Context: r.Context(), UserID: 1}, w, r)
	})).ServeHTTP(recorder, request)
	removed := map[string]bool{}
	for _, cookie := range recorder.Result().Cookies() {
		removed[cookie.Name] = cookie.MaxAge < 0 && cookie.Value == ""
	}
	if !removed[session.Instance.Cookie.Name] || !removed[csrf.CookieName] {
		t.Fatal("logout must expire both session and CSRF cookies")
	}
	_, exists, err := session.Instance.Store.Find(token)
	if err != nil || exists {
		t.Fatal("logout left a valid stored session")
	}
}

func TestSecuritySetupAcknowledgementIsPersistentAndAccountScoped(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "onboarding.db"))
	if err != nil {
		t.Fatal(err)
	}
	previous := db.Instance
	db.Instance = engine
	t.Cleanup(func() { db.Instance = previous; _ = engine.Close() })
	if err := engine.Sync2(&models.User{}); err != nil {
		t.Fatal(err)
	}
	first := models.User{Account: "root", Name: "Root", Role: string(access.RoleSuperAdmin)}
	second := models.User{Account: "alice", Name: "Alice", Role: string(access.RoleUser)}
	if _, err := engine.Insert(&first, &second); err != nil {
		t.Fatal(err)
	}
	if loginSecurityFields(&first)["showSecuritySetupPrompt"] != true {
		t.Fatal("first super administrator login must show security setup")
	}
	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		AcknowledgeSecuritySetup(&appcontext.Context{UserID: first.ID}, recorder,
			httptest.NewRequest(http.MethodPost, "/api/v1/security/setup/acknowledge", strings.NewReader(`{"user_id":2}`)))
		var payload struct {
			ErrorNo int `json:"errorNo"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || payload.ErrorNo != 0 {
			t.Fatalf("acknowledgement failed: %s", recorder.Body.String())
		}
	}
	var loaded, untouched models.User
	if _, err := engine.ID(first.ID).Get(&loaded); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ID(second.ID).Get(&untouched); err != nil {
		t.Fatal(err)
	}
	if loaded.SecuritySetupAcknowledged != 1 || untouched.SecuritySetupAcknowledged != 0 {
		t.Fatal("acknowledgement changed the wrong account")
	}
	fields := loginSecurityFields(&loaded)
	if fields["showSecuritySetupPrompt"] != false || fields["showRecoverySetupPrompt"] != false {
		t.Fatal("acknowledged super administrator must not receive either prompt")
	}
	if untouched.Password != second.Password || loaded.SecurityVersion != first.SecurityVersion {
		t.Fatal("acknowledgement changed security credentials")
	}
}

func TestSecuritySetupRejectsAnonymous(t *testing.T) {
	recorder := httptest.NewRecorder()
	AcknowledgeSecuritySetup(nil, recorder, httptest.NewRequest(http.MethodPost, "/api/v1/security/setup/acknowledge", nil))
	var payload struct {
		ErrorNo int `json:"errorNo"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || payload.ErrorNo == 0 {
		t.Fatal("anonymous acknowledgement was accepted")
	}
}

func TestLegacyConfigStatusReportsUnavailable(t *testing.T) {
	previous := config.CurrentManager()
	config.SetRuntimeManager(nil)
	t.Cleanup(func() { config.SetRuntimeManager(previous) })
	recorder := httptest.NewRecorder()
	ConfigStatus(&appcontext.Context{UserID: 1, Role: access.RoleSuperAdmin}, recorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/config/status", nil))
	var payload struct {
		ErrorNo int `json:"errorNo"`
		Data    struct {
			Available bool   `json:"available"`
			Reason    string `json:"unavailableReason"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || payload.ErrorNo != 0 || payload.Data.Available || payload.Data.Reason == "" {
		t.Fatalf("unexpected config status: %s", recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	if _, ok := configManagerOrError(recorder); ok || recorder.Code != http.StatusServiceUnavailable {
		t.Fatal("unavailable config operation must return HTTP 503")
	}
}
