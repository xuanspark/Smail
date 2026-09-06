package credentials

import (
	"errors"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
)

func TestProtocolClientSwitchesIsolateUsersAndProtocols(t *testing.T) {
	service := newFixture(t)
	other := models.User{Account: "bob", Name: "Bob", Password: "unused", Role: "user"}
	if _, err := service.engine.Insert(&other); err != nil {
		t.Fatal(err)
	}
	codes := map[int]map[string]string{}
	for _, userID := range []int{1, other.ID} {
		statuses, err := service.ListProtocolCredentials(userID)
		if err != nil || len(statuses) != 3 {
			t.Fatalf("初始协议状态不完整：%v", err)
		}
		codes[userID] = map[string]string{}
		for _, status := range statuses {
			if status.Enabled || status.Configured {
				t.Fatal("新账号不应自动获得邮件客户端访问权限")
			}
			if err := service.SetProtocolEnabled(userID, status.Protocol, true); err != nil {
				t.Fatal(err)
			}
			code, err := service.GenerateProtocolCode(userID, status.Protocol)
			if err != nil {
				t.Fatal(err)
			}
			codes[userID][status.Protocol] = code
		}
	}
	for _, protocol := range SupportedProtocols() {
		t.Run(protocol, func(t *testing.T) {
			code := codes[1][protocol]
			if !service.VerifyProtocolCode(1, protocol, code) {
				t.Fatal("开启状态下有效授权码被拒绝")
			}
			if err := service.SetProtocolEnabled(1, protocol, false); err != nil {
				t.Fatal(err)
			}
			if service.VerifyProtocolCode(1, protocol, code) {
				t.Fatal("关闭协议后仍能通过授权码认证")
			}
			if _, err := service.GenerateProtocolCode(1, protocol); !errors.Is(err, ErrProtocolDisabled) {
				t.Fatalf("关闭协议后仍能生成授权码：%v", err)
			}
			for _, otherProtocol := range SupportedProtocols() {
				if otherProtocol != protocol && !service.VerifyProtocolCode(1, otherProtocol, codes[1][otherProtocol]) {
					t.Fatal("关闭一个协议影响了当前用户的其他协议")
				}
				if !service.VerifyProtocolCode(other.ID, otherProtocol, codes[other.ID][otherProtocol]) {
					t.Fatal("关闭协议影响了其他用户")
				}
			}
			if err := service.SetProtocolEnabled(1, protocol, true); err != nil {
				t.Fatal(err)
			}
			if !service.VerifyProtocolCode(1, protocol, code) {
				t.Fatal("重新开启后未撤销的授权码应恢复认证")
			}
			if service.VerifyProtocolCode(other.ID, protocol, code) {
				t.Fatal("授权码可被其他账号使用")
			}
		})
	}
}

func TestProtocolClientRejectsChangesDuringHashVerification(t *testing.T) {
	for _, change := range []string{"disable", "revoke", "rotate", "database_error"} {
		t.Run(change, func(t *testing.T) {
			service := newFixture(t)
			if err := service.SetProtocolEnabled(1, ProtocolSMTP, true); err != nil {
				t.Fatal(err)
			}
			code, err := service.GenerateProtocolCode(1, ProtocolSMTP)
			if err != nil {
				t.Fatal(err)
			}
			writer, err := NewService(service.engine)
			if err != nil {
				t.Fatal(err)
			}
			// 在哈希匹配后、认证提交前模拟另一个请求写入，稳定覆盖竞态而不依赖休眠。
			changed := false
			service.now = func() time.Time {
				changed = true
				var err error
				switch change {
				case "disable":
					err = writer.SetProtocolEnabled(1, ProtocolSMTP, false)
				case "revoke":
					err = writer.RevokeProtocolCode(1, ProtocolSMTP)
				case "rotate":
					_, err = writer.GenerateProtocolCode(1, ProtocolSMTP)
				case "database_error":
					err = service.engine.Close()
				}
				if err != nil {
					t.Fatal(err)
				}
				return time.Now()
			}
			if service.VerifyProtocolCode(1, ProtocolSMTP, code) {
				t.Fatal("凭据状态变化或数据库写入失败后，过期认证仍返回成功")
			}
			if !changed {
				t.Fatal("未进入哈希计算后的竞态验证位置")
			}
		})
	}
}
