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
	return &runner{
		http:    &http.Client{Transport: tr},
		opt:     opt,
		headers: opt.Headers,
	}
}

func (r *runner) get(ctx context.Context, rawURL string, limit int64, accept string, byteRange *m3u8.ByteRange) *fetched {
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
		if end < byteRange.Offset {
			end = byteRange.Offset
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
	}
	return f
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
