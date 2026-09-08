package smtp_server

import (
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"github.com/emersion/go-smtp"
	log "github.com/sirupsen/logrus"
)

const (
	maxSMTPMessageBytes       = 30 << 20
	maxSMTPAttachmentCount    = 50
	maxSMTPRuleProcessingTime = 2 * time.Second
)

var instance *smtp.Server
var instanceTls *smtp.Server
var instanceTlsNew *smtp.Server

func StartWithTLSNew() error {
	cfg := config.CurrentConfig()
	if cfg == nil {
		return fmt.Errorf("runtime configuration is unavailable")
	}
	domains := config.CurrentMailDomains()
	if domains.Primary == "" {
		return fmt.Errorf("mail domain is not configured")
	}
	be := &Backend{}

	instanceTlsNew = smtp.NewServer(be)

	instanceTlsNew.Addr = fmt.Sprintf(":%d", config.CurrentMailPorts().SMTPSTARTTLS)
	instanceTlsNew.Domain = domains.Primary
	instanceTlsNew.ReadTimeout = 10 * time.Second
	instanceTlsNew.WriteTimeout = 10 * time.Second
	instanceTlsNew.MaxMessageBytes = maxSMTPMessageBytes
	instanceTlsNew.MaxRecipients = 50
	// force TLS for auth
	instanceTlsNew.AllowInsecureAuth = false
	// Load the certificate and key
	cer, err := config.LoadRuntimeTLSCertificate(cfg)
	if err != nil {
		return fmt.Errorf("load SMTP STARTTLS certificate: %w", err)
	}
	// Configure the TLS support for STARTTLS
	instanceTlsNew.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cer}, MinVersion: tls.VersionTLS12}

	log.WithFields(log.Fields{"action": "启动 SMTP STARTTLS 监听", "object": instanceTlsNew.Addr, "result": "started"}).Info("开始绑定监听端口")
	// 提交端口使用 STARTTLS（先连接，再升级 TLS），而非隐式 TLS。
	if err := instanceTlsNew.ListenAndServe(); err != nil && !errors.Is(err, smtp.ErrServerClosed) {
		return fmt.Errorf("serve SMTP STARTTLS: %w", err)
	}
	return nil
}

func StartWithTLS() error {
	cfg := config.CurrentConfig()
	if cfg == nil {
		return fmt.Errorf("runtime configuration is unavailable")
	}
	domains := config.CurrentMailDomains()
	if domains.Primary == "" {
		return fmt.Errorf("mail domain is not configured")
	}
	be := &Backend{}

	instanceTls = smtp.NewServer(be)

	instanceTls.Addr = fmt.Sprintf(":%d", config.CurrentMailPorts().SMTPTLS)
	instanceTls.Domain = domains.Primary
	instanceTls.ReadTimeout = 10 * time.Second
	instanceTls.WriteTimeout = 10 * time.Second
	instanceTls.MaxMessageBytes = maxSMTPMessageBytes
	instanceTls.MaxRecipients = 50
	// force TLS for auth
	instanceTls.AllowInsecureAuth = true
	// Load the certificate and key
	cer, err := config.LoadRuntimeTLSCertificate(cfg)
	if err != nil {
		return fmt.Errorf("load SMTP TLS certificate: %w", err)
	}
	// Configure the TLS support
	instanceTls.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cer}, MinVersion: tls.VersionTLS12}

	log.WithFields(log.Fields{"action": "启动 SMTP TLS 监听", "object": instanceTls.Addr, "result": "started"}).Info("开始绑定监听端口")
	if err := instanceTls.ListenAndServeTLS(); err != nil && !errors.Is(err, smtp.ErrServerClosed) {
		return fmt.Errorf("serve SMTP TLS: %w", err)
	}
	return nil
}

func Start() error {
	cfg := config.CurrentConfig()
	if cfg == nil {
		return fmt.Errorf("runtime configuration is unavailable")
	}
	domains := config.CurrentMailDomains()
	if domains.Primary == "" {
		return fmt.Errorf("mail domain is not configured")
	}
	be := &Backend{}

	instance = smtp.NewServer(be)

	instance.Addr = fmt.Sprintf(":%d", config.CurrentMailPorts().SMTP)
	instance.Domain = domains.Primary
	instance.ReadTimeout = 10 * time.Second
	instance.WriteTimeout = 10 * time.Second
	instance.MaxMessageBytes = maxSMTPMessageBytes
	instance.MaxRecipients = 50
	// force TLS for auth
	instance.AllowInsecureAuth = false
	// Load the certificate and key
	cer, err := config.LoadRuntimeTLSCertificate(cfg)
	if err != nil {
		return fmt.Errorf("load SMTP certificate: %w", err)
	}
	// Configure the TLS support
	instance.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cer}, MinVersion: tls.VersionTLS12}

	log.WithFields(log.Fields{"action": "启动 SMTP 收信监听", "object": instance.Addr, "result": "started"}).Info("开始绑定监听端口")
	if err := instance.ListenAndServe(); err != nil && !errors.Is(err, smtp.ErrServerClosed) {
		return fmt.Errorf("serve SMTP: %w", err)
	}
	return nil
}

func Stop() {
	if instance != nil {
		_ = instance.Close()
	}
	if instanceTls != nil {
		_ = instanceTls.Close()
	}

	if instanceTlsNew != nil {
		_ = instanceTlsNew.Close()
	}
}
