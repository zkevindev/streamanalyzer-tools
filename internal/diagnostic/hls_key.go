package diagnostic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type hlsKeyFetchContextKey struct{}

func withHLSKeyFetch(ctx context.Context) context.Context {
	return context.WithValue(ctx, hlsKeyFetchContextKey{}, true)
}

func hlsKeyFetchAllowed(ctx context.Context) bool {
	allowed, _ := ctx.Value(hlsKeyFetchContextKey{}).(bool)
	return allowed
}

func resolveHLSKeyURL(origin, raw string) (*url.URL, error) {
	reference, err := url.Parse(raw)
	if err != nil || reference.Scheme != "" && reference.Scheme != "http" && reference.Scheme != "https" {
		return nil, fmt.Errorf("密钥地址无效")
	}
	if reference.IsAbs() {
		return reference, nil
	}
	if !strings.HasPrefix(raw, "/") {
		return nil, fmt.Errorf("历史证据未保存可安全重建的相对密钥地址")
	}
	base, err := url.Parse(origin)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("采样来源不足以重建密钥地址")
	}
	return base.ResolveReference(reference), nil
}

func fetchHLSAnalysisKey(ctx context.Context, target *url.URL) ([]byte, error) {
	if !hlsKeyFetchAllowed(ctx) {
		return nil, fmt.Errorf("当前离线流程不允许获取解密密钥")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("密钥请求创建失败")
	}
	req.Header.Set("User-Agent", "StreamAnalyzer-ContentAnalysis/1.0")
	req.Header.Set("Accept-Encoding", "identity")
	client := captureClient()
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("密钥请求失败")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("密钥服务返回非 200 状态")
	}
	key, err := io.ReadAll(io.LimitReader(response.Body, 17))
	if err != nil || len(key) != 16 {
		return nil, fmt.Errorf("AES-128 密钥长度无效")
	}
	return key, nil
}
