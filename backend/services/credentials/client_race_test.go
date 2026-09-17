package credentials

import (
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/password"
)

func TestClientCodeRejectsStateChangedDuringVerification(t *testing.T) {
	for _, change := range []string{"revoke", "rotate"} {
		t.Run(change, func(t *testing.T) {
			service := newFixture(t)
			if err := service.engine.Sync2(&models.ClientCredential{}); err != nil {
				t.Fatal(err)
			}
			code, item, err := service.CreateClientCredential(1, "撤销竞态验收")
			if err != nil {
				t.Fatal(err)
			}
			const replacement = "ABCD1234EFGH5678"
			hash, err := password.Hash(replacement)
			if err != nil {
				t.Fatal(err)
			}
			now := service.now
			changed := false
			// 在哈希匹配之后、更新最后使用时间之前注入已完成的撤销或换码。
			service.now = func() time.Time {
				service.now = now
				changed = true
				if change == "revoke" {
					if err := service.RevokeClientCredential(1, item.ID); err != nil {
						t.Fatal(err)
					}
				} else if _, err := service.engine.ID(item.ID).Cols("code_hash", "code_version").Update(&models.ClientCredential{CodeHash: hash, CodeVersion: item.CodeVersion + 1}); err != nil {
					t.Fatal(err)
				}
				return now()
			}
			if matched, err := service.CheckClientOrProtocolCode(1, ProtocolSMTP, code); matched || err != nil || !changed {
				t.Fatalf("已失效授权码通过认证：匹配=%v，状态已变=%v，错误=%v", matched, changed, err)
			}
			if change == "rotate" {
				if matched, err := service.CheckClientOrProtocolCode(1, ProtocolSMTP, replacement); !matched || err != nil {
					t.Fatalf("新授权码不可用：%v", err)
				}
			}
		})
	}
}
