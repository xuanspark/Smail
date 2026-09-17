package dbmigration

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDatabaseDrainWaitsForBorrowedConnectionAfterPoolClose(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	connection, err := database.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := WaitForDatabaseDrain(ctx, database); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("borrowed connection was not protected: %v", err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := WaitForDatabaseDrain(context.Background(), database); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceGuardDrainsRequestsAndKeepsRestartRoute(t *testing.T) {
	executor := &PortableExecutor{}
	service := NewService(&DatabaseExecutor{portable: executor})
	previous := Current()
	SetCurrent(service)
	t.Cleanup(func() { SetCurrent(previous) })
	entered, release, migrated := make(chan struct{}), make(chan struct{}), make(chan struct{})
	handler := GuardHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/write":
			close(entered)
			<-release
		case "/api/v1/database/migrations":
			release, err := service.drainRequests(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusRequestTimeout)
				return
			}
			defer release()
			executor.paused.Store(true)
			close(migrated)
		}
		w.WriteHeader(http.StatusOK)
	}))
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/write", nil))
		close(done)
	}()
	<-entered
	migrationDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/database/migrations", nil))
		close(migrationDone)
	}()
	select {
	case <-migrated:
		t.Error("migration did not drain request")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-done
	<-migrationDone
	for _, test := range []struct {
		method, path string
		code         int
	}{
		{http.MethodPost, "/api/v1/email/send", 503},
		{http.MethodPost, "/api/v1/database/migrations", 503},
		{http.MethodPut, "/api/v1/config/base", 503},
		{http.MethodGet, "/api/v1/config/status", 200},
		{http.MethodGet, "/api/v1/config/base", 200},
		{http.MethodPost, "/api/v1/user/info", 200},
		{http.MethodPost, "/api/v1/system/restart", 200},
		{http.MethodPost, "/api/v1/system/restart/preview", 200},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
		if recorder.Code != test.code {
			t.Errorf("%s: %d", test.path, recorder.Code)
		}
	}
	called := false
	if err := WhileWritable(func() error { called = true; return nil }); err != nil || called {
		t.Fatal("cleanup wrote to frozen source")
	}
}

func TestMaintenanceDrainsBackgroundDeliveryBeforeFreeze(t *testing.T) {
	executor := &PortableExecutor{}
	service := NewService(&DatabaseExecutor{portable: executor})
	previous := Current()
	SetCurrent(service)
	t.Cleanup(func() { SetCurrent(previous) })
	entered, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = WhileWritable(func() error {
			close(entered)
			<-finish
			return nil
		})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	release, err := service.drainRequests(ctx)
	release()
	close(finish)
	<-done
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("migration did not wait for delivery: %v", err)
	}
	release, err = service.drainRequests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	executor.paused.Store(true)
	release()
	called := false
	if err := WhileWritable(func() error { called = true; return nil }); err != nil || called {
		t.Fatal("delivery started after source freeze")
	}
}
