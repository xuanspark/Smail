package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
)

// ExportProjectTLS 为本机 TLS 入口导出一个原子更新的证书私钥合并文件。
// 仅接受项目生成的平铺证书；不允许高权限调用者按配置读取任意服务器路径。
func ExportProjectTLS(root, destination string) error {
	if runtime.GOOS != "linux" {
		return errors.New("本机 Nginx 证书同步仅支持 Linux")
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(destination) {
		return ErrUnsafeSSLFile
	}
	root, destination = filepath.Clean(root), filepath.Clean(destination)
	runtime, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load()
	if err != nil {
		return ErrInvalidSSL
	}
	if EffectiveSSLManagement(runtime.SSL.TLS) != SSLManagementProject || runtime.Root.HTTP.TLSMode == HTTPSTLSModeProxy {
		return nil
	}
	publicPath := filepath.Join(root, filepath.FromSlash(runtime.SSL.TLS.CertificateRef))
	privatePath := filepath.Join(root, filepath.FromSlash(runtime.SSL.TLS.PrivateKeyRef))
	if !flatSSLPair(root, publicPath, privatePath) {
		return ErrInvalidSSLReference
	}
	sourceDirectory := filepath.Join(root, "ssl")
	return exportTLSBundle(sourceDirectory, filepath.Base(publicPath), filepath.Base(privatePath), destination, validationCertificateDomains(&runtime))
}

// PublishExternalTLS 仅由部署管理员显式提供外部续期目录，不从网页配置读取高权限源路径。
// 外部工具使用邮件主域名作为文件名，与续期读取规则一致；校验失败时保留入口证书。
func PublishExternalTLS(root, sourceDirectory, destination string) error {
	if runtime.GOOS != "linux" {
		return errors.New("本机 Nginx 证书同步仅支持 Linux")
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(sourceDirectory) || !filepath.IsAbs(destination) {
		return ErrUnsafeSSLFile
	}
	root, sourceDirectory, destination = filepath.Clean(root), filepath.Clean(sourceDirectory), filepath.Clean(destination)
	runtime, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load()
	if err != nil {
		return ErrInvalidSSL
	}
	if runtime.Root.HTTP.TLSMode != HTTPSTLSModeProxy || EffectiveSSLManagement(runtime.SSL.TLS) == SSLManagementProject {
		return ErrInvalidSSL
	}
	if filepath.Dir(destination) == sourceDirectory {
		return ErrUnsafeSSLFile
	}
	domain := runtime.Root.Domains.Primary
	if domain == "" || domain == "." || domain == ".." || strings.ContainsAny(domain, `/\:`) {
		return ErrUnsafeSSLFile
	}
	return exportTLSBundle(sourceDirectory, domain+".crt", domain+".key", destination, validationCertificateDomains(&runtime))
}

func exportTLSBundle(sourceDirectory, certificateName, privateKeyName, destination string, domains []string) error {
	resolvedSource, err := filepath.EvalSymlinks(sourceDirectory)
	if err != nil || resolvedSource != sourceDirectory {
		return ErrUnsafeSSLFile
	}
	sourceInfo, err := os.Stat(sourceDirectory)
	if err != nil || !sourceInfo.IsDir() || sourceInfo.Mode().Perm()&0022 != 0 {
		return ErrUnsafeSSLFile
	}
	storage, err := os.OpenRoot(sourceDirectory)
	if err != nil {
		return ErrUnsafeSSLFile
	}
	defer storage.Close()
	read := func(name string) ([]byte, error) {
		info, err := storage.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || hasMultipleInfoLinks(info) || info.Size() > maxManagedSSLFileSize || info.Mode().Perm()&0077 != 0 {
			return nil, ErrUnsafeSSLFile
		}
		file, err := storage.Open(name)
		if err != nil {
			return nil, ErrUnsafeSSLFile
		}
		defer file.Close()
		opened, err := file.Stat()
		if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || hasMultipleInfoLinks(opened) || opened.Mode().Perm()&0077 != 0 {
			return nil, ErrUnsafeSSLFile
		}
		data, err := io.ReadAll(io.LimitReader(file, maxManagedSSLFileSize+1))
		if err != nil || len(data) > maxManagedSSLFileSize {
			return nil, ErrUnsafeSSLFile
		}
		after, err := file.Stat()
		if err != nil || !os.SameFile(opened, after) || hasMultipleInfoLinks(after) || after.Size() != int64(len(data)) || !opened.ModTime().Equal(after.ModTime()) || after.Mode().Perm()&0077 != 0 {
			return nil, ErrUnsafeSSLFile
		}
		return data, nil
	}
	certificate, err := read(certificateName)
	if err != nil {
		return err
	}
	privateKey, err := read(privateKeyName)
	if err != nil {
		return err
	}
	if err := validateSSLMaterial(privateKey, certificate, nil, domains); err != nil {
		return err
	}
	combined := append(append(append([]byte{}, certificate...), '\n'), privateKey...)
	// 输出目录由部署管理员创建，拒绝符号链接和其他用户可写目录。
	directory := filepath.Dir(destination)
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory {
		return ErrUnsafeSSLFile
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return ErrUnsafeSSLFile
	}
	output, err := os.OpenRoot(directory)
	if err != nil {
		return ErrUnsafeSSLFile
	}
	defer output.Close()
	name := filepath.Base(destination)
	if info, err := output.Lstat(name); err == nil {
		if !info.Mode().IsRegular() || hasMultipleInfoLinks(info) || info.Mode().Perm()&0077 != 0 {
			return ErrUnsafeSSLFile
		}
		previous, err := output.ReadFile(name)
		if err != nil {
			return ErrUnsafeSSLFile
		}
		if bytes.Equal(previous, combined) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return ErrUnsafeSSLFile
	}
	return atomicfile.WriteRoot(output, name, combined, 0600)
}
