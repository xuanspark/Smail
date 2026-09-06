package res_init

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/recovery"
	"git.pcux.cn/xuans/Smail/services/verification"
	webauthnservice "git.pcux.cn/xuans/Smail/services/webauthn"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func TestAuditStopVerificationWithdrawsPublishedServices(t *testing.T) {
	for _, active := range []bool{false, true} {
		name := "without_cleanup_runtime"
		if active {
			name = "with_cleanup_runtime"
		}
		t.Run(name, func(t *testing.T) {
			if active {
				runtime, err := verification.NewRuntime(8)
				if err != nil {
					t.Fatal(err)
				}
				verificationRuntime = runtime
				t.Cleanup(runtime.Stop)
			}
			credentialService = &credentials.Service{}
			webAuthnRuntimeService = &webauthnservice.Service{}
			controllers.ConfigureSecurityRuntime(&verification.Service{}, &externalemail.Repository{}, &recovery.Service{}, credentialService)
			controllers.ConfigureWebAuthnRuntime(webAuthnRuntimeService)
			t.Cleanup(func() {
				controllers.ConfigureSecurityRuntime(nil, nil, nil, nil)
				controllers.ConfigureWebAuthnRuntime(nil)
				verificationRuntime, credentialService, webAuthnRuntimeService = nil, nil, nil
			})
			stopVerificationRuntime()
			stopVerificationRuntime()
			if verificationRuntime != nil || credentialService != nil || webAuthnRuntimeService != nil {
				t.Fatal("停止必须清除全部运行时引用，且可以重复调用")
			}
			// 已停止的仓储不能继续进入服务调用，必须在控制器入口拒绝。
			writer := httptest.NewRecorder()
			controllers.ListExternalEmails(&appcontext.Context{UserID: 1}, writer, httptest.NewRequest("GET", "/", nil))
			var result response.Response
			if err := json.Unmarshal(writer.Body.Bytes(), &result); err != nil || result.ErrorNo != response.NeedLogin {
				t.Fatalf("停止后的控制器没有拒绝旧服务访问：error=%v code=%d", err, result.ErrorNo)
			}
		})
	}
}
