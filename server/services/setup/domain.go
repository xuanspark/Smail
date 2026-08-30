package setup

import (
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

	// 检查域名是否指向本机 todo

	err = config.WriteConfig(configData)
	if err != nil {
		return errors.Wrap(err)
	}
	return nil
}
