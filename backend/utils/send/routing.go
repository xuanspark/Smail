package send

import (
	stdcontext "context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"sort"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/utils/smtp"
)

var errNullMX = errors.New("收件域名声明不接收邮件")

type mxLookup func(stdcontext.Context, string) ([]*net.MX, error)

func resolveMX(domain string, lookup mxLookup) ([]string, error) {
	return resolveMXContext(stdcontext.Background(), domain, lookup)
}

func resolveMXContext(parent stdcontext.Context, domain string, lookup mxLookup) ([]string, error) {
	ctx, cancel := stdcontext.WithTimeout(parent, 10*time.Second)
	defer cancel()
	records, err := lookup(ctx, domain)
	if err != nil {
		var dnsErr *net.DNSError
		if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound || dnsErr.IsTemporary || dnsErr.IsTimeout {
			return nil, fmt.Errorf("查询收件域名 MX 失败: %w", err)
		}
		// 无 MX 时使用域名自身的地址记录，不猜测 smtp 子域名。
		return []string{domain}, nil
	}
	if len(records) == 0 {
		return []string{domain}, nil
	}
	records = append([]*net.MX(nil), records...)
	for _, record := range records {
		if record == nil || record.Host == "" {
			return nil, errors.New("收件域名 MX 记录无效")
		}
		if record.Host == "." {
			return nil, errNullMX
		}
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].Pref < records[j].Pref })
	hosts := make([]string, 0, len(records))
	seen := make(map[string]bool)
	for _, record := range records {
		host := strings.ToLower(strings.TrimSuffix(record.Host, "."))
		if !seen[host] {
			hosts = append(hosts, host)
			seen[host] = true
		}
	}
	return hosts, nil
}

func stopFallback(err error) bool {
	return smtp.IsDeliveryUncertain(err) || isCertificateVerificationError(err) || isPermanentSMTPResponse(err) || errors.Is(err, smtp.ErrUnsafeRemoteAddress)
}

// deliverMX 只在确认本次未接收时切换安全通道，DATA 不确定结果不进入下一条路由。
func deliverMX(hosts []string, attempt func(host, port string) error) error {
	return deliverMXContext(stdcontext.Background(), hosts, attempt)
}

func deliverMXContext(ctx stdcontext.Context, hosts []string, attempt func(host, port string) error) error {
	var lastErr error
	for _, host := range hosts {
		// 标准投递端口不可达时优先尝试隐式 TLS，避免先等待不提供欢迎响应的提交端口。
		for _, port := range []string{"25", "465", "587"} {
			if err := ctx.Err(); err != nil {
				return err
			}
			lastErr = attempt(host, port)
			if lastErr == nil || stopFallback(lastErr) {
				return lastErr
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("收件域名没有可用邮件服务器")
	}
	return lastErr
}

// Retryable 仅允许有明确临时失败语义的结果进入持久队列重试。
func Retryable(err error) bool {
	if err == nil || stopFallback(err) {
		return false
	}
	var response *textproto.Error
	if errors.As(err, &response) {
		return response.Code >= 400 && response.Code <= 499
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsTimeout || dnsErr.IsTemporary
	}
	var networkErr net.Error
	return errors.As(err, &networkErr)
}
