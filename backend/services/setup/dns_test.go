package setup

import (
	stdcontext "context"
	"net"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestBuildSPFRecordIncludesPublicIPv4AndIPv6(t *testing.T) {
	value := buildSPFRecord("203.0.113.15", "2001:db8::15")
	want := "v=spf1 ip4:203.0.113.15 ip6:2001:db8::15 ~all"
	if value != want {
		t.Fatalf("SPF record = %q, want %q", value, want)
	}
}

func TestBuildSPFRecordSkipsUnsafeAddresses(t *testing.T) {
	cases := []struct {
		name string
		ipv4 string
		ipv6 string
		want string
	}{
		{name: "empty", want: ""},
		{name: "private IPv4", ipv4: "192.168.1.20", want: ""},
		{name: "link local IPv6", ipv6: "fe80::1", want: ""},
		{name: "only IPv4", ipv4: "198.51.100.9", want: "v=spf1 ip4:198.51.100.9 ~all"},
		{name: "only IPv6", ipv6: "2001:db8::9", want: "v=spf1 ip6:2001:db8::9 ~all"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if value := buildSPFRecord(item.ipv4, item.ipv6); value != item.want {
				t.Fatalf("SPF record = %q, want %q", value, item.want)
			}
		})
	}
}

func TestBuildDNSItemsIncludesSPFAndQuarantineDMARC(t *testing.T) {
	cfg := &config.Config{
		Domain:    "example.com",
		WebDomain: "mail.example.com",
	}
	items := buildDNSItems(cfg, "example.com", "v=DKIM1; k=rsa; p=test", "203.0.113.10", "2001:db8::10", "")

	values := make(map[string]string)
	for _, item := range items {
		if item.Type == "TXT" {
			values[item.Host] = item.Value
		}
	}
	if value := values["@"]; value != "v=spf1 ip4:203.0.113.10 ip6:2001:db8::10 ~all" {
		t.Fatalf("SPF TXT value = %q", value)
	}
	if value := values["_dmarc"]; value != "v=DMARC1; p=quarantine; rua=mailto:admin@example.com" {
		t.Fatalf("DMARC TXT value = %q", value)
	}
	if value := values["default._domainkey"]; value != "v=DKIM1; k=rsa; p=test" {
		t.Fatalf("DKIM TXT value = %q", value)
	}
}

func TestBuildDNSItemsCreatesExpectedRecordShape(t *testing.T) {
	cfg := &config.Config{
		Domain:    "example.com",
		WebDomain: "panel.example.com",
	}
	items := buildDNSItemsWithWebIPv4(cfg, "example.com", "v=DKIM1; k=rsa; p=test", "198.51.100.20", "2001:db8::20", "", []string{"203.0.113.20"})

	if len(items) != 14 {
		t.Fatalf("DNS item count = %d, want 14", len(items))
	}
	if items[0].Host != "panel" || items[0].Type != "A" || items[0].Value != "203.0.113.20" {
		t.Fatalf("web A record = %#v", items[0])
	}
	if items[4].Host != "@" || items[4].Type != "A" || items[4].Value != "198.51.100.20" {
		t.Fatalf("root A record = %#v", items[4])
	}
	if items[5].Type != "AAAA" || items[5].Host != "@" || items[5].Value != "2001:db8::20" {
		t.Fatalf("root AAAA record = %#v", items[5])
	}
	for index, host := range map[int]string{6: "smtp", 7: "imap", 8: "pop"} {
		if items[index].Type != "AAAA" || items[index].Host != host || items[index].Value != "2001:db8::20" {
			t.Fatalf("mail service AAAA record = %#v", items[index])
		}
	}
	if items[9].Host != "*" || items[9].Type != "A" || items[9].Value != "198.51.100.20" || items[9].TTL != dnsRecordTTL {
		t.Fatalf("wildcard A record = %#v", items[9])
	}
	if items[10].Value != "smtp.example.com | 1" || items[10].TTL != dnsRecordTTL {
		t.Fatalf("MX record = %#v", items[10])
	}
}

func TestBuildDNSItemsDoesNotReuseMailIPForUnresolvedWebDomain(t *testing.T) {
	cfg := &config.Config{
		Domain:    "example.net",
		WebDomain: "panel.example.net",
	}
	items := buildDNSItemsWithWebIPv4(cfg, "example.net", "v=DKIM1; k=rsa; p=test", "198.51.100.30", "", "", nil)

	if items[0].Host != "panel" || items[0].Value != "" || items[0].UnavailableReason != unresolvedWebIPv4 {
		t.Fatalf("unresolved web A record = %#v", items[0])
	}
	if items[1].Host != "smtp" || items[1].Value != "198.51.100.30" {
		t.Fatalf("mail A record = %#v", items[1])
	}
}

func TestResolvePublicIPv4UsesActualLookupResults(t *testing.T) {
	lookup := func(ctx stdcontext.Context, host string) ([]net.IP, error) {
		if host != "panel.example.org" {
			t.Fatalf("lookup host = %q", host)
		}
		return []net.IP{
			net.ParseIP("203.0.113.40"),
			net.ParseIP("192.168.1.40"),
			net.ParseIP("203.0.113.40"),
		}, nil
	}

	values := resolvePublicIPv4WithLookup("Panel.Example.Org.", lookup)
	if len(values) != 1 || values[0] != "203.0.113.40" {
		t.Fatalf("resolved public IPv4 = %#v", values)
	}
}

func TestDNSHostForDomain(t *testing.T) {
	cases := []struct {
		name   string
		web    string
		domain string
		want   string
	}{
		{name: "subdomain", web: "mail.example.com", domain: "example.com", want: "mail"},
		{name: "root", web: "example.com", domain: "example.com", want: "@"},
		{name: "trailing dot", web: "mail.example.com.", domain: "example.com.", want: "mail"},
		{name: "external domain", web: "panel.other.example", domain: "example.com", want: "panel.other.example"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if value := dnsHostForDomain(item.web, item.domain); value != item.want {
				t.Fatalf("DNS host = %q, want %q", value, item.want)
			}
		})
	}
}

func TestBuildDNSItemsWithoutPublicIPMarksIncompleteRecords(t *testing.T) {
	cfg := &config.Config{Domain: "example.com", WebDomain: "panel.example.com"}
	items := buildDNSItemsWithWebIPv4(cfg, "example.com", "v=DKIM1; k=rsa; p=test", "", "", "", []string{"203.0.113.20"})
	for _, item := range items {
		unavailable := (item.Type == "A" && item.Host != "panel") || (item.Type == "TXT" && item.Host == "@")
		if unavailable {
			if item.Value != "" || item.UnavailableReason == "" {
				t.Fatalf("未就绪记录必须留空并提供原因：%#v", item)
			}
		} else if item.Value == "" || item.UnavailableReason != "" {
			t.Fatalf("有效记录不应被禁用：%#v", item)
		}
	}
}

func TestGetDNSSettingsRejectsInvalidManualIPv4BeforeLoadingConfig(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "192.168.1.1", "::1", "example.com", "203.0.113.20:25", "invalid"} {
		if _, err := GetDNSSettingsWithIPv4(nil, value); err != ErrInvalidDNSIPv4 {
			t.Fatalf("无效手工地址 %q 应返回参数错误，实际为 %v", value, err)
		}
	}
}
