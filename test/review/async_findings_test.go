package async

import (
	"sync"
	"testing"
	"time"
)

// TestAuditAsyncLastErrorMustBeSafeForConcurrentAccess 验证异步错误状态的并发访问安全性。
func TestAuditAsyncLastErrorMustBeSafeForConcurrentAccess(t *testing.T) {
	process := New(nil)
	const workers = 32
	const rounds = 200

	var started sync.WaitGroup
	started.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			started.Done()
			for round := 0; round < rounds; round++ {
				process.Process(func(any) { panic("audit panic") }, nil)
				_ = process.LastError()
			}
		}()
	}
	started.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if process.LastError() != nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("异步任务未记录 panic 错误")
}
