package setup

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
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

	ipv4 := ip.GetIPv4()
	ipv6 := ip.GetIPv6()
	ipTips := i18n.GetText(ctx.Lang, "ip_taps")
	ret := make(map[string][]*DNSItem, len(configData.Domains))
	for _, domain := range configData.Domains {
		ret[domain] = buildDNSItems(configData, domain, dkimPublicKey, ipv4, ipv6, ipTips)
	}

	return ret, nil
}

func buildDNSItems(configData *config.Config, domain, dkimPublicKey, ipv4, ipv6, ipTips string) []*DNSItem {
	address := strings.TrimSpace(ipv4)
	if address == "" {
		address = "[Your Server IP]"
	}
	return []*DNSItem{
		{Type: "A", Host: strings.ReplaceAll(configData.WebDomain, "."+configData.Domain, ""), Value: address, TTL: 3600, Tips: ipTips},
		{Type: "A", Host: "smtp", Value: address, TTL: 3600, Tips: ipTips},
		{Type: "A", Host: "imap", Value: address, TTL: 3600, Tips: ipTips},
		{Type: "A", Host: "pop", Value: address, TTL: 3600, Tips: ipTips},
		{Type: "A", Host: "@", Value: address, TTL: 3600, Tips: ipTips},
		{Type: "MX", Host: "@", Value: fmt.Sprintf("smtp.%s", domain), TTL: 3600},
		{Type: "TXT", Host: "@", Value: buildSPFRecord(ipv4, ipv6), TTL: 3600},
		{Type: "TXT", Host: "_dmarc", Value: "v=DMARC1; p=quarantine", TTL: 3600},
		{Type: "TXT", Host: "default._domainkey", Value: dkimPublicKey, TTL: 3600},
	}
}

func buildSPFRecord(ipv4, ipv6 string) string {
	mechanisms := []string{"v=spf1", "a", "mx"}
	if mechanism := buildSPFAddressMechanism(ipv4); mechanism != "" {
		mechanisms = append(mechanisms, mechanism)
	}
	if mechanism := buildSPFAddressMechanism(ipv6); mechanism != "" {
		mechanisms = append(mechanisms, mechanism)
	}
	mechanisms = append(mechanisms, "~all")
	return strings.Join(mechanisms, " ")
}

func buildSPFAddressMechanism(value string) string {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	address = address.Unmap()
	if !isPublicDNSAddress(address) {
		return ""
	}
	if address.Is4() {
		return "ip4:" + address.String()
	}
	if address.Is6() {
		return "ip6:" + address.String()
	}
	return ""
}

func isPublicDNSAddress(address netip.Addr) bool {
	return address.IsValid() && address.IsGlobalUnicast() &&
		!address.IsPrivate() && !address.IsLoopback() &&
		!address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast() &&
		!address.IsMulticast() && !address.IsUnspecified() &&
		!net.IP(address.AsSlice()).IsInterfaceLocalMulticast()
}
