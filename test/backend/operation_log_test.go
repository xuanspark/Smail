package http_server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/logstore"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	_ "modernc.org/sqlite"
)

func TestOperationLogUsesCompletedIdentityWithoutSensitivePayload(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "operations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	migrations := migration.DefaultMigrations()
	for _, statement := range migrations[len(migrations)-1].StatementsFor(migration.DialectSQLite) {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	store, err := logstore.New(database, migration.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	runtime := logstore.Start(store)
	defer runtime.Stop()
	for _, path := range []string{"/api/v1/login", "/api/v1/security/login/verify", "/api/v1/security/webauthn/login/finish"} {
		ctx := &appcontext.Context{}
		writer, finish := operationLog(ctx, httptest.NewRequest("POST", path, nil), httptest.NewRecorder())
		ctx.UserID, ctx.UserAccount, ctx.Role = 42, "audit-probe", access.RoleSuperAdmin
		response.NewSuccessResponse(map[string]string{"token": "must-not-store"}).FPrint(writer)
		finish()
	}
	entries, err := store.Query(context.Background(), logstore.Query{Kind: "operation", ActorID: "42"})
	if err != nil || len(entries) != 3 {
		t.Fatalf("missing completed login records: %d, %v", len(entries), err)
	}
	for _, entry := range entries {
		var payload map[string]any
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if entry.Result != "success" || payload["actorName"] != "audit-probe" || len(payload) != 3 {
			t.Fatalf("incorrect or excessive audit metadata: %+v", payload)
		}
	}
}
