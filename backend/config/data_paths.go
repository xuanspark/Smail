package config

import "git.pcux.cn/xuans/Smail/utils/runtimepath"

// normalizeSSLDataReferences 在原始摘要校验后兼容旧引用，不修改历史备份的字节及摘要。
func normalizeSSLDataReferences(ssl SSLConfig) SSLConfig {
	ssl.DefaultDirectory = runtimepath.LegacyReference(ssl.DefaultDirectory)
	ssl.CustomDirectory = runtimepath.LegacyReference(ssl.CustomDirectory)
	ssl.CertificateRef = runtimepath.LegacyReference(ssl.CertificateRef)
	ssl.PrivateKeyRef = runtimepath.LegacyReference(ssl.PrivateKeyRef)
	ssl.PreviousCertificateRef = runtimepath.LegacyReference(ssl.PreviousCertificateRef)
	ssl.PreviousPrivateKeyRef = runtimepath.LegacyReference(ssl.PreviousPrivateKeyRef)
	return ssl
}
