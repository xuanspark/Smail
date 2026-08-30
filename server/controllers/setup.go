package controllers

import (
	"encoding/json"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/setup"
	"git.pcux.cn/xuans/Smail/services/setup/ssl"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	"io"
	"net/http"
	"strings"
)

func AcmeChallenge(w http.ResponseWriter, r *http.Request) {
	const prefix = "/.well-known/acme-challenge/"
	if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}

	token := strings.TrimPrefix(r.URL.Path, prefix)
	if token == "" || len(token) > 256 || strings.ContainsAny(token, "/\\") {
		http.NotFound(w, r)
		return
	}

	instance := ssl.GetHttpChallengeInstance()
	keyAuth, exist := instance.KeyAuthorization(token)
	if exist {
		if _, err := w.Write([]byte(keyAuth)); err != nil {
			log.WithError(err).Warn("write ACME challenge response failed")
		}
	} else {
		log.Warn("ACME challenge token not found")
		http.NotFound(w, r)
	}
}

type sslResponse struct {
	Port         int    `json:"port"`
	HTTPPort     int    `json:"httpPort"`
	HTTPSPort    int    `json:"httpsPort"`
	TLSMode      string `json:"tlsMode"`
	HTTPSEnabled bool   `json:"httpsEnabled"`
	WebDomain    string `json:"webDomain"`
	Type         string `json:"type"`
	Ready        bool   `json:"ready"`
}

func buildSetupSSLResponse(sslType string, cfg *config.Config, httpConfig config.HTTPConfig, webDomain string, ready bool) sslResponse {
	httpPort := httpConfig.Port
	if httpPort <= 0 && cfg != nil {
		httpPort = cfg.GetSetupPort()
	}
	httpsPort := httpConfig.HTTPSPort
	if httpsPort <= 0 {
		httpsPort = 443
	}
	return sslResponse{
		Port:         httpPort,
		HTTPPort:     httpPort,
		HTTPSPort:    httpsPort,
		TLSMode:      httpConfig.TLSMode,
		HTTPSEnabled: httpConfig.HTTPSEnabled,
		WebDomain:    webDomain,
		Type:         sslType,
		Ready:        ready,
	}
}

func currentSetupSSLResponse(sslType string) sslResponse {
	cfg := config.CurrentConfig()
	return buildSetupSSLResponse(sslType, cfg, config.CurrentHTTPConfig(), config.CurrentWebDomain(), config.IsSetupReady())
}

func writeSetupError(ctx *context.Context, w http.ResponseWriter, err error) {
	// 保留底层错误原因，便于定位数据库驱动、网络和权限问题；响应仍只返回安全的业务提示。
	log.WithContext(ctx).WithError(err).Errorf("setup request failed: %v", err)
	message := setup.DatabaseSetupErrorMessage(err)
	if message == "" {
		message = "\u521d\u59cb\u5316\u64cd\u4f5c\u5931\u8d25"
	}
	response.NewErrorResponse(response.ServerError, message, "").FPrint(w)
}

func Setup(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	req.Body = http.MaxBytesReader(w, req.Body, 64<<10)
	reqBytes, err := io.ReadAll(req.Body)
	if err != nil {
		log.WithContext(ctx).WithError(err).Info("setup request body rejected")
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}

	var reqData map[string]string
	err = json.Unmarshal(reqBytes, &reqData)
	if err != nil {
		log.WithContext(ctx).WithError(err).Info("setup request json rejected")
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}

	if reqData["step"] == "database" && reqData["action"] == "get" {
		settings, err := setup.GetDatabaseSettingsDetail(ctx)
		if err != nil {
			writeSetupError(ctx, w, err)
			return
		}

		response.NewSuccessResponse(settings).FPrint(w)
		return
	}

	if reqData["step"] == "database" && reqData["action"] == "set" {
		err := setup.SetDatabaseSettingsWithOptions(ctx, setup.DatabaseSettings{
			Type:     cast.ToString(reqData["db_type"]),
			DSN:      cast.ToString(reqData["db_dsn"]),
			Mode:     cast.ToString(reqData["db_mode"]),
			User:     cast.ToString(reqData["db_user"]),
			Host:     cast.ToString(reqData["db_host"]),
			Port:     cast.ToString(reqData["db_port"]),
			Password: cast.ToString(reqData["db_password"]),
			Name:     cast.ToString(reqData["db_name"]),
		})
		if err != nil {
			writeSetupError(ctx, w, err)
			return
		}

		response.NewSuccessResponse("Succ").FPrint(w)
		return
	}

	if reqData["step"] == "password" && reqData["action"] == "get" {
		ok, err := setup.GetAdminPassword(ctx)
		if err != nil {
			writeSetupError(ctx, w, err)
			return
		}
		response.NewSuccessResponse(ok).FPrint(w)
		return
	}

	if reqData["step"] == "password" && reqData["action"] == "set" {
		err := setup.SetAdminPassword(ctx, cast.ToString(reqData["account"]), cast.ToString(reqData["password"]))
		if err != nil {
			writeSetupError(ctx, w, err)
			return
		}
		response.NewSuccessResponse("Succ").FPrint(w)
		return
	}

	if reqData["step"] == "domain" && reqData["action"] == "get" {
		smtpDomain, webDomain, domains, err := setup.GetDomainSettings()
		if err != nil {
			writeSetupError(ctx, w, err)
			return
		}
		response.NewSuccessResponse(map[string]any{
			"smtp_domain": smtpDomain,
			"web_domain":  webDomain,
			"domains":     domains,
		}).FPrint(w)
		return
	}

	if reqData["step"] == "domain" && reqData["action"] == "set" {
		err := setup.SetDomainSettings(cast.ToString(reqData["smtp_domain"]), cast.ToString(reqData["web_domain"]), reqData["multi_domain"])
		if err != nil {
			writeSetupError(ctx, w, err)
			return
		}
		response.NewSuccessResponse("Succ").FPrint(w)
		return
	}

	if reqData["step"] == "dns" && reqData["action"] == "get" {
		dnsInfos, err := setup.GetDNSSettings(ctx)
		if err != nil {
			writeSetupError(ctx, w, err)
			return
		}
		response.NewSuccessResponse(dnsInfos).FPrint(w)
		return
	}

	if reqData["step"] == "ssl" && reqData["action"] == "get" {
		sslType, err := ssl.GetSSL()
		if err != nil {
			writeSetupError(ctx, w, err)
			return
		}
		response.NewSuccessResponse(currentSetupSSLResponse(sslType)).FPrint(w)
		return
	}

	if reqData["step"] == "ssl" && reqData["action"] == "getParams" {
		dnsChallenge := ssl.GetDnsChallengeInstance()

		response.NewSuccessResponse(dnsChallenge.GetDNSSettings(ctx)).FPrint(w)
		return
	}

	if reqData["step"] == "ssl" && reqData["action"] == "set" {
		err = ssl.SetSSL(cast.ToString(reqData["ssl_type"]), cast.ToString(reqData["key_path"]), cast.ToString(reqData["crt_path"]))
		if err != nil {
			writeSetupError(ctx, w, err)
			return
		}

		if reqData["ssl_type"] == config.SSLTypeAutoHTTP || reqData["ssl_type"] == config.SSLTypeAutoDNS {
			err = ssl.GenSSL(false)
			if err != nil {
				writeSetupError(ctx, w, err)
				return
			}
		}

		if reqData["ssl_type"] == config.SSLTypeUser {
			if err := setup.Finish(); err != nil {
				writeSetupError(ctx, w, err)
				return
			}
		}
		response.NewSuccessResponse("Succ").FPrint(w)
		return
	}

	// 初始化接口必须始终返回统一 JSON，避免未知步骤被当成空响应，
	// 进而让前端误判请求已完成并进入错误页面。
	response.NewErrorResponse(response.ParamsError, "不支持的初始化操作", "").FPrint(w)
}
