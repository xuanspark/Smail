package http_server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/session"
	"github.com/alexedwards/scs/v2"
)

// 审查专用：使用内存会话和真实路由，不发送验证码。
func TestAuditAnonymousLoginResendMustReachController(t *testing.T) {
	previousConfig, previousInit := config.Instance, config.IsInit
	previousSession, previousManager := session.Instance, config.CurrentManager()
	t.Cleanup(func() {
		config.Instance = previousConfig
		config.SetInitState(previousInit)
		session.Instance = previousSession
		config.SetRuntimeManager(previousManager)
	})
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{Domain: "localhost"}
	config.SetInitState(true)
	session.Instance = scs.New()
	mux := http.NewServeMux()
	registerV1Routes(mux)
	request := httptest.NewRequest(http.MethodPost, "https://localhost/api/v1/security/login/resend", strings.NewReader(`{"id":"audit-invalid","account":"audit"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	session.Instance.LoadAndSave(mux).ServeHTTP(recorder, request)
	var result response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	t.Logf("HTTP=%d body=%s", recorder.Code, recorder.Body.String())
	if result.ErrorNo == response.NeedLogin {
		t.Fatal("尚未完成二次验证的用户无法调用验证码重发接口")
	}
}
