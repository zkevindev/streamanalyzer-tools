package diagnostic

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const maxCustomHeaderCount = 16

type captureAccess struct {
	headers   http.Header
	client    *http.Client
	tlsConfig *tls.Config
}

type captureAccessContextKey struct{}

func prepareCaptureAccess(base *http.Client, req SampleRequest) (captureAccess, error) {
	result := captureAccess{headers: make(http.Header), client: base}
	if len(req.Headers) > maxCustomHeaderCount {
		return result, fmt.Errorf("%w: 自定义请求头最多 %d 个", ErrInvalid, maxCustomHeaderCount)
	}
	for name, value := range req.Headers {
		name = http.CanonicalHeaderKey(strings.TrimSpace(name))
		if !validHTTPHeaderName(name) || len(value) > 2048 || !validHTTPHeaderValue(value) || restrictedCaptureHeader(name) {
			return result, fmt.Errorf("%w: 自定义请求头 %q 无效或不允许设置", ErrInvalid, name)
		}
		result.headers.Set(name, value)
	}
	proxyURL := strings.TrimSpace(req.ProxyURL)
	caCertificate := strings.TrimSpace(req.CACertificate)
	if proxyURL == "" && caCertificate == "" {
		return result, nil
	}
	client := *base
	transport := base.Transport.(*http.Transport).Clone()
	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err != nil || proxy.Hostname() == "" || proxy.Scheme != "http" && proxy.Scheme != "https" || proxy.RawQuery != "" || proxy.Fragment != "" {
			return result, fmt.Errorf("%w: 代理地址须为有效的 HTTP 或 HTTPS URL", ErrInvalid)
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	if caCertificate != "" {
		if len(caCertificate) > 16<<10 {
			return result, fmt.Errorf("%w: 自定义 CA 证书过大", ErrInvalid)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM([]byte(caCertificate)) {
			return result, fmt.Errorf("%w: 自定义 CA 证书不是有效的 PEM 证书", ErrInvalid)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		result.tlsConfig = transport.TLSClientConfig
	}
	client.Transport = transport
	result.client = &client
	return result, nil
}

func validHTTPHeaderValue(value string) bool {
	return !strings.ContainsAny(value, "\r\n\x00") && strings.ToValidUTF8(value, "") == value
}

func restrictedCaptureHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Host", "Content-Length", "Transfer-Encoding", "Trailer", "Connection", "Upgrade", "Proxy-Connection", "Proxy-Authorization", "Range":
		return true
	default:
		return false
	}
}

func withCaptureAccess(ctx context.Context, access captureAccess) context.Context {
	return context.WithValue(ctx, captureAccessContextKey{}, access)
}

func captureAccessFromContext(ctx context.Context) captureAccess {
	access, _ := ctx.Value(captureAccessContextKey{}).(captureAccess)
	return access
}

func applyCaptureHeaders(ctx context.Context, req *http.Request) {
	for name, values := range captureAccessFromContext(ctx).headers {
		req.Header[name] = append([]string(nil), values...)
	}
}

func applyPlayerCaptureHeaders(req *http.Request, headers http.Header) {
	for name, values := range headers {
		req.Header[name] = append([]string(nil), values...)
	}
}

func captureHTTPClient(ctx context.Context, fallback *http.Client) *http.Client {
	if client := captureAccessFromContext(ctx).client; client != nil {
		return client
	}
	return fallback
}

func captureTLSConfig(ctx context.Context) *tls.Config {
	return captureAccessFromContext(ctx).tlsConfig
}
