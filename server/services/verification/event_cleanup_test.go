package verification

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func newCleanupEngine(t *testing.T) *xorm.Engine {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "cleanup.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Sync2(&models.VerificationEvent{}, &models.VerificationCleanupState{}, &models.VerificationRateLimit{}); err != nil {
		_ = engine.Close()
		t.Fatal(err)
	}
	return engine
}

func TestCleanupLeaseIsSingleInstanceAndHasPersistedSchedule(t *testing.T) {
	engine := newCleanupEngine(t)
	defer engine.Close()
	store, err := NewEventStore(engine)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC)
	lease, acquired, err := store.TryAcquireCleanupLease(context.Background(), now)
	if err != nil || !acquired || lease.Token == "" {
		t.Fatalf("first lease = %+v, %v, %v", lease, acquired, err)
	}
	if _, acquired, err := store.TryAcquireCleanupLease(context.Background(), now); err != nil || acquired {
		t.Fatalf("second instance acquired active lease: %v, %v", acquired, err)
	}
	if err := store.FinishCleanup(context.Background(), lease, now); err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := store.TryAcquireCleanupLease(context.Background(), now.Add(cleanupScheduleInterval-time.Second)); err != nil || acquired {
		t.Fatalf("cleanup ran before persisted schedule: %v, %v", acquired, err)
	}
	second, acquired, err := store.TryAcquireCleanupLease(context.Background(), now.Add(cleanupScheduleInterval))
	if err != nil || !acquired || second.Token == "" {
		t.Fatalf("scheduled cleanup was not due: %+v, %v, %v", second, acquired, err)
	}
}

func TestCleanupFailureSchedulesSixHourRetry(t *testing.T) {
	engine := newCleanupEngine(t)
	defer engine.Close()
	store, err := NewEventStore(engine)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC)
	lease, acquired, err := store.TryAcquireCleanupLease(context.Background(), now)
	if err != nil || !acquired {
		t.Fatal(err)
	}
	if err := store.FailCleanup(context.Background(), lease, now); err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := store.TryAcquireCleanupLease(context.Background(), now.Add(cleanupRetryInterval-time.Second)); err != nil || acquired {
		t.Fatalf("cleanup retry started too early: %v, %v", acquired, err)
	}
	if _, acquired, err := store.TryAcquireCleanupLease(context.Background(), now.Add(cleanupRetryInterval)); err != nil || !acquired {
		t.Fatalf("cleanup retry was not scheduled: %v, %v", acquired, err)
	}
}

func TestCleanupStateInsertQueryUsesPostgresPlaceholders(t *testing.T) {
	query := cleanupStateInsertQuery("postgres")
	if !strings.Contains(query, "VALUES ($1, $2, $3, $4, $5, $6)") {
		t.Fatalf("postgres cleanup query = %q", query)
	}
	if strings.Contains(query, "VALUES (, ") {
		t.Fatalf("postgres cleanup query contains empty placeholders: %q", query)
	}
}

func TestOperationalCleanupRunnerStopIsIdempotentAndConcurrentSafe(t *testing.T) {
	engine := newCleanupEngine(t)
	defer engine.Close()
	store, err := NewEventStore(engine)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewOperationalCleanupRunner(store)
	runner.Start()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runner.Stop()
		}()
	}
	wg.Wait()
	runner.Stop()
}
