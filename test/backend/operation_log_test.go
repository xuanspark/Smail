package http_server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/logstore"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	_ "modernc.org/sqlite"
)

func TestRetiredConfigAuditRoutesReturnGone(t *testing.T) {
	for _, path := range []string{"/api/v1/audits/config", "/api/v1/audits/config/export", "/api/v1/audits/config/audit-00000000000000000000000000000000"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			request := httptest.NewRequest(method, path, nil)
			recorder := httptest.NewRecorder()
			if path == "/api/v1/audits/config" {
				configAuditRoute(&appcontext.Context{}, recorder, request)
			} else {
				configAuditSubRoute(&appcontext.Context{}, recorder, request)
			}
			if recorder.Code != http.StatusGone || recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("Content-Disposition") != "" {
				t.Fatalf("已停用入口仍可查询或导出：%s %s", method, path)
			}
		}
	}
}

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
		if entry.Result != "success" || payload["actorName"] != "audit-probe" || len(payload) != 7 || payload["outcome"] != "操作已完成" || payload["trigger"] == "" || payload["object"] == "" || strings.Contains(string(entry.Payload), "must-not-store") {
			t.Fatalf("incorrect or excessive audit metadata: %+v", payload)
		}
	}
}

func TestOperationResponseDistinguishesPendingHTTPFailureAndUnknown(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "results.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// 与生产 SQLite 连接池一致，避免后台清理和写入争抢无等待的独立连接。
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
	for _, item := range []struct {
		name, want string
		write      func(http.ResponseWriter)
	}{
		{"pending", "pending", func(w http.ResponseWriter) {
			response.NewSuccessResponse(map[string]any{"pending": true, "challenge_id": "private-challenge"}).FPrint(w)
		}},
		{"failure", "failure", func(w http.ResponseWriter) {
			response.NewErrorResponse(response.ParamsError, "private-detail", "private-code").FPrint(w)
		}},
		{"http", "failure", func(w http.ResponseWriter) { http.Error(w, "private-detail", http.StatusForbidden) }},
		{"unknown", "unknown", func(w http.ResponseWriter) { _, _ = w.Write([]byte("not-a-business-response")) }},
	} {
		ctx := &appcontext.Context{UserID: 42, UserAccount: item.name}
		writer, finish := operationLog(ctx, httptest.NewRequest("POST", "/api/v1/login?password=private-query", strings.NewReader("private-request")), httptest.NewRecorder())
		item.write(writer)
		finish()
		entries, err := store.Query(context.Background(), logstore.Query{Kind: "operation", Limit: 1})
		if err != nil || len(entries) != 1 {
			t.Fatalf("操作日志条数为 %d，查询错误：%v", len(entries), err)
		}
		entry := entries[0]
		if entry.Result != item.want || strings.Contains(string(entry.Payload), "private-") || strings.Contains(string(entry.Payload), "not-a-business-response") {
			t.Fatalf("结果错误或泄露请求：%s %+v", item.name, entry)
		}
	}
	before, _ := store.Query(context.Background(), logstore.Query{Kind: "operation"})
	_, finish := operationLog(&appcontext.Context{}, httptest.NewRequest("POST", "/api/v1/email/list", nil), httptest.NewRecorder())
	finish()
	after, _ := store.Query(context.Background(), logstore.Query{Kind: "operation"})
	if len(before) != len(after) {
		t.Fatal("只读查询不能新增操作记录")
	}
}
