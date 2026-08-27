package verification

import (
	"errors"
	"sync"
	"time"
)

var ErrCleanupAlreadyStarted = errors.New("verification cleanup already started")

type CleanupRunner struct {
	store *Store
	once  sync.Once
	mu    sync.Mutex
	stop  chan struct{}
	done  chan struct{}
}

func NewCleanupRunner(store *Store) *CleanupRunner {
	return &CleanupRunner{store: store}
}

// Start 启动单实例过期清理器；验证码不会从持久化介质恢复。
func (r *CleanupRunner) Start(interval time.Duration) error {
	if r == nil || r.store == nil || interval <= 0 {
		return ErrInvalidChallenge
	}
	started := false
	r.once.Do(func() {
		started = true
		r.mu.Lock()
		r.stop = make(chan struct{})
		r.done = make(chan struct{})
		stop, done := r.stop, r.done
		r.mu.Unlock()
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			defer close(done)
			for {
				select {
				case <-ticker.C:
					r.store.Cleanup()
				case <-stop:
					return
				}
			}
		}()
	})
	if !started {
		return ErrCleanupAlreadyStarted
	}
	return nil
}

func (r *CleanupRunner) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	stop, done := r.stop, r.done
	r.stop, r.done = nil, nil
	r.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}
