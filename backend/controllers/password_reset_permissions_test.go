package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/verification"
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

func TestSuperAdminCannotChangePasswordWithoutEmailVerification(t *testing.T) {
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
	if result.ErrorNo != response.ParamsError {
		t.Fatalf("expected web reset to be denied, got %d", result.ErrorNo)
	}
	assertUserPassword(t, user.ID, "OldPass123")
}

func TestSuperAdminPasswordChangeVerification(t *testing.T) {
	for _, scenario := range []string{"success", "backup", "primary-priority", "binding-disabled", "binding-changed", "missing", "wrong-code", "wrong-purpose", "wrong-email", "unverified", "expired", "security-version", "disabled", "invalid-password", "replay"} {
		t.Run(scenario, func(t *testing.T) {
			testRoot := t.TempDir()
			oldConfig, oldDB, oldSession := config.Instance, db.Instance, session.Instance
			oldRuntime := currentSecurityRuntime()
			oldPair, oldAccount, oldIP := loginPairAttempts, loginAccountAttempts, loginIPAttempts
			loginPairAttempts = newLoginLimiter(loginFailureLimit)
			loginAccountAttempts = newLoginLimiter(loginAccountFailureLimit)
			loginIPAttempts = newLoginLimiter(loginIPFailureLimit)
			session.Instance = scs.New()
			t.Cleanup(func() {
				_ = db.Instance.Close()
				config.Instance, db.Instance, session.Instance = oldConfig, oldDB, oldSession
				loginPairAttempts, loginAccountAttempts, loginIPAttempts = oldPair, oldAccount, oldIP
				ConfigureSecurityRuntime(oldRuntime.verification, oldRuntime.external, oldRuntime.recovery, oldRuntime.credentials)
			})
			config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(testRoot, "password.db")}
			if err := db.Init(""); err != nil {
				t.Fatal(err)
			}
			user := newControllerTestUser(t, "verified-super", "超级管理员", access.RoleSuperAdmin, "OldPass123")
			user.SecurityVersion = 1
			if _, err := db.Instance.Insert(&user); err != nil {
				t.Fatal(err)
			}
			vault := externalemail.NewVaultWithKeys([]byte("01234567890123456789012345678901"), []byte("abcdefabcdefabcdefabcdefabcdefab"))
			repo, err := externalemail.NewRepository(db.Instance, vault)
			if err != nil {
				t.Fatal(err)
			}
			bindingType := models.ExternalEmailTypePrimary
			if scenario == "backup" || scenario == "primary-priority" || scenario == "binding-changed" {
				bindingType = models.ExternalEmailTypeBackup
			}
			binding, err := repo.Bind(externalemail.BindRequest{UserID: user.ID, Email: "owner@example.com", Type: bindingType})
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "unverified" {
				if err := repo.Verify(user.ID, binding.ID); err != nil {
					t.Fatal(err)
				}
			}
			store, err := verification.NewStore(32)
			if err != nil {
				t.Fatal(err)
			}
			var code string
			var delivered verification.Challenge
			send := func(challenge verification.Challenge, value string) error {
				delivered, code = challenge, value
				return nil
			}
			service, err := verification.NewService(store, send, nil)
			if err != nil {
				t.Fatal(err)
			}
			ConfigureSecurityRuntime(service, repo, nil, nil)
			ctx := &appcontext.Context{UserID: user.ID, UserAccount: user.Account, Role: access.RoleSuperAdmin, Lang: "zhCn"}
			// 客户端既不能替换收件地址，也不能为其他账号申请改密验证码。
			for _, body := range []string{
				`{"purpose":"password_change","account":"verified-super","email":"attacker@example.com"}`,
				`{"purpose":"password_change","account":"another-user"}`,
			} {
				RequestVerification(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
				if delivered.ID != "" {
					t.Fatal("issued verification for client-controlled recipient or account")
				}
			}
			addPrimary := func() {
				primary, err := repo.Bind(externalemail.BindRequest{UserID: user.ID, Email: "primary@example.com", Type: models.ExternalEmailTypePrimary})
				if err != nil {
					t.Fatal(err)
				}
				if err := repo.Verify(user.ID, primary.ID); err != nil {
					t.Fatal(err)
				}
			}
			expectedEmail := "owner@example.com"
			if scenario == "primary-priority" {
				addPrimary()
				expectedEmail = "primary@example.com"
			}
			issueRecorder := httptest.NewRecorder()
			RequestVerification(ctx, issueRecorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"purpose":"password_change","account":"verified-super"}`)))
			if scenario == "unverified" {
				if delivered.ID != "" {
					t.Fatal("sent to unverified email")
				}
				return
			}
			if delivered.ID == "" || delivered.Email != expectedEmail {
				t.Fatalf("issue failed: %s", issueRecorder.Body.String())
			}
			input := modifyPasswordRequest{Password: "NewPass123", Code: code, ChallengeID: delivered.ID}
			switch scenario {
			case "binding-disabled":
				err = repo.Disable(user.ID, binding.ID)
			case "binding-changed":
				addPrimary()
			case "missing":
				input.Code = ""
			case "wrong-code":
				input.Code = "not-a-code"
			case "wrong-email":
				input.Email = "other@example.com"
			case "invalid-password":
				input.Password = "short"
			case "security-version":
				_, err = db.Instance.ID(user.ID).Cols("security_version").Update(&models.User{SecurityVersion: 2})
			case "disabled":
				_, err = db.Instance.ID(user.ID).Cols("disabled").Update(&models.User{Disabled: 1})
			case "wrong-purpose", "expired":
				purpose, ttl := verification.PurposeLogin, time.Minute
				if scenario == "expired" {
					purpose, ttl = verification.PurposePasswordChange, time.Nanosecond
				}
				challenge, issueErr := store.Issue(verification.IssueRequest{Purpose: purpose, Account: user.Account, Email: delivered.Email, ResourceID: binding.ID, BrowserTransaction: strconv.Itoa(user.SecurityVersion), TTL: ttl}, send)
				if issueErr != nil {
					t.Fatal(issueErr)
				}
				input.ChallengeID, input.Code = challenge.ID, code
				if scenario == "expired" {
					time.Sleep(time.Millisecond)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			change := func() int {
				body, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
				loaded, err := session.Instance.Load(req.Context(), "")
				if err != nil {
					t.Fatal(err)
				}
				session.Instance.Put(loaded, "user_id", user.ID)
				recorder := httptest.NewRecorder()
				ModifyPassword(ctx, recorder, req.WithContext(loaded))
				var result struct {
					ErrorNo int `json:"errorNo"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.ErrorNo == 0 && session.Instance.Exists(loaded, "user_id") {
					t.Fatal("session survived password change")
				}
				return result.ErrorNo
			}
			succeeded := scenario == "success" || scenario == "backup" || scenario == "primary-priority" || scenario == "replay"
			if result := change(); (result == 0) != succeeded {
				t.Fatalf("unexpected result: %d", result)
			}
			if !succeeded {
				assertUserPassword(t, user.ID, "OldPass123")
				if scenario == "invalid-password" {
					input.Password = "NewPass123"
					if change() != 0 {
						t.Fatal("invalid password consumed verification")
					}
				}
				return
			}
			assertUserPassword(t, user.ID, "NewPass123")
			var updated models.User
			if _, err := db.Instance.ID(user.ID).Get(&updated); err != nil {
				t.Fatal(err)
			}
			if updated.SessionVersion != 2 || updated.SecurityVersion != 2 {
				t.Fatal("security versions not rotated")
			}
			if scenario == "replay" && change() == 0 {
				t.Fatal("verification replay accepted")
			}
		})
	}
}
