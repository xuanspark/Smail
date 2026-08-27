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
	be := &Backend{}

	instanceTlsNew = smtp.NewServer(be)

	instanceTlsNew.Addr = ":587"
	instanceTlsNew.Domain = cfg.Domain
	instanceTlsNew.ReadTimeout = 10 * time.Second
	instanceTlsNew.WriteTimeout = 10 * time.Second
	instanceTlsNew.MaxMessageBytes = maxSMTPMessageBytes
	instanceTlsNew.MaxRecipients = 50
	// force TLS for auth
	instanceTlsNew.AllowInsecureAuth = false
	// Load the certificate and key
	cer, err := tls.LoadX509KeyPair(cfg.SSLPublicKeyPath, cfg.SSLPrivateKeyPath)
	if err != nil {
		return fmt.Errorf("load SMTP STARTTLS certificate: %w", err)
	}
	// Configure the TLS support for STARTTLS
	instanceTlsNew.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cer}, MinVersion: tls.VersionTLS12}

	log.Println("Starting Smtp With STARTTLS Server Port:", instanceTlsNew.Addr)
	// 587端口使用STARTTLS（先明文连接，再升级TLS），而非隐式TLS
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
	be := &Backend{}

	instanceTls = smtp.NewServer(be)

	instanceTls.Addr = ":465"
	instanceTls.Domain = cfg.Domain
	instanceTls.ReadTimeout = 10 * time.Second
	instanceTls.WriteTimeout = 10 * time.Second
	instanceTls.MaxMessageBytes = maxSMTPMessageBytes
	instanceTls.MaxRecipients = 50
	// force TLS for auth
	instanceTls.AllowInsecureAuth = true
	// Load the certificate and key
	cer, err := tls.LoadX509KeyPair(cfg.SSLPublicKeyPath, cfg.SSLPrivateKeyPath)
	if err != nil {
		return fmt.Errorf("load SMTP TLS certificate: %w", err)
	}
	// Configure the TLS support
	instanceTls.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cer}, MinVersion: tls.VersionTLS12}

	log.Println("Starting Smtp With SSL Server Port:", instanceTls.Addr)
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
	be := &Backend{}

	instance = smtp.NewServer(be)

	instance.Addr = ":25"
	instance.Domain = cfg.Domain
	instance.ReadTimeout = 10 * time.Second
	instance.WriteTimeout = 10 * time.Second
	instance.MaxMessageBytes = maxSMTPMessageBytes
	instance.MaxRecipients = 50
	// force TLS for auth
	instance.AllowInsecureAuth = false
	// Load the certificate and key
	cer, err := tls.LoadX509KeyPair(cfg.SSLPublicKeyPath, cfg.SSLPrivateKeyPath)
	if err != nil {
		return fmt.Errorf("load SMTP certificate: %w", err)
	}
	// Configure the TLS support
	instance.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cer}, MinVersion: tls.VersionTLS12}

	log.Println("Starting Smtp Server Port:", instance.Addr)
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
