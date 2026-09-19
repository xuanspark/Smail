package config

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/utils/credential"
)

func TestDNSCredentialsEncryptedPreservedAndRedacted(t *testing.T) {
	root := t.TempDir()
	previousRoot := ROOT_PATH
	ROOT_PATH = root
	t.Cleanup(func() { ROOT_PATH = previousRoot })
	vault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	before := validRuntimeConfig()
	candidate := cloneRuntimeConfig(before)
	candidate.SSL.TLS.DNSProvider = DNSProviderAliDNS
	changes := map[string]SecretChange{
		"tls.dnsAccessKey": {Operation: SecretReplace, Value: &SecretInput{Plaintext: "dns-access-fixture"}},
		"tls.dnsSecretKey": {Operation: SecretReplace, Value: &SecretInput{Plaintext: "dns-secret-fixture"}},
	}
	if err := applySecretChanges(&candidate, SectionSSL, changes, vault); err != nil {
		t.Fatal(err)
	}
	if err := validateDNSProvider(candidate.SSL.TLS); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRuntimeSecrets(vault, candidate); err != nil {
		t.Fatal(err)
	}
	access, secret, err := DNSProviderCredentials(candidate.SSL.TLS)
	if err != nil || access != "dns-access-fixture" || secret != "dns-secret-fixture" {
		t.Fatal("DNS credentials did not round trip through vault")
	}
	data, _ := json.Marshal(candidate)
	for _, value := range []string{access, secret} {
		if strings.Contains(string(data), value) {
			t.Fatal("plaintext persisted")
		}
	}
	audit, _ := json.Marshal(AuditChanges(before, candidate))
	public, _ := json.Marshal(RedactPublicValue(candidate.SSL))
	for _, data := range [][]byte{audit, public} {
		for _, value := range []string{access, secret, candidate.SSL.TLS.DNSAccessKey.Ciphertext, candidate.SSL.TLS.DNSSecretKey.Ciphertext} {
			if strings.Contains(string(data), value) {
				t.Fatal("DNS secret leaked into audit or public view")
			}
		}
	}
	section := candidate.SSL
	section.TLS.DNSAccessKey, section.TLS.DNSSecretKey = StoredSecret{}, StoredSecret{}
	payload, _ := json.Marshal(section)
	kept, err := applySectionPayload(candidate, SectionSSL, payload, nil, vault)
	if err != nil || kept.SSL.TLS.DNSAccessKey != candidate.SSL.TLS.DNSAccessKey || kept.SSL.TLS.DNSSecretKey != candidate.SSL.TLS.DNSSecretKey {
		t.Fatal("public edit lost stored DNS credentials")
	}
	payload, _ = json.Marshal(candidate.SSL)
	if _, err := applySectionPayload(candidate, SectionSSL, payload, nil, vault); !errors.Is(err, ErrInvalidSecretChange) {
		t.Fatal("direct stored-secret injection accepted")
	}
	clear := map[string]SecretChange{"tls.dnsSecretKey": {Operation: SecretClear, Confirmed: true}}
	if err := applySecretChanges(&candidate, SectionSSL, clear, vault); !errors.Is(err, ErrSecretClearDenied) {
		t.Fatal("enabled DNS provider allowed clearing its secret")
	}
	candidate.SSL.TLS.DNSProvider = ""
	if err := applySecretChanges(&candidate, SectionSSL, clear, vault); err != nil {
		t.Fatal(err)
	}
}
