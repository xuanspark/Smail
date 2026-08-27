package setup

import (
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"strings"

	"git.pcux.cn/xuans/Smail/i18n"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/errors"
	"git.pcux.cn/xuans/Smail/utils/ip"
)

type DNSItem struct {
	Type  string `json:"type"`
	Host  string `json:"host"`
	Value string `json:"value"`
	TTL   int    `json:"ttl"`
	Tips  string `json:"tips"`
}

func GetDNSSettings(ctx *context.Context) (map[string][]*DNSItem, error) {
	configData, err := config.ReadConfig()
	if err != nil {
		return nil, errors.Wrap(err)
	}

	dkimPublicKey, err := auth.DkimGen()
	if err != nil {
		return nil, errors.Wrap(err)
	}

	ret := make(map[string][]*DNSItem)

	for _, domain := range configData.Domains {
		ret[domain] = []*DNSItem{
			{Type: "A", Host: strings.ReplaceAll(configData.WebDomain, "."+configData.Domain, ""), Value: ip.GetIp(), TTL: 3600, Tips: i18n.GetText(ctx.Lang, "ip_taps")},
			{Type: "A", Host: "smtp", Value: ip.GetIp(), TTL: 3600, Tips: i18n.GetText(ctx.Lang, "ip_taps")},
			{Type: "A", Host: "imap", Value: ip.GetIp(), TTL: 3600, Tips: i18n.GetText(ctx.Lang, "ip_taps")},
			{Type: "A", Host: "pop", Value: ip.GetIp(), TTL: 3600, Tips: i18n.GetText(ctx.Lang, "ip_taps")},
			{Type: "A", Host: "@", Value: ip.GetIp(), TTL: 3600, Tips: i18n.GetText(ctx.Lang, "ip_taps")},
			{Type: "MX", Host: "@", Value: fmt.Sprintf("smtp.%s", domain), TTL: 3600},
			{Type: "TXT", Host: "@", Value: "v=spf1 a mx ~all", TTL: 3600},
			{Type: "TXT", Host: "default._domainkey", Value: dkimPublicKey, TTL: 3600},
		}
	}

	return ret, nil
}
