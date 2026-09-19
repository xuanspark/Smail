package ssl

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
)

type aliDNSRecord struct {
	ID    string
	Name  string
	Value string
}

// 每次签发独立记录创建结果，清理时只删除本实例创建且内容未被修改的记录。
type aliDNSChallenge struct {
	mu        sync.Mutex
	zone      string
	accessKey string
	secretKey string
	endpoint  string
	client    *http.Client
	records   map[string]aliDNSRecord
}

func automaticDNSProvider(cfg *config.Config) (challenge.Provider, error) {
	if cfg == nil || cfg.EffectiveSSLType() != config.SSLTypeAutoDNS {
		return nil, newUserCertificateError("当前配置未启用项目 DNS 证书")
	}
	if cfg.SSL.DNSProvider == "" {
		return GetDnsChallengeInstance(), nil
	}
	accessKey, secretKey, err := config.DNSProviderCredentials(cfg.SSL)
	if err != nil {
		return nil, newUserCertificateError("DNS 服务商凭据不可用，请在 SSL 配置中重新设置")
	}
	zone := cfg.SSL.DNSZone
	if zone == "" {
		zone = strings.ToLower(strings.TrimSuffix(cfg.Domain, "."))
	}
	if cfg.Domain != zone && !strings.HasSuffix(cfg.Domain, "."+zone) {
		return nil, newUserCertificateError("DNS 托管区域必须包含邮件主域名")
	}
	return &aliDNSChallenge{
		zone: zone, accessKey: accessKey, secretKey: secretKey,
		endpoint: "https://alidns.aliyuncs.com/",
		client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		records: make(map[string]aliDNSRecord),
	}, nil
}

func (provider *aliDNSChallenge) Timeout() (time.Duration, time.Duration) {
	return 5 * time.Minute, 5 * time.Second
}

func (provider *aliDNSChallenge) record(domain, keyAuth string) (aliDNSRecord, error) {
	domain = strings.TrimPrefix(strings.TrimSuffix(strings.ToLower(domain), "."), "*.")
	info := dns01.GetChallengeInfo(domain, keyAuth)
	fqdn := strings.TrimSuffix(info.EffectiveFQDN, ".")
	if !strings.HasSuffix(fqdn, "."+provider.zone) {
		return aliDNSRecord{}, errors.New("DNS 挑战不属于配置的托管区域")
	}
	name := strings.TrimSuffix(fqdn, "."+provider.zone)
	if name != "_acme-challenge" && !strings.HasPrefix(name, "_acme-challenge.") {
		return aliDNSRecord{}, errors.New("DNS 挑战目标不是允许的 ACME 记录")
	}
	return aliDNSRecord{Name: name, Value: info.Value}, nil
}

func (provider *aliDNSChallenge) Present(domain, token, keyAuth string) error {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	key := domain + ":" + keyAuth
	if _, exists := provider.records[key]; exists {
		return nil
	}
	record, err := provider.record(domain, keyAuth)
	if err != nil {
		return err
	}
	var response struct{ RecordID string `json:"RecordId"` }
	err = provider.call("AddDomainRecord", url.Values{
		"DomainName": {provider.zone}, "RR": {record.Name}, "Type": {"TXT"},
		"Value": {record.Value}, "TTL": {"600"},
	}, &response)
	if err != nil {
		return err
	}
	if response.RecordID == "" {
		return errors.New("DNS 服务商未返回新建记录编号")
	}
	record.ID = response.RecordID
	provider.records[key] = record
	return nil
}

func (provider *aliDNSChallenge) CleanUp(domain, token, keyAuth string) error {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	// 清理使用创建时保存的目标，避免 CNAME 变化后误删或遗漏原记录。
	key := domain + ":" + keyAuth
	created, exists := provider.records[key]
	if !exists {
		return nil
	}
	var current struct {
		RecordID string `json:"RecordId"`
		Domain   string `json:"DomainName"`
		Name     string `json:"RR"`
		Type     string `json:"Type"`
		Value    string `json:"Value"`
	}
	if err := provider.call("DescribeDomainRecordInfo", url.Values{"RecordId": {created.ID}}, &current); err != nil {
		return err
	}
	if current.RecordID != created.ID || current.Domain != provider.zone || current.Name != created.Name || current.Type != "TXT" || current.Value != created.Value {
		return errors.New("DNS 挑战记录已被修改，保留记录并停止自动清理")
	}
	if err := provider.call("DeleteDomainRecord", url.Values{"RecordId": {created.ID}}, nil); err != nil {
		return err
	}
	delete(provider.records, key)
	return nil
}

func aliDNSPercentEncode(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func (provider *aliDNSChallenge) call(action string, values url.Values, target any) error {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return errors.New("DNS 请求随机数生成失败")
	}
	values.Set("Action", action)
	values.Set("Version", "2015-01-09")
	values.Set("Format", "JSON")
	values.Set("AccessKeyId", provider.accessKey)
	values.Set("SignatureMethod", "HMAC-SHA1")
	values.Set("SignatureVersion", "1.0")
	values.Set("SignatureNonce", hex.EncodeToString(nonce))
	values.Set("Timestamp", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	canonical := strings.ReplaceAll(values.Encode(), "+", "%20")
	mac := hmac.New(sha1.New, []byte(provider.secretKey+"&"))
	_, _ = mac.Write([]byte("POST&%2F&" + aliDNSPercentEncode(canonical)))
	values.Set("Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	request, err := http.NewRequest(http.MethodPost, provider.endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return errors.New("DNS 请求创建失败")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := provider.client.Do(request)
	if err != nil {
		return errors.New("DNS 服务商连接失败或超时")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("DNS 服务商响应无效")
	}
	var status struct{ Code string `json:"Code"` }
	if json.Unmarshal(data, &status) != nil || response.StatusCode != http.StatusOK || status.Code != "" {
		// 服务商错误可能包含凭据、签名或请求原文，不能直接交给日志。
		return errors.New("DNS 服务商拒绝请求，请检查凭据、区域和记录管理权限")
	}
	if target != nil && json.Unmarshal(data, target) != nil {
		return errors.New("DNS 服务商响应解析失败")
	}
	return nil
}
