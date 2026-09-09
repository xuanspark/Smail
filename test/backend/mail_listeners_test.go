package res_init_test

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/listen/imap_server"
	"git.pcux.cn/xuans/Smail/listen/pop3_server"
	"git.pcux.cn/xuans/Smail/listen/smtp_server"
)

// 使用空白临时配置和临时证书，在非默认端口调用生产监听入口；不连接真实账号库。
func TestConfiguredMailListenerPortsAcceptProtocolConnections(t *testing.T) {
	root := t.TempDir()
	certificate, key := writeCertificate(t, root)
	ports := reservePorts(t, 6)
	meta := config.SectionMeta{Schema: config.CurrentSectionSchema, Generation: 1}
	smtp := config.SMTPSectionConfig{Meta: meta, Listener: config.SMTPListenerConfig{
		Port: ports[0], STARTTLSPort: ports[1], TLSPort: ports[2],
	}}
	// 中继与 IMAP 共用数值端口也不冲突；中继是外部连接，不参与本地监听端口占用。
	smtp.Relay = config.SMTPConfig{
		RelayHost:  "127.0.0.1",
		RelayPort:  ports[3],
		Security:   "plain",
		From:       "no-reply@example.com",
		FromDomain: "example.com",
	}
	email := config.EmailSectionConfig{Meta: meta, IMAP: config.IMAPListenerConfig{TLSPort: ports[3]}, POP3: config.POP3ListenerConfig{Port: ports[4], TLSPort: ports[5]}}
	ssl := config.SSLSectionConfig{Meta: meta, TLS: config.SSLConfig{
		Management: config.SSLManagementCustom, Mode: config.ManagedSSLModeUpload,
		DefaultDirectory: "ssl", CustomDirectory: "ssl", CertificateRef: certificate, PrivateKeyRef: key,
	}}
	rootConfig := config.RootConfig{
		ConfigVersion: config.CurrentConfigVersion, SchemaVersion: config.CurrentSchemaVersion, Generation: 1, SetupState: config.SetupStateBootstrap,
		Database: config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "unused.db"},
		Domains:  config.DomainConfig{Primary: "mail.example.com", Web: "mail.example.com", Accepted: []string{"mail.example.com"}},
		Security: config.SecurityConfig{PasswordVersion: "argon2id"},
		Sections: map[config.Section]config.SectionRef{},
	}
	directory := filepath.Join(root, "config")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for section, value := range map[config.Section]any{
		config.SectionSMTP: smtp, config.SectionEmail: email, config.SectionSSL: ssl,
		config.SectionPlugins: config.PluginsSectionConfig{Meta: meta},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		name, _ := config.FixedSectionFile(section)
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		rootConfig.Sections[section] = config.SectionRef{File: name, Schema: 1, Generation: 1, SHA256: config.SectionDigest(data)}
	}
	data, err := json.Marshal(rootConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := config.NewConfigManager(root, config.ConfigLoaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	previousManager, previousConfig, previousRoot := config.CurrentManager(), config.CurrentConfig(), config.ROOT_PATH
	t.Cleanup(func() {
		config.SetRuntimeManager(previousManager)
		config.PublishConfigSnapshot(previousConfig)
		config.ROOT_PATH = previousRoot
	})
	config.ROOT_PATH = root
	config.SetRuntimeManager(manager)
	config.PublishConfigSnapshot(&config.Config{SSLPublicKeyPath: certificate, SSLPrivateKeyPath: key})
	cases := []struct {
		name   string
		port   int
		tls    bool
		prefix string
		start  func() error
	}{
		{"smtp", ports[0], false, "220", smtp_server.Start},
		{"smtp_starttls", ports[1], false, "220", smtp_server.StartWithTLSNew},
		{"smtp_tls", ports[2], true, "220", smtp_server.StartWithTLS},
		{"imap_tls", ports[3], true, "* OK", imap_server.StarTLS},
		{"pop3_stls", ports[4], false, "+OK", pop3_server.Start},
		{"pop3_tls", ports[5], true, "+OK", pop3_server.StartWithTls},
	}
	var results []<-chan error
	t.Cleanup(func() {
		smtp_server.Stop()
		imap_server.Stop()
		pop3_server.Stop()
		for _, result := range results {
			select {
			case <-result:
			case <-time.After(3 * time.Second):
				t.Error("listener did not stop before configuration cleanup")
			}
		}
	})
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			result := make(chan error, 1)
			results = append(results, result)
			go func() {
				defer close(result)
				result <- item.start()
			}()
			address := fmt.Sprintf("127.0.0.1:%d", item.port)
			var conn net.Conn
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				select {
				case err := <-result:
					t.Fatalf("listener exited before connection: %v", err)
				default:
				}
				dialer := &net.Dialer{Timeout: 200 * time.Millisecond}
				if item.tls {
					conn, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
				} else {
					conn, err = dialer.Dial("tcp", address)
				}
				if err == nil {
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			if err != nil {
				t.Fatalf("connect custom port: %v", err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			greeting, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil || !strings.HasPrefix(greeting, item.prefix) {
				t.Fatalf("protocol greeting = %q, error = %v", greeting, err)
			}
			t.Logf("%s accepted protocol connection on custom port %d", item.name, item.port)
		})
	}
}

func reservePorts(t *testing.T, count int) []int {
	t.Helper()
	var ports []int
	var listeners []net.Listener
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	for range count {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, listener)
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
	}
	return ports
}

func writeCertificate(t *testing.T, root string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost", "mail.example.com", "*.mail.example.com"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificatePath, keyPath := filepath.Join(root, "certificate.pem"), filepath.Join(root, "key.pem")
	for path, block := range map[string]*pem.Block{
		certificatePath: {Type: "CERTIFICATE", Bytes: der}, keyPath: {Type: "PRIVATE KEY", Bytes: keyDER},
	} {
		if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return certificatePath, keyPath
}
