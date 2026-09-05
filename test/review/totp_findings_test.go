package totp

import (
	"sync"
	"testing"
)

// 同一用户并发打开绑定流程，检查返回密钥与旧登记清理之间的数据竞争。
func TestAuditConcurrentEnrollmentMustNotRaceSecretEncoding(t *testing.T) {
	service, engine := newTestService(t)
	engine.SetMaxOpenConns(1)
	var workers sync.WaitGroup
	for index := 0; index < 16; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for attempt := 0; attempt < 150; attempt++ {
				if _, err := service.BeginEnrollment(1, "audit-user"); err != nil {
					t.Errorf("并发初始化认证器失败: %v", err)
					return
				}
			}
		}()
	}
	workers.Wait()
	t.Log("concurrent_enrollment_requests=2400")
}
