package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/logstore"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"xorm.io/xorm"
)

func TestAccountRoleScopeAndOwnProfile(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "roles.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine.SetMaxOpenConns(1)
	engine.SetMaxIdleConns(1)
	previous := db.Instance
	db.Instance = engine
	t.Cleanup(func() { db.Instance = previous; _ = engine.Close() })
	if err := engine.Sync2(&models.User{}, &models.RecoveryCode{}, &models.ExternalEmail{}); err != nil {
		t.Fatal(err)
	}
	users := []models.User{
		{Account: "root", Name: "Root", Role: "super_admin", IsAdmin: 1},
		{Account: "manager", Name: "Manager", Role: "admin", IsAdmin: 1},
		{Account: "other-manager", Name: "Other", Role: "admin", IsAdmin: 1},
		{Account: "alice", Name: "Alice", Role: "user"},
		{Account: "legacy", Name: "Legacy", Role: "old", IsAdmin: 0},
		{Account: "legacy-manager", Name: "Legacy admin", Role: "old", IsAdmin: 1},
	}
	for i := range users {
		if _, err := engine.Insert(&users[i]); err != nil {
			t.Fatal(err)
		}
	}
	admin := &appcontext.Context{UserID: users[1].ID, Role: access.RoleAdmin}
	ordinary := &appcontext.Context{UserID: users[3].ID, Role: access.RoleUser}
	call := func(ctx *appcontext.Context, handler func(*appcontext.Context, http.ResponseWriter, *http.Request), body string) response.Response {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler(ctx, recorder, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		var result response.Response
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, ctx := range []*appcontext.Context{admin, ordinary} {
		if got := call(ctx, CreateUser, `{"account":"blocked","username":"Blocked","password":"TestPass12","role":"user"}`); got.ErrorNo != response.NoAccessPrivileges {
			t.Fatalf("低权限创建账号：%+v", got)
		}
	}
	for page, expected := range []string{"alice", "legacy"} {
		body, _ := json.Marshal(map[string]int{"current_page": page + 1, "page_size": 1})
		got := call(admin, UserList, string(body))
		if got.ErrorNo != 0 {
			t.Fatalf("用户列表失败：%+v", got)
		}
		data := got.Data.(map[string]any)
		items := data["list"].([]any)
		if data["total_page"] != float64(2) || len(items) != 1 || items[0].(map[string]any)["Account"] != expected {
			t.Fatalf("分页前权限过滤错误：%+v", data)
		}
	}
	if got := call(ordinary, UserList, `{}`); got.ErrorNo != response.NoAccessPrivileges {
		t.Fatal("普通用户可以枚举账号")
	}
	for _, body := range []string{`{"name":"Bad","id":1}`, `{"name":"Bad","role":"super_admin"}`, `{"name":"Bad","password":"Changed12"}`, `{"name":" "}`, `{"name":"Good"} {}`} {
		if got := call(ordinary, UpdateProfile, body); got.ErrorNo == 0 {
			t.Fatalf("越权字段未拒绝：%s", body)
		}
	}
	for i := 0; i < 2; i++ {
		if got := call(ordinary, UpdateProfile, `{"name":" 新名称 "}`); got.ErrorNo != 0 {
			t.Fatalf("本人资料保存失败：%+v", got)
		}
	}
	var updated, untouched models.User
	_, _ = engine.ID(users[3].ID).Get(&updated)
	_, _ = engine.ID(users[0].ID).Get(&untouched)
	if updated.Name != "新名称" || updated.Role != "user" || untouched.Name != "Root" {
		t.Fatal("资料更新改变了角色或他人资料")
	}
	if got := call(&appcontext.Context{UserID: 9999, Role: access.RoleUser}, UpdateProfile, `{"name":"Missing"}`); got.ErrorNo == 0 {
		t.Fatal("不存在账号虚报保存成功")
	}
	for _, ctx := range []*appcontext.Context{admin, ordinary} {
		ctx.IsAdmin = true
		got := call(ctx, GetPluginList, `{}`)
		if ctx == ordinary && got.ErrorNo != response.NoAccessPrivileges {
			t.Fatal("旧管理员标志绕过插件列表授权")
		}
		if ctx == admin && got.ErrorNo != 0 {
			t.Fatal("管理员角色无法读取插件列表")
		}
		pageRecorder := httptest.NewRecorder()
		SettingsHtml(ctx, pageRecorder, httptest.NewRequest(http.MethodGet, "/api/plugin/settings/missing/index.html", nil))
		if ctx == ordinary && !strings.Contains(pageRecorder.Body.String(), "无权访问") {
			t.Fatal("旧管理员标志绕过插件页面授权")
		}
		if ctx == admin && strings.Contains(pageRecorder.Body.String(), "无权访问") {
			t.Fatal("管理员角色未获得插件页面授权")
		}
	}
	migrations := migration.DefaultMigrations()
	runner := migration.Runner{DB: engine.DB().DB, Dialect: migration.DialectSQLite, Migrations: migrations}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	store, err := logstore.New(engine.DB().DB, migration.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range users {
		if err := store.Write(context.Background(), logstore.Entry{Kind: "operation", ActorID: itoa(user.ID), ActorRole: string(user.EffectiveRole()), CreatedAt: time.Now(), Action: "权限验证", Result: "success", Payload: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	runtime := logstore.Start(store)
	defer runtime.Stop()
	for _, item := range []struct {
		ctx   *appcontext.Context
		query string
		count int
	}{
		{admin, "kind=operation", 3}, {ordinary, "kind=operation", 1},
		{admin, "kind=operation&actorId=" + itoa(users[0].ID), 0}, {ordinary, "kind=operation&actorId=" + itoa(admin.UserID), 0},
	} {
		recorder := httptest.NewRecorder()
		GetLogs(item.ctx, recorder, httptest.NewRequest("GET", "/api/v1/logs?"+item.query, nil))
		var result response.Response
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.ErrorNo != 0 || len(result.Data.(map[string]any)["items"].([]any)) != item.count {
			t.Fatalf("日志接口范围失效：%+v", result)
		}
	}
}
