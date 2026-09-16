package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func TestProtocolClientHandlersIsolateEveryRole(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "protocol-access.db"))
	if err != nil {
		t.Fatal(err)
	}
	previousDB, previousRuntime := db.Instance, currentSecurityRuntime()
	previousConfig, previousManager := config.Instance, config.CurrentManager()
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{Domain: "example.com", Domains: []string{"example.com"}}
	t.Cleanup(func() {
		ConfigureSecurityRuntime(previousRuntime.verification, previousRuntime.external, previousRuntime.recovery, previousRuntime.credentials)
		config.Instance = previousConfig
		config.SetRuntimeManager(previousManager)
		db.Instance = previousDB
		_ = engine.Close()
	})
	if _, err := auth.NormalizeProtocolIdentifier("actor0@example.com"); err != nil {
		t.Fatalf("测试邮件域名未生效：%v", err)
	}
	if err := engine.Sync2(&models.User{}, &models.UserEmail{}, &models.ProtocolCredential{}, &models.ClientCredential{}); err != nil {
		t.Fatal(err)
	}
	db.Instance = engine
	service, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	ConfigureSecurityRuntime(nil, nil, nil, service)
	newUser := func(account string, role access.Role) models.User {
		user := models.User{Account: account, Name: account}
		user.ApplyRole(role)
		if err := auth.SetPassword(&user, "WebOnly42"); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Insert(&user); err != nil {
			t.Fatal(err)
		}
		now, address := time.Now(), account+"@example.com"
		if _, err := engine.Insert(&models.UserEmail{UserID: user.ID, Address: address, AddressCanonical: &address, VerifiedAt: &now}); err != nil {
			t.Fatal(err)
		}
		return user
	}
	victim := newUser("other", access.RoleUser)
	victimCodes := map[string]string{}
	for _, protocol := range credentials.SupportedProtocols() {
		if err := service.SetProtocolEnabled(victim.ID, protocol, true); err != nil {
			t.Fatal(err)
		}
		victimCodes[protocol], err = service.GenerateProtocolCode(victim.ID, protocol)
		if err != nil {
			t.Fatal(err)
		}
	}
	for index, role := range []access.Role{access.RoleUser, access.RoleAdmin, access.RoleSuperAdmin} {
		t.Run(string(role), func(t *testing.T) {
			user := newUser(fmt.Sprintf("actor%d", index), role)
			ctx := &appcontext.Context{UserID: user.ID, UserAccount: user.Account, Role: role, IsAdmin: user.IsAdmin == 1}
			call := func(handler func(*appcontext.Context, http.ResponseWriter, *http.Request), method, body string) response.Response {
				t.Helper()
				// 即使查询参数伪造目标账号，所有操作也只能作用于会话所属用户。
				request := httptest.NewRequest(method, fmt.Sprintf("/api/security/credentials/protocols?user_id=%d", victim.ID), strings.NewReader(body))
				recorder := httptest.NewRecorder()
				handler(ctx, recorder, request)
				if strings.Contains(recorder.Body.String(), "code_hash") {
					t.Fatal("接口泄露授权码哈希")
				}
				var result response.Response
				if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			mustSucceed := func(result response.Response) {
				t.Helper()
				if result.ErrorNo != 0 {
					t.Fatalf("当前角色操作自己的协议设置失败：%d", result.ErrorNo)
				}
			}
			for _, protocol := range credentials.SupportedProtocols() {
				body := fmt.Sprintf(`{"protocol":%q}`, protocol)
				malicious := fmt.Sprintf(`{"protocol":%q,"enabled":false,"user_id":%d}`, protocol, victim.ID)
				if result := call(SetProtocolCredentialEnabled, http.MethodPut, malicious); result.ErrorNo == 0 {
					t.Fatal("接口接受了未知目标用户字段")
				}
				mustSucceed(call(SetProtocolCredentialEnabled, http.MethodPut, fmt.Sprintf(`{"protocol":%q,"enabled":true}`, protocol)))
				generated := call(GenerateProtocolCredential, http.MethodPost, body)
				mustSucceed(generated)
				data, ok := generated.Data.(map[string]any)
				if !ok || data["shown_once"] != true {
					t.Fatal("授权码未按一次性响应返回")
				}
				code, ok := data["code"].(string)
				if !ok || code == "" {
					t.Fatal("生成的授权码为空")
				}
				login := func(plain string) bool {
					got, err := auth.AuthenticateProtocol(protocol, "192.0.2.231:12345", user.Account+"@example.com", plain)
					return err == nil && got != nil && got.ID == user.ID
				}
				if !login(code) {
					t.Fatalf("%s 拒绝了当前用户的有效协议授权码", protocol)
				}
				if login("WebOnly42") {
					t.Fatalf("%s 错误接受了网页登录密码", protocol)
				}
				for _, identifier := range []string{user.Account, user.Account + "@external.example"} {
					if _, err := auth.AuthenticateProtocol(protocol, "192.0.2.232:12345", identifier, code); err == nil {
						t.Fatalf("%s 错误接受了短账号或未配置域名", protocol)
					}
				}
				listed := call(ListProtocolCredentials, http.MethodGet, "")
				mustSucceed(listed)
				encoded, err := json.Marshal(listed.Data)
				if err != nil || strings.Contains(string(encoded), code) {
					t.Fatal("状态查询重新返回了授权码明文")
				}
				mustSucceed(call(SetProtocolCredentialEnabled, http.MethodPut, fmt.Sprintf(`{"protocol":%q,"enabled":false}`, protocol)))
				if login(code) {
					t.Fatal("关闭协议后实际认证入口仍放行")
				}
				mustSucceed(call(SetProtocolCredentialEnabled, http.MethodPut, fmt.Sprintf(`{"protocol":%q,"enabled":true}`, protocol)))
				if !login(code) {
					t.Fatal("重新开启协议后有效授权码未恢复")
				}
				mustSucceed(call(RevokeProtocolCredential, http.MethodPost, body))
				if login(code) {
					t.Fatal("撤销授权码后实际认证入口仍放行")
				}
				for _, otherProtocol := range credentials.SupportedProtocols() {
					if !service.VerifyProtocolCode(victim.ID, otherProtocol, victimCodes[otherProtocol]) {
						t.Fatal("当前角色的操作影响了其他用户")
					}
				}
			}
		})
	}
}
