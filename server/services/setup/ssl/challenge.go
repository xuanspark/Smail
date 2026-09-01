package ssl

import (
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/go-acme/lego/v4/challenge/dns01"
	log "github.com/sirupsen/logrus"
	"strings"
	"sync"
	"time"
)

type authInfo struct {
	Domain  string
	Token   string
	KeyAuth string
}

type HttpChallenge struct {
	mu       sync.RWMutex
	authInfo map[string]*authInfo
}

var instance = &HttpChallenge{authInfo: map[string]*authInfo{}}

func (h *HttpChallenge) Present(domain, token, keyAuth string) error {
	h.mu.Lock()
	h.authInfo[token] = &authInfo{
		Domain:  domain,
		Token:   token,
		KeyAuth: keyAuth,
	}
	h.mu.Unlock()

	return nil
}

func (h *HttpChallenge) CleanUp(domain, token, keyAuth string) error {
	h.mu.Lock()
	delete(h.authInfo, token)
	h.mu.Unlock()
	return nil
}

func GetHttpChallengeInstance() *HttpChallenge {
	return instance
}

func (h *HttpChallenge) KeyAuthorization(token string) (string, bool) {
	h.mu.RLock()
	info, ok := h.authInfo[token]
	h.mu.RUnlock()
	if !ok || info == nil {
		return "", false
	}
	return info.KeyAuth, true
}

type DNSChallenge struct {
	mu       sync.RWMutex
	authInfo map[string]*authInfo
}

var dnsInstance = &DNSChallenge{authInfo: map[string]*authInfo{}}

func GetDnsChallengeInstance() *DNSChallenge {
	return dnsInstance
}

func (h *DNSChallenge) Present(domain, token, keyAuth string) error {
	challengeDomain := strings.TrimSpace(domain)
	challengeDomain = strings.TrimSuffix(challengeDomain, ".")
	challengeDomain = strings.TrimPrefix(challengeDomain, "*.")
	info := dns01.GetChallengeInfo(challengeDomain, keyAuth)
	log.Infof("SSL DNS challenge generated for domain=%s", domain)
	h.mu.Lock()
	h.authInfo[token] = &authInfo{
		Domain:  info.FQDN,
		Token:   token,
		KeyAuth: info.Value,
	}
	h.mu.Unlock()
	log.Info("SSL challenge value generated")
	return nil
}

func (h *DNSChallenge) CleanUp(domain, token, keyAuth string) error {
	h.mu.Lock()
	delete(h.authInfo, token)
	h.mu.Unlock()
	return nil
}

func (h *DNSChallenge) Timeout() (timeout, interval time.Duration) {
	return 60 * time.Minute, 5 * time.Second
}

type DNSItem struct {
	Type  string `json:"type"`
	Host  string `json:"host"`
	Value string `json:"value"`
	TTL   int    `json:"ttl"`
	Tips  string `json:"tips"`
}

func (h *DNSChallenge) GetDNSSettings(ctx *context.Context) []*DNSItem {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ret := []*DNSItem{}
	for _, info := range h.authInfo {
		ret = append(ret, &DNSItem{
			Type:  "TXT",
			Host:  info.Domain,
			Value: info.KeyAuth,
			TTL:   3600,
		})
	}

	return ret
}
