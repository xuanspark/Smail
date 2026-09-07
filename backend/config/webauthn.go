package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ResolveWebAuthnSecurity 为旧配置补足安全密钥站点信息，不修改持久化配置或用户开关。
// 信赖方只能来自受控配置，不能根据请求 Host 或转发头自动扩展。
func ResolveWebAuthnSecurity(root RootConfig) (SecurityConfig, error) {
	security := root.Security
	security.WebAuthnRPOrigins = append([]string(nil), security.WebAuthnRPOrigins...)
	web, err := normalizeConfigDomain(root.Domains.Web)
	if err != nil {
		return SecurityConfig{}, fmt.Errorf("安全密钥缺少有效的站点域名")
	}
	if security.WebAuthnRPID == "" {
		security.WebAuthnRPID = web
	}
	rpID, err := normalizeConfigDomain(security.WebAuthnRPID)
	if err != nil || rpID != security.WebAuthnRPID {
		return SecurityConfig{}, fmt.Errorf("安全密钥信赖方标识必须是规范域名")
	}
	if security.WebAuthnRPDisplayName == "" {
		security.WebAuthnRPDisplayName = "Smail"
	}
	if strings.TrimSpace(security.WebAuthnRPDisplayName) == "" {
		return SecurityConfig{}, fmt.Errorf("安全密钥站点显示名称不能为空")
	}
	if len(security.WebAuthnRPOrigins) == 0 {
		origin := "https://" + web
		httpConfig := ResolveHTTPPortDefaults(root.HTTP, 0)
		// 反代的外部端口不等于内部 HTTP 端口，非标准外部端口需显式配置。
		if httpConfig.TLSMode != HTTPSTLSModeProxy {
			port := httpConfig.HTTPSPort
			if !validServicePort(port) {
				return SecurityConfig{}, fmt.Errorf("安全密钥站点 HTTPS 端口无效")
			}
			if port != 443 {
				origin += ":" + strconv.Itoa(port)
			}
		}
		security.WebAuthnRPOrigins = []string{origin}
	}
	for _, origin := range security.WebAuthnRPOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" ||
			parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
			strings.ContainsAny(origin, "#?\\") {
			return SecurityConfig{}, fmt.Errorf("安全密钥来源必须是完整且不含路径的站点地址")
		}
		host := parsed.Hostname()
		normalizedHost, err := normalizeConfigDomain(host)
		if err != nil || normalizedHost != host || strings.HasSuffix(parsed.Host, ":") {
			return SecurityConfig{}, fmt.Errorf("安全密钥来源必须使用规范域名和有效端口")
		}
		if host != rpID && !strings.HasSuffix(host, "."+rpID) {
			return SecurityConfig{}, fmt.Errorf("安全密钥来源不属于配置信赖方")
		}
		if parsed.Scheme != "https" && !(parsed.Scheme == "http" && host == "localhost") {
			return SecurityConfig{}, fmt.Errorf("安全密钥来源必须使用 HTTPS，本机 localhost 除外")
		}
		if port := parsed.Port(); port != "" {
			number, err := strconv.Atoi(port)
			if err != nil || !validServicePort(number) {
				return SecurityConfig{}, fmt.Errorf("安全密钥来源端口无效")
			}
		}
	}
	return security, nil
}
