package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

func TestAuditWeChatBusinessResponses(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		clear      bool
	}{
		{"success", `{"errcode":0}`, 200, false},
		{"expired", `{"errcode":42001,"errmsg":"private-body"}`, 200, true},
		{"invalid", `{"errcode":40001}`, 200, true},
		{"revoked", `{"errcode":40014}`, 200, true},
		{"business", `{"errcode":43004,"errmsg":"private-body"}`, 200, false},
		{"missing", `{}`, 200, false},
		{"malformed", `not-json`, 200, false},
		{"oversize", strings.Repeat("x", maxWeChatResponseBytes+1), 200, false},
		{"http", `private-body`, 500, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := log.StandardLogger()
			old := logger.Out
			logger.SetOutput(&output)
			defer logger.SetOutput(old)
			calls := 0
			hook := &WeChatPushHook{token: "private-token", tokenExpires: time.Now().Add(time.Hour).Unix()}
			hook.httpClient = &http.Client{Transport: weChatRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})}
			hook.sendUserMsg(&appcontext.Context{}, "private-recipient", "private-message")
			if calls != 1 || (hook.token == "") != test.clear {
				t.Fatalf("响应处理错误: calls=%d cleared=%v", calls, hook.token == "")
			}
			if test.name != "success" && output.Len() == 0 {
				t.Fatal("失败响应未记录")
			}
			if strings.Contains(output.String(), "private-") {
				t.Fatal("日志泄露响应或凭据")
			}
		})
	}
}
