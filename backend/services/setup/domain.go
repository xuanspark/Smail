package setup

import (
	"fmt"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/errors"
	"git.pcux.cn/xuans/Smail/utils/validation"
)

func GetDomainSettings() (string, string, []string, error) {
	configData, err := config.ReadConfig()
	if err != nil {
		return "", "", []string{}, errors.Wrap(err)
	}

	return configData.Domain, configData.WebDomain, array.Difference(configData.Domains, []string{configData.Domain}), nil
}

func SetDomainSettings(smtpDomain, webDomain, multiDomains string) error {
	return setDomainSettings(smtpDomain, webDomain, multiDomains, nil)
}

// SetDomainSettingsWithPublicHTTPSPort 同时保存公网入口端口，不改变内部监听端口。
func SetDomainSettingsWithPublicHTTPSPort(smtpDomain, webDomain, multiDomains string, publicHTTPSPort int) error {
	if publicHTTPSPort < 1 || publicHTTPSPort > 65535 {
		return fmt.Errorf("公网 HTTPS 端口必须是 1 到 65535 的整数")
	}
	return setDomainSettings(smtpDomain, webDomain, multiDomains, &publicHTTPSPort)
}

func setDomainSettings(smtpDomain, webDomain, multiDomains string, publicHTTPSPort *int) error {
	configData, err := config.ReadConfig()
	if err != nil {
		return errors.Wrap(err)
	}
	markSetupInProgress(configData)

	smtpDomain, err = validation.NormalizeDomain(smtpDomain)
	if err != nil {
		return errors.Wrap(err)
	}
	webDomain, err = validation.NormalizeDomain(webDomain)
	if err != nil {
		return errors.Wrap(err)
	}
	domains, err := validation.NormalizeDomainList(multiDomains)
	if err != nil {
		return errors.Wrap(err)
	}

	configData.Domains = domains
	if !array.InArray(smtpDomain, configData.Domains) {
		configData.Domains = append(configData.Domains, smtpDomain)
	}

	configData.Domain = smtpDomain
	configData.WebDomain = webDomain
	if publicHTTPSPort != nil {
		configData.PublicHTTPSPort = *publicHTTPSPort
	}

	// 检查域名是否指向本机 todo

	err = config.WriteSetupConfig(configData)
	if err != nil {
		return errors.Wrap(err)
	}
	return nil
}
