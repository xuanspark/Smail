package logstore

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"git.pcux.cn/xuans/Smail/utils/logpolicy"
	log "github.com/sirupsen/logrus"
)

var active atomic.Pointer[Store]

func Current() *Store { return active.Load() }

// Record 同步落库，不建立无限内存队列；故障必须进入运行日志。
func Record(ctx context.Context, entry Entry) {
	store := Current()
	if store == nil {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := store.Write(bounded, entry); err != nil {
		log.WithError(err).Error("业务日志写入失败")
	}
}

type Runtime struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
	store  *Store
}

func Start(store *Store) *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &Runtime{cancel: cancel, done: make(chan struct{}), store: store}
	active.Store(store)
	go func() {
		defer close(runtime.done)
		ticker := time.NewTicker(logpolicy.MaintenanceInterval)
		defer ticker.Stop()
		for {
			bounded, stop := context.WithTimeout(ctx, 2*time.Minute)
			_, err := store.Cleanup(bounded, time.Now())
			stop()
			if err != nil && ctx.Err() == nil {
				log.WithError(err).Error("过期业务日志清理失败，将在下一轮重试")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return runtime
}

func (r *Runtime) Stop() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		active.CompareAndSwap(r.store, nil)
		r.cancel()
		<-r.done
	})
}
