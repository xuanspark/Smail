package config

import (
	"errors"
	"strings"
	"testing"
)

type strictParserFixture struct {
	Generation uint64 `json:"generation"`
	Nested     struct {
		Enabled bool `json:"enabled"`
	} `json:"nested"`
	Values []struct {
		Port uint16 `json:"port"`
	} `json:"values"`
}

func TestParseStrictRejectsMaliciousFixtures(t *testing.T) {
	deep := strings.Repeat(`{"a":`, maxConfigDepth+2) + `1` + strings.Repeat(`}`, maxConfigDepth+2)
	oversized := make([]byte, maxConfigFileSize+1)
	for i := range oversized {
		oversized[i] = 'a'
	}
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{name: "bom", data: append([]byte{0xef, 0xbb, 0xbf}, []byte(`{"generation":1}`)...), want: ErrConfigBOM},
		{name: "invalid_utf8", data: []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}},
		{name: "nested_duplicate", data: []byte(`{"generation":1,"nested":{"enabled":true,"enabled":false}}`), want: ErrConfigDuplicate},
		{name: "case_similar", data: []byte(`{"Generation":1,"nested":{"enabled":true},"values":[]}`)},
		{name: "unknown_nested", data: []byte(`{"generation":1,"nested":{"enabled":true,"extra":1},"values":[]}`)},
		{name: "overflow", data: []byte(`{"generation":1,"nested":{"enabled":true},"values":[{"port":65536}]}`)},
		{name: "multiple_values", data: []byte(`{"generation":1}[]`), want: ErrConfigTrailing},
		{name: "control_character", data: []byte("{\"generation\":1,\"nested\":{\"enabled\":true},\"values\":[],\"x\":\"\x01\"}")},
		{name: "depth", data: []byte(deep), want: ErrConfigDepth},
		{name: "size", data: oversized, want: ErrConfigTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var target strictParserFixture
			err := ParseStrict(test.data, &target)
			if err == nil {
				t.Fatal("malicious fixture was accepted")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseStrictAcceptsExactNestedFields(t *testing.T) {
	var target strictParserFixture
	data := []byte(`{"generation":7,"nested":{"enabled":true},"values":[{"port":443}]}`)
	if err := ParseStrict(data, &target); err != nil {
		t.Fatal(err)
	}
	if target.Generation != 7 || !target.Nested.Enabled || len(target.Values) != 1 || target.Values[0].Port != 443 {
		t.Fatalf("target = %+v", target)
	}
}

func TestParseStrictDoesNotLeakSecretOrAbsolutePath(t *testing.T) {
	secret := "super-secret-value"
	absolutePath := `C:\private\smail\config.json`
	tests := [][]byte{
		[]byte(`{"password":"` + secret + `"}`),
		[]byte(`{"absolutePath":"` + absolutePath + `"}`),
	}
	for _, data := range tests {
		var target strictParserFixture
		err := ParseStrict(data, &target)
		if err == nil {
			t.Fatal("unsafe configuration was accepted")
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), absolutePath) {
			t.Fatalf("error leaked sensitive value: %v", err)
		}
	}
}
