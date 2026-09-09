package config

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"git.pcux.cn/xuans/Smail/utils/runtimepath"
)

// MigrateLegacySSLStorage 在监听器启动前迁移旧引用；提交失败时不删除旧材料。
// 自定义模式优先复用 ssl 根目录中完全相同的原件，无法匹配时才迁出旧版本。
func MigrateLegacySSLStorage(root string) error {
	if !runtimepath.DeploymentLayout(root) {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	partitioned, err := isPartitionedConfig(data)
	if err != nil || !partitioned {
		return err
	}
	options := RuntimeValidationOptions{HasSuperAdmin: true}
	store := NewTransactionStore(root, options)
	if err := store.Recover(); err != nil {
		return err
	}
	current, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: options}).Load()
	if err != nil || !legacyManagedSSLReference(current.SSL.TLS.CertificateRef) {
		return err
	}
	ref, err := resolveManagedSSLReferencesAtRoot(root, current.SSL.TLS)
	if err != nil {
		return err
	}
	key, err := readExternalSSLFile(ref.PrivateKeyPath, true)
	if err != nil {
		return err
	}
	certificate, err := readExternalSSLFile(ref.CertificatePath, false)
	if err != nil {
		return err
	}
	candidate := cloneRuntimeConfig(current)
	next := &candidate.SSL.TLS
	if EffectiveSSLManagement(*next) != SSLManagementProject {
		ref, err = matchingDirectSSL(root, key, certificate)
	} else {
		ref = ManagedSSLReference{}
	}
	if err != nil {
		return err
	}
	if ref.CertificateRef == "" {
		ref, err = stageFlatSSLAtRoot(root, next.Mode, key, certificate, nil, validationCertificateDomains(&candidate))
		if err != nil {
			return err
		}
		defer releaseManagedSSL(root, SSLConfig{CertificateRef: ref.CertificateRef})
	}
	next.CertificateRef, next.PrivateKeyRef = ref.CertificateRef, ref.PrivateKeyRef
	next.DefaultDirectory = "ssl"
	if next.Mode == ManagedSSLModeUpload {
		next.CustomDirectory = "ssl"
	}
	previous, err := previousLegacySSL(root, current.SSL.TLS, certificate, validationCertificateDomains(&candidate))
	if err != nil {
		return err
	}
	if previous.CertificateRef != "" {
		defer releaseManagedSSL(root, SSLConfig{CertificateRef: previous.CertificateRef})
		next.PreviousCertificateRef, next.PreviousPrivateKeyRef = previous.CertificateRef, previous.PrivateKeyRef
	}
	if err := validateCustomSSLReferences(root, candidate); err != nil {
		return err
	}
	setCandidateGeneration(&candidate, current.Generation+1)
	return store.Commit(current, candidate)
}

func matchingDirectSSL(root string, key, certificate []byte) (ManagedSSLReference, error) {
	directory := filepath.Join(root, "ssl")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return ManagedSSLReference{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".crt") || strings.HasPrefix(entry.Name(), flatSSLPrefix) {
			continue
		}
		publicPath := filepath.Join(directory, entry.Name())
		privatePath := strings.TrimSuffix(publicPath, ".crt") + ".key"
		publicBytes, publicErr := readExternalSSLFile(publicPath, false)
		privateBytes, privateErr := readExternalSSLFile(privatePath, true)
		if publicErr == nil && privateErr == nil && bytes.Equal(certificate, publicBytes) && bytes.Equal(key, privateBytes) {
			return ManagedSSLReference{Mode: ManagedSSLModeUpload, CertificateRef: directSSLReference(root, publicPath), PrivateKeyRef: directSSLReference(root, privatePath)}, nil
		}
	}
	return ManagedSSLReference{}, nil
}

// 重复副本不算上一套证书；只迁移最近一套仍有效且覆盖当前服务域名的不同材料。
func previousLegacySSL(root string, active SSLConfig, certificate []byte, domains []string) (ManagedSSLReference, error) {
	var candidates []retainedSSLVersion
	for _, mode := range []string{ManagedSSLModeDefault, ManagedSSLModeUpload} {
		directory, err := managedSSLRootPathAtRoot(root, mode)
		if err != nil {
			return ManagedSSLReference{}, err
		}
		entries, err := os.ReadDir(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return ManagedSSLReference{}, err
		}
		for _, entry := range entries {
			if !validSSLVersion(entry.Name()) && entry.Name() != "current" {
				continue
			}
			if info, err := entry.Info(); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				candidates = append(candidates, retainedSSLVersion{directory: filepath.Join(directory, entry.Name()), modified: info.ModTime()})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].modified.After(candidates[j].modified) })
	for _, candidate := range candidates {
		publicBytes, publicErr := readExternalSSLFile(filepath.Join(candidate.directory, "public.crt"), false)
		privateBytes, privateErr := readExternalSSLFile(filepath.Join(candidate.directory, "private.key"), true)
		if publicErr != nil || privateErr != nil || bytes.Equal(certificate, publicBytes) || validateSSLMaterial(privateBytes, publicBytes, nil, domains) != nil {
			continue
		}
		return stageFlatSSLAtRoot(root, active.Mode, privateBytes, publicBytes, nil, domains)
	}
	return ManagedSSLReference{}, nil
}
