package controllers

import (
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

const (
	maxPluginSettingsPageBytes = 1 << 20
	maxPluginSettingsSaveBytes = 4 << 10
)

func GetPluginList(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if err := authorizeCapability(ctx, access.CapabilityPluginRead, "", false); err != nil {
		response.NewErrorResponse(response.NoAccessPrivileges, "\u65e0\u6743\u8bbf\u95ee", "").FPrint(w)
		return
	}
	ret := []string{}
	for _, s := range hooks.HookNames() {
		ret = append(ret, s)
	}
	response.NewSuccessResponse(ret).FPrint(w)

}

func SettingsHtml(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if err := authorizeCapability(ctx, access.CapabilityPluginReadGlobal, "", false); err != nil {
		response.NewErrorResponse(response.NoAccessPrivileges, "\u65e0\u6743\u8bbf\u95ee", "").FPrint(w)
		return
	}
	rawPath := strings.TrimPrefix(req.URL.Path, "/api/plugin/settings/")
	parts := strings.Split(rawPath, "/")
	if len(parts) < 2 {
		response.NewErrorResponse(response.ParamsError, "404", "").FPrint(w)
		return
	}

	pluginName, err := url.PathUnescape(parts[0])
	if err != nil || pluginName == "" || pluginName == "." || pluginName == ".." || path.Base(pluginName) != pluginName {
		response.NewErrorResponse(response.ParamsError, "404", "").FPrint(w)
		return
	}
	resourcePath := path.Clean(strings.Join(parts[1:], "/"))
	if resourcePath == "." || strings.HasPrefix(resourcePath, "../") || strings.Contains(resourcePath, "\\") {
		response.NewErrorResponse(response.ParamsError, "404", "").FPrint(w)
		return
	}

	plugin, ok := hooks.GetHook(pluginName)
	if !ok {
		response.NewErrorResponse(response.ParamsError, "404", "").FPrint(w)
		return
	}
	if req.Method == http.MethodGet {
		if resourcePath != "index.html" && !strings.HasSuffix(resourcePath, ".js") && !strings.HasSuffix(resourcePath, ".css") {
			response.NewErrorResponse(response.ParamsError, "404", "").FPrint(w)
			return
		}
	} else if req.Method != http.MethodPost || resourcePath != "save" {
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte("旧版插件配置保存接口已停用，请使用新版插件配置接口"))
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, 64<<10)
	requestData, err := io.ReadAll(req.Body)
	if err != nil {
		response.NewErrorResponse(response.ParamsError, "request body is invalid", "").FPrint(w)
		return
	}
	content := plugin.SettingsHtml(ctx, path.Join(pluginName, resourcePath), string(requestData))
	responseLimit := maxPluginSettingsPageBytes
	if req.Method == http.MethodPost {
		responseLimit = maxPluginSettingsSaveBytes
	}
	if len(content) > responseLimit {
		log.WithContext(ctx).Warn("plugin settings response exceeded size limit")
		response.NewErrorResponse(response.ServerError, "", "").FPrint(w)
		return
	}

	if req.Method == http.MethodGet {
		if strings.HasSuffix(resourcePath, ".js") {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		} else if strings.HasSuffix(resourcePath, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	if _, err := w.Write([]byte(content)); err != nil {
		log.WithContext(ctx).WithError(err).Error("write plugin settings response failed")
	}
}
