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
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/verification"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/password"
)

func TestEditUserProtectsAdministrativeCredentialsAndRevokesUserSessions(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	testRoot := t.TempDir()
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		config.Instance = oldConfig
		db.Instance = oldDB
	})

	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(testRoot, "user-security.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	admin := models.User{Account: "admin-user", Name: "Admin", IsAdmin: 1, SessionVersion: 1}
	if err := auth.SetPassword(&admin, "AdminPass123"); err != nil {
		t.Fatal(err)
	}
	regular := models.User{Account: "regular-user", Name: "User", SessionVersion: 1}
	if err := auth.SetPassword(&regular, "UserPass123"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&admin, &regular); err != nil {
		t.Fatal(err)
	}
	ctx := &appcontext.Context{
		UserID: admin.ID, UserAccount: admin.Account, IsAdmin: true, Lang: "zhCn",
		RecentAuth: true, RecoveryCodeConfigured: true,
	}

	result := editUserRequest(t, ctx, `{"id":`+itoa(admin.ID)+`,"password":"BypassPass123","disabled":0}`)
	if result == 0 {
		t.Fatal("administrator changed the current password without current-password verification")
	}
	var loadedAdmin models.User
	if _, err := db.Instance.ID(admin.ID).Get(&loadedAdmin); err != nil {
		t.Fatal(err)
	}
	if !password.Verify(loadedAdmin.Password, "AdminPass123") {
		t.Fatal("administrator password changed through the user management bypass")
	}

	result = editUserRequest(t, ctx, `{"id":`+itoa(admin.ID)+`,"disabled":1}`)
	if result == 0 {
		t.Fatal("administrator account was disabled")
	}

	result = editUserRequest(t, ctx, `{"id":`+itoa(regular.ID)+`,"password":"ResetPass123","disabled":0}`)
	if result != 0 {
		t.Fatal("administrator could not reset another user password")
	}
	var resetUser models.User
	if _, err := db.Instance.ID(regular.ID).Get(&resetUser); err != nil {
		t.Fatal(err)
	}
	if resetUser.SessionVersion != regular.SessionVersion+1 || !password.Verify(resetUser.Password, "ResetPass123") {
		t.Fatalf("password reset did not revoke existing sessions: version=%d", resetUser.SessionVersion)
	}

	result = editUserRequest(t, ctx, `{"id":`+itoa(regular.ID)+`,"disabled":1}`)
	if result != 0 {
		t.Fatal("administrator could not disable a regular user")
	}
	var disabledUser models.User
	if _, err := db.Instance.ID(regular.ID).Get(&disabledUser); err != nil {
		t.Fatal(err)
	}
	if disabledUser.Disabled != 1 || disabledUser.SessionVersion != resetUser.SessionVersion+1 {
		t.Fatalf("disabling a user did not revoke existing sessions: %+v", disabledUser)
	}
}

func TestUserListReturnsNormalizedRolesAndSecurityStatus(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	testRoot := t.TempDir()
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		config.Instance = oldConfig
		db.Instance = oldDB
	})

	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(testRoot, "user-list.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	legacyAdmin := models.User{Account: "legacy-admin", Name: "Legacy", IsAdmin: 1, ExternalEmailEnabled: 1, SessionVersion: 1, SecurityVersion: 1}
	regularUser := models.User{Account: "regular-list", Name: "Regular", Role: string(access.RoleUser), ExternalEmailEnabled: 0, SessionVersion: 1, SecurityVersion: 1}
	if err := auth.SetPassword(&legacyAdmin, "AdminPass123"); err != nil {
		t.Fatal(err)
	}
	if err := auth.SetPassword(&regularUser, "UserPass123"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&legacyAdmin, &regularUser); err != nil {
		t.Fatal(err)
	}
	usedAt := time.Now().UTC()
	fixtures := []any{
		&models.RecoveryCode{UserID: legacyAdmin.ID, CodeHash: "active", CodeVersion: 1},
		&models.RecoveryCode{UserID: regularUser.ID, CodeHash: "used", CodeVersion: 1, UsedAt: &usedAt},
		&models.ExternalEmail{UserID: legacyAdmin.ID, EmailCiphertext: "ciphertext-verified", EmailFingerprint: "verified-email", EmailType: models.ExternalEmailTypePrimary, Status: models.ExternalEmailStatusVerified},
		&models.ExternalEmail{UserID: regularUser.ID, EmailCiphertext: "ciphertext-pending", EmailFingerprint: "pending-email", EmailType: models.ExternalEmailTypePrimary, Status: models.ExternalEmailStatusPending},
	}
	for _, fixture := range fixtures {
		if _, err := db.Instance.Insert(fixture); err != nil {
			t.Fatal(err)
		}
	}

	ctx := &appcontext.Context{UserID: legacyAdmin.ID, UserAccount: legacyAdmin.Account, IsAdmin: true, Lang: "zhCn"}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/user/list", strings.NewReader(`{"current_page":1,"page_size":15}`))
	UserList(ctx, recorder, request)
	var result struct {
		ErrorNo int `json:"errorNo"`
		Data    struct {
			List []userListItem `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ErrorNo != 0 || len(result.Data.List) != 2 {
		t.Fatalf("unexpected user list response: %s", recorder.Body.String())
	}
	items := make(map[string]userListItem, len(result.Data.List))
	for _, item := range result.Data.List {
		items[item.Account] = item
	}
	adminItem := items[legacyAdmin.Account]
	if adminItem.Role != access.RoleSuperAdmin || !adminItem.RecoveryCodeConfigured || !adminItem.ExternalEmailEnabled || !adminItem.ExternalEmailConfigured {
		t.Fatalf("legacy administrator status was not normalized: %+v", adminItem)
	}
	regularItem := items[regularUser.Account]
	if regularItem.Role != access.RoleUser || regularItem.RecoveryCodeConfigured || regularItem.ExternalEmailEnabled || regularItem.ExternalEmailConfigured {
		t.Fatalf("regular user status was incorrect: %+v", regularItem)
	}
	for _, secret := range []string{"AdminPass123", "UserPass123", "ciphertext-verified", "ciphertext-pending", "active", "used"} {
		if strings.Contains(recorder.Body.String(), secret) {
			t.Fatalf("user list exposed sensitive value %q", secret)
		}
	}
}

func editUserRequest(t *testing.T, ctx *appcontext.Context, payload string) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/user/edit", strings.NewReader(payload))
	EditUser(ctx, recorder, request)
	var result struct {
		ErrorNo int `json:"errorNo"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.ErrorNo
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 20)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}

func TestUserRequestsRejectInvalidJSON(t *testing.T) {
	ctx := &appcontext.Context{IsAdmin: true, Lang: "zhCn"}
	tests := []struct {
		name    string
		handler func(*appcontext.Context, http.ResponseWriter, *http.Request)
		payload string
	}{
		{name: "list malformed", handler: UserList, payload: `{"current_page":`},
		{name: "list trailing", handler: UserList, payload: `{"current_page":1,"page_size":15} {}`},
		{name: "list oversized", handler: UserList, payload: `{"current_page":1,"page_size":15,"padding":"` + strings.Repeat("a", int(maxUserRequestBytes)) + `"}`},
		{name: "create trailing", handler: CreateUser, payload: `{"account":"user","username":"User","password":"Password123"} {}`},
		{name: "edit trailing", handler: EditUser, payload: `{"id":1,"disabled":0} {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/user", strings.NewReader(test.payload))
			test.handler(ctx, recorder, request)
			var result struct {
				ErrorNo int `json:"errorNo"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.ErrorNo == 0 {
				t.Fatalf("invalid request was accepted: %s", test.name)
			}
		})
	}
}

func TestManagedRecoveryCodesAreScopedAndShownOnce(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	oldRuntime := currentSecurityRuntime()
	testRoot := t.TempDir()
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		config.Instance = oldConfig
		db.Instance = oldDB
		ConfigureSecurityRuntime(oldRuntime.verification, oldRuntime.external, oldRuntime.recovery, oldRuntime.credentials)
		ConfigureWebAuthnRuntime(oldRuntime.webauthn)
	})

	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(testRoot, "managed-recovery.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	actor := models.User{Account: "security-owner", Name: "Owner", SessionVersion: 1, SecurityVersion: 1}
	actor.ApplyRole(access.RoleSuperAdmin)
	target := models.User{Account: "security-target", Name: "Target", SessionVersion: 1, SecurityVersion: 1}
	target.ApplyRole(access.RoleUser)
	if err := auth.SetPassword(&actor, "OwnerPass123"); err != nil {
		t.Fatal(err)
	}
	if err := auth.SetPassword(&target, "TargetP123"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&actor, &target); err != nil {
		t.Fatal(err)
	}
	credentialService, err := credentials.NewService(db.Instance)
	if err != nil {
		t.Fatal(err)
	}
	ConfigureSecurityRuntime(nil, nil, nil, credentialService)
	ctx := &appcontext.Context{
		UserID: actor.ID, UserAccount: actor.Account, Role: access.RoleSuperAdmin, RecentAuth: true,
	}

	generateRecorder := httptest.NewRecorder()
	generateRequest := httptest.NewRequest(http.MethodPost, "/api/v1/user/security/recovery-codes/generate", strings.NewReader(`{"user_id":`+itoa(target.ID)+`,"count":5}`))
	GenerateManagedRecoveryCodes(ctx, generateRecorder, generateRequest)
	var generated struct {
		ErrorNo int `json:"errorNo"`
		Data    struct {
			UserID    int      `json:"user_id"`
			Codes     []string `json:"codes"`
			ShownOnce bool     `json:"shown_once"`
		} `json:"data"`
	}
	if err := json.Unmarshal(generateRecorder.Body.Bytes(), &generated); err != nil {
		t.Fatal(err)
	}
	if generated.ErrorNo != 0 || generated.Data.UserID != target.ID || len(generated.Data.Codes) != 5 || !generated.Data.ShownOnce {
		t.Fatalf("unexpected managed recovery response: %s", generateRecorder.Body.String())
	}
	for _, code := range generated.Data.Codes {
		if code == "" || !strings.Contains(code, "-") {
			t.Fatalf("invalid one-time recovery code: %q", code)
		}
	}

	statusRecorder := httptest.NewRecorder()
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/user/security/recovery-codes?id="+itoa(target.ID), nil)
	GetManagedRecoveryCodeStatus(ctx, statusRecorder, statusRequest)
	if !strings.Contains(statusRecorder.Body.String(), `"configured":true`) {
		t.Fatalf("managed recovery status was not enabled: %s", statusRecorder.Body.String())
	}
	for _, code := range generated.Data.Codes {
		if strings.Contains(statusRecorder.Body.String(), code) {
			t.Fatalf("status response leaked one-time code %q", code)
		}
	}

	revokeRecorder := httptest.NewRecorder()
	revokeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/user/security/recovery-codes/revoke", strings.NewReader(`{"user_id":`+itoa(target.ID)+`}`))
	RevokeManagedRecoveryCodes(ctx, revokeRecorder, revokeRequest)
	if !strings.Contains(revokeRecorder.Body.String(), `"revoked":true`) {
		t.Fatalf("managed recovery codes were not revoked: %s", revokeRecorder.Body.String())
	}

	deniedRecorder := httptest.NewRecorder()
	deniedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/user/security/recovery-codes?id="+itoa(actor.ID), nil)
	GetManagedRecoveryCodeStatus(ctx, deniedRecorder, deniedRequest)
	var denied struct {
		ErrorNo int `json:"errorNo"`
	}
	if err := json.Unmarshal(deniedRecorder.Body.Bytes(), &denied); err != nil {
		t.Fatal(err)
	}
	if denied.ErrorNo == 0 {
		t.Fatal("web route allowed management of super-administrator recovery codes")
	}
}

func TestManagedExternalEmailChangeInvalidatesPreviousChallenge(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	oldRuntime := currentSecurityRuntime()
	testRoot := t.TempDir()
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		config.Instance = oldConfig
		db.Instance = oldDB
		ConfigureSecurityRuntime(oldRuntime.verification, oldRuntime.external, oldRuntime.recovery, oldRuntime.credentials)
		ConfigureWebAuthnRuntime(oldRuntime.webauthn)
	})

	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(testRoot, "managed-external-email.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	actor := models.User{Account: "email-owner", Name: "Owner", SessionVersion: 1, SecurityVersion: 1}
	actor.ApplyRole(access.RoleSuperAdmin)
	target := models.User{Account: "email-target", Name: "Target", SessionVersion: 1, SecurityVersion: 1}
	target.ApplyRole(access.RoleUser)
	if err := auth.SetPassword(&actor, "OwnerPass123"); err != nil {
		t.Fatal(err)
	}
	if err := auth.SetPassword(&target, "TargetP123"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&actor, &target); err != nil {
		t.Fatal(err)
	}

	vault := externalemail.NewVaultWithKeys(
		[]byte("01234567890123456789012345678901"),
		[]byte("abcdefabcdefabcdefabcdefabcdefab"),
	)
	external, err := externalemail.NewRepository(db.Instance, vault)
	if err != nil {
		t.Fatal(err)
	}
	store, err := verification.NewStore(32)
	if err != nil {
		t.Fatal(err)
	}
	issuedCodes := make(map[string]string)
	verificationService, err := verification.NewService(store, func(challenge verification.Challenge, code string) error {
		issuedCodes[challenge.ID] = code
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ConfigureSecurityRuntime(verificationService, external, nil, nil)

	pending, err := external.BeginReplacement(externalemail.BindRequest{
		UserID: target.ID,
		Email:  "first@example.com",
		Type:   models.ExternalEmailTypePrimary,
	})
	if err != nil {
		t.Fatal(err)
	}
	oldChallenge, err := store.Issue(verification.IssueRequest{
		Purpose:    verification.PurposeExternalEmailChange,
		UserID:     target.ID,
		Account:    target.Account,
		Email:      "first@example.com",
		ResourceID: pending.ID,
	}, func(challenge verification.Challenge, code string) error {
		issuedCodes[challenge.ID] = code
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := &appcontext.Context{
		UserID:      actor.ID,
		UserAccount: actor.Account,
		Role:        access.RoleSuperAdmin,
		RecentAuth:  true,
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/user/security/external-email/change",
		strings.NewReader(`{"user_id":`+itoa(target.ID)+`,"email":"second@example.com","type":"primary"}`),
	)
	ChangeManagedExternalEmail(ctx, recorder, request)
	var result struct {
		ErrorNo int `json:"errorNo"`
		Data    struct {
			BindingID   int    `json:"binding_id"`
			ChallengeID string `json:"challenge_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ErrorNo != 0 || result.Data.BindingID != pending.ID || result.Data.ChallengeID == "" {
		t.Fatalf("unexpected managed external email response: %s", recorder.Body.String())
	}
	if err := store.VerifyBound(
		oldChallenge.ID,
		target.Account,
		"first@example.com",
		issuedCodes[oldChallenge.ID],
		verification.PurposeExternalEmailChange,
		pending.ID,
	); err != verification.ErrInvalidChallenge {
		t.Fatalf("previous challenge remained valid after pending email changed: %v", err)
	}
	if err := store.VerifyBound(
		result.Data.ChallengeID,
		target.Account,
		"second@example.com",
		issuedCodes[result.Data.ChallengeID],
		verification.PurposeExternalEmailChange,
		pending.ID,
	); err != nil {
		t.Fatalf("new challenge was not valid: %v", err)
	}
}
