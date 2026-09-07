package imap_server

import (
	"crypto/tls"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
)

var instance *imapserver.Server
var instanceTLS *imapserver.Server

func Stop() {
	if instance != nil {
		if err := instance.Close(); err != nil {
			log.WithError(err).Warn("close IMAP STARTTLS server failed")
		}
		instance = nil
	}
	if instanceTLS != nil {
		if err := instanceTLS.Close(); err != nil {
			log.WithError(err).Warn("close IMAP server failed")
		}
		instanceTLS = nil
	}
}

// Start 启动普通 IMAP 监听，认证前必须通过 STARTTLS 升级连接。
func Start() error {
	server, err := newIMAPServer()
	if err != nil {
		return err
	}
	instance = server
	imapPort := config.CurrentMailPorts().IMAP
	log.Infof("IMAP With STARTTLS Server Start On Port :%d", imapPort)
	if err := instance.ListenAndServe(fmt.Sprintf(":%d", imapPort)); err != nil {
		return fmt.Errorf("serve IMAP STARTTLS: %w", err)
	}
	return nil
}

// StarTLS 启动隐式 TLS 监听，不提供明文认证入口。
func StarTLS() error {
	server, err := newIMAPServer()
	if err != nil {
		return err
	}
	instanceTLS = server
	imapTLSPort := config.CurrentMailPorts().IMAP
	log.Infof("IMAP With TLS Server Start On Port :%d", imapTLSPort)
	if err := instanceTLS.ListenAndServeTLS(fmt.Sprintf(":%d", imapTLSPort)); err != nil {
		return fmt.Errorf("serve IMAP TLS: %w", err)
	}
	return nil
}

func newIMAPServer() (*imapserver.Server, error) {
	cfg := config.CurrentConfig()
	if cfg == nil {
		return nil, fmt.Errorf("runtime configuration is unavailable")
	}
	crt, err := config.LoadRuntimeTLSCertificate(cfg)
	if err != nil {
		return nil, fmt.Errorf("load IMAP TLS certificate: %w", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{crt},
		MinVersion:   tls.VersionTLS12,
	}

	memServer := NewServer()

	option := &imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memServer.NewSession(conn.NetConn().RemoteAddr()), nil, nil
		},
		Caps: imap.CapSet{
			imap.CapIMAP4rev1: {},
			imap.CapIdle:      {},
		},
		TLSConfig:    tlsConfig,
		InsecureAuth: false,
	}

	return imapserver.New(option), nil
}
