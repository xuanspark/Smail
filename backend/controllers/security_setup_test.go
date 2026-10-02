package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func setupSecurityPromptTest(t *testing.T) *xorm.Engine {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "onboarding.db"))
	if err != nil {
		t.Fatal(err)
	}
	previous := db.Instance
	db.Instance = engine
	t.Cleanup(func() { db.Instance = previous; _ = engine.Close() })
	if err := engine.Sync2(&models.User{}, &models.ExternalEmail{}, &models.RecoveryCode{}, &models.TOTPAuthenticator{}); err != nil {
		t.Fatal(err)
	}
	previousRuntime := securityRuntime
	securityRuntime.credentials = nil
	t.Cleanup(func() { securityRuntime = previousRuntime })
	return engine
}

func TestSecurityContextRecoveryOptionsRefresh(t *testing.T) {
	for _, role := range []access.Role{access.RoleAdmin, access.RoleUser} {
		t.Run(string(role), func(t *testing.T) {
			engine := setupSecurityPromptTest(t)
			user := models.User{Account: "option-user", Role: string(role)}
			if _, err := engine.Insert(&user); err != nil {
				t.Fatal(err)
			}
			ctx := &appcontext.Context{UserID: user.ID, Role: role, RecentAuth: true, RecoveryCodeConfigured: true, ExternalEmailConfigured: true}
			assertState := func(code, email bool) {
				t.Helper()
				LoadSecurityContext(ctx)
				if ctx.RecoveryCodeConfigured != code || ctx.ExternalEmailConfigured != email {
					t.Fatalf("unexpected context: code=%v email=%v", ctx.RecoveryCodeConfigured, ctx.ExternalEmailConfigured)
				}
			}
			assertState(false, false)
			email := models.ExternalEmail{UserID: user.ID, Status: models.ExternalEmailStatusPending, EmailFingerprint: "fixture"}
			if _, err := engine.Insert(&email); err != nil {
				t.Fatal(err)
			}
			assertState(false, false)
			if _, err := engine.ID(email.ID).Cols("status").Update(&models.ExternalEmail{Status: models.ExternalEmailStatusVerified}); err != nil {
				t.Fatal(err)
			}
			assertState(false, true)
			if role == access.RoleAdmin {
				if err := authorizeCapability(ctx, access.CapabilityPasswordReset, access.RoleUser, false); err != nil {
					t.Fatalf("email state not forwarded: %v", err)
				}
			}
			if _, err := engine.ID(email.ID).Cols("status").Update(&models.ExternalEmail{Status: models.ExternalEmailStatusDisabled}); err != nil {
				t.Fatal(err)
			}
			assertState(false, false)
			code := models.RecoveryCode{UserID: user.ID, CodeHash: "fixture"}
			if _, err := engine.Insert(&code); err != nil {
				t.Fatal(err)
			}
			assertState(true, false)
			now := time.Now()
			if _, err := engine.ID(code.ID).Cols("used_at").Update(&models.RecoveryCode{UsedAt: &now}); err != nil {
				t.Fatal(err)
			}
			assertState(false, false)
			ctx.UserID = 0
			ctx.ExternalEmailConfigured, ctx.RecoveryCodeConfigured = true, true
			assertState(false, false)
			ctx.UserID = user.ID
			db.Instance = nil
			ctx.ExternalEmailConfigured, ctx.RecoveryCodeConfigured = true, true
			assertState(false, false)
		})
	}
	LoadSecurityContext(nil)
}

func TestRoleAuthorizationForwardsTargetEmail(t *testing.T) {
	ctx := &appcontext.Context{UserID: 1, Role: access.RoleSuperAdmin, RecentAuth: true, TargetExternalEmailConfigured: true}
	if err := authorizeCapabilityWithNewRole(ctx, access.CapabilityRoleManage, access.RoleUser, access.RoleAdmin, false); err != nil {
		t.Fatalf("target email was not forwarded: %v", err)
	}
	ctx.TargetExternalEmailConfigured = false
	ctx.ExternalEmailConfigured = true
	if err := authorizeCapabilityWithNewRole(ctx, access.CapabilityRoleManage, access.RoleUser, access.RoleAdmin, false); !access.IsDenied(err, access.CodeRecoveryCodeRequired) {
		t.Fatalf("actor state substituted for target: %v", err)
	}
}

func TestSecuritySetupAcknowledgementIsSessionScoped(t *testing.T) {
	engine := setupSecurityPromptTest(t)
	previousSession, previousConfig, previousManager := session.Instance, config.Instance, config.CurrentManager()
	session.Instance = scs.New()
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{Domain: "example.com"}
	t.Cleanup(func() {
		session.Instance, config.Instance = previousSession, previousConfig
		config.SetRuntimeManager(previousManager)
	})
	first := models.User{Account: "admin", Name: "Admin", Role: string(access.RoleAdmin)}
	second := models.User{Account: "alice", Name: "Alice", Role: string(access.RoleUser)}
	if _, err := engine.Insert(&first, &second); err != nil {
		t.Fatal(err)
	}
	if loginSecurityFields(&first)["showSecuritySetupPrompt"] != true {
		t.Fatal("first super administrator login must show security setup")
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/security/setup/acknowledge", strings.NewReader(`{"user_id":2}`))
	loadedSession, err := session.Instance.Load(request.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithContext(loadedSession)
	if err := session.PutUser(loadedSession, &first); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		AcknowledgeSecuritySetup(&appcontext.Context{UserID: first.ID}, recorder, request)
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
	if loaded.SecuritySetupAcknowledged != 0 || untouched.SecuritySetupAcknowledged != 0 {
		t.Fatal("dismissal must not persist on either account")
	}
	fields := loginSecurityFields(&loaded)
	if fields["showSecuritySetupPrompt"] != true || fields["showRecoverySetupPrompt"] != true {
		t.Fatal("incomplete setup must be reported again on login")
	}
	if untouched.Password != second.Password || loaded.SecurityVersion != first.SecurityVersion {
		t.Fatal("acknowledgement changed security credentials")
	}
	otherSession, err := session.Instance.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if session.Instance.GetBool(otherSession, session.SecuritySetupDismissedKey) {
		t.Fatal("dismissal leaked to another session")
	}
	token, _, err := session.Instance.Commit(loadedSession)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := session.Instance.Load(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	assertInfoPrompt := func(want bool) {
		t.Helper()
		recorder := httptest.NewRecorder()
		Info(&appcontext.Context{UserID: first.ID, Role: access.RoleAdmin}, recorder, request.WithContext(refreshed))
		var payload struct {
			ErrorNo int `json:"errorNo"`
			Data    struct {
				Show bool `json:"showSecuritySetupPrompt"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || payload.ErrorNo != 0 || payload.Data.Show != want {
			t.Fatalf("unexpected refreshed prompt: %s", recorder.Body.String())
		}
	}
	assertInfoPrompt(false)
	if err := session.PutUser(refreshed, &first); err != nil {
		t.Fatal(err)
	}
	assertInfoPrompt(true)
}

func TestSecuritySetupRequiresUsableCredentials(t *testing.T) {
	for _, role := range []access.Role{access.RoleUser, access.RoleAdmin, access.RoleSuperAdmin} {
		for _, emailStatus := range []string{"", models.ExternalEmailStatusPending, models.ExternalEmailStatusVerified, models.ExternalEmailStatusDisabled} {
			for _, codeStatus := range []string{"missing", "valid", "used"} {
				t.Run(string(role)+"/"+emailStatus+"/"+codeStatus, func(t *testing.T) {
					engine := setupSecurityPromptTest(t)
					user := models.User{Account: "alice", Role: string(role), SecuritySetupAcknowledged: 1}
					if _, err := engine.Insert(&user); err != nil {
						t.Fatal(err)
					}
					if emailStatus != "" {
						if _, err := engine.Insert(&models.ExternalEmail{UserID: user.ID, Status: emailStatus, EmailFingerprint: "fixture"}); err != nil {
							t.Fatal(err)
						}
					}
					if codeStatus != "missing" {
						code := models.RecoveryCode{UserID: user.ID, CodeHash: "fixture"}
						if codeStatus == "used" {
							now := time.Now()
							code.UsedAt = &now
						}
						if _, err := engine.Insert(&code); err != nil {
							t.Fatal(err)
						}
					}
					fields := loginSecurityFields(&user)
					emailReady := emailStatus == models.ExternalEmailStatusVerified
					codeReady := codeStatus == "valid"
					wantPrompt := !emailReady && !codeReady
					wantRequired := role == access.RoleSuperAdmin && wantPrompt
					wantRecoveryPrompt := role != access.RoleSuperAdmin && wantPrompt
					if fields["externalEmailConfigured"] != emailReady || fields["recoveryCodesConfigured"] != codeReady ||
						fields["showSecuritySetupPrompt"] != wantPrompt || fields["showRecoverySetupPrompt"] != wantRecoveryPrompt ||
						fields["securitySetupRequired"] != wantRequired {
						t.Fatalf("unexpected security state: %#v", fields)
					}
				})
			}
		}
	}
}

func TestNonSuperAdminRecoverySetupTracksUsableOptions(t *testing.T) {
	for _, role := range []access.Role{access.RoleUser, access.RoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			engine := setupSecurityPromptTest(t)
			user := models.User{Account: "alice", Role: string(role)}
			if _, err := engine.Insert(&user); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Insert(&models.TOTPAuthenticator{UserID: user.ID, Enabled: true}); err != nil {
				t.Fatal(err)
			}
			assertPrompt := func(want bool) {
				t.Helper()
				fields := loginSecurityFields(&user)
				if fields["showSecuritySetupPrompt"] != want || fields["showRecoverySetupPrompt"] != want ||
					fields["securitySetupRequired"] != false {
					t.Fatalf("unexpected security state: %#v", fields)
				}
			}
			assertPrompt(true)
			email := models.ExternalEmail{UserID: user.ID, Status: models.ExternalEmailStatusVerified, EmailFingerprint: "fixture"}
			if _, err := engine.Insert(&email); err != nil {
				t.Fatal(err)
			}
			assertPrompt(false)
			if _, err := engine.ID(email.ID).Cols("status").Update(&models.ExternalEmail{Status: models.ExternalEmailStatusDisabled}); err != nil {
				t.Fatal(err)
			}
			assertPrompt(true)
			code := models.RecoveryCode{UserID: user.ID, CodeHash: "fixture"}
			if _, err := engine.Insert(&code); err != nil {
				t.Fatal(err)
			}
			assertPrompt(false)
			now := time.Now()
			if _, err := engine.ID(code.ID).Cols("used_at").Update(&models.RecoveryCode{UsedAt: &now}); err != nil {
				t.Fatal(err)
			}
			assertPrompt(true)
		})
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

func TestSuperAdminCannotDismissRequiredSetupAndTOTPQualifies(t *testing.T) {
	engine := setupSecurityPromptTest(t)
	previous := session.Instance
	session.Instance = scs.New()
	t.Cleanup(func() { session.Instance = previous })
	user := models.User{Account: "root", Role: string(access.RoleSuperAdmin)}
	if _, err := engine.Insert(&user); err != nil {
		t.Fatal(err)
	}
	loaded, err := session.Instance.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/security/setup/acknowledge", nil).WithContext(loaded)
	recorder := httptest.NewRecorder()
	AcknowledgeSecuritySetup(&appcontext.Context{UserID: user.ID, Role: access.RoleSuperAdmin}, recorder, request)
	var payload struct {
		ErrorNo int `json:"errorNo"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || payload.ErrorNo == 0 {
		t.Fatal("required setup was dismissed")
	}
	if session.Instance.GetBool(loaded, session.SecuritySetupDismissedKey) {
		t.Fatal("dismissal persisted")
	}
	if _, err := engine.Insert(&models.TOTPAuthenticator{UserID: user.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if RequiresSecuritySetup(user.ID, user.EffectiveRole()) {
		t.Fatal("enabled TOTP did not qualify")
	}
	now := time.Now()
	if _, err := engine.Where("user_id = ?", user.ID).Cols("revoked_at").Update(&models.TOTPAuthenticator{RevokedAt: &now}); err != nil {
		t.Fatal(err)
	}
	if !RequiresSecuritySetup(user.ID, user.EffectiveRole()) {
		t.Fatal("revoked TOTP still qualifies")
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
