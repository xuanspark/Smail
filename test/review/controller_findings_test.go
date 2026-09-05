package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/verification"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

// 真实控制器约束对照：输错后绑定仍待验证，重新绑定失败，但原挑战可继续验证。
func TestAuditPendingExternalBindingRequiresExistingChallenge(t *testing.T) {
	_, _, engine, user, _ := newPublicSecurityFixture(t)
	previous := securityRuntime
	t.Cleanup(func() { securityRuntime = previous })
	vault := externalemail.NewVaultWithKeys([]byte("01234567890123456789012345678901"), []byte("abcdefabcdefabcdefabcdefabcdefab"))
	repo, err := externalemail.NewRepository(engine, vault)
	if err != nil {
		t.Fatal(err)
	}
	store, err := verification.NewStore(32)
	if err != nil {
		t.Fatal(err)
	}
	var issued verification.Challenge
	var deliveredCode string
	service, err := verification.NewService(store, func(challenge verification.Challenge, code string) error {
		issued, deliveredCode = challenge, code
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	securityRuntime.external, securityRuntime.verification = repo, service
	ctx := &appcontext.Context{UserID: user.ID, UserAccount: user.Account, Role: access.RoleUser, RecentAuth: true, Lang: "zhCn"}
	invoke := func(handler func(*appcontext.Context, http.ResponseWriter, *http.Request), input any) response.Response {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "https://example.com/api/v1/security/external-email", strings.NewReader(string(body)))
		request.RemoteAddr = "192.0.2.10:12345"
		handler(ctx, recorder, request)
		var result response.Response
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	bind := map[string]any{"email": "binding@example.com", "type": "backup"}
	if result := invoke(BindExternalEmail, bind); result.ErrorNo != 0 || issued.ID == "" || deliveredCode == "" {
		t.Fatalf("首次绑定前置条件未成立: error=%d", result.ErrorNo)
	}
	check := map[string]any{"id": issued.ID, "binding_id": issued.ResourceID, "email": "binding@example.com", "code": "incorrect"}
	wrong := invoke(VerifyExternalEmail, check)
	if wrong.ErrorNo == 0 {
		t.Fatal("错误验证码意外通过")
	}
	var stored models.ExternalEmail
	if has, err := engine.ID(issued.ResourceID).Get(&stored); err != nil || !has || stored.Status != models.ExternalEmailStatusPending {
		t.Fatalf("错误验证码后绑定状态异常: found=%v err=%v status=%s", has, err, stored.Status)
	}
	retry := invoke(BindExternalEmail, bind)
	if retry.ErrorNo == 0 {
		t.Fatal("重复绑定应被真实仓储拒绝，前端必须恢复原挑战")
	}
	check["code"] = deliveredCode
	continued := invoke(VerifyExternalEmail, check)
	if continued.ErrorNo != 0 {
		t.Fatalf("原挑战正确验证码未通过: error=%d", continued.ErrorNo)
	}
	t.Logf("after_wrong_status=%s retry_bind_error=%d continued_verify_error=%d", stored.Status, retry.ErrorNo, continued.ErrorNo)
}

// 使用临时数据库和真实控制器，正向验证授权器允许的角色调整能够到达持久化。
func TestAuditAuthorizedRoleChangeMustSucceed(t *testing.T) {
	_, _, engine, user, super := newPublicSecurityFixture(t)
	previousDB, previousRuntime := db.Instance, securityRuntime
	db.Instance = engine
	t.Cleanup(func() {
		db.Instance = previousDB
		securityRuntime = previousRuntime
	})
	credentialService, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	securityRuntime.credentials = credentialService
	if _, err := credentialService.GenerateRecoveryCodes(user.ID, 5); err != nil {
		t.Fatal(err)
	}
	request := access.Request{
		ActorRole: access.RoleSuperAdmin, Capability: access.CapabilityRoleManage,
		TargetRole: access.RoleUser, NewRole: access.RoleAdmin,
		RecentAuth: true, TargetRecoveryConfigured: true,
	}
	if err := access.Authorize(request); err != nil {
		t.Fatalf("角色变更授权前置条件未成立: %v", err)
	}
	ctx := &appcontext.Context{
		UserID: super.ID, UserAccount: super.Account, Role: access.RoleSuperAdmin,
		RecentAuth: true, Lang: "zhCn",
	}
	result := editUserRequest(t, ctx, `{"id":`+itoa(user.ID)+`,"role":"admin","disabled":0}`)
	var stored models.User
	if found, err := engine.ID(user.ID).Get(&stored); err != nil || !found {
		t.Fatalf("读取目标角色失败: found=%v err=%v", found, err)
	}
	t.Logf("policy_allows=true controller_error=%d stored_role=%s", result, stored.EffectiveRole())
	if result != 0 || stored.EffectiveRole() != access.RoleAdmin {
		t.Fatal("符合近期认证和目标恢复码条件的超级管理员仍无法修改用户角色")
	}
}

// 使用真实 TLS HTTP 客户端及标准 CookieJar 检查浏览器同样遵循的 Path 匹配。
// 只核对 Cookie 是否随请求携带，不把隔离发送器当成真实邮件投递验收。
func TestAuditRecoveryCookieMustReachVersionedAPI(t *testing.T) {
	for _, prefix := range []string{"/api/security/recovery", "/api/v1/security/recovery"} {
		t.Run(prefix, func(t *testing.T) {
			enablePublicRecoveryForTest(t)
			recover, _, _, _, _ := newPublicSecurityFixture(t)
			previous := securityRuntime
			securityRuntime.recovery = recover
			t.Cleanup(func() { securityRuntime = previous })
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/begin") {
					BeginPasswordRecovery(&appcontext.Context{}, w, r)
					return
				}
				_, ok := recoveryBrowserContext(r, w, false)
				_ = json.NewEncoder(w).Encode(map[string]bool{"context_received": ok})
			}))
			defer server.Close()
			client := server.Client()
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			client.Jar = jar
			begin, err := client.Post(server.URL+prefix+"/begin", "application/json", strings.NewReader(`{"identifier":"alice","email":"alice@example.com"}`))
			if err != nil {
				t.Fatal(err)
			}
			var result response.Response
			err = json.NewDecoder(begin.Body).Decode(&result)
			_ = begin.Body.Close()
			if err != nil || result.ErrorNo != 0 {
				t.Fatalf("恢复申请前置条件失败: code=%d err=%v", result.ErrorNo, err)
			}
			cookiePath := ""
			for _, cookie := range begin.Cookies() {
				if cookie.Name == recoveryContextCookieName {
					cookiePath = cookie.Path
				}
			}
			if cookiePath == "" {
				t.Fatal("恢复申请没有签发浏览器上下文 Cookie")
			}
			target, err := url.Parse(server.URL + prefix + "/verify")
			if err != nil {
				t.Fatal(err)
			}
			verify, err := client.Get(target.String())
			if err != nil {
				t.Fatal(err)
			}
			var received struct {
				ContextReceived bool `json:"context_received"`
			}
			err = json.NewDecoder(verify.Body).Decode(&received)
			_ = verify.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("request_prefix=%s cookie_path=%s sent_cookie_count=%d context_received=%v", prefix, cookiePath, len(jar.Cookies(target)), received.ContextReceived)
			if !received.ContextReceived {
				t.Fatal("恢复上下文 Cookie 的路径不覆盖前端使用的版本化接口，后续验证无法获取上下文")
			}
		})
	}
}
