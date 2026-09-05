package controllers

import (
	"sync"
	"testing"
)

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
