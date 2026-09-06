package controllers

import (
	"sync"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/recovery"
	"git.pcux.cn/xuans/Smail/services/verification"
	webauthnservice "git.pcux.cn/xuans/Smail/services/webauthn"
)

func TestAuditClearSecurityRuntimeRemovesEveryPublishedService(t *testing.T) {
	previous := currentSecurityRuntime()
	t.Cleanup(func() {
		ConfigureSecurityRuntime(previous.verification, previous.external, previous.recovery, previous.credentials)
		ConfigureWebAuthnRuntime(previous.webauthn)
	})
	ConfigureSecurityRuntime(&verification.Service{}, &externalemail.Repository{}, &recovery.Service{}, &credentials.Service{})
	ConfigureWebAuthnRuntime(&webauthnservice.Service{})
	loginVerificationState.Lock()
	loginVerificationState.items["audit-stop"] = loginVerificationItem{ExpiresAt: time.Now().Add(time.Minute)}
	loginVerificationState.Unlock()
	ClearSecurityRuntime()
	ClearSecurityRuntime()
	if currentSecurityRuntime() != (securityRuntimeState{}) {
		t.Fatal("安全服务必须整批撤下")
	}
	loginVerificationState.Lock()
	defer loginVerificationState.Unlock()
	if len(loginVerificationState.items) != 0 {
		t.Fatal("停止后不能保留旧登录验证挑战")
	}
}

func TestAuditSecurityRuntimeSnapshotPublication(t *testing.T) {
	previous := currentSecurityRuntime()
	t.Cleanup(func() {
		ConfigureSecurityRuntime(previous.verification, previous.external, previous.recovery, previous.credentials)
		ConfigureWebAuthnRuntime(previous.webauthn)
	})
	first := securityRuntimeState{
		verification: &verification.Service{}, external: &externalemail.Repository{},
		recovery: &recovery.Service{}, credentials: &credentials.Service{},
	}
	second := securityRuntimeState{
		verification: &verification.Service{}, external: &externalemail.Repository{},
		recovery: &recovery.Service{}, credentials: &credentials.Service{},
	}
	key := &webauthnservice.Service{}
	ConfigureSecurityRuntime(first.verification, first.external, first.recovery, first.credentials)
	ConfigureWebAuthnRuntime(key)
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func(writer bool) {
			defer workers.Done()
			for index := 0; index < 1000; index++ {
				if writer {
					next := first
					if index%2 == 0 {
						next = second
					}
					ConfigureSecurityRuntime(next.verification, next.external, next.recovery, next.credentials)
					continue
				}
				snapshot := currentSecurityRuntime()
				if snapshot.webauthn != key {
					t.Error("安全服务发布不能清空独立配置的安全密钥服务")
					return
				}
				snapshot.webauthn = nil
				if snapshot != first && snapshot != second {
					t.Error("一次读取不能混合不同发布批次的服务指针")
					return
				}
			}
		}(worker%2 == 0)
	}
	workers.Wait()
}

// 并发模拟停止阶段清空安全服务与在途控制器获取服务；必须以 -race 运行。
func TestAuditWebAuthnRuntimePublication(t *testing.T) {
	previous := webAuthnRuntime()
	t.Cleanup(func() { ConfigureWebAuthnRuntime(previous) })
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		for index := 0; index < 1000; index++ {
			ConfigureWebAuthnRuntime(nil)
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for index := 0; index < 1000; index++ {
			_ = webAuthnRuntime()
		}
	}()
	close(start)
	workers.Wait()
}
