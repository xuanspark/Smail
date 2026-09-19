package config

import "git.pcux.cn/xuans/Smail/utils/credential"

const DNSProviderAliDNS = "alidns"

func validateDNSProvider(ssl SSLConfig) error {
	if ssl.DNSProvider != "" && ssl.DNSProvider != DNSProviderAliDNS {
		return ErrInvalidSSL
	}
	if ssl.DNSZone != "" {
		domain, err := normalizeConfigDomain(ssl.DNSZone)
		if err != nil || domain != ssl.DNSZone {
			return ErrInvalidSSL
		}
	}
	for _, stored := range []StoredSecret{ssl.DNSAccessKey, ssl.DNSSecretKey} {
		if err := ValidateStoredSecret(stored, ssl.DNSProvider != ""); err != nil {
			return ErrInvalidSSL
		}
	}
	return nil
}

// DNSProviderCredentials 只在签发进程内解密，调用方不得记录返回值。
func DNSProviderCredentials(ssl SSLConfig) (string, string, error) {
	if ssl.DNSProvider != DNSProviderAliDNS || validateDNSProvider(ssl) != nil {
		return "", "", ErrSecretUnavailable
	}
	vault, err := credential.NewVault(ROOT_PATH)
	if err != nil {
		return "", "", ErrSecretUnavailable
	}
	accessKey, err := OpenStoredSecret(vault, ssl.DNSAccessKey)
	if err != nil {
		return "", "", ErrSecretUnavailable
	}
	secretKey, err := OpenStoredSecret(vault, ssl.DNSSecretKey)
	if err != nil {
		return "", "", ErrSecretUnavailable
	}
	return accessKey, secretKey, nil
}
