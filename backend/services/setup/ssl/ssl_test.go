package ssl

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
)

func TestCheckSSLCrtInfo(t *testing.T) {
	if err := config.Init(); err != nil {
		t.Fatal(err)
	}

	got, got1, match, err := CheckSSLCrtInfo()

	fmt.Println(got, got1, match, err)
}

func TestCertificateMatchesConfiguredDomainsUsesHostnameVerification(t *testing.T) {
	cert := createCertificateForDomains(t, []string{"example.com", "*.example.com"})
	parsed, err := x509.ParseCertificate(cert)
	if err != nil {
		t.Fatal(err)
	}
	if !certificateMatchesConfiguredDomains(parsed, &config.Config{
		Domain:    "example.com",
		WebDomain: "web.example.com",
		Domains:   []string{"example.com"},
	}) {
		t.Fatal("wildcard certificate did not match configured service domains")
	}
	if certificateMatchesConfiguredDomains(parsed, &config.Config{
		Domain:    "example.com",
		WebDomain: "web.other.example.com",
		Domains:   []string{"example.com"},
	}) {
		t.Fatal("certificate unexpectedly matched a domain outside its wildcard")
	}
}

func createCertificateForDomains(t *testing.T, domains []string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: domains[0]},
		DNSNames:              domains,
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}
