package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
)

// ExportProjectTLS 为本机 TLS 入口导出一个原子更新的证书私钥合并文件。
// 仅接受项目生成的平铺证书；不允许高权限调用者按配置读取任意服务器路径。
func ExportProjectTLS(root, destination string) error {
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
	resolvedSource, err := filepath.EvalSymlinks(sourceDirectory)
	if err != nil || resolvedSource != sourceDirectory { return ErrUnsafeSSLFile }
	storage, err := os.OpenRoot(sourceDirectory)
	if err != nil { return ErrUnsafeSSLFile }
	defer storage.Close()
	read := func(name string) ([]byte, error) {
		info, err := storage.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxManagedSSLFileSize || info.Mode().Perm() & 0077 != 0 { return nil, ErrUnsafeSSLFile }
		file, err := storage.Open(name)
		if err != nil { return nil, ErrUnsafeSSLFile }
		defer file.Close()
		opened, err := file.Stat()
		if err != nil || !os.SameFile(info, opened) { return nil, ErrUnsafeSSLFile }
		data, err := io.ReadAll(io.LimitReader(file, maxManagedSSLFileSize+1))
		if err != nil || len(data) > maxManagedSSLFileSize { return nil, ErrUnsafeSSLFile }
		return data, nil
	}
	certificate, err := read(filepath.Base(publicPath))
	if err != nil { return err }
	privateKey, err := read(filepath.Base(privatePath))
	if err != nil { return err }
	if err := validateSSLMaterial(privateKey, certificate, nil, validationCertificateDomains(&runtime)); err != nil { return err }
	combined := append(append(append([]byte{}, certificate...), '\n'), privateKey...)
	// 输出目录由部署管理员创建，拒绝符号链接和其他用户可写目录。
	directory := filepath.Dir(destination)
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory { return ErrUnsafeSSLFile }
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() & 0022 != 0 { return ErrUnsafeSSLFile }
	output, err := os.OpenRoot(directory)
	if err != nil { return ErrUnsafeSSLFile }
	defer output.Close()
	name := filepath.Base(destination)
	if info, err := output.Lstat(name); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() & 0077 != 0 { return ErrUnsafeSSLFile }
		previous, err := output.ReadFile(name)
		if err != nil { return ErrUnsafeSSLFile }
		if bytes.Equal(previous, combined) { return nil }
	} else if !os.IsNotExist(err) { return ErrUnsafeSSLFile }
	return atomicfile.WriteRoot(output, name, combined, 0600)
}
