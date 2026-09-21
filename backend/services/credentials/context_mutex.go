package credentials

import (
	"context"
	"sync"
)

// contextMutex 保留凭据写入的互斥顺序，同时允许等待中的请求及时取消。
// 零值可用，初始化后不得复制。
type contextMutex struct {
	once sync.Once
	slot chan struct{}
}

func (m *contextMutex) LockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.once.Do(func() { m.slot = make(chan struct{}, 1) })
	select {
	case m.slot <- struct{}{}:
		if err := ctx.Err(); err != nil {
			m.Unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *contextMutex) Lock() {
	_ = m.LockContext(context.Background())
}

func (m *contextMutex) Unlock() {
	select {
	case <-m.slot:
	default:
		panic("unlock of unlocked credential mutex")
	}
}
