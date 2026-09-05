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
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/session"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/password"
	"github.com/alexedwards/scs/v2"
)

func TestEditUserPasswordResetRoleMatrix(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	oldCredentials := securityRuntime.credentials
	oldSession := session.Instance
	session.Instance = scs.New()
	testRoot := t.TempDir()
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		config.Instance = oldConfig
		db.Instance = oldDB
		securityRuntime.credentials = oldCredentials
		session.Instance = oldSession
	})

	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(testRoot, "permissions.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}

	superAdmin := newControllerTestUser(t, "super-admin", "超级管理员", access.RoleSuperAdmin, "SuperPass123")
	admin := newControllerTestUser(t, "admin", "管理员", access.RoleAdmin, "AdminPass123")
	user := newControllerTestUser(t, "user", "普通用户", access.RoleUser, "UserPass123")
	if _, err := db.Instance.Insert(&superAdmin, &admin, &user); err != nil {
		t.Fatal(err)
	}

	superContext := &appcontext.Context{UserID: superAdmin.ID, UserAccount: superAdmin.Account, Role: access.RoleSuperAdmin, RecentAuth: true, RecoveryCodeConfigured: true, Lang: "zhCn"}
	adminContext := &appcontext.Context{UserID: admin.ID, UserAccount: admin.Account, Role: access.RoleAdmin, RecentAuth: true, RecoveryCodeConfigured: true, Lang: "zhCn"}
	userContext := &appcontext.Context{UserID: user.ID, UserAccount: user.Account, Role: access.RoleUser, RecentAuth: true, RecoveryCodeConfigured: true, Lang: "zhCn"}

	if result := editUserRequest(t, superContext, `{"id":`+itoa(admin.ID)+`,"password":"ResetAdm12","disabled":0}`); result != 0 {
		t.Fatalf("super administrator could not reset administrator: %d", result)
	}
	assertUserPassword(t, admin.ID, "ResetAdm12")

	if result := editUserRequest(t, adminContext, `{"id":`+itoa(user.ID)+`,"password":"ResetUser123","disabled":0}`); result != 0 {
		t.Fatalf("administrator could not reset regular user: %d", result)
	}
	assertUserPassword(t, user.ID, "ResetUser123")

	if result := editUserRequest(t, adminContext, `{"id":`+itoa(superAdmin.ID)+`,"password":"DeniedPass123","disabled":0}`); result == 0 {
		t.Fatal("administrator reset of super administrator was accepted")
	}
	if result := editUserRequest(t, adminContext, `{"id":`+itoa(admin.ID)+`,"password":"DeniedPass123","disabled":0}`); result == 0 {
		t.Fatal("administrator reset of another administrator was accepted")
	}
	if result := editUserRequest(t, userContext, `{"id":`+itoa(admin.ID)+`,"password":"DeniedPass123","disabled":0}`); result == 0 {
		t.Fatal("regular user reset of another account was accepted")
	}
	if result := editUserRequest(t, superContext, `{"id":`+itoa(superAdmin.ID)+`,"password":"DeniedPass123","disabled":0}`); result == 0 {
		t.Fatal("web reset of super administrator's own password was accepted")
	}
}

func TestPasswordChangeRevokesOfflineCredentialsAtomically(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	oldCredentials := securityRuntime.credentials
	oldSession := session.Instance
	session.Instance = scs.New()
	testRoot := t.TempDir()
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		config.Instance = oldConfig
		db.Instance = oldDB
		securityRuntime.credentials = oldCredentials
		session.Instance = oldSession
	})

	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(testRoot, "credentials.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	user := newControllerTestUser(t, "credential-user", "凭据用户", access.RoleUser, "OldPass123")
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}
	credentialService, err := credentials.NewService(db.Instance)
	if err != nil {
		t.Fatal(err)
	}
	securityRuntime.credentials = credentialService
	if err := credentialService.SetSecurityQuestion(user.ID, "最喜欢的颜色", "蓝色答案123"); err != nil {
		t.Fatal(err)
	}
	codes, err := credentialService.GenerateRecoveryCodes(user.ID, 5)
	if err != nil || len(codes) != 5 {
		t.Fatalf("generate recovery codes failed: %v", err)
	}

	ctx := &appcontext.Context{UserID: user.ID, UserAccount: user.Account, Role: access.RoleUser, Lang: "zhCn"}
	request := httptest.NewRequest(http.MethodPost, "/api/settings/modify_password", strings.NewReader(`{"current_password":"OldPass123","password":"NewPass123"}`))
	loadedContext, err := session.Instance.Load(request.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithContext(loadedContext)
	recorder := httptest.NewRecorder()
	ModifyPassword(ctx, recorder, request)
	var result struct {
		ErrorNo int `json:"errorNo"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ErrorNo != 0 {
		t.Fatalf("password change failed: %s", recorder.Body.String())
	}
	configured, err := credentialService.HasValidRecoveryCode(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		t.Fatal("recovery codes remained valid after password change")
	}
	_, configured, err = credentialService.GetSecurityQuestion(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		t.Fatal("security question remained configured after password change")
	}
	if err := credentialService.ConsumeRecoveryCode(user.ID, codes[0]); err == nil {
		t.Fatal("revoked recovery code was accepted after password change")
	}
}

func newControllerTestUser(t *testing.T, account, name string, role access.Role, plainPassword string) models.User {
	t.Helper()
	user := models.User{Account: account, Name: name, Role: string(role), SessionVersion: 1}
	user.ApplyRole(role)
	if err := auth.SetPassword(&user, plainPassword); err != nil {
		t.Fatal(err)
	}
	return user
}

func assertUserPassword(t *testing.T, userID int, plainPassword string) {
	t.Helper()
	var user models.User
	found, err := db.Instance.ID(userID).Get(&user)
	if err != nil {
		t.Fatal(err)
	}
	if !found || !authPasswordMatches(user.Password, plainPassword) {
		t.Fatalf("user %d password did not change", userID)
	}
}

func authPasswordMatches(hash, plain string) bool {
	return password.Verify(hash, plain)
}

func TestSuperAdminCannotChangePasswordThroughWeb(t *testing.T) {
	testRoot := t.TempDir()
	oldConfig := config.Instance
	oldDB := db.Instance
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		config.Instance = oldConfig
		db.Instance = oldDB
	})
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(testRoot, "web-password.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	user := newControllerTestUser(t, "web-super-admin", "super admin", access.RoleSuperAdmin, "OldPass123")
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}
	ctx := &appcontext.Context{UserID: user.ID, UserAccount: user.Account, Role: access.RoleSuperAdmin, Lang: "zhCn"}
	request := httptest.NewRequest(http.MethodPost, "/api/settings/modify_password", strings.NewReader(`{"current_password":"OldPass123","password":"NewPass123"}`))
	recorder := httptest.NewRecorder()
	ModifyPassword(ctx, recorder, request)
	var result struct {
		ErrorNo int `json:"errorNo"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ErrorNo != response.NoAccessPrivileges {
		t.Fatalf("expected web reset to be denied, got %d", result.ErrorNo)
	}
	assertUserPassword(t, user.ID, "OldPass123")
}
