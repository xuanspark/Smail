package config

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

func encodeSSLCertificates(certificates []*x509.Certificate) []byte {
	var result []byte
	for _, certificate := range certificates {
		result = append(result, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})...)
	}
	return result
}

// 所有协议共享已完整校验的内存证书；只影响新握手，不关闭既有连接。
var liveTLS = struct {
	sync.Mutex
	root        string
	certificate *tls.Certificate
	checked     time.Time
	warned      time.Time
}{}

func CurrentCertificateConfig() *Config {
	cfg := CurrentConfig()
	if cfg == nil {
		return nil
	}
	if manager := CurrentManager(); manager != nil {
		runtime := manager.Snapshot()
		cfg.SSL = runtime.SSL.TLS
		cfg.Domain, cfg.WebDomain = runtime.Root.Domains.Primary, runtime.Root.Domains.Web
		cfg.Domains = runtime.Root.Domains.Accepted
		cfg.AdminRecoveryDomain = runtime.Root.Domains.AdminRecovery
		cfg.TLSMode = runtime.Root.HTTP.TLSMode
		cfg.HttpsEnabled = 2
		if runtime.Root.HTTP.HTTPSEnabled {
			cfg.HttpsEnabled = 1
		}
		cfg.SSLType = SSLTypeUser
		if EffectiveSSLManagement(cfg.SSL) == SSLManagementProject {
			cfg.SSLType = SSLTypeAutoHTTP
			if cfg.SSL.Challenge == SSLChallengeDNS {
				cfg.SSLType = SSLTypeAutoDNS
			}
		}
	}
	return cfg
}

func runtimeTLSCertificate(force bool) (*tls.Certificate, error) {
	liveTLS.Lock()
	defer liveTLS.Unlock()
	now := time.Now()
	if liveTLS.root != ROOT_PATH {
		liveTLS.certificate, liveTLS.checked, liveTLS.warned = nil, time.Time{}, time.Time{}
		liveTLS.root = ROOT_PATH
	}
	if !force && liveTLS.certificate != nil && now.Sub(liveTLS.checked) < time.Second && now.Before(liveTLS.certificate.Leaf.NotAfter) {
		return liveTLS.certificate, nil
	}
	liveTLS.checked = now
	cfg := CurrentCertificateConfig()
	certificate, err := loadValidatedRuntimeCertificate(cfg)
	if err == nil {
		liveTLS.certificate = &certificate
		return liveTLS.certificate, nil
	}
	if (liveTLS.certificate == nil || !now.Before(liveTLS.certificate.Leaf.NotAfter)) && cfg != nil && cfg.SSL.PreviousCertificateRef != "" {
		previous := *cfg
		previous.SSL.CertificateRef = cfg.SSL.PreviousCertificateRef
		previous.SSL.PrivateKeyRef = cfg.SSL.PreviousPrivateKeyRef
		if fallback, fallbackErr := loadValidatedRuntimeCertificate(&previous); fallbackErr == nil {
			liveTLS.certificate = &fallback
		}
	}
	if liveTLS.certificate != nil && now.Before(liveTLS.certificate.Leaf.NotAfter) {
		if now.Sub(liveTLS.warned) >= time.Minute {
			log.WithFields(log.Fields{"action": "热加载 TLS 证书", "result": "failure"}).Warn("新证书不可用，继续使用尚未过期的已校验证书")
			liveTLS.warned = now
		}
		return liveTLS.certificate, err
	}
	return nil, err
}

func loadValidatedRuntimeCertificate(cfg *Config) (tls.Certificate, error) {
	certificate, err := LoadRuntimeTLSCertificate(cfg)
	if err != nil {
		return tls.Certificate{}, err
	}
	certificate.Leaf, err = x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return tls.Certificate{}, err
	}
	runtime := RuntimeConfig{Root: RootConfig{Domains: DomainConfig{
		Primary:       cfg.Domain,
		Web:           cfg.WebDomain,
		Accepted:      cfg.Domains,
		AdminRecovery: cfg.AdminRecoveryDomain,
	}}}
	runtime.SSL.TLS = cfg.SSL
	if cfg.EffectiveSSLType() == SSLTypeAutoDNS {
		runtime.SSL.TLS.Management = SSLManagementProject
		runtime.SSL.TLS.Challenge = SSLChallengeDNS
	}
	for _, domain := range validationCertificateDomains(&runtime) {
		if err := certificate.Leaf.VerifyHostname(domain); err != nil {
			return tls.Certificate{}, ErrSSLCertificateDomain
		}
	}
	return certificate, nil
}

// NewRuntimeTLSConfig 在启动时验证证书，后续握手自动使用最新有效证书。
func NewRuntimeTLSConfig() (*tls.Config, error) {
	if certificate, err := runtimeTLSCertificate(true); certificate == nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		certificate, err := runtimeTLSCertificate(false)
		if certificate != nil {
			return certificate, nil
		}
		return nil, err
	}}, nil
}

func ReloadRuntimeTLSCertificate() error {
	_, err := runtimeTLSCertificate(true)
	return err
}
