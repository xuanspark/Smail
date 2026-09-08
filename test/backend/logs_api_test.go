package controllers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/logstore"
	"git.pcux.cn/xuans/Smail/services/runtimelog"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	_ "modernc.org/sqlite"
)

func TestLogsAPIRejectsUnscopedAccessAndValidatesQueries(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// 与正式 SQLite 连接池一致，避免测试自行引入多写连接锁冲突。
	database.SetMaxOpenConns(1)
	items := migration.DefaultMigrations()
	for _, statement := range items[len(items)-1].StatementsFor(migration.DialectSQLite) {
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
	writer, err := runtimelog.Start(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := store.Write(context.Background(), logstore.Entry{Kind: "operation", CreatedAt: time.Now(), ActorID: "42", Action: "probe", Result: "success", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	call := func(role access.Role, query string) response.Response {
		t.Helper()
		request := httptest.NewRequest("GET", "/api/v1/logs?"+query, nil)
		recorder := httptest.NewRecorder()
		GetLogs(&appcontext.Context{Role: role, UserID: 42}, recorder, request)
		if recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("sensitive log response can be cached")
		}
		var result response.Response
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, role := range []access.Role{access.RoleUser, access.RoleAdmin} {
		for _, kind := range []string{"operation", "config", "mail", "runtime"} {
			if got := call(role, "kind="+kind+"&actorId=42"); got.ErrorNo != response.NoAccessPrivileges {
				t.Fatalf("unscoped role %s accessed %s: %d", role, kind, got.ErrorNo)
			}
		}
	}
	for _, query := range []string{"kind=unknown", "kind=mail&limit=501", "kind=mail&offset=-1", "kind=mail&from=invalid", "kind=mail&from=2026-09-08T00:00:00Z&to=2026-09-01T00:00:00Z"} {
		if got := call(access.RoleSuperAdmin, query); got.ErrorNo != response.ParamsError {
			t.Fatalf("invalid query accepted: %s => %d", query, got.ErrorNo)
		}
	}
	for _, query := range []string{"kind=runtime&from=invalid", "kind=runtime&to=", "kind=runtime&file=../config/config.json", "kind=runtime&from=2026-09-08T00:00:00Z&to=2026-09-01T00:00:00Z"} {
		if got := call(access.RoleSuperAdmin, query); got.ErrorNo != response.ParamsError {
			t.Fatalf("运行日志未校验参数：%s => %d", query, got.ErrorNo)
		}
	}
	result := call(access.RoleSuperAdmin, "kind=operation&actorId=42&limit=1")
	if result.ErrorNo != 0 {
		t.Fatalf("administrator query failed: %d", result.ErrorNo)
	}
	data := result.Data.(map[string]any)
	if len(data["items"].([]any)) != 1 || data["retentionDays"] != float64(30) {
		t.Fatal("log query lost records or retention policy")
	}
	now := time.Now().UTC().Add(-time.Minute)
	for index := 0; index < 14; index++ {
		at := now.Add(time.Duration(index) * time.Second)
		if index == 13 {
			at = now.Add(-25 * time.Hour)
		}
		if err := writer.WriteAt(at, fmt.Sprintf("运行事件%d", index)); err != nil {
			t.Fatal(err)
		}
		if err := store.Write(context.Background(), logstore.Entry{Kind: "mail", CreatedAt: at, Action: "投递邮件", Result: "success", Payload: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"mail", "runtime"} {
		key := "items"
		if kind == "runtime" {
			key = "lines"
		}
		first := call(access.RoleSuperAdmin, "kind="+kind)
		data := first.Data.(map[string]any)
		if first.ErrorNo != 0 || len(data[key].([]any)) != 10 || data["hasMore"] != true {
			t.Fatalf("默认十条：%+v", first)
		}
		second := call(access.RoleSuperAdmin, "kind="+kind+"&offset=10")
		data = second.Data.(map[string]any)
		if len(data[key].([]any)) != 3 || data["hasMore"] != false {
			t.Fatalf("默认时段不是最近二十四小时：%+v", second)
		}
		values := url.Values{"kind": {kind}, "from": {now.Add(-26 * time.Hour).Format(time.RFC3339Nano)}, "to": {now.Add(-24 * time.Hour).Format(time.RFC3339Nano)}}
		history := call(access.RoleSuperAdmin, values.Encode())
		if history.ErrorNo != 0 || len(history.Data.(map[string]any)[key].([]any)) != 1 {
			t.Fatalf("指定时段：%+v", history)
		}
	}
}
