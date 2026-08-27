package ssl

import (
	"fmt"
	"sync"
	"testing"
)

func TestHttpChallengeConcurrentAccess(t *testing.T) {
	h := &HttpChallenge{authInfo: map[string]*authInfo{}}
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		token := fmt.Sprintf("token-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.Present("example.com", token, "key-"+token); err != nil {
				t.Errorf("Present returned an error: %v", err)
				return
			}
			if keyAuth, ok := h.KeyAuthorization(token); !ok || keyAuth != "key-"+token {
				t.Errorf("unexpected key authorization for %s", token)
			}
			if err := h.CleanUp("example.com", token, "key-"+token); err != nil {
				t.Errorf("CleanUp returned an error: %v", err)
			}
		}()
	}

	wg.Wait()
}

func TestDNSChallengeConcurrentAccess(t *testing.T) {
	h := &DNSChallenge{authInfo: map[string]*authInfo{}}
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		token := fmt.Sprintf("token-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.Present("example.com", token, "key-"+token); err != nil {
				t.Errorf("Present returned an error: %v", err)
				return
			}
			_ = h.GetDNSSettings(nil)
			if err := h.CleanUp("example.com", token, "key-"+token); err != nil {
				t.Errorf("CleanUp returned an error: %v", err)
			}
		}()
	}

	wg.Wait()
}
