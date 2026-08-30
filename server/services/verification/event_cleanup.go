package verification

import (
	"context"
	"sync"
	"time"
)

const DefaultOperationalCleanupInterval = time.Hour

type OperationalCleanupRunner struct {
	mu         sync.Mutex
	store      *EventStore
	rateLimits *PersistentRateLimiter
	interval   time.Duration
	once       sync.Once
	stop       chan struct{}
	done       chan struct{}
	now        func() time.Time
}

func NewOperationalCleanupRunner(store *EventStore, rateLimits ...*PersistentRateLimiter) *OperationalCleanupRunner {
	runner := &OperationalCleanupRunner{store: store, interval: DefaultOperationalCleanupInterval, now: time.Now}
	if len(rateLimits) > 0 {
		runner.rateLimits = rateLimits[0]
	}
	return runner
}

func (r *OperationalCleanupRunner) Start() {
	if r == nil || r.store == nil {
		return
	}
	r.once.Do(func() {
		r.mu.Lock()
		r.stop = make(chan struct{})
		r.done = make(chan struct{})
		stop, done := r.stop, r.done
		interval := r.interval
		r.mu.Unlock()

		go func() {
			defer close(done)
			r.runOnce()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					r.runOnce()
				case <-stop:
					return
				}
			}
		}()
	})
}

func (r *OperationalCleanupRunner) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	stop, done := r.stop, r.done
	if stop == nil {
		r.mu.Unlock()
		return
	}
	r.stop, r.done = nil, nil
	r.mu.Unlock()

	close(stop)
	<-done
}

func (r *OperationalCleanupRunner) runOnce() {
	if r == nil || r.store == nil {
		return
	}
	now := time.Now()
	if r.now != nil {
		now = r.now()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	lease, acquired, err := r.store.TryAcquireCleanupLease(ctx, now)
	if err != nil || !acquired {
		return
	}
	if _, err = r.store.Cleanup(ctx, now); err == nil && r.rateLimits != nil {
		_, err = r.rateLimits.Cleanup(ctx, now)
	}
	if err != nil {
		_ = r.store.FailCleanup(ctx, lease, now)
		_ = r.store.Record(EventInput{Purpose: PurposeLogin, Action: "cleanup", Outcome: "failure"})
		return
	}
	_ = r.store.FinishCleanup(ctx, lease, now)
	_ = r.store.Record(EventInput{Purpose: PurposeLogin, Action: "cleanup", Outcome: "success"})
}
