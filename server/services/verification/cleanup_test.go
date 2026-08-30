package verification

import (
	"testing"
	"time"
)

func TestCleanupRunnerHasSingleInstance(t *testing.T) {
	store, err := NewStore(4)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewCleanupRunner(store)
	if err := runner.Start(time.Hour); err != nil {
		t.Fatal(err)
	}
	defer runner.Stop()
	if err := runner.Start(time.Hour); err != ErrCleanupAlreadyStarted {
		t.Fatalf("second cleanup runner start error = %v", err)
	}
}

func TestCleanupRunnerCanRestartAfterStop(t *testing.T) {
	store, err := NewStore(4)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewCleanupRunner(store)
	if err := runner.Start(time.Hour); err != nil {
		t.Fatal(err)
	}
	runner.Stop()
	if err := runner.Start(time.Hour); err != nil {
		t.Fatalf("cleanup runner restart error = %v", err)
	}
	runner.Stop()
}
