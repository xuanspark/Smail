package clientip

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Resolve 仅信任显式配置的代理，并从右向左寻找转发链中的首个非可信地址。
// 原始 RemoteAddr 保持不变，初始化和本机访问控制仍应检查真实连接来源。
func Resolve(request *http.Request, trustedCIDRs []string) string {
	if request == nil {
		return ""
	}
	peer := normalizePeer(request.RemoteAddr)
	address, err := netip.ParseAddr(peer)
	if err != nil || len(trustedCIDRs) == 0 {
		return peer
	}
	networks := make([]netip.Prefix, 0, len(trustedCIDRs))
	for _, value := range trustedCIDRs {
		network, err := netip.ParsePrefix(value)
		if err != nil {
			return peer
		}
		networks = append(networks, network)
	}
	trusted := func(address netip.Addr) bool {
		for _, network := range networks {
			if network.Contains(address.Unmap()) {
				return true
			}
		}
		return false
	}
	if !trusted(address) {
		return peer
	}
	values := request.Header.Values("X-Forwarded-For")
	if len(values) != 1 || len(values[0]) > 4096 {
		return peer
	}
	parts := strings.Split(values[0], ",")
	if len(parts) > 32 {
		return peer
	}
	chain := make([]netip.Addr, 0, len(parts))
	for _, value := range parts {
		address, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || address.Zone() != "" {
			return peer
		}
		address = address.Unmap()
		if address.IsUnspecified() || address.IsMulticast() {
			return peer
		}
		chain = append(chain, address)
	}
	for index := len(chain) - 1; index >= 0; index-- {
		if !trusted(chain[index]) || index == 0 {
			return chain[index].String()
		}
	}
	return peer
}

func normalizePeer(value string) string {
	value = strings.TrimSpace(value)
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Unmap().String()
	}
	return value
}
