package onvifdiscovery

import (
	"context"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func Interfaces() ([]Interface, error) {
	nics, err := net.Interfaces()
	if err != nil {
		return nil, &Error{Code: "interface_error", Message: "无法读取本机网卡"}
	}
	result := []Interface{}
	for _, nic := range nics {
		if nic.Flags&net.FlagUp == 0 || nic.Flags&(net.FlagMulticast|net.FlagLoopback) == 0 {
			continue
		}
		addresses, err := nic.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, prefix, err := net.ParseCIDR(address.String())
			if err != nil || ip.To4() == nil || !unicast(ip) {
				continue
			}
			result = append(result, Interface{Name: nic.Name, Index: nic.Index, IP: ip.String(), Prefix: prefix.String(), Loopback: ip.IsLoopback()})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Loopback != result[j].Loopback {
			return !result[i].Loopback
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].IP < result[j].IP
	})
	return result, nil
}

func bindInterface(value string) (Interface, error) {
	choices, err := Interfaces()
	if err != nil {
		return Interface{}, err
	}
	for _, nic := range choices {
		if value == nic.IP {
			return nic, nil
		}
	}
	return Interface{}, invalid("请选择本机有效的 IPv4 网卡地址")
}

func unicast(ip net.IP) bool {
	return ip != nil && ip.To4() != nil && (ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}

func serviceURL(value string) (*url.URL, error) {
	if len(value) == 0 || len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\x00") {
		return nil, invalid("ONVIF 服务地址无效")
	}
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" {
		return nil, invalid("ONVIF 服务地址须使用 HTTP 或 HTTPS，账号密码请在独立字段填写")
	}
	if u.Port() != "" {
		if _, err := net.LookupPort("tcp", u.Port()); err != nil {
			return nil, invalid("ONVIF 服务端口无效")
		}
	}
	return u, nil
}

type target struct {
	url  *url.URL
	peer net.IP
	bind Interface
}

func resolveTarget(ctx context.Context, raw string, nic Interface, expected net.IP) (target, error) {
	u, err := serviceURL(raw)
	if err != nil {
		return target{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var addresses []net.IP
	if literal := net.ParseIP(u.Hostname()); literal != nil {
		addresses = []net.IP{literal}
	} else {
		resolved, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
		if err != nil {
			return target{}, &Error{Code: "dns_error", Message: "无法解析 ONVIF 设备主机名"}
		}
		for _, address := range resolved {
			addresses = append(addresses, address.IP)
		}
	}
	for _, ip := range addresses {
		if !unicast(ip) || expected != nil && !ip.Equal(expected) {
			continue
		}
		return target{url: u, peer: ip.To4(), bind: nic}, nil
	}
	return target{}, &Error{Code: "endpoint_scope", Message: "服务地址未指向所选设备的 IPv4 地址，未向其他主机发送凭据"}
}

func mediaTarget(ctx context.Context, raw string, original target) (target, error) {
	if strings.HasPrefix(raw, "/") {
		relative, err := url.Parse(raw)
		if err != nil {
			return target{}, invalid("设备媒体服务地址无效")
		}
		raw = original.url.ResolveReference(relative).String()
	}
	value, err := resolveTarget(ctx, raw, original.bind, original.peer)
	if err != nil {
		return target{}, err
	}
	if original.url.Scheme == "https" && value.url.Scheme != "https" {
		return target{}, &Error{Code: "endpoint_scope", Message: "设备返回了不安全的 HTTP 降级地址，未继续发送凭据"}
	}
	return value, nil
}
