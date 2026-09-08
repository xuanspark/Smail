package config

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
)

const (
	ManagedSSLModeDefault  = "managed_default"
	ManagedSSLModeUpload   = "managed_upload"
	SSLManagementWebServer = "web_server"
	SSLManagementProject   = "project"
	SSLManagementCustom    = "custom"

	managedSSLRelativeRoot = "config/ssl/managed"
	maxManagedSSLFileSize  = 1 << 20
)

// EffectiveSSLManagement 返回面向用户的证书管理方式，并兼容旧配置中的底层存储模式。
func EffectiveSSLManagement(ssl SSLConfig) string {
	switch ssl.Management {
	case SSLManagementWebServer, SSLManagementProject, SSLManagementCustom:
		return ssl.Management
	}
	if ssl.Mode == ManagedSSLModeDefault || ssl.Mode == "default" {
		return SSLManagementProject
	}
	return SSLManagementCustom
}

// ValidSSLManagement 判断证书管理责任是否为当前支持的三种业务模式。
func ValidSSLManagement(management string) bool {
	return management == SSLManagementWebServer || management == SSLManagementProject || management == SSLManagementCustom
}

var (
	ErrInvalidSSLMode          = errors.New("invalid SSL managed mode")
	ErrInvalidSSLMaterial      = errors.New("invalid SSL certificate material")
	ErrInvalidSSLReference     = errors.New("invalid SSL certificate reference")
	ErrUnsafeSSLFile           = errors.New("unsafe SSL certificate file")
	ErrSSLCertificateMismatch  = errors.New("SSL certificate and private key do not match")
	ErrSSLCertificateDomain    = errors.New("SSL certificate domain does not match")
	ErrSSLCertificateExpired   = errors.New("SSL certificate is expired or not yet valid")
	ErrSSLPrivateKeyPermission = errors.New("SSL private key permissions are too broad")
)

type ManagedSSLReference struct {
	Mode            string
	CertificateRef  string
	PrivateKeyRef   string
	CertificatePath string
	PrivateKeyPath  string
}

// ResolveRuntimeSSLPaths 返回服务启动时使用的受控证书路径。
// 新配置优先使用内部引用，旧配置只通过只读兼容适配层加载。
func ResolveRuntimeSSLPaths(cfg *Config) (string, string, error) {
	if cfg == nil {
		return "", "", ErrInvalidSSLReference
	}
	if cfg.SSL.Mode != "" || cfg.SSL.CertificateRef != "" || cfg.SSL.PrivateKeyRef != "" {
		if !validManagedSSLMode(cfg.SSL.Mode) || cfg.SSL.CertificateRef == "" || cfg.SSL.PrivateKeyRef == "" {
			return "", "", ErrInvalidSSLReference
		}
		resolved, err := ResolveManagedSSLReferences(cfg.SSL)
		if err != nil {
			return "", "", err
		}
		return resolved.CertificatePath, resolved.PrivateKeyPath, nil
	}
	return resolveLegacySSLPaths(cfg.SSLPublicKeyPath, cfg.SSLPrivateKeyPath)
}

// LoadRuntimeTLSCertificate 在所有 TLS 服务中统一解析并加载证书。
func LoadRuntimeTLSCertificate(cfg *Config) (tls.Certificate, error) {
	certificatePath, privateKeyPath, err := ResolveRuntimeSSLPaths(cfg)
	if err != nil {
		return tls.Certificate{}, err
	}
	certificate, err := tls.LoadX509KeyPair(certificatePath, privateKeyPath)
	if err != nil {
		return tls.Certificate{}, err
	}
	return certificate, nil
}

// ImportManagedSSL 校验证书材料后写入候选目录，并原子切换 current 目录。
func ImportManagedSSL(mode string, privateKeyPEM, certificatePEM, issuerPEM []byte, domains []string) (ManagedSSLReference, error) {
	return importManagedSSLAtRoot(ROOT_PATH, mode, privateKeyPEM, certificatePEM, issuerPEM, domains)
}

func importManagedSSLAtRoot(root string, mode string, privateKeyPEM, certificatePEM, issuerPEM []byte, domains []string) (ManagedSSLReference, error) {
	if !validManagedSSLMode(mode) {
		return ManagedSSLReference{}, ErrInvalidSSLMode
	}
	if err := validateSSLMaterial(privateKeyPEM, certificatePEM, issuerPEM, domains); err != nil {
		return ManagedSSLReference{}, err
	}

	rootPath, err := managedSSLRootPathAtRoot(root, mode)
	if err != nil {
		return ManagedSSLReference{}, err
	}
	if err := ensureManagedDirectoryAtRoot(root, rootPath); err != nil {
		return ManagedSSLReference{}, err
	}

	token, err := randomStorageToken()
	if err != nil {
		return ManagedSSLReference{}, err
	}
	candidatePath := filepath.Join(rootPath, ".candidate-"+token)
	if err := os.Mkdir(candidatePath, 0700); err != nil {
		return ManagedSSLReference{}, err
	}
	defer func() {
		if _, statErr := os.Stat(candidatePath); statErr == nil {
			_ = os.RemoveAll(candidatePath)
		}
	}()

	candidateRoot, err := os.OpenRoot(candidatePath)
	if err != nil {
		return ManagedSSLReference{}, err
	}
	write := func(name string, data []byte, permission os.FileMode) error {
		if len(data) == 0 {
			return nil
		}
		return atomicfile.WriteRoot(candidateRoot, name, data, permission)
	}
	if err := write("private.key", privateKeyPEM, 0600); err != nil {
		candidateRoot.Close()
		return ManagedSSLReference{}, err
	}
	if err := write("public.crt", certificatePEM, 0600); err != nil {
		candidateRoot.Close()
		return ManagedSSLReference{}, err
	}
	if err := write("issuer.crt", issuerPEM, 0600); err != nil {
		candidateRoot.Close()
		return ManagedSSLReference{}, err
	}
	if err := candidateRoot.Close(); err != nil {
		return ManagedSSLReference{}, err
	}

	activePath := filepath.Join(rootPath, "current")
	backupPath := filepath.Join(rootPath, ".previous-"+token)
	previousMoved := false
	activeInstalled := false
	if info, statErr := os.Lstat(activePath); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ManagedSSLReference{}, ErrUnsafeSSLFile
		}
		if err := os.Rename(activePath, backupPath); err != nil {
			return ManagedSSLReference{}, err
		}
		previousMoved = true
		if err := os.Rename(candidatePath, activePath); err != nil {
			_ = os.Rename(backupPath, activePath)
			return ManagedSSLReference{}, err
		}
		activeInstalled = true
	} else if !os.IsNotExist(statErr) {
		return ManagedSSLReference{}, statErr
	} else if err := os.Rename(candidatePath, activePath); err != nil {
		return ManagedSSLReference{}, err
	} else {
		activeInstalled = true
	}

	certificatePath := filepath.Join(activePath, "public.crt")
	privateKeyPath := filepath.Join(activePath, "private.key")
	if err := validateManagedSSLFiles(certificatePath, privateKeyPath); err != nil {
		rollbackErr := rollbackManagedSSLActivation(activePath, backupPath, previousMoved, activeInstalled)
		if rollbackErr != nil {
			return ManagedSSLReference{}, fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
		}
		return ManagedSSLReference{}, err
	}
	return ManagedSSLReference{
		Mode:            mode,
		CertificateRef:  managedSSLReference(mode, "public.crt"),
		PrivateKeyRef:   managedSSLReference(mode, "private.key"),
		CertificatePath: certificatePath,
		PrivateKeyPath:  privateKeyPath,
	}, nil
}

// ResolveManagedSSLReferences 将内部引用解析为受控文件路径，拒绝任意路径输入。
func ResolveManagedSSLReferences(ssl SSLConfig) (ManagedSSLReference, error) {
	return resolveManagedSSLReferencesAtRoot(ROOT_PATH, ssl)
}

func resolveManagedSSLReferencesAtRoot(root string, ssl SSLConfig) (ManagedSSLReference, error) {
	if !validManagedSSLMode(ssl.Mode) {
		return ManagedSSLReference{}, ErrInvalidSSLMode
	}
	if ssl.CertificateRef == "" || ssl.PrivateKeyRef == "" {
		return ManagedSSLReference{}, ErrInvalidSSLReference
	}
	rootPath, err := managedSSLRootPathAtRoot(root, ssl.Mode)
	if err != nil {
		return ManagedSSLReference{}, err
	}
	certificatePath, err := resolveManagedReferenceAtRoot(root, rootPath, ssl.CertificateRef, "public.crt")
	if err != nil {
		return ManagedSSLReference{}, err
	}
	privateKeyPath, err := resolveManagedReferenceAtRoot(root, rootPath, ssl.PrivateKeyRef, "private.key")
	if err != nil {
		return ManagedSSLReference{}, err
	}
	if filepath.Dir(certificatePath) != filepath.Dir(privateKeyPath) {
		return ManagedSSLReference{}, ErrInvalidSSLReference
	}
	if err := validateManagedSSLFiles(certificatePath, privateKeyPath); err != nil {
		return ManagedSSLReference{}, err
	}
	return ManagedSSLReference{
		Mode:            ssl.Mode,
		CertificateRef:  ssl.CertificateRef,
		PrivateKeyRef:   ssl.PrivateKeyRef,
		CertificatePath: certificatePath,
		PrivateKeyPath:  privateKeyPath,
	}, nil
}

// ValidateSSLMaterial 校验证书链、私钥匹配、有效期和可选域名。
func ValidateSSLMaterial(privateKeyPEM, certificatePEM, issuerPEM []byte, domains []string) error {
	return validateSSLMaterial(privateKeyPEM, certificatePEM, issuerPEM, domains)
}

func validateSSLMaterial(privateKeyPEM, certificatePEM, issuerPEM []byte, domains []string) error {
	if len(privateKeyPEM) == 0 || len(certificatePEM) == 0 || len(privateKeyPEM) > maxManagedSSLFileSize || len(certificatePEM) > maxManagedSSLFileSize || len(issuerPEM) > maxManagedSSLFileSize {
		return ErrInvalidSSLMaterial
	}
	certificates, err := parseCertificatePEM(certificatePEM)
	if err != nil || len(certificates) == 0 {
		return ErrInvalidSSLMaterial
	}
	if len(issuerPEM) > 0 {
		issuerCertificates, issuerErr := parseCertificatePEM(issuerPEM)
		if issuerErr != nil {
			return ErrInvalidSSLMaterial
		}
		certificates = appendUniqueCertificates(certificates, issuerCertificates...)
	}
	if _, err := tls.X509KeyPair(certificatePEM, privateKeyPEM); err != nil {
		return ErrSSLCertificateMismatch
	}
	leaf := certificates[0]
	now := time.Now()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return ErrSSLCertificateExpired
	}
	if err := validateCertificateChain(certificates); err != nil {
		return err
	}
	if err := validateCertificateKeyStrength(leaf); err != nil {
		return err
	}
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if err := leaf.VerifyHostname(domain); err != nil {
			return fmt.Errorf("%w: %s", ErrSSLCertificateDomain, domain)
		}
	}
	return nil
}

func appendUniqueCertificates(certificates []*x509.Certificate, additions ...*x509.Certificate) []*x509.Certificate {
	seen := make(map[string]struct{}, len(certificates)+len(additions))
	for _, certificate := range certificates {
		if certificate == nil {
			continue
		}
		seen[string(certificate.Raw)] = struct{}{}
	}
	for _, certificate := range additions {
		if certificate == nil {
			continue
		}
		key := string(certificate.Raw)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		certificates = append(certificates, certificate)
	}
	return certificates
}

func parseCertificatePEM(data []byte) ([]*x509.Certificate, error) {
	var certificates []*x509.Certificate
	remaining := data
	for {
		block, rest := pem.Decode(remaining)
		if block == nil {
			if len(strings.TrimSpace(string(remaining))) != 0 {
				return nil, ErrInvalidSSLMaterial
			}
			break
		}
		if block.Type != "CERTIFICATE" || len(block.Bytes) == 0 {
			return nil, ErrInvalidSSLMaterial
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, ErrInvalidSSLMaterial
		}
		certificates = append(certificates, certificate)
		remaining = rest
	}
	return certificates, nil
}

func validateCertificateChain(certificates []*x509.Certificate) error {
	for index := 0; index+1 < len(certificates); index++ {
		issuer := certificates[index+1]
		if !issuer.IsCA || issuer.BasicConstraintsValid && !issuer.IsCA {
			return ErrInvalidSSLMaterial
		}
		if err := certificates[index].CheckSignatureFrom(issuer); err != nil {
			return ErrInvalidSSLMaterial
		}
	}
	return nil
}

func validateCertificateKeyStrength(certificate *x509.Certificate) error {
	switch publicKey := certificate.PublicKey.(type) {
	case *rsa.PublicKey:
		return validateRSAKeyStrength(publicKey)
	case *ecdsa.PublicKey:
		if publicKey.Curve == nil || publicKey.Curve.Params().BitSize < 256 {
			return ErrInvalidSSLMaterial
		}
		return nil
	default:
		return ErrInvalidSSLMaterial
	}
}

func validateRSAKeyStrength(publicKey *rsa.PublicKey) error {
	if publicKey == nil || publicKey.N == nil || publicKey.N.BitLen() < 2048 {
		return ErrInvalidSSLMaterial
	}
	return nil
}

func validManagedSSLMode(mode string) bool {
	return mode == ManagedSSLModeDefault || mode == ManagedSSLModeUpload
}

func managedSSLRootPath(mode string) (string, error) {
	return managedSSLRootPathAtRoot(ROOT_PATH, mode)
}

func managedSSLRootPathAtRoot(root string, mode string) (string, error) {
	if !validManagedSSLMode(mode) {
		return "", ErrInvalidSSLMode
	}
	rootPath, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	folder := "default"
	if mode == ManagedSSLModeUpload {
		folder = "upload"
	}
	return filepath.Join(rootPath, managedSSLRelativeRoot, folder), nil
}

func managedSSLReference(mode, name string) string {
	folder := "default"
	if mode == ManagedSSLModeUpload {
		folder = "upload"
	}
	return filepath.ToSlash(filepath.Join(managedSSLRelativeRoot, folder, "current", name))
}

func resolveManagedReference(rootPath, reference, expectedName string) (string, error) {
	return resolveManagedReferenceAtRoot(ROOT_PATH, rootPath, reference, expectedName)
}

func resolveManagedReferenceAtRoot(root, rootPath, reference, expectedName string) (string, error) {
	rawReference := strings.TrimSpace(reference)
	if rawReference == "" || strings.Contains(rawReference, `\`) || filepath.IsAbs(rawReference) || isWindowsAbsolute(rawReference) {
		return "", ErrInvalidSSLReference
	}
	reference = filepath.FromSlash(rawReference)
	if filepath.ToSlash(filepath.Clean(reference)) != rawReference {
		return "", ErrInvalidSSLReference
	}
	workspaceRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", ErrInvalidSSLReference
	}
	target, err := filepath.Abs(filepath.Join(workspaceRoot, reference))
	if err != nil || filepath.Base(target) != expectedName || !pathWithin(rootPath, target) {
		return "", ErrInvalidSSLReference
	}
	directory := filepath.Dir(target)
	version := filepath.Base(directory)
	if filepath.Dir(directory) != rootPath || (version != "current" && !validSSLVersion(version)) {
		return "", ErrInvalidSSLReference
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(directory) {
		return "", ErrUnsafeSSLFile
	}
	return target, nil
}

func validSSLVersion(version string) bool {
	if !strings.HasPrefix(version, "version-") || len(version) != len("version-")+32 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(version, "version-"))
	return err == nil
}

// StageManagedSSL 只写入独立版本，不替换现用文件；配置事务成功后引用才生效。
func StageManagedSSL(mode string, privateKeyPEM, certificatePEM, issuerPEM []byte, domains []string) (ManagedSSLReference, error) {
	return stageManagedSSLAtRoot(ROOT_PATH, mode, privateKeyPEM, certificatePEM, issuerPEM, domains)
}

func stageManagedSSLAtRoot(root string, mode string, privateKeyPEM, certificatePEM, issuerPEM []byte, domains []string) (ManagedSSLReference, error) {
	if err := validateSSLMaterial(privateKeyPEM, certificatePEM, issuerPEM, domains); err != nil {
		return ManagedSSLReference{}, err
	}
	managedRoot, err := managedSSLRootPathAtRoot(root, mode)
	if err != nil {
		return ManagedSSLReference{}, err
	}
	if err := ensureManagedDirectoryAtRoot(root, managedRoot); err != nil {
		return ManagedSSLReference{}, err
	}
	token, err := randomStorageToken()
	if err != nil {
		return ManagedSSLReference{}, err
	}
	directory := filepath.Join(managedRoot, "version-"+token)
	if err := os.Mkdir(directory, 0700); err != nil {
		return ManagedSSLReference{}, err
	}
	storage, err := os.OpenRoot(directory)
	if err != nil {
		return ManagedSSLReference{}, err
	}
	defer storage.Close()
	for _, file := range []struct {
		name string
		data []byte
	}{{"private.key", privateKeyPEM}, {"public.crt", certificatePEM}, {"issuer.crt", issuerPEM}} {
		if len(file.data) > 0 {
			if err := atomicfile.WriteRoot(storage, file.name, file.data, 0600); err != nil {
				return ManagedSSLReference{}, err
			}
		}
	}
	publicPath, privatePath := filepath.Join(directory, "public.crt"), filepath.Join(directory, "private.key")
	if err := validateManagedSSLFiles(publicPath, privatePath); err != nil {
		return ManagedSSLReference{}, err
	}
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return ManagedSSLReference{}, err
	}
	publicRef, err := filepath.Rel(rootPath, publicPath)
	if err != nil {
		return ManagedSSLReference{}, err
	}
	privateRef, err := filepath.Rel(rootPath, privatePath)
	if err != nil {
		return ManagedSSLReference{}, err
	}
	return ManagedSSLReference{Mode: mode, CertificateRef: filepath.ToSlash(publicRef), PrivateKeyRef: filepath.ToSlash(privateRef), CertificatePath: publicPath, PrivateKeyPath: privatePath}, nil
}

func validateManagedSSLFiles(certificatePath, privateKeyPath string) error {
	for _, item := range []struct {
		path      string
		isPrivate bool
	}{{certificatePath, false}, {privateKeyPath, true}} {
		info, err := os.Lstat(item.path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxManagedSSLFileSize || hasMultipleFileLinks(item.path) {
			return ErrUnsafeSSLFile
		}
		if item.isPrivate && info.Mode().Perm()&0077 != 0 && runtimeSupportsPermissionCheck() {
			return ErrSSLPrivateKeyPermission
		}
	}
	privateKeyPEM, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return ErrUnsafeSSLFile
	}
	certificatePEM, err := os.ReadFile(certificatePath)
	if err != nil {
		return ErrUnsafeSSLFile
	}
	return validateSSLMaterial(privateKeyPEM, certificatePEM, nil, nil)
}

func resolveLegacySSLPaths(certificatePath, privateKeyPath string) (string, string, error) {
	certificatePath, certificatePEM, err := readLegacySSLFile(certificatePath, false)
	if err != nil {
		return "", "", err
	}
	privateKeyPath, privateKeyPEM, err := readLegacySSLFile(privateKeyPath, true)
	if err != nil {
		return "", "", err
	}
	if err := validateSSLMaterial(privateKeyPEM, certificatePEM, nil, nil); err != nil {
		return "", "", err
	}
	return certificatePath, privateKeyPath, nil
}

func readLegacySSLFile(value string, privateKey bool) (string, []byte, error) {
	value = strings.TrimSpace(value)
	if value == "" || !filepath.IsAbs(value) {
		return "", nil, ErrInvalidSSLReference
	}
	path := filepath.Clean(value)
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxManagedSSLFileSize || hasMultipleFileLinks(path) {
		return "", nil, ErrUnsafeSSLFile
	}
	resolvedDirectory, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil || filepath.Clean(resolvedDirectory) != filepath.Clean(filepath.Dir(path)) {
		return "", nil, ErrUnsafeSSLFile
	}
	if privateKey && info.Mode().Perm()&0077 != 0 && runtimeSupportsPermissionCheck() {
		return "", nil, ErrSSLPrivateKeyPermission
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, ErrUnsafeSSLFile
	}
	return path, data, nil
}

func rollbackManagedSSLActivation(activePath, backupPath string, previousMoved, activeInstalled bool) error {
	if activeInstalled {
		if err := removeManagedSSLDirectory(activePath); err != nil {
			return err
		}
	}
	if previousMoved {
		return os.Rename(backupPath, activePath)
	}
	return nil
}

func removeManagedSSLDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrUnsafeSSLFile
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != "private.key" && entry.Name() != "public.crt" && entry.Name() != "issuer.crt" {
			return ErrUnsafeSSLFile
		}
		entryPath := filepath.Join(directory, entry.Name())
		entryInfo, statErr := os.Lstat(entryPath)
		if statErr != nil || entryInfo.Mode()&os.ModeSymlink != 0 || !entryInfo.Mode().IsRegular() {
			return ErrUnsafeSSLFile
		}
		if err := os.Remove(entryPath); err != nil {
			return err
		}
	}
	return os.Remove(directory)
}

func ensureManagedDirectory(directory string) error {
	return ensureManagedDirectoryAtRoot(ROOT_PATH, directory)
}

func ensureManagedDirectoryAtRoot(root, directory string) error {
	rootPath, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return err
	}
	if !pathWithin(rootPath, directory) {
		return ErrUnsafeSSLFile
	}
	relative, err := filepath.Rel(rootPath, directory)
	if err != nil || filepath.IsAbs(relative) {
		return ErrUnsafeSSLFile
	}
	current := rootPath
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		if part == "" || part == "." || part == ".." {
			return ErrUnsafeSSLFile
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if err := os.Mkdir(current, 0700); err != nil {
				return err
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrUnsafeSSLFile
		}
		if err := os.Chmod(current, 0700); err != nil {
			return err
		}
	}
	return nil
}

func pathWithin(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func randomStorageToken() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func runtimeSupportsPermissionCheck() bool {
	return os.PathSeparator == '/'
}
