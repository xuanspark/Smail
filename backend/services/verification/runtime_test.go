package verification

import "testing"

func TestRuntimeStartsWithInMemoryStore(t *testing.T) {
	runtime, err := NewRuntime(8)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Store == nil || runtime.Store.Size() != 0 {
		t.Fatal("runtime did not create an empty in-memory store")
	}
	runtime.Stop()
}
