package config

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"git.pcux.cn/xuans/Smail/services/access"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

func TestPluginRolesValidateUpdateAndPending(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManagerWithSecurity(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}, ConfigSecurity{Audit: &MemoryAuditWriter{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin := AuditActor{UserID: "2", Role: "admin", SessionID: "admin-session", RecentAuth: true, RecoveryCodeConfigured: true}
	current := manager.Snapshot()
	for _, change := range []string{"executable", "permissions", "install", "remove"} {
		candidate := cloneRuntimeConfig(current)
		plugin := candidate.Plugins.Plugins["telegram_push"]
		switch change {
		case "executable":
			plugin.Executable = "other-program.exe"
		case "permissions":
			plugin.Permissions.Network[0].Host = "other.example.com"
		case "install":
			candidate.Plugins.Plugins["other"] = plugin
		}
		candidate.Plugins.Plugins["telegram_push"] = plugin
		if change == "remove" {
			delete(candidate.Plugins.Plugins, "telegram_push")
		}
		payload, _ := json.Marshal(candidate.Plugins)
		if _, err := manager.ValidateAuthorized(ctx, ValidateRequest{Section: SectionPlugins, ExpectedGeneration: current.Generation, Payload: payload}, admin); !errors.Is(err, ErrAuthorizationRequired) {
			t.Fatalf("校验允许越权 %s：%v", change, err)
		}
		if _, err := manager.Update(ctx, UpdateRequest{Section: SectionPlugins, ExpectedGeneration: current.Generation, Payload: payload, Actor: admin}); !errors.Is(err, ErrAuthorizationRequired) {
			t.Fatalf("保存允许越权 %s：%v", change, err)
		}
	}
	plugins := current.Plugins
	plugin := plugins.Plugins["telegram_push"]
	plugin.Settings = json.RawMessage(`{"displayName":"修改通知名称"}`)
	plugins.Plugins["telegram_push"] = plugin
	payload, _ := json.Marshal(plugins)
	validation, err := manager.ValidateAuthorized(ctx, ValidateRequest{Section: SectionPlugins, ExpectedGeneration: current.Generation, Payload: payload}, admin)
	if err != nil || !validation.Valid {
		t.Fatalf("管理员参数校验失败：%+v %v", validation, err)
	}
	updated, err := manager.Update(ctx, UpdateRequest{Section: SectionPlugins, ExpectedGeneration: current.Generation, Payload: payload, Actor: admin})
	if err != nil {
		t.Fatal(err)
	}
	other := admin
	other.UserID, other.SessionID = "3", "other-session"
	user := AuditActor{UserID: "4", Role: "user", SessionID: "user-session", RecentAuth: true}
	for _, actor := range []AuditActor{other, user} {
		if manager.Pending(actor) != nil {
			t.Fatal("读取了其他操作者候选")
		}
		if _, err := manager.ApplyPendingAuthorized(ctx, updated.Generation, updated.CandidateID, SectionPlugins, actor); err == nil {
			t.Fatal("应用了其他操作者候选")
		}
		if err := manager.ManagePending(ctx, updated.Generation, updated.CandidateID, actor, StepUpConfirmation{Confirmed: true, Method: "recent_auth"}, true); err == nil {
			t.Fatal("取消了其他操作者候选")
		}
	}
	if view := manager.Pending(admin); view == nil || !view.SameSession {
		t.Fatal("本人候选不可见")
	}
	if _, err := manager.ApplyPendingAuthorized(ctx, updated.Generation, updated.CandidateID, SectionRoot, admin); err == nil {
		t.Fatal("候选可跨分区应用")
	}
	if _, err := manager.ApplyPendingAuthorized(ctx, updated.Generation, updated.CandidateID, SectionPlugins, admin); err != nil {
		t.Fatal(err)
	}
	super := authorizedConfigActor()
	mail, _ := json.Marshal(manager.Snapshot().Email)
	if _, err := manager.ValidateAuthorized(ctx, ValidateRequest{Section: SectionEmail, Payload: mail}, admin); err == nil {
		t.Fatal("管理员可校验系统配置")
	}
	if _, err := manager.Update(ctx, UpdateRequest{Section: SectionEmail, ExpectedGeneration: manager.Snapshot().Generation, Payload: mail, Actor: super}); err != nil {
		t.Fatal(err)
	}
	if manager.Pending(admin) != nil {
		t.Fatal("管理员读到了系统配置候选")
	}
}

func TestRuntimeActorCannotBeSuppliedByLogFields(t *testing.T) {
	entry := &log.Entry{Data: log.Fields{"actorId": "1", "actorRole": "super_admin"}}
	fields := runtimeEventFields(entry)
	if fields["actorId"] != nil || fields["actorRole"] != nil {
		t.Fatal("任意日志字段可伪造归属")
	}
	entry.Context = &appcontext.Context{UserID: 4, Role: access.RoleUser}
	fields = runtimeEventFields(entry)
	if fields["actorId"] != "4" || fields["actorRole"] != "user" || entry.Data["actorId"] != "1" {
		t.Fatal("可信认证归属未生效或修改了调用方字段")
	}
}
