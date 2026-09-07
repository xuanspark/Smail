package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/session"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"github.com/alexedwards/scs/v2"
)

func TestReauthenticateCurrentUser(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	oldSession := session.Instance
	oldUserAttempts := reauthUserAttempts
	oldIPAttempts := reauthIPAttempts
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		config.Instance = oldConfig
		db.Instance = oldDB
		session.Instance = oldSession
		reauthUserAttempts = oldUserAttempts
		reauthIPAttempts = oldIPAttempts
	})

	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "reauth.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	user := models.User{Account: "reauth-user", Name: "续验用户", SessionVersion: 1, SecurityVersion: 1}
	if err := auth.SetPassword(&user, "CorrectP12"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}

	reauthUserAttempts = newLoginLimiter(reauthUserFailureLimit)
	reauthIPAttempts = newLoginLimiter(reauthIPFailureLimit)
	manager := scs.New()
	loaded, err := manager.Load(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	session.Instance = manager
	if err := session.PutUser(loaded, &user); err != nil {
		t.Fatal(err)
	}
	identity, err := session.GetUser(loaded)
	if err != nil {
		t.Fatal(err)
	}
	identity.AuthenticatedAt = time.Now().Add(-time.Hour).Unix()
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	manager.Put(loaded, "user", string(encoded))
	ctx := &appcontext.Context{Context: loaded, UserID: user.ID, UserAccount: user.Account}

	status, failed := invokeReauth(t, ctx, `{"password":"WrongPass12"}`)
	if status != http.StatusUnauthorized || failed.ErrorNo == 0 {
		t.Fatalf("incorrect password response: status=%d body=%+v", status, failed)
	}
	unchanged, err := session.GetUser(loaded)
	if err != nil || unchanged.AuthenticatedAt != identity.AuthenticatedAt {
		t.Fatalf("failed reauthentication changed session: identity=%+v err=%v", unchanged, err)
	}

	status, succeeded := invokeReauth(t, ctx, `{"password":"CorrectP12"}`)
	if status != http.StatusOK || succeeded.ErrorNo != 0 {
		t.Fatalf("correct password response: status=%d body=%+v", status, succeeded)
	}
	refreshed, err := session.GetUser(loaded)
	if err != nil || !refreshed.HasRecentAuthentication(time.Now().UTC()) {
		t.Fatalf("recent authentication was not refreshed: identity=%+v err=%v", refreshed, err)
	}

	if _, err := db.Instance.ID(user.ID).Cols("disabled").Update(&models.User{Disabled: 1}); err != nil {
		t.Fatal(err)
	}
	status, disabled := invokeReauth(t, ctx, `{"password":"CorrectP12"}`)
	if status != http.StatusUnauthorized || disabled.ErrorNo == 0 {
		t.Fatalf("disabled user was reauthenticated: status=%d body=%+v", status, disabled)
	}
}

func TestReauthenticateCurrentUserRequiresSessionAndRateLimitsFailures(t *testing.T) {
	oldUserAttempts := reauthUserAttempts
	oldIPAttempts := reauthIPAttempts
	oldSession := session.Instance
	t.Cleanup(func() {
		reauthUserAttempts = oldUserAttempts
		reauthIPAttempts = oldIPAttempts
		session.Instance = oldSession
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/security/reauth", strings.NewReader(`{"password":"AnyPass12"}`))
	recorder := httptest.NewRecorder()
	ReauthenticateCurrentUser(&appcontext.Context{}, recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", recorder.Code)
	}

	reauthUserAttempts = newLoginLimiter(1)
	reauthIPAttempts = newLoginLimiter(10)
	reauthUserAttempts.recordFailure(reauthUserAttempts.accountKey("7"))
	ctx := &appcontext.Context{UserID: 7, UserAccount: "limited"}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/security/reauth", strings.NewReader(`{"password":"AnyPass12"}`))
	request.RemoteAddr = "192.0.2.50:1234"
	recorder = httptest.NewRecorder()
	session.Instance = scs.New()
	ReauthenticateCurrentUser(ctx, recorder, request)
	if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "REAUTH_RATE_LIMITED") {
		t.Fatalf("rate limit response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func invokeReauth(t *testing.T, ctx *appcontext.Context, body string) (int, response.Response) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/security/reauth", strings.NewReader(body))
	request.RemoteAddr = "192.0.2.10:1234"
	request = request.WithContext(ctx.Context)
	recorder := httptest.NewRecorder()
	ReauthenticateCurrentUser(ctx, recorder, request)
	var result response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid response: %v body=%s", err, recorder.Body.String())
	}
	return recorder.Code, result
}
