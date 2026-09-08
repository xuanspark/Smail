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
