package analyze

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"time"

	"streamanalyzer/internal/hlsdelivery/m3u8"
)

const (
	maxPlaylist = 8 << 20
	maxSegment  = 80 << 20
	hlsAccept   = "application/vnd.apple.mpegurl, application/x-mpegURL, application/mpegurl, */*"
)

type fetched struct {
	URL    string
	Status int
	Header http.Header
	Body   []byte
	TTFB   time.Duration
	Total  time.Duration
	IP     string
	Addr   string
	Err    error
	Kind   string
}

type runner struct {
	http    *http.Client
	opt     Options
	headers http.Header
}

func newRunner(opt Options) *runner {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 0
	if opt.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	dialer := &net.Dialer{Timeout: opt.Timeout}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialChecked(ctx, dialer, network, addr)
	}
	client := &http.Client{Transport: tr}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("重定向次数过多")
		}
		if err := ValidateURL(req.URL.String()); err != nil {
			return err
		}
		if req.URL.Host != via[0].URL.Host {
			for key := range opt.Headers {
				req.Header.Del(key)
			}
			req.Header.Del("Authorization")
			req.Header.Del("Cookie")
		}
		return nil
	}
	return &runner{
		http:    client,
		opt:     opt,
		headers: opt.Headers,
	}
}

// ValidateURL 只允许 http(s)，并拒绝链路本地、未指定和组播地址。
// 回环和局域网地址保留，本工具要分析本机和内网的播放列表。
func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return errors.New("地址不合法")
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return errors.New("只允许 http 和 https")
	}
	return checkHost(u.Hostname())
}

func checkHost(host string) error {
	ips, err := hostIPs(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if blockedFetchIP(ip) {
			return errors.New("不允许访问该地址")
		}
	}
	return nil
}

func hostIPs(host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, errors.New("无法解析地址")
	}
	if len(ips) == 0 {
		return nil, errors.New("无法解析地址")
	}
	return ips, nil
}

func blockedFetchIP(ip net.IP) bool {
	return ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

func dialChecked(ctx context.Context, dialer *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if err := checkHost(host); err != nil {
		return nil, err
	}
	ips, err := hostIPs(host)
	if err != nil {
		return nil, err
	}
	var last error
	for _, ip := range ips {
		if blockedFetchIP(ip) {
			last = errors.New("不允许访问该地址")
			continue
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("不允许访问该地址")
	}
	return nil, last
}

func (r *runner) get(ctx context.Context, rawURL string, limit int64, accept string, byteRange *m3u8.ByteRange) *fetched {
	if err := ValidateURL(rawURL); err != nil {
		return &fetched{URL: rawURL, Err: err, Kind: "other"}
	}
	ctx, cancel := context.WithTimeout(ctx, r.opt.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return &fetched{URL: rawURL, Err: err, Kind: "other"}
	}
	req.Header.Set("User-Agent", "StreamAnalyzer-HLSStats/"+Version)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	for k, vv := range r.headers {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	if byteRange != nil {
		end := byteRange.Offset + byteRange.Length - 1
		if byteRange.Offset < 0 || byteRange.Length <= 0 || end < byteRange.Offset {
			return &fetched{URL: rawURL, Err: errors.New("无效的字节范围"), Kind: "other"}
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", byteRange.Offset, end))
	}

	var ip, addr string
	var ttfb time.Duration
	start := time.Now()
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			if info.Conn == nil {
				return
			}
			addr = info.Conn.RemoteAddr().String()
			host, _, splitErr := net.SplitHostPort(addr)
			if splitErr != nil {
				ip = addr
				return
			}
			ip = host
		},
		GotFirstResponseByte: func() {
			ttfb = time.Since(start)
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	resp, err := r.http.Do(req)
	total := time.Since(start)
	if err != nil {
		kind := "other"
		if isTimeout(err) {
			kind = "timeout"
		}
		return &fetched{URL: rawURL, TTFB: ttfb, Total: total, IP: ip, Addr: addr, Err: err, Kind: kind}
	}
	defer resp.Body.Close()
	if byteRange != nil && resp.StatusCode < 400 {
		if err := validateRangeResponse(resp, byteRange); err != nil {
			return &fetched{URL: resp.Request.URL.String(), Status: resp.StatusCode, Header: resp.Header.Clone(),
				TTFB: ttfb, Total: time.Since(start), IP: ip, Addr: addr, Err: err, Kind: "other"}
		}
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	total = time.Since(start)
	if ttfb == 0 {
		ttfb = total
	}
	f := &fetched{
		URL:    resp.Request.URL.String(),
		Status: resp.StatusCode,
		Header: resp.Header.Clone(),
		Body:   body,
		TTFB:   ttfb,
		Total:  total,
		IP:     ip,
		Addr:   addr,
	}
	switch {
	case int64(len(body)) > limit:
		f.Body = body[:limit]
		f.Err = fmt.Errorf("响应超过 %d 字节", limit)
		f.Kind = "other"
	case readErr != nil:
		f.Err = readErr
		if errors.Is(readErr, io.ErrUnexpectedEOF) || errors.Is(readErr, io.EOF) {
			f.Kind = "eof"
		} else if isTimeout(readErr) {
			f.Kind = "timeout"
		} else {
			f.Kind = "other"
		}
	case resp.StatusCode >= 400:
		f.Err = fmt.Errorf("HTTP %d", resp.StatusCode)
		f.Kind = "http"
	case byteRange != nil && len(body) != byteRange.Length:
		f.Err = fmt.Errorf("字节范围响应长度为 %d，预期 %d", len(body), byteRange.Length)
		f.Kind = "eof"
	}
	return f
}

func validateRangeResponse(resp *http.Response, br *m3u8.ByteRange) error {
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("服务器未返回请求的字节范围: HTTP %d", resp.StatusCode)
	}
	end := br.Offset + br.Length - 1
	prefix := fmt.Sprintf("bytes %d-%d/", br.Offset, end)
	contentRange := resp.Header.Get("Content-Range")
	if !strings.HasPrefix(contentRange, prefix) {
		return fmt.Errorf("Content-Range 与请求的字节范围不匹配: %q", contentRange)
	}
	total := strings.TrimPrefix(contentRange, prefix)
	if total != "*" {
		size, err := strconv.ParseInt(total, 10, 64)
		if err != nil || size <= int64(end) {
			return fmt.Errorf("无效的 Content-Range: %q", contentRange)
		}
	}
	return nil
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
