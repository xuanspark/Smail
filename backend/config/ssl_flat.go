package config

import (
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
)

const flatSSLPrefix = "smail-tls-"

func legacyManagedSSLReference(value string) bool {
	value = runtimepath.LegacyReference(value)
	return strings.HasPrefix(value, "data/ssl/managed/") || strings.HasPrefix(value, "ssl/managed/")
}

func flatSSLPair(root, certificatePath, privateKeyPath string) bool {
	directory, err := filepath.Abs(filepath.Join(root, "ssl"))
	if err != nil || filepath.Dir(certificatePath) != directory || filepath.Dir(privateKeyPath) != directory {
		return false
	}
	name := strings.TrimSuffix(filepath.Base(certificatePath), ".crt")
	return strings.HasSuffix(certificatePath, ".crt") && strings.HasPrefix(name, flatSSLPrefix) &&
		validSSLVersion("version-"+strings.TrimPrefix(name, flatSSLPrefix)) && filepath.Base(privateKeyPath) == name+".key"
}

// 新材料使用不可变的成对文件；配置事务是唯一的生效指针，不覆盖当前文件。
func stageFlatSSLAtRoot(root, mode string, privateKey, certificate, issuer []byte, domains []string) (ManagedSSLReference, error) {
	managedSSLStorage.Lock()
	defer managedSSLStorage.Unlock()
	if !validManagedSSLMode(mode) {
		return ManagedSSLReference{}, ErrInvalidSSLMode
	}
	if err := validateSSLMaterial(privateKey, certificate, issuer, domains); err != nil {
		return ManagedSSLReference{}, err
	}
	if runtimepath.DeploymentLayout(root) {
		unlock, err := NewTransactionStore(root, RuntimeValidationOptions{}).lockState()
		if err != nil {
			return ManagedSSLReference{}, err
		}
		defer unlock()
	}
	directory, err := filepath.Abs(filepath.Join(root, "ssl"))
	if err != nil {
		return ManagedSSLReference{}, err
	}
	if err := ensureManagedDirectoryAtRoot(root, directory); err != nil {
		return ManagedSSLReference{}, err
	}
	token, err := randomStorageToken()
	if err != nil {
		return ManagedSSLReference{}, err
	}
	base := flatSSLPrefix + token
	storage, err := os.OpenRoot(directory)
	if err != nil {
		return ManagedSSLReference{}, err
	}
	defer storage.Close()
	// 证书链合并到一个证书文件，不额外生成 issuer 文件。
	certificates, _ := parseCertificatePEM(certificate)
	issuers, _ := parseCertificatePEM(issuer)
	certificate = encodeSSLCertificates(appendUniqueCertificates(certificates, issuers...))
	if len(certificate) > maxManagedSSLFileSize {
		return ManagedSSLReference{}, ErrInvalidSSLMaterial
	}
	var written []string
	complete := false
	defer func() {
		if !complete {
			for _, name := range written {
				_ = storage.Remove(name)
			}
		}
	}()
	for _, file := range []struct {
		name string
		data []byte
	}{{base + ".key", privateKey}, {base + ".crt", certificate}} {
		if _, err := storage.Lstat(file.name); !os.IsNotExist(err) {
			return ManagedSSLReference{}, ErrUnsafeSSLFile
		}
		written = append(written, file.name)
		if err := atomicfile.WriteRoot(storage, file.name, file.data, 0600); err != nil {
			return ManagedSSLReference{}, err
		}
	}
	publicPath, privatePath := filepath.Join(directory, base+".crt"), filepath.Join(directory, base+".key")
	if err := validateManagedSSLFiles(publicPath, privatePath); err != nil {
		return ManagedSSLReference{}, err
	}
	lease, err := openSSLLease(privatePath)
	if err != nil {
		return ManagedSSLReference{}, err
	}
	unlock, err := lockStateFile(lease)
	if err != nil {
		return ManagedSSLReference{}, errors.Join(err, lease.Close())
	}
	managedSSLStorage.staged[publicPath] = func() { releaseConfigLease(unlock, lease) }
	complete = true
	return ManagedSSLReference{Mode: mode, CertificateRef: "ssl/" + base + ".crt", PrivateKeyRef: "ssl/" + base + ".key", CertificatePath: publicPath, PrivateKeyPath: privatePath}, nil
}

// 只有上一套由本程序生成的证书才纳入到期自动删除；手动文件不会被接管。
func retainPreviousSSL(root string, next *SSLConfig, previous SSLConfig) {
	next.PreviousCertificateRef = previous.PreviousCertificateRef
	next.PreviousPrivateKeyRef = previous.PreviousPrivateKeyRef
	if next.CertificateRef == previous.CertificateRef && next.PrivateKeyRef == previous.PrivateKeyRef {
		return
	}
	ref, err := resolveManagedSSLPathsAtRoot(root, previous)
	if err == nil && flatSSLPair(root, ref.CertificatePath, ref.PrivateKeyPath) {
		if validateManagedSSLFiles(ref.CertificatePath, ref.PrivateKeyPath) == nil {
			next.PreviousCertificateRef = previous.CertificateRef
			next.PreviousPrivateKeyRef = previous.PrivateKeyRef
		}
	}
	if next.PreviousCertificateRef == next.CertificateRef {
		next.PreviousCertificateRef, next.PreviousPrivateKeyRef = "", ""
	}
}

func pruneFlatSSLLocked(root string, protected []SSLConfig) error {
	// 当前材料不可用时暂停回收，避免丢失仍可恢复的历史证书。
	if len(protected) == 0 {
		return nil
	}
	if _, err := resolveManagedSSLReferencesAtRoot(root, protected[0]); err != nil {
		return err
	}
	directory, err := filepath.Abs(filepath.Join(root, "ssl"))
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || filepath.Clean(resolved) != directory {
		return ErrUnsafeSSLFile
	}
	keep := make(map[string]bool)
	for _, ssl := range protected {
		if path, err := resolveCustomSSLInput(root, ssl.CertificateRef); ssl.CertificateRef != "" && err == nil {
			keep[path] = true
		}
		// 已经降为上一套的配置不能继续保护更早一代，避免保留链无限延长。
		if ssl.CertificateRef == protected[0].PreviousCertificateRef {
			continue
		}
		publicPath, err := resolveCustomSSLInput(root, ssl.PreviousCertificateRef)
		privatePath, keyErr := resolveCustomSSLInput(root, ssl.PreviousPrivateKeyRef)
		if err == nil && keyErr == nil && flatSSLPair(root, publicPath, privatePath) {
			// 即使已经到期，也必须等配置事务解除引用后才能删除。
			keep[publicPath] = true
		}
	}
	storage, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer storage.Close()
	for _, entry := range entries {
		publicPath := filepath.Join(directory, entry.Name())
		privatePath := strings.TrimSuffix(publicPath, ".crt") + ".key"
		if !flatSSLPair(root, publicPath, privatePath) || keep[publicPath] || managedSSLStorage.staged[publicPath] != nil {
			continue
		}
		// 租约覆盖整个删除过程；未知、链接、不完整材料保留给管理员处理。
		certificate, err := readExternalSSLFile(publicPath, false)
		if err != nil {
			continue
		}
		certificates, err := parseCertificatePEM(certificate)
		if err != nil || len(certificates) == 0 {
			continue
		}
		privateKey, err := readExternalSSLFile(privatePath, true)
		if err != nil {
			// 先删私钥、后删证书；中断留下的孤立证书可在下轮安全重试。
			if _, statErr := os.Lstat(privatePath); os.IsNotExist(statErr) {
				if err := storage.Remove(filepath.Base(publicPath)); err != nil {
					return err
				}
			}
			continue
		}
		if _, err := tls.X509KeyPair(certificate, privateKey); err != nil {
			continue
		}
		lease, err := openSSLLease(privatePath)
		if err != nil {
			continue
		}
		unlock, err := lockStateFile(lease)
		if err != nil {
			if closeErr := lease.Close(); closeErr != nil {
				return errors.Join(err, closeErr)
			}
			continue
		}
		err = storage.Remove(filepath.Base(privatePath))
		if err == nil {
			err = storage.Remove(filepath.Base(publicPath))
		}
		unlock()
		if err := errors.Join(err, lease.Close()); err != nil {
			return err
		}
	}
	return nil
}
