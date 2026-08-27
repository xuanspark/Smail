package imap_server

import (
	"crypto/tls"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
)

var instanceTLS *imapserver.Server

func Stop() {
	if instanceTLS != nil {
		if err := instanceTLS.Close(); err != nil {
			log.WithError(err).Warn("close IMAP server failed")
		}
		instanceTLS = nil
	}
}

// StarTLS 启动TLS端口监听，不加密的代码就懒得写了
func StarTLS() error {
	server, err := newIMAPServer()
	if err != nil {
		return err
	}
	instanceTLS = server
	log.Infof("IMAP With TLS Server Start On Port :993")
	if err := instanceTLS.ListenAndServeTLS(":993"); err != nil {
		return fmt.Errorf("serve IMAP TLS: %w", err)
	}
	return nil
}

func newIMAPServer() (*imapserver.Server, error) {
	cfg := config.CurrentConfig()
	if cfg == nil {
		return nil, fmt.Errorf("runtime configuration is unavailable")
	}
	crt, err := tls.LoadX509KeyPair(cfg.SSLPublicKeyPath, cfg.SSLPrivateKeyPath)
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
