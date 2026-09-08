package logstore

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/textproto"
	"strings"
	"testing"
)

func TestMailLogKeepsTriggerStageAndOutcomeWithoutRemoteSecrets(t *testing.T) {
	s := testStore(t)
	runtime := Start(s)
	defer runtime.Stop()
	for _, item := range []struct{ result, trigger, stage string }{
		{"accepted", "网页写信", "对外投递"},
		{"partial", "邮件规则转发", "对外投递"},
		{"failure", "SMTP 收信", "解析邮件"},
		{"discarded", "SMTP 收信", "收信策略"},
	} {
		MailResult(42, "outbound", "sender@example.com", 3, 100, item.result,
			MailDetails{Trigger: item.trigger, Stage: item.stage, AcceptedCount: 1})
		entries, err := s.Query(context.Background(), Query{Kind: "mail", Limit: 1})
		if err != nil || len(entries) != 1 {
			t.Fatal(err)
		}
		var payload map[string]any
		if json.Unmarshal(entries[0].Payload, &payload) != nil {
			t.Fatal("无效邮件载荷")
		}
		if entries[0].Result != item.result || payload["trigger"] != item.trigger || payload["stage"] != item.stage || payload["outcome"] == "" || payload["acceptedCount"] != float64(1) {
			t.Fatalf("缺少操作或结果：%+v", payload)
		}
	}
	for _, cause := range []error{
		errors.New("private-token"), &textproto.Error{Code: 550, Msg: "private-token"},
		&net.DNSError{Err: "private-token", Name: "private-domain", IsNotFound: true},
	} {
		if reason := MailFailureReason(cause); reason == "" || strings.Contains(reason, "private-") {
			t.Fatal("远端敏感内容泄露")
		}
	}
}
