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
			want:   []string{"*.xuans.com"},
		},
		{
			name:   "trimmed wildcard domain",
			config: &config.Config{Domain: "  *.xuans.com.  "},
			want:   []string{"*.xuans.com"},
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
