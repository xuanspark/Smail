package config

// ResolvePublicHTTPSPort 只从受控配置确定公网端口，不改变内部监听端口。
// 未设置时保留直连端口及 HTTP 反代模式的 443 默认值；非法显式值交由调用方拒绝。
func ResolvePublicHTTPSPort(value HTTPConfig) int {
	if value.PublicHTTPSPort != 0 {
		return value.PublicHTTPSPort
	}
	resolved := ResolveHTTPPortDefaults(value, 0)
	if resolved.TLSMode == HTTPSTLSModeProxy {
		return 443
	}
	return resolved.HTTPSPort
}
