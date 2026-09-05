package session

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	_ "modernc.org/sqlite"
)

func TestMigrationSessionResponseDoesNotWriteLockedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	database, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec("CREATE TABLE sessions (token TEXT PRIMARY KEY, data BLOB NOT NULL, expiry REAL NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	manager := scs.New()
	manager.IdleTimeout = time.Hour
	paused := false
	store := &migrationStore{Store: sqlite3store.NewWithCleanupInterval(database, 0), codec: manager.Codec, paused: func() bool { return paused }}
	manager.Store = store
	ctx, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	manager.Put(ctx, "role", "super_admin")
	token, _, err := manager.Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var originalExpiry float64
	if err := database.QueryRow("SELECT expiry FROM sessions").Scan(&originalExpiry); err != nil {
		t.Fatal(err)
	}
	lockDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer lockDB.Close()
	lock, err := lockDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	handler := manager.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if manager.GetString(r.Context(), "role") != "super_admin" {
			t.Error("identity lost")
		}
		if !paused {
			if _, err := lock.ExecContext(r.Context(), "BEGIN IMMEDIATE"); err != nil {
				t.Error(err)
			}
			paused = true
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer lock.ExecContext(context.Background(), "ROLLBACK")
	for range 2 {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/config/status", nil)
		request.AddCookie(&http.Cookie{Name: manager.Cookie.Name, Value: token})
		handler.ServeHTTP(recorder, request)
		if recorder.Code != 200 || recorder.Body.String() != "ok" {
			t.Fatalf("session response: %d %s", recorder.Code, recorder.Body.String())
		}
	}
	ctx, err = manager.Load(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	manager.Put(ctx, "role", "admin")
	if _, _, err := manager.Commit(ctx); !errors.Is(err, errMigrationSessionWrite) {
		t.Fatalf("changed session accepted: %v", err)
	}
	if err := store.Delete(token); !errors.Is(err, errMigrationSessionWrite) {
		t.Fatal("deletion accepted")
	}
	var expiry float64
	if err := database.QueryRow("SELECT expiry FROM sessions").Scan(&expiry); err != nil || expiry != originalExpiry {
		t.Fatal("session expiry changed while frozen")
	}
}
