package controllers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/totp"
	"git.pcux.cn/xuans/Smail/services/verification"
	"git.pcux.cn/xuans/Smail/session"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"github.com/alexedwards/scs/v2"
)

func setupLoginMethods(t *testing.T, delivery verification.Delivery) (*models.User, *totp.Service) {
	t.Helper()
	oldConfig, oldDB, oldSession, oldManager := config.Instance, db.Instance, session.Instance, config.CurrentManager()
	oldRuntime, oldAttempts := currentSecurityRuntime(), loginSecondFactorAttempts
	root := t.TempDir()
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(root, "login.db"), Domain: "example.com", VerificationEnabled: true, LoginPolicy: "always"}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	session.Instance = scs.New()
	loginSecondFactorAttempts = newLoginLimiter(20)
	t.Cleanup(func() {
		ClearSecurityRuntime()
		_ = db.Instance.Close()
		config.Instance, db.Instance, session.Instance = oldConfig, oldDB, oldSession
		config.SetRuntimeManager(oldManager)
		securityRuntimeMu.Lock()
		securityRuntime = oldRuntime
		securityRuntimeMu.Unlock()
		loginSecondFactorAttempts = oldAttempts
	})
	user := &models.User{Account: "login-method-user", Role: string(access.RoleUser), SessionVersion: 1}
	if _, err := db.Instance.Insert(user); err != nil {
		t.Fatal(err)
	}
	vault := externalemail.NewVaultWithKeys([]byte("01234567890123456789012345678901"), []byte("abcdefabcdefabcdefabcdefabcdefab"))
	repo, err := externalemail.NewRepository(db.Instance, vault)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.Bind(externalemail.BindRequest{UserID: user.ID, Email: "owner@example.net", Type: models.ExternalEmailTypePrimary})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Verify(user.ID, binding.ID); err != nil {
		t.Fatal(err)
	}
	store, err := verification.NewStore(100)
	if err != nil {
		t.Fatal(err)
	}
	service, err := verification.NewService(store, delivery, nil)
	if err != nil {
		t.Fatal(err)
	}
	authenticator, err := totp.NewService(db.Instance, root)
	if err != nil {
		t.Fatal(err)
	}
	ConfigureSecurityRuntime(service, repo, nil, nil)
	ConfigureTOTPRuntime(authenticator)
	return user, authenticator
}

func callLoginHandler(t *testing.T, handler func(*appcontext.Context, http.ResponseWriter, *http.Request), input any) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://example.com/api/v1/security/login/verify", strings.NewReader(string(body)))
	loaded, err := session.Instance.Load(req.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(loaded)
	recorder := httptest.NewRecorder()
	handler(&appcontext.Context{Context: loaded}, recorder, req)
	var result struct {
		ErrorNo int             `json:"errorNo"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var data map[string]any
	if result.ErrorNo == 0 {
		if err := json.Unmarshal(result.Data, &data); err != nil {
			t.Fatal(err)
		}
	}
	return result.ErrorNo, data
}

func TestLoginEmailDeliveryDoesNotBlockHTTP(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	user, _ := setupLoginMethods(t, func(verification.Challenge, string) error {
		calls.Add(1)
		once.Do(func() { close(started) })
		<-release
		return nil
	})
	t.Cleanup(func() {
		close(release)
		deadline := time.After(2 * time.Second)
		for len(loginDeliverySlots) != 0 {
			select {
			case <-deadline:
				t.Error("mail worker did not stop")
				return
			case <-time.After(time.Millisecond):
			}
		}
	})
	begin := time.Now()
	pending, err := beginLoginVerification(user, "127.0.0.1")
	if err != nil || calls.Load() != 0 {
		t.Fatalf("begin invoked mail delivery: %v", err)
	}
	input := resendLoginVerificationRequest{ID: pending["challenge_id"].(string), Account: user.Account}
	code, data := callLoginHandler(t, ResendLoginVerification, input)
	if code != 0 || data["delivery_status"] != "sending" {
		t.Fatalf("send response: %d %+v", code, data)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("mail worker did not start")
	}
	for i := 0; i < 3; i++ {
		code, _ = callLoginHandler(t, ResendLoginVerification, input)
		if code != 0 {
			t.Fatal("duplicate request failed")
		}
		code, data = callLoginHandler(t, LoginVerificationStatus, input)
		if code != 0 || data["delivery_status"] != "sending" {
			t.Fatal("status failed while mail blocked")
		}
	}
	if elapsed := time.Since(begin); elapsed > time.Second {
		t.Fatalf("HTTP operations blocked for %s", elapsed)
	} else {
		t.Logf("begin/send/status with blocked sender: %s", elapsed)
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicate mail jobs: %d", calls.Load())
	}
	if code, _ := callLoginHandler(t, LoginVerificationStatus, resendLoginVerificationRequest{ID: input.ID, Account: "another-user"}); code == 0 {
		t.Fatal("status disclosed across accounts")
	}
	InvalidateSecurityRuntimeUser(user.ID, user.Account)
}

func testTOTPCode(secret string, step int64) string {
	decoded, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha256.New, decoded)
	_, _ = mac.Write(counter[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 15
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(digest[offset:offset+4])&0x7fffffff)%1000000)
}

func TestLoginMethodsRolesAndSingleConsumption(t *testing.T) {
	for _, role := range []access.Role{access.RoleSuperAdmin, access.RoleAdmin, access.RoleUser} {
		t.Run(string(role), func(t *testing.T) {
			user, service := setupLoginMethods(t, func(verification.Challenge, string) error { return nil })
			user.Role = string(role)
			if _, err := db.Instance.ID(user.ID).Cols("role").Update(user); err != nil {
				t.Fatal(err)
			}
			ctx := &appcontext.Context{UserID: user.ID, UserAccount: user.Account, Role: role, RecentAuth: true}
			recorder := httptest.NewRecorder()
			TOTPSettings(ctx, recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"action":"begin"}`)))
			var result struct {
				ErrorNo int             `json:"errorNo"`
				Data    totp.Enrollment `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.ErrorNo != 0 || result.Data.Secret == "" {
				t.Fatalf("role cannot enroll: %v", err)
			}
			step := time.Now().Unix() / 30
			if err := service.ConfirmEnrollment(user.ID, result.Data.ID, testTOTPCode(result.Data.Secret, step-1)); err != nil {
				t.Fatal(err)
			}
			pending, err := beginLoginVerification(user, "127.0.0.1")
			if err != nil || len(pending["methods"].([]string)) != 2 {
				t.Fatalf("expected both methods: %v", err)
			}
			id := pending["challenge_id"].(string)
			var emailCode string
			challenge, err := currentSecurityRuntime().verification.Store.Issue(verification.IssueRequest{Purpose: verification.PurposeLogin, Account: user.Account, Email: "owner@example.net"}, func(_ verification.Challenge, code string) error { emailCode = code; return nil })
			if err != nil {
				t.Fatal(err)
			}
			loginVerificationState.Lock()
			item := loginVerificationState.items[id]
			item.EmailChallenge, item.DeliveryStatus = challenge.ID, "sent"
			loginVerificationState.items[id] = item
			loginVerificationState.Unlock()
			var successes atomic.Int32
			var wg sync.WaitGroup
			for _, method := range []string{"email", "totp"} {
				wg.Add(1)
				go func(method string) {
					defer wg.Done()
					code := emailCode
					if method == "totp" {
						code = testTOTPCode(result.Data.Secret, step)
					}
					errorNo, _ := callLoginHandler(t, CompleteLoginVerification, loginVerificationRequest{ID: id, Account: user.Account, Method: method, Code: code})
					if errorNo == 0 {
						successes.Add(1)
					}
				}(method)
			}
			wg.Wait()
			if successes.Load() != 1 {
				t.Fatalf("expected one login, got %d", successes.Load())
			}
			if _, ok := pendingLoginVerification(id, user.Account); ok {
				t.Fatal("successful challenge retained")
			}
			// 新的登录流程必须能独立使用 TOTP，且旧动态码不能重放。
			pending, err = beginLoginVerification(user, "127.0.0.1")
			if err != nil {
				t.Fatal(err)
			}
			input := loginVerificationRequest{ID: pending["challenge_id"].(string), Account: user.Account, Method: "totp", Code: testTOTPCode(result.Data.Secret, step+1)}
			if code, _ := callLoginHandler(t, CompleteLoginVerification, input); code != 0 {
				t.Fatal("TOTP login failed")
			}
			pending, _ = beginLoginVerification(user, "127.0.0.1")
			input.ID = pending["challenge_id"].(string)
			if code, _ := callLoginHandler(t, CompleteLoginVerification, input); code == 0 {
				t.Fatal("TOTP replay accepted")
			}
		})
	}
}

func TestEmailLoginAfterAsyncDelivery(t *testing.T) {
	codes := make(chan string, 1)
	user, _ := setupLoginMethods(t, func(_ verification.Challenge, code string) error {
		codes <- code
		return nil
	})
	pending, err := beginLoginVerification(user, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	id := pending["challenge_id"].(string)
	input := resendLoginVerificationRequest{ID: id, Account: user.Account}
	if code, _ := callLoginHandler(t, ResendLoginVerification, input); code != 0 {
		t.Fatal("failed to start delivery")
	}
	var emailCode string
	select {
	case emailCode = <-codes:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery did not run")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		code, data := callLoginHandler(t, LoginVerificationStatus, input)
		if code != 0 {
			t.Fatal("status failed")
		}
		if data["delivery_status"] == "sent" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("delivery status never became sent")
		}
		time.Sleep(time.Millisecond)
	}
	verify := loginVerificationRequest{ID: id, Account: user.Account, Method: "email", Code: emailCode}
	if code, _ := callLoginHandler(t, CompleteLoginVerification, verify); code != 0 {
		t.Fatal("delivered email code did not complete login")
	}
	if code, _ := callLoginHandler(t, CompleteLoginVerification, verify); code == 0 {
		t.Fatal("consumed code accepted again")
	}
	if _, err := currentSecurityRuntime().verification.Issue(verification.IssueRequest{
		Purpose: verification.PurposePasswordChange, Account: user.Account, Email: "owner@example.net",
	}, "127.0.0.1"); err != nil {
		t.Fatalf("登录完成后其他业务无法发送验证码：%v", err)
	}
}

func TestLoginMethodsRejectInvalidState(t *testing.T) {
	user, _ := setupLoginMethods(t, func(verification.Challenge, string) error { return nil })
	pending, _ := beginLoginVerification(user, "127.0.0.1")
	id := pending["challenge_id"].(string)
	for i := 0; i < 5; i++ {
		if code, _ := callLoginHandler(t, CompleteLoginVerification, loginVerificationRequest{ID: id, Account: user.Account, Method: "totp", Code: "123456"}); code == 0 {
			t.Fatal("disabled TOTP accepted")
		}
	}
	if _, ok := pendingLoginVerification(id, user.Account); ok {
		t.Fatal("five failed attempts did not expire login")
	}
	pending, _ = beginLoginVerification(user, "127.0.0.1")
	id = pending["challenge_id"].(string)
	user.SessionVersion++
	if _, err := db.Instance.ID(user.ID).Cols("session_version").Update(user); err != nil {
		t.Fatal(err)
	}
	if code, _ := callLoginHandler(t, ResendLoginVerification, resendLoginVerificationRequest{ID: id, Account: user.Account}); code == 0 {
		t.Fatal("stale session version sent mail")
	}
	loginVerificationState.Lock()
	item := loginVerificationState.items[id]
	item.ExpiresAt = time.Now().Add(-time.Second)
	loginVerificationState.items[id] = item
	loginVerificationState.Unlock()
	if consumePendingLoginVerification(id, item) {
		t.Fatal("expired login consumed")
	}
}
