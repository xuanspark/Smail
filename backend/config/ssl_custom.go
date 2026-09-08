package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrSSLFileUnavailable = errors.New("SSL certificate file is missing or unreadable")

// customSSLMaterial 在只读准备阶段保留已校验的材料，确认后再暂存，避免重新读取外部文件。
type customSSLMaterial struct {
	privateKey  []byte
	certificate []byte
	domains     []string
}

func prepareCustomSSLPaths(root string, candidate *RuntimeConfig) (*customSSLMaterial, error) {
	candidate.SSL.TLS = normalizeSSLDataReferences(candidate.SSL.TLS)
	ssl := &candidate.SSL.TLS
	if EffectiveSSLManagement(*ssl) != SSLManagementCustom {
		return nil, nil
	}
	certificatePath := strings.TrimSpace(ssl.CertificateRef)
	privateKeyPath := strings.TrimSpace(ssl.PrivateKeyRef)
	if certificatePath == "" && privateKeyPath == "" {
		return nil, nil
	}
	if certificatePath == "" || privateKeyPath == "" {
		return nil, ErrInvalidSSLReference
	}
	// 已保存的受管引用只复验，不重复导入；普通相对路径以项目运行根目录为基准。
	if strings.HasPrefix(certificatePath, managedSSLRelativeRoot+"/") && strings.HasPrefix(privateKeyPath, managedSSLRelativeRoot+"/") {
		return nil, validateCustomSSLReferences(root, *candidate)
	}
	var err error
	certificatePath, err = resolveCustomSSLInput(root, certificatePath)
	if err != nil {
		return nil, err
	}
	privateKeyPath, err = resolveCustomSSLInput(root, privateKeyPath)
	if err != nil {
		return nil, err
	}
	material := &customSSLMaterial{domains: runtimeCertificateDomains(candidate)}
	material.privateKey, err = readExternalSSLFile(privateKeyPath, true)
	if err != nil {
		return nil, err
	}
	material.certificate, err = readExternalSSLFile(certificatePath, false)
	if err != nil {
		return nil, err
	}
	if err := validateSSLMaterial(material.privateKey, material.certificate, nil, material.domains); err != nil {
		return nil, err
	}
	ssl.Mode = ManagedSSLModeUpload
	ssl.DefaultDirectory = "data/ssl"
	ssl.CustomDirectory = "data/ssl"
	// 仅在内存中使用合法占位引用完成跨分区校验，最终候选必须换成独立版本。
	ssl.CertificateRef = managedSSLReference(ManagedSSLModeUpload, "public.crt")
	ssl.PrivateKeyRef = managedSSLReference(ManagedSSLModeUpload, "private.key")
	return material, nil
}

func (material *customSSLMaterial) stage(root string, candidate *RuntimeConfig) error {
	if material == nil {
		return nil
	}
	reference, err := stageManagedSSLAtRoot(root, ManagedSSLModeUpload, material.privateKey, material.certificate, nil, material.domains)
	if err != nil {
		return err
	}
	candidate.SSL.TLS.CertificateRef = reference.CertificateRef
	candidate.SSL.TLS.PrivateKeyRef = reference.PrivateKeyRef
	return nil
}

func resolveCustomSSLInput(root, value string) (string, error) {
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return "", ErrInvalidSSLReference
		}
	}
	if !filepath.IsAbs(value) {
		if isWindowsAbsolute(value) || filepath.VolumeName(value) != "" || strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\`) {
			return "", ErrInvalidSSLReference
		}
		value = filepath.Join(root, value)
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return "", ErrInvalidSSLReference
	}
	return path, nil
}

func readExternalSSLFile(path string, privateKey bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, ErrSSLFileUnavailable
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxManagedSSLFileSize || hasMultipleFileLinks(path) {
		return nil, ErrUnsafeSSLFile
	}
	resolvedDirectory, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil || filepath.Clean(resolvedDirectory) != filepath.Clean(filepath.Dir(path)) {
		return nil, ErrUnsafeSSLFile
	}
	if privateKey && info.Mode().Perm()&0077 != 0 && runtimeSupportsPermissionCheck() {
		return nil, ErrSSLPrivateKeyPermission
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrSSLFileUnavailable
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, ErrUnsafeSSLFile
	}
	data, err := io.ReadAll(io.LimitReader(file, maxManagedSSLFileSize+1))
	if err != nil {
		return nil, ErrSSLFileUnavailable
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) || len(data) != int(info.Size()) || len(data) > maxManagedSSLFileSize {
		return nil, ErrUnsafeSSLFile
	}
	return data, nil
}

func validateCustomSSLReferences(root string, candidate RuntimeConfig) error {
	ssl := candidate.SSL.TLS
	if EffectiveSSLManagement(ssl) != SSLManagementCustom || ssl.CertificateRef == "" && ssl.PrivateKeyRef == "" {
		return nil
	}
	reference, err := resolveManagedSSLReferencesAtRoot(root, ssl)
	if err != nil {
		return err
	}
	privateKey, err := readExternalSSLFile(reference.PrivateKeyPath, true)
	if err != nil {
		return err
	}
	certificate, err := readExternalSSLFile(reference.CertificatePath, false)
	if err != nil {
		return err
	}
	return validateSSLMaterial(privateKey, certificate, nil, runtimeCertificateDomains(&candidate))
}

func runtimeCertificateDomains(candidate *RuntimeConfig) []string {
	return SSLCertificateDomains(candidate.Root.Domains.Primary, candidate.Root.Domains.Web, candidate.Root.Domains.Accepted)
}

// SSLCertificateDomains 统一初始化和配置管理所需的 Web、主域及邮件协议域名。
func SSLCertificateDomains(primary, web string, accepted []string) []string {
	seen := make(map[string]bool)
	var domains []string
	appendDomain := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && !seen[value] {
			seen[value] = true
			domains = append(domains, value)
		}
	}
	appendDomain(web)
	appendDomain(primary)
	for _, domain := range accepted {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		appendDomain(domain)
		appendDomain("smtp." + domain)
		appendDomain("pop." + domain)
		appendDomain("imap." + domain)
	}
	return domains
}

// SSLValidationIssue 将已知证书错误转换成安全的中文提示，不回显磁盘路径或私钥材料。
func SSLValidationIssue(err error) (PublicIssue, bool) {
	issue := PublicIssue{Code: "SSL_INVALID"}
	switch {
	case errors.Is(err, ErrSSLFileUnavailable):
		issue.Message = "证书或私钥文件不存在或无法读取，请检查服务器路径和运行用户权限"
	case errors.Is(err, ErrSSLCertificateMismatch):
		issue.Message = "证书与私钥不匹配，请选择同一套证书文件"
	case errors.Is(err, ErrSSLCertificateExpired):
		issue.Message = "证书尚未生效或已经过期，请更换有效证书"
	case errors.Is(err, ErrSSLCertificateDomain):
		issue.Message = "证书未覆盖已配置的主域名、Web 域名或 SMTP、IMAP、POP3 域名"
	case errors.Is(err, ErrSSLPrivateKeyPermission):
		issue.Message = "私钥权限过于宽松，请限制为仅运行用户可读取"
	case errors.Is(err, ErrUnsafeSSLFile):
		issue.Message = "证书文件不安全或大小无效，请使用不超过 1 MB 的普通文件，避免链接文件"
	case errors.Is(err, ErrInvalidSSLMaterial):
		issue.Message = "证书内容、证书链或密钥强度无效，请使用有效的 PEM 证书和私钥"
	case errors.Is(err, ErrInvalidSSLReference):
		issue.Message = "请填写完整的证书和私钥路径，路径不能包含上级目录跳转"
	case errors.Is(err, ErrInvalidSSLMode), errors.Is(err, ErrInvalidSSL):
		issue.Message = "证书管理方式或存储配置无效，请检查 SSL 配置"
	default:
		return PublicIssue{}, false
	}
	return issue, true
}
