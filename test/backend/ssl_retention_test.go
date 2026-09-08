package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sslConfigForReference(ref ManagedSSLReference) SSLConfig {
	return SSLConfig{Mode: ref.Mode, CertificateRef: ref.CertificateRef, PrivateKeyRef: ref.PrivateKeyRef}
}

func countManagedVersions(t *testing.T, root string) int {
	t.Helper()
	count := 0
	for _, mode := range []string{ManagedSSLModeDefault, ManagedSSLModeUpload} {
		path, err := managedSSLRootPathAtRoot(root, mode)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if validSSLVersion(entry.Name()) {
				count++
			}
		}
	}
	return count
}

func TestSSLRetentionHandlesExpiredPreviousAndLegacyCurrent(t *testing.T) {
	root := t.TempDir()
	if err := InitializeDeployment(root); err != nil {
		t.Fatal(err)
	}
	key, cert := testManagedCertificate(t, "example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048)
	legacy, err := importManagedSSLAtRoot(root, ManagedSSLModeDefault, key, cert, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var refs []ManagedSSLReference
	for index := 0; index < 4; index++ {
		ref, err := stageManagedSSLAtRoot(root, ManagedSSLModeUpload, key, cert, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		releaseManagedSSL(root, sslConfigForReference(ref))
		refs = append(refs, ref)
	}
	expiredKey, expiredCert := testManagedCertificate(t, "example.com", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour), 2048)
	if err := os.WriteFile(refs[2].PrivateKeyPath, expiredKey, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refs[2].CertificatePath, expiredCert, 0600); err != nil {
		t.Fatal(err)
	}
	maintainManagedSSL(root, sslConfigForReference(refs[3]), sslConfigForReference(refs[2]))
	if countManagedVersions(t, root) != 2 {
		t.Fatal("上一份证书过期阻止了清理")
	}
	if _, err := os.Stat(legacy.CertificatePath); !os.IsNotExist(err) {
		t.Fatal("未被引用的旧 current 未清理")
	}
	if _, err := os.Stat(refs[2].CertificatePath); err != nil {
		t.Fatal("清理删除了受保护的上一份证书")
	}
	old := validRuntimeConfig()
	old.SSL.TLS = sslConfigForReference(legacy)
	if err := validateRollbackSSL(root, old); !errors.Is(err, ErrSSLHistoryUnavailable) {
		t.Fatal("历史 current 丢失后仍允许回滚")
	}
}

func TestSSLRetentionProtectsIndependentLease(t *testing.T) {
	root := t.TempDir()
	if err := InitializeDeployment(root); err != nil {
		t.Fatal(err)
	}
	key, cert := testManagedCertificate(t, "example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048)
	var refs []ManagedSSLReference
	for index := 0; index < 4; index++ {
		ref, err := stageManagedSSLAtRoot(root, ManagedSSLModeUpload, key, cert, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		releaseManagedSSL(root, sslConfigForReference(ref))
		refs = append(refs, ref)
	}
	lease, err := openSSLLease(refs[0].PrivateKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	unlock, err := lockStateFile(lease)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	maintainManagedSSL(root, sslConfigForReference(refs[3]))
	if _, err := os.Stat(refs[0].CertificatePath); err != nil {
		t.Fatal("清理忽略了其他句柄持有的暂存租约")
	}
	if countManagedVersions(t, root) != 2 {
		t.Fatal("暂存和生效证书以外的历史未清理")
	}
}

func TestSSLRetentionKeepsTwoAcrossModesAndProtectsStaged(t *testing.T) {
	root := t.TempDir()
	if err := InitializeDeployment(root); err != nil {
		t.Fatal(err)
	}
	key, cert := testManagedCertificate(t, "example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048)
	var refs []ManagedSSLReference
	for index := 0; index < 4; index++ {
		mode := ManagedSSLModeDefault
		if index%2 == 1 {
			mode = ManagedSSLModeUpload
		}
		ref, err := stageManagedSSLAtRoot(root, mode, key, cert, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
		if index > 0 {
			maintainManagedSSL(root, sslConfigForReference(refs[index-1]))
			if _, err := os.Stat(ref.PrivateKeyPath); err != nil {
				t.Fatal("清理误删尚未提交的续期版本")
			}
			maintainManagedSSL(root, sslConfigForReference(ref), sslConfigForReference(refs[index-1]))
		} else {
			maintainManagedSSL(root, sslConfigForReference(ref))
		}
		want := index + 1
		if want > 2 {
			want = 2
		}
		if actual := countManagedVersions(t, root); actual != want {
			t.Fatalf("跨模式保留总量错误：%d，期望 %d", actual, want)
		}
	}
	old := validRuntimeConfig()
	old.SSL.TLS = sslConfigForReference(refs[0])
	if err := validateRollbackSSL(root, old); !errors.Is(err, ErrSSLHistoryUnavailable) {
		t.Fatalf("旧证书已清理却允许回滚：%v", err)
	}
	// 即使当前证书的目录时间最旧，也必须保留当前证书。
	if err := os.Chtimes(filepath.Dir(refs[2].CertificatePath), time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	maintainManagedSSL(root, sslConfigForReference(refs[2]))
	if _, err := resolveManagedSSLReferencesAtRoot(root, sslConfigForReference(refs[2])); err != nil {
		t.Fatal("清理删除了旧时间的生效证书")
	}
}

func TestSSLRetentionDoesNotPartiallyDeleteUnknownFiles(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "version-00000000000000000000000000000000")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"private.key", "public.crt", "zzz-user-note.txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("preserved"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeManagedSSLDirectory(directory); !errors.Is(err, ErrUnsafeSSLFile) {
		t.Fatal("未知文件没有阻止删除")
	}
	for _, name := range []string{"private.key", "public.crt", "zzz-user-note.txt"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatal("预检期间已经删除了部分文件")
		}
	}
}

func TestSSLRetentionRunsOnApplyCancelAndReload(t *testing.T) {
	root, manager, request, _ := customSSLFixture(t)
	var first RuntimeConfig
	for index := 0; index < 4; index++ {
		request.ExpectedGeneration = manager.Snapshot().Generation
		updated, err := manager.Update(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := manager.ApplyPendingAuthorized(context.Background(), updated.Generation, updated.CandidateID, SectionSSL, request.Actor); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = manager.Snapshot()
			if _, err := manager.transactions.CreateBackup(first); err != nil {
				t.Fatal(err)
			}
		}
		if index > 0 && countManagedVersions(t, root) != 2 {
			t.Fatal("配置应用完成后未自动保留两份")
		}
	}
	if _, err := manager.PreviewRollback(context.Background(), first.Generation, request.Actor); !errors.Is(err, ErrSSLHistoryUnavailable) {
		t.Fatalf("预览允许回滚已经清理的证书：%v", err)
	}
	if _, _, err := manager.rollbackWithState(context.Background(), first.Generation, request.Actor, StepUpConfirmation{Confirmed: true, Method: "ticket"}); !errors.Is(err, ErrSSLHistoryUnavailable) {
		t.Fatalf("实际回滚没有拒绝已清理证书：%v", err)
	}
	request.ExpectedGeneration = manager.Snapshot().Generation
	updated, err := manager.Update(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ManagePending(context.Background(), updated.Generation, updated.CandidateID, request.Actor, StepUpConfirmation{}, true); err != nil {
		t.Fatal(err)
	}
	if countManagedVersions(t, root) != 2 {
		t.Fatal("取消候选未收敛到两份")
	}
	if err := manager.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if countManagedVersions(t, root) != 2 {
		t.Fatal("重新加载后保留数量错误")
	}
	// 确认五分区仍可完整读取，证书删除不能导致摘要被直接修改。
	data, _ := json.Marshal(manager.Snapshot().SSL)
	if len(data) == 0 {
		t.Fatal("配置快照不可读")
	}
	request.ExpectedGeneration = manager.Snapshot().Generation
	updated, err = manager.Update(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	manager.releasePendingSSL()
	path, err := resolveManagedSSLPathsAtRoot(root, updated.Candidate.SSL.TLS)
	if err != nil {
		t.Fatal(err)
	}
	managedSSLStorage.Lock()
	stillLeased := managedSSLStorage.staged[filepath.Dir(path.CertificatePath)] != nil
	managedSSLStorage.Unlock()
	if stillLeased {
		t.Fatal("管理器停止后仍持有暂存租约")
	}
}
