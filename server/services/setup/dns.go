package setup

import (
	stdcontext "context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

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

const dnsRecordTTL = 600

const unresolvedWebIPv4 = "[未解析到 Web 域名公网 IPv4]"

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
	webIPv4 := resolvePublicIPv4(configData.WebDomain)
	ipTips := i18n.GetText(ctx.Lang, "ip_taps")
	ret := make(map[string][]*DNSItem, len(configData.Domains))
	for _, domain := range configData.Domains {
		ret[domain] = buildDNSItemsWithWebIPv4(configData, domain, dkimPublicKey, ipv4, ipv6, ipTips, webIPv4)
	}

	return ret, nil
}

func buildDNSItems(configData *config.Config, domain, dkimPublicKey, ipv4, ipv6, ipTips string) []*DNSItem {
	return buildDNSItemsWithWebIPv4(configData, domain, dkimPublicKey, ipv4, ipv6, ipTips, nil)
}

func buildDNSItemsWithWebIPv4(configData *config.Config, domain, dkimPublicKey, ipv4, ipv6, ipTips string, webIPv4 []string) []*DNSItem {
	address := normalizeDNSAddress(ipv4)
	if address == "" {
		address = "[\u672a\u68c0\u6d4b\u5230\u516c\u7f51 IPv4]"
	}

	items := make([]*DNSItem, 0, 13)
	appendARecord := func(host string) {
		for _, item := range items {
			if item.Type == "A" && item.Host == host {
				return
			}
		}
		items = append(items, &DNSItem{
			Type:  "A",
			Host:  host,
			Value: address,
			TTL:   dnsRecordTTL,
			Tips:  ipTips,
		})
	}
	appendAAAARecord := func(host, address string) {
		for _, item := range items {
			if item.Type == "AAAA" && item.Host == host {
				return
			}
		}
		items = append(items, &DNSItem{
			Type:  "AAAA",
			Host:  host,
			Value: address,
			TTL:   dnsRecordTTL,
			Tips:  ipTips,
		})
	}

	webHost := dnsHostForDomain(configData.WebDomain, domain)
	if webHost == "@" {
		appendARecord(webHost)
	} else if addresses := normalizeDNSAddresses(webIPv4); len(addresses) > 0 {
		for _, webAddress := range addresses {
			items = append(items, &DNSItem{
				Type:  "A",
				Host:  webHost,
				Value: webAddress,
				TTL:   dnsRecordTTL,
				Tips:  ipTips,
			})
		}
	} else {
		items = append(items, &DNSItem{
			Type:  "A",
			Host:  webHost,
			Value: unresolvedWebIPv4,
			TTL:   dnsRecordTTL,
			Tips:  "Web 域名未解析到公网 IPv4，请确认域名解析状态",
		})
	}
	appendARecord("smtp")
	appendARecord("imap")
	appendARecord("pop")
	appendARecord("@")

	if ipv6Address := buildSPFAddressMechanism(ipv6); strings.HasPrefix(ipv6Address, "ip6:") {
		address := strings.TrimPrefix(ipv6Address, "ip6:")
		for _, host := range []string{"@", "smtp", "imap", "pop"} {
			appendAAAARecord(host, address)
		}
	}

	items = append(items,
		&DNSItem{Type: "MX", Host: "@", Value: fmt.Sprintf("smtp.%s | 1", domain), TTL: dnsRecordTTL},
		&DNSItem{Type: "TXT", Host: "@", Value: buildSPFRecord(ipv4, ipv6), TTL: dnsRecordTTL},
		&DNSItem{Type: "TXT", Host: "_dmarc", Value: buildDMARCRecord(domain), TTL: dnsRecordTTL},
		&DNSItem{Type: "TXT", Host: "default._domainkey", Value: dkimPublicKey, TTL: dnsRecordTTL},
	)
	return items
}

func buildSPFRecord(ipv4, ipv6 string) string {
	mechanisms := []string{"v=spf1"}
	if mechanism := buildSPFAddressMechanism(ipv4); mechanism != "" {
		mechanisms = append(mechanisms, mechanism)
	}
	if mechanism := buildSPFAddressMechanism(ipv6); mechanism != "" {
		mechanisms = append(mechanisms, mechanism)
	}
	mechanisms = append(mechanisms, "~all")
	return strings.Join(mechanisms, " ")
}

func buildDMARCRecord(domain string) string {
	return fmt.Sprintf("v=DMARC1; p=quarantine; rua=mailto:admin@%s", domain)
}

func normalizeDNSAddress(value string) string {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || !isPublicDNSAddress(address) || !address.Is4() {
		return ""
	}
	return address.String()
}

func normalizeDNSAddresses(values []string) []string {
	addresses := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		address := normalizeDNSAddress(value)
		if address == "" {
			continue
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}
	return addresses
}

func resolvePublicIPv4(host string) []string {
	return resolvePublicIPv4WithLookup(host, func(ctx stdcontext.Context, target string) ([]net.IP, error) {
		return net.DefaultResolver.LookupIP(ctx, "ip4", target)
	})
}

func resolvePublicIPv4WithLookup(host string, lookup func(stdcontext.Context, string) ([]net.IP, error)) []string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || lookup == nil {
		return nil
	}

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 3*time.Second)
	defer cancel()
	addresses, err := lookup(ctx, host)
	if err != nil {
		return nil
	}

	values := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if address == nil {
			continue
		}
		values = append(values, address.String())
	}
	return normalizeDNSAddresses(values)
}

func dnsHostForDomain(webDomain, domain string) string {
	webDomain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(webDomain)), ".")
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if webDomain == "" || webDomain == domain {
		return "@"
	}

	suffix := "." + domain
	if strings.HasSuffix(webDomain, suffix) {
		host := strings.TrimSuffix(webDomain, suffix)
		if host != "" {
			return host
		}
	}
	return webDomain
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
