package config

const (
	MailSecurityBoth        = "both"
	MailSecuritySTARTTLS    = "starttls"
	MailSecurityImplicitTLS = "implicit_tls"
)

// 邮件监听与发信中继相互独立。全部启用模式同时开放两套安全监听。
type SMTPListenerConfig struct {
	Port         int    `json:"port"`
	STARTTLSPort int    `json:"starttlsPort"`
	TLSPort      int    `json:"tlsPort"`
	Security     string `json:"security,omitempty"`
}

type IMAPListenerConfig struct {
	TLSPort int `json:"tlsPort"`
}

type POP3ListenerConfig struct {
	Port    int `json:"port"`
	TLSPort int `json:"tlsPort"`
}

func ResolveMailSecurity(listener SMTPListenerConfig) string {
	switch NormalizeMailSecurity(listener.Security) {
	case MailSecuritySTARTTLS:
		return MailSecuritySTARTTLS
	case MailSecurityImplicitTLS:
		return MailSecurityImplicitTLS
	case "", MailSecurityBoth:
		return MailSecurityBoth
	default:
		return listener.Security
	}
}

// NormalizeMailSecurity 保留空值及未知值，只将旧版 TLS 名称规范为当前名称。
func NormalizeMailSecurity(value string) string {
	if value == "tls" {
		return MailSecurityImplicitTLS
	}
	return value
}

// PublicMailSecurity 保留旧配置的空值，只规范公开视图中的旧版名称。
func PublicMailSecurity(listener SMTPListenerConfig) string {
	return NormalizeMailSecurity(listener.Security)
}

// ResolveMailPorts 是公开配置视图、冲突校验及真实监听的共同端口来源。
func ResolveMailPorts(smtp SMTPSectionConfig, email EmailSectionConfig) MailPorts {
	portOr := func(port, fallback int) int {
		if validPort(port) {
			return port
		}
		return fallback
	}
	ports := MailPorts{
		SMTP:         portOr(smtp.Listener.Port, portOr(email.Policy.SMTPPort, 25)),
		SMTPSTARTTLS: portOr(smtp.Listener.STARTTLSPort, 587),
		SMTPTLS:      portOr(smtp.Listener.TLSPort, 465),
		// 旧版 imapPort 从未用于监听；保留原有的 993，避免升级后擅自切换客户端端口。
		IMAP:    portOr(email.IMAP.TLSPort, 993),
		POP3:    portOr(email.POP3.Port, portOr(email.Policy.POP3Port, 110)),
		POP3TLS: portOr(email.POP3.TLSPort, 995),
	}
	switch ResolveMailSecurity(smtp.Listener) {
	case MailSecuritySTARTTLS:
		ports.SMTPTLS = 0
		ports.IMAP = portOr(email.IMAP.TLSPort, 143)
		ports.POP3TLS = 0
	case MailSecurityImplicitTLS:
		ports.SMTPSTARTTLS = 0
		ports.IMAP = portOr(email.IMAP.TLSPort, 993)
		ports.POP3 = 0
	}
	return ports
}

func validateMailListeners(smtp SMTPSectionConfig, email EmailSectionConfig) error {
	security := ResolveMailSecurity(smtp.Listener)
	if security != MailSecurityBoth && security != MailSecuritySTARTTLS && security != MailSecurityImplicitTLS {
		return ErrInvalidRuntimeConfig
	}
	for _, port := range []int{
		smtp.Listener.Port, smtp.Listener.STARTTLSPort, smtp.Listener.TLSPort,
		email.IMAP.TLSPort, email.POP3.Port, email.POP3.TLSPort,
	} {
		if port != 0 && !validPort(port) {
			return ErrPortConflict
		}
	}
	return nil
}
