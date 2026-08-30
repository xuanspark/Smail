package verification

import (
	"errors"
	"sync"
	"time"
)

var ErrCleanupAlreadyStarted = errors.New("verification cleanup already started")

type CleanupRunner struct {
	store    *Store
	mu       sync.Mutex
	stop     chan struct{}
	done     chan struct{}
	stopping bool
}

func NewCleanupRunner(store *Store) *CleanupRunner {
	return &CleanupRunner{store: store}
}

// Start 启动单实例过期清理器；验证码不会从持久化介质恢复。
func (r *CleanupRunner) Start(interval time.Duration) error {
	if r == nil || r.store == nil || interval <= 0 {
		return ErrInvalidChallenge
	}
	r.mu.Lock()
	if r.stop != nil || r.stopping {
		r.mu.Unlock()
		return ErrCleanupAlreadyStarted
	}
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
	return nil
}

func (r *CleanupRunner) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	stop, done := r.stop, r.done
	if stop == nil || r.stopping {
		r.mu.Unlock()
		return
	}
	r.stopping = true
	r.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
	r.mu.Lock()
	r.stop, r.done, r.stopping = nil, nil, false
	r.mu.Unlock()
}
