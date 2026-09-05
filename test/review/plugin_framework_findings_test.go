package framework

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuditSendAfterFailureMustReachPlugin(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "success"
		failure := error(nil)
		if failed {
			name = "delivery-failed"
			failure = errors.New("audit delivery rejected")
		}
		t.Run(name, func(t *testing.T) {
			payload := HookDTO{ErrMap: map[string]error{"example.invalid": failure}}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			plugin := &Plugin{name: "audit"}
			called := false
			received := ""
			handler := plugin.handleHookDTO("SendAfter", func(dto *HookDTO) {
				called = true
				if dto.ErrMap["example.invalid"] != nil {
					received = dto.ErrMap["example.invalid"].Error()
				}
			})
			recorder := httptest.NewRecorder()
			handler(recorder, httptest.NewRequest(http.MethodPost, "/SendAfter", bytes.NewReader(body)))
			t.Logf("failed=%v status=%d callback=%v preserved_error=%v", failed, recorder.Code, called, received == "audit delivery rejected")
			if recorder.Code != http.StatusOK || !called || (failed && received != "audit delivery rejected") {
				t.Error("发送失败时插件回调必须收到可序列化的错误信息")
			}
		})
	}
}
