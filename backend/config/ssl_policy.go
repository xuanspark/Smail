package config

// UsesProxyHTTPS 表示网页 HTTPS 由反向代理终止，不代表邮件协议可以关闭 TLS。
func (c *Config) UsesProxyHTTPS() bool {
	if c == nil {
		return false
	}
	return ResolveHTTPPortDefaults(HTTPConfig{TLSMode: c.TLSMode}, 0).TLSMode == HTTPSTLSModeProxy
}

// EffectiveSSLType 以证书管理责任为准，仅在旧配置未记录管理方式时兼容旧类型。
func (c *Config) EffectiveSSLType() string {
	if c == nil || c.UsesProxyHTTPS() {
		return SSLTypeUser
	}
	management := c.SSL.Management
	if management == "" && c.SSL.Mode != "" {
		management = EffectiveSSLManagement(c.SSL)
	}
	switch management {
	case SSLManagementProject:
		if c.SSL.Challenge == SSLChallengeDNS || (c.SSL.Challenge == "" && c.SSLType == SSLTypeAutoDNS) {
			return SSLTypeAutoDNS
		}
		return SSLTypeAutoHTTP
	case "":
		if c.SSLType == SSLTypeAutoHTTP || c.SSLType == SSLTypeAutoDNS {
			return c.SSLType
		}
	}
	return SSLTypeUser
}

// AutomaticSSLEnabled 仅允许项目自托管证书自动签发和续期。
func (c *Config) AutomaticSSLEnabled() bool {
	return c.EffectiveSSLType() != SSLTypeUser
}
