package config

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stageLifecycleCertificate(t *testing.T, root string) ManagedSSLReference {
	t.Helper()
	key, cert := testManagedCertificate(t, "example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048)
	ref, err := stageFlatSSLAtRoot(root, ManagedSSLModeUpload, key, cert, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseManagedSSL(root, sslConfigForReference(ref)) })
	return ref
}

func TestFlatSSLRetentionAndExpiryRecovery(t *testing.T) {
	root := t.TempDir()
	if err := InitializeDeployment(root); err != nil {
		t.Fatal(err)
	}
	first, second, third := stageLifecycleCertificate(t, root), stageLifecycleCertificate(t, root), stageLifecycleCertificate(t, root)
	for _, ref := range []ManagedSSLReference{first, second, third} {
		releaseManagedSSL(root, sslConfigForReference(ref))
	}
	previous := sslConfigForReference(second)
	previous.PreviousCertificateRef, previous.PreviousPrivateKeyRef = first.CertificateRef, first.PrivateKeyRef
	active := sslConfigForReference(third)
	active.Management, active.DefaultDirectory, active.CustomDirectory = SSLManagementCustom, "ssl", "ssl"
	retainPreviousSSL(root, &active, previous)
	manual := filepath.Join(root, "ssl", "manual-note.txt")
	if err := os.WriteFile(manual, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	maintainManagedSSL(root, active, previous)
	if countFlatVersions(t, root) != 2 {
		t.Fatal("稳定状态未立即收敛为当前与上一套")
	}
	current := validRuntimeConfig()
	current.Root.Domains = DomainConfig{Primary: "example.com", Web: "example.com", Accepted: []string{"example.com"}}
	current.SSL.TLS = active
	writeRuntimeFixture(t, root, current)
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.clearExpiredPreviousSSL(); err != nil || manager.Snapshot().Generation != current.Generation {
		t.Fatal("未到期证书不应清除引用", err)
	}
	key, cert := testManagedCertificate(t, "example.com", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour), 2048)
	if err := os.WriteFile(second.CertificatePath, cert, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second.PrivateKeyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	maintainManagedSSL(root, active)
	if countFlatVersions(t, root) != 2 {
		t.Fatal("配置尚未解除过期引用就删除了材料")
	}
	manager.snapshots = &flakyRuntimeSnapshotStore{current: manager.Snapshot(), failures: 1}
	if err := manager.clearExpiredPreviousSSL(); !errors.Is(err, ErrRuntimeSnapshotUnavailable) {
		t.Fatalf("发布失败未留下恢复状态：%v", err)
	}
	if manager.pending == nil || !manager.pending.Committed {
		t.Fatal("丢失已提交候选")
	}
	if err := manager.clearExpiredPreviousSSL(); err != nil {
		t.Fatal("下轮维护未恢复发布", err)
	}
	loaded, err := manager.loader.Load()
	if err != nil || loaded.Generation != current.Generation+1 || loaded.SSL.TLS.PreviousCertificateRef != "" {
		t.Fatal("磁盘引用或代次不一致", err)
	}
	maintainManagedSSL(root, manager.Snapshot().SSL.TLS)
	if countFlatVersions(t, root) != 1 {
		t.Fatal("过期上一套未删除")
	}
	if _, err := os.Stat(manual); err != nil {
		t.Fatal("误删未知文件", err)
	}
}

func TestFlatSSLPreservesLeaseAndUnreadableActive(t *testing.T) {
	root := t.TempDir()
	first, second := stageLifecycleCertificate(t, root), stageLifecycleCertificate(t, root)
	maintainManagedSSL(root, sslConfigForReference(second))
	if countFlatVersions(t, root) != 2 {
		t.Fatal("误删正在暂存的材料")
	}
	releaseManagedSSL(root, sslConfigForReference(first))
	if err := os.WriteFile(second.CertificatePath, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	maintainManagedSSL(root, sslConfigForReference(second))
	if countFlatVersions(t, root) != 2 {
		t.Fatal("当前证书损坏时仍删除了恢复材料")
	}
}

func TestFlatSSLPrunePreservesMalformedAndRetriesOrphan(t *testing.T) {
	root := t.TempDir()
	active := stageLifecycleCertificate(t, root)
	orphan, malformed := stageLifecycleCertificate(t, root), stageLifecycleCertificate(t, root)
	releaseManagedSSL(root, sslConfigForReference(orphan))
	releaseManagedSSL(root, sslConfigForReference(malformed))
	if err := os.Remove(orphan.PrivateKeyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(malformed.CertificatePath, []byte("preserve unknown material"), 0600); err != nil {
		t.Fatal(err)
	}
	maintainManagedSSL(root, sslConfigForReference(active))
	if _, err := os.Stat(orphan.CertificatePath); !os.IsNotExist(err) {
		t.Fatal("删除中断后留下的证书没有清理", err)
	}
	if _, err := os.Stat(malformed.PrivateKeyPath); err != nil {
		t.Fatal("无法确认的材料被误删", err)
	}
}

func TestLegacySSLMigrationReusesIdenticalDirectFiles(t *testing.T) {
	root := t.TempDir()
	if err := InitializeDeployment(root); err != nil {
		t.Fatal(err)
	}
	key, cert := testManagedCertificate(t, "example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048, "*.example.com")
	legacy, err := stageManagedSSLAtRoot(root, ManagedSSLModeUpload, key, cert, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	releaseManagedSSL(root, sslConfigForReference(legacy))
	for name, data := range map[string][]byte{"example.com.crt": cert, "example.com.key": key} {
		if err := os.WriteFile(filepath.Join(root, "ssl", name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	current := validRuntimeConfig()
	current.Root.Domains = DomainConfig{Primary: "example.com", Web: "example.com", Accepted: []string{"example.com"}}
	current.SSL.TLS = sslConfigForReference(legacy)
	current.SSL.TLS.Management, current.SSL.TLS.DefaultDirectory, current.SSL.TLS.CustomDirectory = SSLManagementCustom, "ssl", "ssl"
	writeRuntimeFixture(t, root, current)
	if err := MigrateLegacySSLStorage(root); err != nil {
		t.Fatal(err)
	}
	loader := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true}})
	loaded, err := loader.Load()
	if err != nil || loaded.SSL.TLS.CertificateRef != "ssl/example.com.crt" || loaded.Generation != current.Generation+1 {
		t.Fatal("迁移未使用同一原件或事务代次错误", err)
	}
	if err := MigrateLegacySSLStorage(root); err != nil {
		t.Fatal("重复启动失败", err)
	}
	maintainManagedSSL(root, loaded.SSL.TLS)
	if _, err := os.Stat(filepath.Join(root, "ssl", "managed")); !os.IsNotExist(err) {
		t.Fatal("迁移完成仍保留旧副本目录", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "ssl", "example.com.key")); err != nil || !bytes.Equal(data, key) {
		t.Fatal("迁移改变了原始私钥", err)
	}
}

func TestLiveSSLSwitchKeepsConnectionAndRestoresPrevious(t *testing.T) {
	root := t.TempDir()
	oldRoot, oldPublished, oldFlag := ROOT_PATH, publishedConfig.Load(), hasPublishedConfig.Load()
	oldManager := CurrentManager()
	SetRuntimeManager(nil)
	ROOT_PATH = root
	t.Cleanup(func() {
		ROOT_PATH = oldRoot
		publishedConfig.Store(oldPublished)
		hasPublishedConfig.Store(oldFlag)
		SetRuntimeManager(oldManager)
	})
	first, second := stageLifecycleCertificate(t, root), stageLifecycleCertificate(t, root)
	cfg := &Config{Domain: "example.com", WebDomain: "example.com", SSL: sslConfigForReference(first)}
	PublishConfigSnapshot(cfg)
	tlsConfig, err := NewRuntimeTLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func(connection net.Conn) {
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
				_, _ = io.Copy(connection, connection)
			}(connection)
		}
	}()
	dial := func() *tls.Conn {
		t.Helper()
		connection, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", listener.Addr().String(), &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { connection.Close() })
		return connection
	}
	firstClient := dial()
	oldSerial := firstClient.ConnectionState().PeerCertificates[0].SerialNumber.String()
	cfg.SSL = sslConfigForReference(second)
	cfg.SSL.PreviousCertificateRef, cfg.SSL.PreviousPrivateKeyRef = first.CertificateRef, first.PrivateKeyRef
	PublishConfigSnapshot(cfg)
	if err := ReloadRuntimeTLSCertificate(); err != nil {
		t.Fatal(err)
	}
	if dial().ConnectionState().PeerCertificates[0].SerialNumber.String() == oldSerial {
		t.Fatal("新握手仍使用旧证书")
	}
	_ = firstClient.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := firstClient.Write([]byte("ping")); err != nil {
		t.Fatal("换证断开已有连接", err)
	}
	message := make([]byte, 4)
	if _, err := io.ReadFull(firstClient, message); err != nil || string(message) != "ping" {
		t.Fatal("已有连接无法继续通信", err)
	}
	if err := os.WriteFile(second.CertificatePath, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	liveTLS.Lock()
	liveTLS.certificate = nil
	liveTLS.Unlock()
	if _, err := NewRuntimeTLSConfig(); err != nil {
		t.Fatal("无内存缓存时无法恢复上一套有效证书", err)
	}
	if dial().ConnectionState().PeerCertificates[0].SerialNumber.String() != oldSerial {
		t.Fatal("恢复握手没有使用上一套证书")
	}
	liveTLS.Lock()
	liveTLS.certificate.Leaf.NotAfter = time.Now().Add(-time.Second)
	liveTLS.Unlock()
	if _, err := NewRuntimeTLSConfig(); err != nil {
		t.Fatal("内存证书到期时没有重新尝试有效的上一套", err)
	}
}
