package ssl

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type dnsSignatureTransport struct {
	t      *testing.T
	nonces map[string]bool
}

// 独立按字节编码，避免验证器与生产编码函数共享同一种错误。
func referenceDNSEncode(value string) string {
	var result strings.Builder
	for _, value := range []byte(value) {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || strings.ContainsRune("-_.~", rune(value)) {
			result.WriteByte(value)
		} else {
			fmt.Fprintf(&result, "%%%02X", value)
		}
	}
	return result.String()
}

func referenceDNSSignature(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "Signature" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		for _, value := range values[key] {
			parts = append(parts, referenceDNSEncode(key)+"="+referenceDNSEncode(value))
		}
	}
	mac := hmac.New(sha1.New, []byte("test-secret&"))
	_, _ = mac.Write([]byte("POST&%2F&" + referenceDNSEncode(strings.Join(parts, "&"))))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (transport *dnsSignatureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t := transport.t
	if request.Method != http.MethodPost || request.URL.Scheme != "https" || request.URL.Host != "alidns.aliyuncs.com" || request.URL.RawQuery != "" {
		t.Error("DNS 签名请求没有使用预期的地址和方法")
	}
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	if values.Get("Signature") != referenceDNSSignature(values) {
		t.Error("DNS 请求签名不匹配，可能包含上一次的签名")
	}
	for key, expected := range map[string]string{
		"Action": "AddDomainRecord", "Version": "2015-01-09", "Format": "JSON",
		"AccessKeyId": "test-access", "SignatureMethod": "HMAC-SHA1", "SignatureVersion": "1.0",
		"Value": "中文 a b+*/~=&%", "DomainName": "example.com",
	} {
		if values.Get(key) != expected {
			t.Errorf("DNS 请求字段不符合预期：%s", key)
		}
	}
	nonce := values.Get("SignatureNonce")
	decoded, err := hex.DecodeString(nonce)
	if err != nil || len(decoded) != 16 || transport.nonces[nonce] {
		t.Error("DNS 请求随机数无效或被重复使用")
	}
	transport.nonces[nonce] = true
	timestamp, err := time.Parse("2006-01-02T15:04:05Z", values.Get("Timestamp"))
	if err != nil || time.Since(timestamp) > time.Minute || time.Until(timestamp) > time.Minute {
		t.Error("DNS 请求时间戳无效")
	}
	if strings.Contains(string(body), "test-secret") || request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Error("DNS 请求泄露测试密钥或使用了错误的内容类型")
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"RecordId":"created"}`)), Header: make(http.Header)}, nil
}

func TestAutomaticDNSSigningPreservesReusableParameters(t *testing.T) {
	for _, name := range []string{"fresh", "stale"} {
		t.Run(name, func(t *testing.T) {
			transport := &dnsSignatureTransport{t: t, nonces: make(map[string]bool)}
			provider := &aliDNSChallenge{
				accessKey: "test-access", secretKey: "test-secret", endpoint: "https://alidns.aliyuncs.com/",
				client: &http.Client{Transport: transport},
			}
			values := url.Values{"DomainName": {"example.com"}, "Value": {"中文 a b+*/~=&%"}}
			expected := url.Values{"DomainName": {"example.com"}, "Value": {"中文 a b+*/~=&%"}}
			if name == "stale" {
				values.Set("Signature", "stale-signature")
				expected.Set("Signature", "stale-signature")
			}
			for i := 0; i < 3; i++ {
				var result struct {
					RecordID string `json:"RecordId"`
				}
				if err := provider.call("AddDomainRecord", values, &result); err != nil || result.RecordID != "created" {
					t.Fatal("DNS 签名请求未正确返回")
				}
				if !reflect.DeepEqual(values, expected) {
					t.Error("DNS 请求改写了调用者参数，重试会带入之前的签名")
				}
			}
		})
	}
}

func TestAutomaticDNSSignatureReferenceVector(t *testing.T) {
	// 固定向量由独立 Python 标准库计算；只是测试样本，不是真实云凭据。
	values := url.Values{
		"Action": {"AddDomainRecord"}, "Version": {"2015-01-09"}, "Format": {"JSON"},
		"AccessKeyId": {"test-access"}, "SignatureMethod": {"HMAC-SHA1"}, "SignatureVersion": {"1.0"},
		"SignatureNonce": {"00000000000000000000000000000000"}, "Timestamp": {"2026-09-21T00:00:00Z"},
		"DomainName": {"example.com"}, "RR": {"_acme-challenge"}, "Type": {"TXT"}, "Value": {"a b+*/~=&%"},
	}
	if referenceDNSSignature(values) != "s+e5Vs2kLbIFh5nFBykkdS2IrFU=" {
		t.Fatal("独立签名验证器不符合固定向量")
	}
}
