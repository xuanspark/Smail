package controllers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks"
	hooksframework "git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/utils/context"
)

type pluginTestHook struct {
	resource string
	body     string
	response string
}

func (h *pluginTestHook) ReceiveSaveAfter(ctx *context.Context, email *parsemail.Email, userEmails []*models.UserEmail) {
}
func (h *pluginTestHook) SendBefore(ctx *context.Context, email *parsemail.Email) {}
func (h *pluginTestHook) SendAfter(ctx *context.Context, email *parsemail.Email, errs map[string]error) {
}
func (h *pluginTestHook) ReceiveParseBefore(ctx *context.Context, email *[]byte)         {}
func (h *pluginTestHook) ReceiveParseAfter(ctx *context.Context, email *parsemail.Email) {}
func (h *pluginTestHook) GetName(ctx *context.Context) string                            { return "PluginTest" }
func (h *pluginTestHook) SettingsHtml(ctx *context.Context, resource string, body string) string {
	h.resource = resource
	h.body = body
	if h.response != "" {
		return h.response
	}
	return "ok"
}

func TestSettingsHtmlRejectsPathTraversal(t *testing.T) {
	oldHooks := hooks.HookList
	defer func() { hooks.HookList = oldHooks }()
	hooks.HookList = map[string]hooksframework.EmailHook{}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/plugin/settings/%2e%2e/index.html", nil)
	SettingsHtml(&context.Context{UserID: 1, Role: access.RoleSuperAdmin}, recorder, request)
	if strings.Contains(recorder.Body.String(), "ok") {
		t.Fatal("path traversal reached a plugin")
	}
}

func TestSettingsHtmlSeparatesStaticAndSaveRequests(t *testing.T) {
	oldHooks := hooks.HookList
	defer func() { hooks.HookList = oldHooks }()
	hook := &pluginTestHook{}
	hooks.HookList = map[string]hooksframework.EmailHook{"PluginTest": hook}

	getRecorder := httptest.NewRecorder()
	SettingsHtml(&context.Context{UserID: 1, Role: access.RoleSuperAdmin}, getRecorder, httptest.NewRequest(http.MethodGet, "/api/plugin/settings/PluginTest/index.html", nil))
	if hook.resource != "PluginTest/index.html" || !strings.HasPrefix(getRecorder.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("static plugin page was not served safely: resource=%s type=%s", hook.resource, getRecorder.Header().Get("Content-Type"))
	}

	postRecorder := httptest.NewRecorder()
	SettingsHtml(&context.Context{UserID: 1, Role: access.RoleSuperAdmin}, postRecorder, httptest.NewRequest(http.MethodPost, "/api/plugin/settings/PluginTest/save", strings.NewReader(`{"url":"https://model.example.com"}`)))
	if postRecorder.Code != http.StatusGone {
		t.Fatalf("legacy plugin save status = %d, want %d", postRecorder.Code, http.StatusGone)
	}
	if hook.resource != "PluginTest/index.html" || hook.body != "" {
		t.Fatalf("stopped plugin save request reached the plugin: resource=%s body=%s", hook.resource, hook.body)
	}
}

func TestSettingsHtmlRejectsOversizedPluginResponse(t *testing.T) {
	oldHooks := hooks.HookList
	defer func() { hooks.HookList = oldHooks }()
	hook := &pluginTestHook{response: strings.Repeat("a", maxPluginSettingsPageBytes+1)}
	hooks.HookList = map[string]hooksframework.EmailHook{"PluginTest": hook}

	recorder := httptest.NewRecorder()
	SettingsHtml(&context.Context{UserID: 1, Role: access.RoleSuperAdmin}, recorder, httptest.NewRequest(http.MethodGet, "/api/plugin/settings/PluginTest/index.html", nil))
	if strings.Contains(recorder.Body.String(), hook.response) {
		t.Fatal("oversized plugin response was returned")
	}
}

func TestSettingsHtmlRejectsNonAdminSave(t *testing.T) {
	oldHooks := hooks.HookList
	defer func() { hooks.HookList = oldHooks }()
	hook := &pluginTestHook{}
	hooks.HookList = map[string]hooksframework.EmailHook{"PluginTest": hook}

	recorder := httptest.NewRecorder()
	SettingsHtml(&context.Context{}, recorder, httptest.NewRequest(http.MethodPost, "/api/plugin/settings/PluginTest/save", strings.NewReader(`{"url":"https://model.example.com"}`)))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "无权访问") {
		t.Fatalf("non-admin plugin save was not rejected: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if hook.resource != "" || hook.body != "" {
		t.Fatal("non-admin plugin save reached the plugin")
	}
}

func TestPluginConfigurationRejectsNonAdminAccess(t *testing.T) {
	oldHooks := hooks.HookList
	defer func() { hooks.HookList = oldHooks }()
	hook := &pluginTestHook{}
	hooks.HookList = map[string]hooksframework.EmailHook{"PluginTest": hook}

	listRecorder := httptest.NewRecorder()
	GetPluginList(&context.Context{}, listRecorder, httptest.NewRequest(http.MethodGet, "/api/plugin/list", nil))
	if !strings.Contains(listRecorder.Body.String(), "无权访问") {
		t.Fatalf("unauthenticated plugin list was not rejected: body=%s", listRecorder.Body.String())
	}

	userListRecorder := httptest.NewRecorder()
	GetPluginList(&context.Context{UserID: 2, Role: access.RoleUser}, userListRecorder, httptest.NewRequest(http.MethodGet, "/api/plugin/list", nil))
	if !strings.Contains(userListRecorder.Body.String(), "无权访问") {
		t.Fatalf("ordinary user can read global plugin list: body=%s", userListRecorder.Body.String())
	}

	pageRecorder := httptest.NewRecorder()
	SettingsHtml(&context.Context{UserID: 2, Role: access.RoleUser}, pageRecorder, httptest.NewRequest(http.MethodGet, "/api/plugin/settings/PluginTest/index.html", nil))
	if !strings.Contains(pageRecorder.Body.String(), "无权访问") {
		t.Fatalf("non-admin plugin page was not rejected: body=%s", pageRecorder.Body.String())
	}
	if hook.resource != "" || hook.body != "" {
		t.Fatal("non-admin plugin page reached the plugin")
	}
}
