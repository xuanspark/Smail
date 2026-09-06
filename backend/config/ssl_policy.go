package config

import "strings"

// UsesProxyHTTPS 表示网页 HTTPS 由反向代理终止，不代表邮件协议可以关闭 TLS。
func (c *Config) UsesProxyHTTPS() bool {
	return c != nil && strings.EqualFold(strings.TrimSpace(c.TLSMode), HTTPSTLSModeProxy)
}

// AutomaticSSLEnabled 保留原证书策略，但反代模式下禁止 Smail 自动签发和续期。
func (c *Config) AutomaticSSLEnabled() bool {
	return c != nil && !c.UsesProxyHTTPS() && (c.SSLType == SSLTypeAutoHTTP || c.SSLType == SSLTypeAutoDNS)
}
