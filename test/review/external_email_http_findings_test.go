package http_server

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/verification"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/utils/csrf"
	"github.com/alexedwards/scs/v2"
	"xorm.io/xorm"
)

type auditExternalHTTPFixture struct {
	engine *xorm.Engine
	user models.User
	repository *externalemail.Repository
	server *httptest.Server
	client *http.Client
	csrfToken string
	issued verification.Challenge
	deliveredCode string
}

// 仅测试专用入口签发真实会话；被审查的路由、会话中间件、CSRF 和数据库均不替换。
func newAuditExternalHTTPFixture(t *testing.T, age time.Duration) *auditExternalHTTPFixture {
	t.Helper()
	oldConfig, oldInit, oldDevelopment := config.Instance, config.IsInit, config.DevelopmentMode
	oldDB, oldSession, oldManager := db.Instance, session.Instance, config.CurrentManager()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "external-http.db"))
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() {
		controllers.ConfigureSecurityRuntime(nil, nil, nil, nil)
		config.Instance, config.DevelopmentMode = oldConfig, oldDevelopment
		config.SetInitState(oldInit)
		config.SetRuntimeManager(oldManager)
		db.Instance, session.Instance = oldDB, oldSession
		_ = engine.Close()
	})
	for _, model := range []any{&models.User{}, &models.UserEmail{}, &models.Email{}, &models.ExternalEmail{}, &models.RecoveryCode{}} {
		if err := engine.Sync2(model); err != nil { t.Fatal(err) }
	}
	fixture := &auditExternalHTTPFixture{engine: engine, user: models.User{
		Account: "audit-user", Name: "Audit", Role: "user", SessionVersion: 1, SecurityVersion: 1,
	}}
	if _, err := engine.Insert(&fixture.user); err != nil { t.Fatal(err) }
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{Domain: "mail.example.test", WebDomain: "127.0.0.1"}
	config.DevelopmentMode = false
	config.SetInitState(true)
	db.Instance = engine
	session.Instance = scs.New()
	session.Instance.Cookie.Secure = true
	vault := externalemail.NewVaultWithKeys([]byte("01234567890123456789012345678901"), []byte("abcdefabcdefabcdefabcdefabcdefab"))
	fixture.repository, err = externalemail.NewRepository(engine, vault)
	if err != nil { t.Fatal(err) }
	store, err := verification.NewStore(32)
	if err != nil { t.Fatal(err) }
	service, err := verification.NewService(store, func(challenge verification.Challenge, code string) error {
		fixture.issued, fixture.deliveredCode = challenge, code
		return nil
	}, nil)
	if err != nil { t.Fatal(err) }
	controllers.ConfigureSecurityRuntime(service, fixture.repository, nil, nil)
	mux := http.NewServeMux()
	if err := router(mux); err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("/audit/session", func(w http.ResponseWriter, r *http.Request) {
		if err := session.PutUser(r.Context(), &fixture.user); err != nil { http.Error(w, "session", 500); return }
		identity, err := session.GetUser(r.Context())
		if err != nil { http.Error(w, "identity", 500); return }
		identity.AuthenticatedAt = time.Now().Add(-age).Unix()
		encoded, err := json.Marshal(identity)
		if err != nil { http.Error(w, "encoding", 500); return }
		session.Instance.Put(r.Context(), "user", string(encoded))
		token, err := csrf.Issue(w, r, true)
		if err != nil { http.Error(w, "csrf", 500); return }
		_ = json.NewEncoder(w).Encode(map[string]any{"token": token, "recent": identity.HasRecentAuthentication(time.Now())})
	})
	fixture.server = httptest.NewTLSServer(session.Instance.LoadAndSave(mux))
	t.Cleanup(fixture.server.Close)
	fixture.client = fixture.server.Client()
	fixture.client.Jar, err = cookiejar.New(nil)
	if err != nil { t.Fatal(err) }
	seed, err := fixture.client.Get(fixture.server.URL + "/audit/session")
	if err != nil { t.Fatal(err) }
	defer seed.Body.Close()
	var seeded struct { Token string `json:"token"`; Recent bool `json:"recent"` }
	if err := json.NewDecoder(seed.Body).Decode(&seeded); err != nil { t.Fatal(err) }
	if seeded.Recent != (age < 10*time.Minute) || seeded.Token == "" { t.Fatal("会话近期认证前置条件不符") }
	fixture.csrfToken = seeded.Token
	return fixture
}

func (fixture *auditExternalHTTPFixture) post(t *testing.T, path string, input any, withCSRF bool) response.Response {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil { t.Fatal(err) }
	request, err := http.NewRequest(http.MethodPost, fixture.server.URL+path, strings.NewReader(string(body)))
	if err != nil { t.Fatal(err) }
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", fixture.server.URL)
	if withCSRF { request.Header.Set(csrf.HeaderName, fixture.csrfToken) }
	result, err := fixture.client.Do(request)
	if err != nil { t.Fatal(err) }
	defer result.Body.Close()
	var decoded response.Response
	if err := json.NewDecoder(result.Body).Decode(&decoded); err != nil { t.Fatal(err) }
	return decoded
}

// 近期会话为正向对照，过期近期认证的会话仍登录有效，但不应能新增恢复凭据。
func TestAuditExternalBindingMustRequireRecentAuthentication(t *testing.T) {
	for _, age := range []time.Duration{time.Minute, 11*time.Minute} {
		t.Run(age.String(), func(t *testing.T) {
			fixture := newAuditExternalHTTPFixture(t, age)
			input := map[string]any{"email": "external@example.com", "type": "primary"}
			blocked := fixture.post(t, "/api/v1/security/external-email/bind", input, false)
			if blocked.ErrorNo == 0 || fixture.issued.ID != "" { t.Fatal("缺少 CSRF 的对照请求未被拒绝") }
			bound := fixture.post(t, "/api/v1/security/external-email/bind", input, true)
			if bound.ErrorNo != 0 {
				if age < 10*time.Minute { t.Fatalf("近期会话绑定对照失败: %d", bound.ErrorNo) }
				return
			}
			if fixture.issued.ID == "" || fixture.deliveredCode == "" { t.Fatal("成功绑定未签发挑战") }
			verified := fixture.post(t, "/api/v1/security/external-email/verify", map[string]any{
				"id": fixture.issued.ID, "binding_id": fixture.issued.ResourceID,
				"email": input["email"], "code": fixture.deliveredCode,
			}, true)
			stored, err := fixture.repository.FindVerifiedPrimaryByUser(fixture.user.ID)
			t.Logf("session_age=%s missing_csrf_error=%d bind_error=%d verify_error=%d verified_binding=%v", age, blocked.ErrorNo, bound.ErrorNo, verified.ErrorNo, err == nil && stored != nil)
			if verified.ErrorNo != 0 || err != nil { t.Fatal("绑定成功后的验证前置条件未成立") }
			if age >= 10*time.Minute { t.Fatal("已失去近期认证的会话仍能添加并验证外部安全邮箱") }
		})
	}
}

// 真实主邮箱绑定可被认证解析，不能再被当作独立的外部邮箱绑定和验证。
func TestAuditExternalBindingMustRejectOwnPrimaryMailbox(t *testing.T) {
	fixture := newAuditExternalHTTPFixture(t, time.Minute)
	address, now := fixture.user.Account+"@mail.example.test", time.Now().UTC()
	primary := &models.UserEmail{UserID: fixture.user.ID, Address: address, AddressCanonical: &address, IsPrimary: 1, VerifiedAt: &now}
	if _, err := fixture.engine.Insert(primary); err != nil { t.Fatal(err) }
	identity, err := auth.ResolveIdentifier(address)
	if err != nil || identity.UserID != fixture.user.ID { t.Fatal("主邮箱真实身份解析前置条件未成立") }
	bound := fixture.post(t, "/api/v1/security/external-email/bind", map[string]any{"email": address, "type": "primary"}, true)
	if bound.ErrorNo != 0 { return }
	verified := fixture.post(t, "/api/v1/security/external-email/verify", map[string]any{
		"id": fixture.issued.ID, "binding_id": fixture.issued.ResourceID,
		"email": address, "code": fixture.deliveredCode,
	}, true)
	stored, err := fixture.repository.FindVerifiedPrimaryByUser(fixture.user.ID)
	t.Logf("primary_identity_resolved=true sent_to_own_mailbox=%v bind_error=%d verify_error=%d verified_primary_external=%v", fixture.issued.Email == address, bound.ErrorNo, verified.ErrorNo, err == nil && stored != nil)
	if err != nil || verified.ErrorNo != 0 { t.Fatal("自身邮箱绑定成功但验证前置条件未成立") }
	t.Fatal("已确认的 Smail 主邮箱仍可绑定为主要外部邮箱，验证码发往同一邮箱")
}
