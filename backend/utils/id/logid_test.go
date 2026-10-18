package id

import (
	"sync"
	"testing"
)

func TestGetLocalIPConcurrent(t *testing.T) {
	const goroutines = 50
	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ip := getLocalIP()
				if ip == "" {
					t.Errorf("getLocalIP returned empty")
				}
				logID := GenLogID()
				if logID == "" {
					t.Errorf("GenLogID returned empty")
				}
			}
		}()
	}

	wg.Wait()
}
