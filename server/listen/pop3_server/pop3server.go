package pop3_server

import (
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/internal/gopop"
	log "github.com/sirupsen/logrus"
	"time"
)

var instance *gopop.Server
var instanceTls *gopop.Server

func StartWithTls() error {
	cfg := config.CurrentConfig()
	if cfg == nil {
		return fmt.Errorf("runtime configuration is unavailable")
	}
	crt, err := config.LoadRuntimeTLSCertificate(cfg)
	if err != nil {
		return fmt.Errorf("load POP3 TLS certificate: %w", err)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	tlsConfig.Certificates = []tls.Certificate{crt}
	tlsConfig.Time = time.Now
	tlsConfig.Rand = rand.Reader
	instanceTls = gopop.NewPop3Server(995, "pop."+cfg.Domain, true, tlsConfig, action{})
	instanceTls.ConnectAliveTime = 5 * time.Minute

	log.Infof("POP3 With TLS Server Start On Port :995")

	err = instanceTls.Start()
	if err != nil {
		return fmt.Errorf("serve POP3 TLS: %w", err)
	}
	return nil
}

func Start() error {
	cfg := config.CurrentConfig()
	if cfg == nil {
		return fmt.Errorf("runtime configuration is unavailable")
	}
	crt, err := config.LoadRuntimeTLSCertificate(cfg)
	if err != nil {
		return fmt.Errorf("load POP3 certificate: %w", err)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	tlsConfig.Certificates = []tls.Certificate{crt}
	tlsConfig.Time = time.Now
	tlsConfig.Rand = rand.Reader
	instance = gopop.NewPop3Server(110, "pop."+cfg.Domain, false, tlsConfig, action{})
	instance.ConnectAliveTime = 5 * time.Minute
	log.Infof("POP3 Server Start On Port :110")

	err = instance.Start()
	if err != nil {
		return fmt.Errorf("serve POP3: %w", err)
	}
	return nil
}

func Stop() {
	if instance != nil {
		instance.Stop()
		instance = nil
	}

	if instanceTls != nil {
		instanceTls.Stop()
		instanceTls = nil
	}
}
