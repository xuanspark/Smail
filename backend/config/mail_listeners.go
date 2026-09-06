package config

// 邮件监听与发信中继相互独立。零值表示沿用兼容默认值，不表示关闭服务。
type SMTPListenerConfig struct {
	Port         int `json:"port"`
	STARTTLSPort int `json:"starttlsPort"`
	TLSPort      int `json:"tlsPort"`
}

type IMAPListenerConfig struct {
	TLSPort int `json:"tlsPort"`
}

type POP3ListenerConfig struct {
	Port    int `json:"port"`
	TLSPort int `json:"tlsPort"`
}

// ResolveMailPorts 是公开配置视图、冲突校验及真实监听的共同端口来源。
func ResolveMailPorts(smtp SMTPSectionConfig, email EmailSectionConfig) MailPorts {
	portOr := func(port, fallback int) int {
		if validPort(port) {
			return port
		}
		return fallback
	}
	return MailPorts{
		SMTP:         portOr(smtp.Listener.Port, portOr(email.Policy.SMTPPort, 25)),
		SMTPSTARTTLS: portOr(smtp.Listener.STARTTLSPort, 587),
		SMTPTLS:      portOr(smtp.Listener.TLSPort, 465),
		// 旧版 imapPort 从未用于监听；保留原有的 993，避免升级后擅自切换客户端端口。
		IMAP:    portOr(email.IMAP.TLSPort, 993),
		POP3:    portOr(email.POP3.Port, portOr(email.Policy.POP3Port, 110)),
		POP3TLS: portOr(email.POP3.TLSPort, 995),
	}
}

func validateMailListeners(smtp SMTPSectionConfig, email EmailSectionConfig) error {
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
