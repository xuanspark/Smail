package verification

import (
	"time"

	"xorm.io/xorm"
)

const DefaultCleanupInterval = time.Minute

type Runtime struct {
	Store              *Store
	Events             *EventStore
	RateLimits         *PersistentRateLimiter
	cleanup            *CleanupRunner
	operationalCleanup *OperationalCleanupRunner
}

func NewRuntime(maxEntries int) (*Runtime, error) {
	return NewRuntimeWithEventStore(maxEntries, nil)
}

func NewRuntimeWithEventStore(maxEntries int, engine *xorm.Engine) (*Runtime, error) {
	store, err := NewStore(maxEntries)
	if err != nil {
		return nil, err
	}
	runtime := &Runtime{Store: store, cleanup: NewCleanupRunner(store)}
	if engine != nil {
		events, eventErr := NewEventStore(engine)
		if eventErr != nil {
			return nil, eventErr
		}
		runtime.Events = events
		rateLimits, limitErr := NewPersistentRateLimiter(engine)
		if limitErr != nil {
			return nil, limitErr
		}
		runtime.RateLimits = rateLimits
		runtime.operationalCleanup = NewOperationalCleanupRunner(events, rateLimits)
		runtime.operationalCleanup.Start()
	}
	if err := runtime.cleanup.Start(DefaultCleanupInterval); err != nil {
		if runtime.operationalCleanup != nil {
			runtime.operationalCleanup.Stop()
		}
		return nil, err
	}
	return runtime, nil
}

func (r *Runtime) Stop() {
	if r == nil {
		return
	}
	if r.cleanup != nil {
		r.cleanup.Stop()
	}
	if r.operationalCleanup != nil {
		r.operationalCleanup.Stop()
	}
}
