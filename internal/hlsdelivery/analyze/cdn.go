package analyze

import (
	"net/http"
	"strconv"
	"strings"
)

func DetectCDN(h http.Header) string {
	if h == nil {
		return ""
	}
	server := strings.ToLower(h.Get("Server"))
	via := strings.ToLower(h.Get("Via"))
	served := strings.ToLower(h.Get("X-Served-By"))
	switch {
	case h.Get("Cf-Ray") != "" || strings.Contains(server, "cloudflare"):
		return "Cloudflare"
	case h.Get("X-Amz-Cf-Id") != "" || h.Get("X-Amz-Cf-Pop") != "" || strings.Contains(via, "cloudfront"):
		return "CloudFront"
	case strings.Contains(server, "akamaighost") || h.Get("X-Akamai-Request-Id") != "" || strings.Contains(via, "akamai"):
		return "Akamai"
	case h.Get("X-Timer") != "" || strings.Contains(served, "cache-"):
		return "Fastly"
	case h.Get("X-Azure-Ref") != "" || strings.Contains(server, "ecacc"):
		return "Azure"
	case strings.Contains(via, "google") || h.Get("X-Guploader-Uploadid") != "":
		return "Google"
	case h.Get("X-Qiniu-Cache") != "" || strings.Contains(strings.ToLower(h.Get("X-Cdn-Provider")), "qiniu"):
		return "Qiniu"
	case h.Get("Ali-Swift-Global-Savetime") != "" || h.Get("Eagleid") != "":
		return "Alibaba"
	case h.Get("Eo-Cache-Status") != "" || h.Get("X-Tencent-Ua") != "":
		return "Tencent"
	case strings.Contains(server, "bunnycdn") || h.Get("Cdn-Pullzone") != "":
		return "Bunny"
	case strings.Contains(server, "keycdn"):
		return "KeyCDN"
	default:
		return ""
	}
}

func CacheInfo(h http.Header) (status string, age *int) {
	if h == nil {
		return "", nil
	}
	for _, c := range []string{
		h.Get("Cf-Cache-Status"),
		h.Get("X-Cache-Status"),
		h.Get("Eo-Cache-Status"),
		h.Get("X-Cache"),
	} {
		if s := normalizeCache(c); s != "" {
			status = s
			break
		}
	}
	if raw := strings.TrimSpace(h.Get("Age")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			age = &n
		}
	}
	return status, age
}

func normalizeCache(v string) string {
	s := strings.ToUpper(strings.TrimSpace(v))
	if s == "" {
		return ""
	}
	switch {
	case strings.Contains(s, "MISS"):
		return "MISS"
	case strings.Contains(s, "EXPIRED"):
		return "EXPIRED"
	case strings.Contains(s, "STALE"):
		return "STALE"
	case strings.Contains(s, "REVALIDAT"):
		return "REVALIDATED"
	case strings.Contains(s, "BYPASS"):
		return "BYPASS"
	case strings.Contains(s, "DYNAMIC"):
		return "DYNAMIC"
	case strings.Contains(s, "UPDATING"):
		return "UPDATING"
	case strings.Contains(s, "HIT"):
		return "HIT"
	default:
		return ""
	}
}

func cacheHit(status string) bool {
	switch status {
	case "HIT", "REVALIDATED", "UPDATING":
		return true
	default:
		return false
	}
}
