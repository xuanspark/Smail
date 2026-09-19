package ssl

import (
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestWildcardCertificateDomains(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
		want   []string
	}{
		{
			name:   "configured domain",
			config: &config.Config{Domain: "xuans.com", WebDomain: "web.xuans.com", Domains: []string{"xuans.com", "other.example"}},
			want:   []string{"xuans.com", "*.xuans.com"},
		},
		{
			name:   "trimmed wildcard domain",
			config: &config.Config{Domain: "  *.xuans.com.  "},
			want:   []string{"xuans.com", "*.xuans.com"},
		},
		{
			name:   "case normalized domain",
			config: &config.Config{Domain: "Example.COM"},
			want:   []string{"example.com", "*.example.com"},
		},
		{
			name:   "empty domain",
			config: &config.Config{},
			want:   nil,
		},
		{
			name:   "nil config",
			config: nil,
			want:   nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := wildcardCertificateDomains(test.config)
			if len(got) != len(test.want) {
				t.Fatalf("got %d domains, want %d: %v", len(got), len(test.want), got)
			}
			for index := range test.want {
				if got[index] != test.want[index] {
					t.Fatalf("got domain %q, want %q", got[index], test.want[index])
				}
			}
		})
	}
}

func TestCertificateDomainsFollowChallengeMode(t *testing.T) {
	cfg := &config.Config{
		Domain:    "xuans.com",
		WebDomain: "web.xuans.com",
		Domains:   []string{"xuans.com"},
	}

	cfg.SSLType = config.SSLTypeAutoHTTP
	httpDomains := certificateDomains(cfg)
	if len(httpDomains) != 5 {
		t.Fatalf("got %d HTTP domains, want 5: %v", len(httpDomains), httpDomains)
	}
	if httpDomains[0] != "web.xuans.com" || httpDomains[1] != "xuans.com" || httpDomains[2] != "smtp.xuans.com" {
		t.Fatalf("unexpected HTTP domains: %v", httpDomains)
	}

	cfg.SSLType = config.SSLTypeAutoDNS
	dnsDomains := certificateDomains(cfg)
	if len(dnsDomains) != 2 || dnsDomains[0] != "xuans.com" || dnsDomains[1] != "*.xuans.com" {
		t.Fatalf("unexpected DNS domains: %v", dnsDomains)
	}
}

func TestDNSWildcardConfigurationCoverage(t *testing.T) {
	for _, test := range []struct {
		name      string
		webDomain string
		domains   []string
		valid     bool
	}{
		{name: "root", webDomain: "example.com", valid: true},
		{name: "normalized root", webDomain: "Example.COM.", valid: true},
		{name: "subdomain", webDomain: "web.example.com", domains: []string{"example.com"}, valid: true},
		{name: "nested web", webDomain: "a.web.example.com"},
		{name: "foreign web", webDomain: "other.example"},
		{name: "nested mail services", webDomain: "web.example.com", domains: []string{"mail.example.com"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := &config.Config{Domain: "example.com", WebDomain: test.webDomain, Domains: test.domains, SSLType: config.SSLTypeAutoDNS}
			if err := validateWildcardConfiguration(cfg); (err == nil) != test.valid {
				t.Fatalf("证书域名覆盖校验结果不符：%v", err)
			}
		})
	}
}

func TestDNSChallengeRootAndWildcardKeepBothRecords(t *testing.T) {
	challenge := &DNSChallenge{authInfo: map[string]*authInfo{}}
	for _, item := range []struct{ domain, token, key string }{
		{"example.com", "root-token", "root-key"},
		{"*.example.com", "wildcard-token", "wildcard-key"},
	} {
		if err := challenge.Present(item.domain, item.token, item.key); err != nil {
			t.Fatal(err)
		}
	}
	records := challenge.GetDNSSettings(nil)
	if len(records) != 2 || records[0].Host != records[1].Host || records[0].Value == records[1].Value {
		t.Fatalf("根域名和泛域名必须保留同一主机名的两条独立 TXT 记录：%v", records)
	}
	if err := challenge.CleanUp("example.com", "root-token", "root-key"); err != nil {
		t.Fatal(err)
	}
	if records = challenge.GetDNSSettings(nil); len(records) != 1 {
		t.Fatal("清理根域名验证不能删除泛域名验证记录")
	}
}

func TestDNSWildcardIgnoresLegacyAdminRecoveryDomain(t *testing.T) {
	base := &config.Config{
		Domain:              "example.com",
		WebDomain:           "mail.example.com",
		Domains:             []string{"example.com"},
		SSLType:             config.SSLTypeAutoDNS,
		AdminRecoveryDomain: "recovery.example.com",
	}
	if err := validateWildcardConfiguration(base); err != nil {
		t.Fatalf("legacy recovery domain changed wildcard validation: %v", err)
	}
	for _, value := range []string{"example.com", "a.recovery.example.com", "recovery.other.example"} {
		candidate := *base
		candidate.AdminRecoveryDomain = value
		if err := validateWildcardConfiguration(&candidate); err != nil {
			t.Fatalf("legacy recovery domain %q changed wildcard validation: %v", value, err)
		}
	}
}

func TestDNSChallengeWildcardProducesSingleRecord(t *testing.T) {
	challenge := &DNSChallenge{authInfo: map[string]*authInfo{}}
	if err := challenge.Present("*.xuans.com", "token", "key-auth"); err != nil {
		t.Fatalf("Present returned an error: %v", err)
	}

	records := challenge.GetDNSSettings(nil)
	if len(records) != 1 {
		t.Fatalf("got %d DNS records, want 1", len(records))
	}
	if records[0].Type != "TXT" {
		t.Fatalf("got record type %q, want TXT", records[0].Type)
	}
	if strings.TrimSuffix(records[0].Host, ".") != "_acme-challenge.xuans.com" {
		t.Fatalf("got record host %q, want _acme-challenge.xuans.com", records[0].Host)
	}
	if records[0].Value == "" {
		t.Fatal("DNS record value must not be empty")
	}
}
