package credentials

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestContextMutexKeepsMutualExclusion(t *testing.T) {
	var mutex contextMutex
	var group sync.WaitGroup
	count := 0
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for j := 0; j < 100; j++ {
				mutex.Lock()
				count++
				mutex.Unlock()
			}
		}()
	}
	group.Wait()
	if count != 1200 {
		t.Fatal("并发写入失去互斥保护")
	}
}

func TestContextMutexCancellationDoesNotLoseLock(t *testing.T) {
	var mutex contextMutex
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(mutex.LockContext(cancelled), context.Canceled) {
		t.Fatal("已取消请求获取了锁")
	}
	mutex.Lock()
	ctx, cancelWait := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelWait()
	if !errors.Is(mutex.LockContext(ctx), context.DeadlineExceeded) {
		t.Fatal("等待锁未响应截止时间")
	}
	mutex.Unlock()
	if err := mutex.LockContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	mutex.Unlock()
}

func TestContextMutexInvalidUnlockPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("未持有锁的解锁没有报错")
		}
	}()
	var mutex contextMutex
	mutex.Unlock()
}
