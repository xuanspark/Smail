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
	runner := migration.Runner{DB: database, Dialect: migration.DialectSQLite, Migrations: migrations}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	store, err := logstore.New(database, migration.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	runtime := logstore.Start(store)
	defer runtime.Stop()
	for _, path := range []string{"/api/v1/login", "/api/v1/security/login/verify", "/api/v1/security/webauthn/login/finish", "/api/v1/plugin/secrets/telegram_push", "/api/v1/plugin/secrets/wechat_push"} {
		ctx := &appcontext.Context{}
		method := http.MethodPost
		if strings.Contains(path, "/plugin/secrets/") {
			method = http.MethodPut
		}
		writer, finish := operationLog(ctx, httptest.NewRequest(method, path, strings.NewReader(`{"plaintext":"must-not-store"}`)), httptest.NewRecorder())
		ctx.UserID, ctx.UserAccount, ctx.Role = 42, "audit-probe", access.RoleSuperAdmin
		response.NewSuccessResponse(map[string]string{"token": "must-not-store"}).FPrint(writer)
		finish()
	}
	entries, err := store.Query(context.Background(), logstore.Query{Kind: "operation", ActorID: "42"})
	if err != nil || len(entries) != 5 {
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
	runner := migration.Runner{DB: database, Dialect: migration.DialectSQLite, Migrations: migrations}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
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

func TestOperationLogCoversFullOperationsAndRejectsReadonly(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "full_operations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	migrations := migration.DefaultMigrations()
	runner := migration.Runner{DB: database, Dialect: migration.DialectSQLite, Migrations: migrations}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	store, err := logstore.New(database, migration.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	runtime := logstore.Start(store)
	defer runtime.Stop()

	// 验证只读 POST 查询不会被误记为操作日志
	readonlyCases := []string{
		"/api/v1/user/info",
		"/api/v1/user/list",
		"/api/v1/email/list",
		"/api/v1/email/detail",
		"/api/rule/get",
	}
	for _, path := range readonlyCases {
		ctx := &appcontext.Context{UserID: 1, UserAccount: "admin", Role: access.RoleSuperAdmin}
		writer, finish := operationLog(ctx, httptest.NewRequest(http.MethodPost, path, nil), httptest.NewRecorder())
		response.NewSuccessResponse(map[string]any{"data": "ok"}).FPrint(writer)
		finish()
	}
	entries, err := store.Query(context.Background(), logstore.Query{Kind: "operation"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("只读接口不应记录操作日志，实际记录条数：%d", len(entries))
	}

	// 验证全量代表性操作正常记入日志且元数据分类正确
	testCases := []struct {
		method     string
		path       string
		wantAction string
		wantObject string
	}{
		{http.MethodPost, "/api/v1/email/send", "发送邮件", "邮件"},
		{http.MethodPost, "/api/v1/email/contacts/delete", "删除常用联系人", "邮件"},
		{http.MethodPost, "/api/v1/email/trust-sender", "设置信任发件人", "邮件"},
		{http.MethodPost, "/api/v1/security/credentials/protocols/reveal", "查看邮件客户端安全码", "账号安全"},
		{http.MethodPut, "/api/v1/security/credentials/protocols/enabled", "设置邮件客户端授权", "账号安全"},
		{http.MethodPost, "/api/v1/security/credentials/protocols/generate", "生成邮件客户端安全码", "账号安全"},
		{http.MethodPost, "/api/v1/security/credentials/protocols/revoke", "撤销邮件客户端安全码", "账号安全"},
		{http.MethodPost, "/api/v1/security/credentials/question", "设置密保问题", "账号安全"},
		{http.MethodPost, "/api/v1/security/setup/acknowledge", "确认安全初始化引导", "账号安全"},
		{http.MethodPost, "/api/v1/security/reauth", "完成身份重新认证", "账号安全"},
		{http.MethodPost, "/api/v1/security/verification/request", "申请安全验证码", "账号安全"},
		{http.MethodPost, "/api/v1/security/verification/verify", "完成安全验证", "账号安全"},
		{http.MethodPost, "/api/v1/security/login/resend", "重新发送登录验证码", "账号安全"},
		{http.MethodPost, "/api/v1/security/webauthn/registration/begin", "发起通行密钥注册", "账号安全"},
		{http.MethodPost, "/api/v1/security/webauthn/registration/finish", "完成通行密钥注册", "账号安全"},
		{http.MethodPost, "/api/v1/security/webauthn/credentials/rename", "重命名通行密钥", "账号安全"},
		{http.MethodPost, "/api/v1/security/webauthn/credentials/revoke", "撤销通行密钥", "账号安全"},
		{http.MethodPost, "/api/v1/security/recovery/begin", "发起账号恢复", "账号安全"},
		{http.MethodPost, "/api/v1/security/recovery/verify", "验证账号恢复凭据", "账号安全"},
		{http.MethodPost, "/api/v1/security/recovery/complete", "完成账号恢复", "账号安全"},
		{http.MethodPost, "/api/v1/user/security/external-email/change", "申请修改用户外部邮箱", "账号安全"},
		{http.MethodPost, "/api/v1/user/security/external-email/verify", "验证用户外部邮箱修改", "账号安全"},
		{http.MethodPost, "/api/v1/system/restart", "请求重启系统服务", "系统服务"},
		{http.MethodPost, "/api/v1/system/restart/preview", "预览系统服务重启", "系统服务"},
		{http.MethodPost, "/api/v1/database/migrations", "执行数据库迁移", "数据库运维"},
		{http.MethodPost, "/api/v1/database/migrations/preview", "预览数据库迁移", "数据库运维"},
		{http.MethodPost, "/api/v1/setup", "执行系统初始化配置", "系统服务"},
		{http.MethodPut, "/api/v1/plugin/settings/telegram_push/user", "更新电报通知设置", "插件配置"},
		{http.MethodPost, "/api/v1/plugin/settings/telegram_push/test", "测试电报消息推送", "插件配置"},
		// 动态系统配置接口测试
		{http.MethodPut, "/api/v1/config/base", "保存基础配置", "系统配置"},
		{http.MethodPost, "/api/v1/config/mail/apply", "应用邮件服务与策略配置", "系统配置"},
		{http.MethodPost, "/api/v1/config/mail/validate", "校验邮件服务与策略配置", "系统配置"},
		{http.MethodPut, "/api/v1/config/smtp", "保存SMTP服务配置", "系统配置"},
		{http.MethodPut, "/api/v1/config/ssl", "保存证书配置", "系统配置"},
		{http.MethodPut, "/api/v1/config/plugins", "保存插件配置", "系统配置"},
		{http.MethodPost, "/api/v1/config/pending", "管理候选配置", "系统配置"},
		{http.MethodPost, "/api/v1/config/rollback/preview", "预览配置回滚", "系统配置"},
		{http.MethodPost, "/api/v1/config/rollback/ticket", "签发配置回滚票据", "系统配置"},
		{http.MethodPost, "/api/v1/config/rollback", "回滚系统配置", "系统配置"},
	}

	for _, tc := range testCases {
		ctx := &appcontext.Context{UserID: 88, UserAccount: "admin", Role: access.RoleSuperAdmin}
		writer, finish := operationLog(ctx, httptest.NewRequest(tc.method, tc.path, nil), httptest.NewRecorder())
		response.NewSuccessResponse(map[string]any{"status": "ok"}).FPrint(writer)
		finish()

		queryEntries, err := store.Query(context.Background(), logstore.Query{Kind: "operation", Limit: 1})
		if err != nil || len(queryEntries) == 0 {
			t.Fatalf("路径 %s %s 期望记录操作日志，但未查询到记录：%v", tc.method, tc.path, err)
		}
		entry := queryEntries[0]
		if entry.Action != tc.wantAction {
			t.Fatalf("路径 %s 动作不匹配，期望 %s，实际 %s", tc.path, tc.wantAction, entry.Action)
		}
		var payload map[string]any
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["object"] != tc.wantObject {
			t.Fatalf("路径 %s 类别不匹配，期望 %s，实际 %v", tc.path, tc.wantObject, payload["object"])
		}
	}
}
