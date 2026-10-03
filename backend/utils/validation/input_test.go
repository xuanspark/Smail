package validation

import (
	"strings"
	"testing"
)

func TestNormalizeAccount(t *testing.T) {
	for _, value := range []string{"user", "user.name", "user_name", "user-name", "User123"} {
		if normalized, err := NormalizeAccount(value); err != nil || normalized != value {
			t.Fatalf("valid account rejected: %q %v", value, err)
		}
	}
	for _, value := range []string{"", ".user", "user-", "user@example.com", "user name", strings.Repeat("a", MaxAccountBytes+1)} {
		if _, err := NormalizeAccount(value); err == nil {
			t.Fatalf("invalid account accepted: %q", value)
		}
	}
}

func TestNormalizeDisplayName(t *testing.T) {
	for _, value := range []string{strings.Repeat("名", MaxNameRunes), strings.Repeat("😀", MaxNameRunes)} {
		if normalized, err := NormalizeDisplayName(" " + value + " "); err != nil || normalized != value {
			t.Fatalf("valid Unicode name rejected: %q %v", value, err)
		}
		if _, err := NormalizeDisplayName(value + "名"); err == nil {
			t.Fatal("oversized Unicode name accepted")
		}
	}
	if value, err := NormalizeDisplayName(" Test User "); err != nil || value != "Test User" {
		t.Fatalf("valid display name rejected: %q %v", value, err)
	}
	for _, value := range []string{"", "bad\nname", strings.Repeat("a", MaxNameRunes+1), string([]byte{0xff})} {
		if _, err := NormalizeDisplayName(value); err == nil {
			t.Fatalf("invalid display name accepted: %q", value)
		}
	}
}

func TestNormalizeDomain(t *testing.T) {
	for input, expected := range map[string]string{
		"Mail.Example.COM.": "mail.example.com",
		"mx-1.example.com":  "mx-1.example.com",
	} {
		if value, err := NormalizeDomain(input); err != nil || value != expected {
			t.Fatalf("valid domain rejected: %q %q %v", input, value, err)
		}
	}
	for _, value := range []string{"", "https://example.com", "example.com:443", "user@example.com", "127.0.0.1", "-bad.example", "bad-.example", "bad_name.example", strings.Repeat("a", 64) + ".example"} {
		if _, err := NormalizeDomain(value); err == nil {
			t.Fatalf("invalid domain accepted: %q", value)
		}
	}
}

func TestNormalizeDomainListDeduplicates(t *testing.T) {
	values, err := NormalizeDomainList("a.example.com, A.EXAMPLE.COM ,b.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0] != "a.example.com" || values[1] != "b.example.com" {
		t.Fatalf("unexpected normalized domain list: %#v", values)
	}
	if _, err := NormalizeDomainList(strings.Repeat("a.example.com,", MaxDomainCount) + "b.example.com"); err == nil {
		t.Fatal("oversized domain list was accepted")
	}
}
