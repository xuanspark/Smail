package setup

import (
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestBuildSPFRecordIncludesPublicIPv4AndIPv6(t *testing.T) {
	value := buildSPFRecord("203.0.113.15", "2001:db8::15")
	want := "v=spf1 a mx ip4:203.0.113.15 ip6:2001:db8::15 ~all"
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
		{name: "empty", want: "v=spf1 a mx ~all"},
		{name: "private IPv4", ipv4: "192.168.1.20", want: "v=spf1 a mx ~all"},
		{name: "link local IPv6", ipv6: "fe80::1", want: "v=spf1 a mx ~all"},
		{name: "only IPv4", ipv4: "198.51.100.9", want: "v=spf1 a mx ip4:198.51.100.9 ~all"},
		{name: "only IPv6", ipv6: "2001:db8::9", want: "v=spf1 a mx ip6:2001:db8::9 ~all"},
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
	if value := values["@"]; value != "v=spf1 a mx ip4:203.0.113.10 ip6:2001:db8::10 ~all" {
		t.Fatalf("SPF TXT value = %q", value)
	}
	if value := values["_dmarc"]; value != "v=DMARC1; p=quarantine" {
		t.Fatalf("DMARC TXT value = %q", value)
	}
	if value := values["default._domainkey"]; value != "v=DKIM1; k=rsa; p=test" {
		t.Fatalf("DKIM TXT value = %q", value)
	}
}
