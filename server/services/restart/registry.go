package restart

import "sync"

var (
	currentMu sync.RWMutex
	current   Trigger
)

func SetCurrent(trigger Trigger) {
	currentMu.Lock()
	current = trigger
	currentMu.Unlock()
}

func Current() Trigger {
	currentMu.RLock()
	defer currentMu.RUnlock()
	return current
}
