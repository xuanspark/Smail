package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/recovery"
	"git.pcux.cn/xuans/Smail/services/verification"
	webauthnservice "git.pcux.cn/xuans/Smail/services/webauthn"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func newPublicSecurityFixture(t *testing.T) (*recovery.Service, *webauthnservice.Service, *xorm.Engine, *models.User, *models.User) {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "public-security.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	for _, table := range []any{
		&models.User{},
		&models.ExternalEmail{},
		&models.Sessions{},
		&models.SecurityQuestion{},
		&models.RecoveryCode{},
		&models.WebAuthnCredential{},
	} {
		if err := engine.Sync2(table); err != nil {
			t.Fatal(err)
		}
	}
	passwordHash, err := auth.HashPassword("FixturePassword@123")
	if err != nil {
		t.Fatal(err)
	}
	user := &models.User{Account: "alice", Name: "Alice", Password: passwordHash, Role: string(access.RoleUser), SessionVersion: 1}
	super := &models.User{Account: "root", Name: "Root", Password: passwordHash, Role: string(access.RoleSuperAdmin), SessionVersion: 1}
	if _, err := engine.Insert(user, super); err != nil {
		t.Fatal(err)
	}
	vault := externalemail.NewVaultWithKeys([]byte("01234567890123456789012345678901"), []byte("abcdefabcdefabcdefabcdefabcdefab"))
	external, err := externalemail.NewRepository(engine, vault)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := external.Bind(externalemail.BindRequest{UserID: user.ID, Email: "alice@example.com", Type: models.ExternalEmailTypePrimary})
	if err != nil {
		t.Fatal(err)
	}
	if err := external.Verify(user.ID, binding.ID); err != nil {
		t.Fatal(err)
	}
	store, err := verification.NewStore(32)
	if err != nil {
		t.Fatal(err)
	}
	verify, err := verification.NewService(store, func(verification.Challenge, string) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	credentialService, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	recoveryService, err := recovery.NewService(engine, verify, external, credentialService)
	if err != nil {
		t.Fatal(err)
	}
	webAuthnService, err := webauthnservice.NewService(engine, webauthnservice.Config{
		RPID:          "example.com",
		RPDisplayName: "Smail",
		RPOrigins:     []string{"https://example.com"},
		VaultRoot:     t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	recoveryService.SetWebAuthnVerifier(webAuthnService)
	return recoveryService, webAuthnService, engine, user, super
}

func invokePublicSecurityHandler(t *testing.T, handler func(*appcontext.Context, http.ResponseWriter, *http.Request), body string) (int, response.Response, []string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "https://example.com/api/security", strings.NewReader(body))
	request.RemoteAddr = "192.0.2.10:12345"
	recorder := httptest.NewRecorder()
	handler(&appcontext.Context{}, recorder, request)
	var result response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON response: %v; body=%s", err, recorder.Body.String())
	}
	return recorder.Code, result, jsonShape(result.Data)
}

func enablePublicRecoveryForTest(t *testing.T) {
	t.Helper()
	old := config.Instance
	oldManager := config.CurrentManager()
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{SetupState: config.SetupStateReady, WebDomain: "example.com", ExternalRecoveryEnabled: true}
	t.Cleanup(func() {
		config.Instance = old
		config.SetRuntimeManager(oldManager)
	})
}

func jsonShape(value any) []string {
	paths := make([]string, 0)
	var walk func(string, any)
	walk = func(path string, current any) {
		switch typed := current.(type) {
		case map[string]any:
			paths = append(paths, path+":object")
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				walk(path+"."+key, typed[key])
			}
		case []any:
			paths = append(paths, path+":array")
			if len(typed) > 0 {
				walk(path+"[]", typed[0])
			}
		case nil:
			paths = append(paths, path+":null")
		default:
			paths = append(paths, path+":"+reflect.TypeOf(current).Kind().String())
		}
	}
	walk("data", value)
	return paths
}

func assertPublicResponseEquivalent(t *testing.T, baselineStatus int, baseline response.Response, baselineShape []string, status int, current response.Response, shape []string) {
	t.Helper()
	if status != baselineStatus {
		t.Fatalf("HTTP status = %d, want %d", status, baselineStatus)
	}
	if current.ErrorNo != baseline.ErrorNo || current.ErrorMsg != baseline.ErrorMsg {
		t.Fatalf("response result differs: got errorNo=%d errorMsg=%q, want errorNo=%d errorMsg=%q", current.ErrorNo, current.ErrorMsg, baseline.ErrorNo, baseline.ErrorMsg)
	}
	if !reflect.DeepEqual(shape, baselineShape) {
		t.Fatalf("response shape differs: got %v, want %v", shape, baselineShape)
	}
}

func TestPasswordRecoveryBeginResponsesDoNotRevealAccountState(t *testing.T) {
	enablePublicRecoveryForTest(t)
	recoveryService, _, _, _, _ := newPublicSecurityFixture(t)
	previous := securityRuntime
	securityRuntime.recovery = recoveryService
	t.Cleanup(func() { securityRuntime = previous })

	bodies := []string{
		`{"identifier":"alice","email":"alice@example.com"}`,
		`{"identifier":"missing","email":"missing@example.com"}`,
		`{"identifier":"alice","email":"wrong@example.com"}`,
		`{"identifier":"root","email":"alice@example.com"}`,
	}
	baselineStatus, baseline, baselineShape := invokePublicSecurityHandler(t, BeginPasswordRecovery, bodies[0])
	if baseline.ErrorNo != 0 || baseline.ErrorMsg != "" {
		t.Fatalf("unexpected baseline response: %+v", baseline)
	}
	for _, body := range bodies[1:] {
		status, current, shape := invokePublicSecurityHandler(t, BeginPasswordRecovery, body)
		assertPublicResponseEquivalent(t, baselineStatus, baseline, baselineShape, status, current, shape)
	}
	for _, body := range bodies {
		_, result, _ := invokePublicSecurityHandler(t, BeginPasswordRecovery, body)
		data, ok := result.Data.(map[string]any)
		if !ok {
			t.Fatalf("response data type = %T", result.Data)
		}
		id, ok := data["id"].(string)
		if !ok || len(id) != 32 {
			t.Fatalf("challenge id = %#v, want 32-character string", data["id"])
		}
	}
}

func TestWebAuthnLoginBeginRequiresPendingLoginTransaction(t *testing.T) {
	_, webAuthnService, _, _, _ := newPublicSecurityFixture(t)
	previous := securityRuntime
	securityRuntime.webauthn = webAuthnService
	t.Cleanup(func() { securityRuntime = previous })

	bodies := []string{`{}`, `{"challenge_id":"unknown","account":"alice"}`, `{"challenge_id":"unknown","account":"missing"}`}
	baselineStatus, baseline, baselineShape := invokePublicSecurityHandler(t, BeginWebAuthnLogin, bodies[0])
	if baseline.ErrorNo == 0 || baseline.ErrorMsg == "" {
		t.Fatalf("unexpected baseline response: %+v", baseline)
	}
	for _, body := range bodies[1:] {
		status, current, shape := invokePublicSecurityHandler(t, BeginWebAuthnLogin, body)
		assertPublicResponseEquivalent(t, baselineStatus, baseline, baselineShape, status, current, shape)
	}
}

func TestWebAuthnRecoveryBeginResponsesDoNotRevealAccountState(t *testing.T) {
	enablePublicRecoveryForTest(t)
	recoveryService, _, _, _, _ := newPublicSecurityFixture(t)
	previous := securityRuntime
	securityRuntime.recovery = recoveryService
	t.Cleanup(func() { securityRuntime = previous })

	bodies := []string{
		`{"identifier":"alice"}`,
		`{"identifier":"missing"}`,
		`{"identifier":"root"}`,
	}
	baselineStatus, baseline, baselineShape := invokePublicSecurityHandler(t, BeginWebAuthnRecovery, bodies[0])
	if baseline.ErrorNo != 0 || baseline.ErrorMsg != "" {
		t.Fatalf("unexpected baseline response: %+v", baseline)
	}
	for _, body := range bodies[1:] {
		status, current, shape := invokePublicSecurityHandler(t, BeginWebAuthnRecovery, body)
		assertPublicResponseEquivalent(t, baselineStatus, baseline, baselineShape, status, current, shape)
	}
}
