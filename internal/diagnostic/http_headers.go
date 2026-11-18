package diagnostic

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxHTTPHeaderBytes = 2048
	maxHTTPHeaderCount = 32
	maxHTTPHeaderName  = 128
	maxHTTPHeaderValue = 512
	redactedHTTPHeader = "[REDACTED]"
)

var httpLinkTarget = regexp.MustCompile(`<([^<>]*)>`)

// sanitizeHTTPHeaderValue is also used when importing evidence, so a bundle
// cannot introduce credential values that newly captured reports would redact.
func sanitizeHTTPHeaderValue(name, value string) string {
	key := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, name)
	for _, secret := range []string{"auth", "cookie", "apikey", "token", "secret", "sign", "credential", "session", "password", "passwd"} {
		if strings.Contains(key, secret) {
			return redactedHTTPHeader
		}
	}
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(value, "�")))
	if value == redactedHTTPHeader {
		return value
	}
	switch key {
	case "link":
		// Retain the referenced targets. Omit extension parameters: they may
		// themselves contain quoted URIs or provider-specific credentials.
		matches := httpLinkTarget.FindAllStringSubmatch(value, -1)
		if len(matches) == 0 {
			return redactedHTTPHeader
		}
		targets := make([]string, 0, len(matches))
		for _, match := range matches {
			targets = append(targets, "<"+sanitizeHTTPURL(match[1])+">")
		}
		return strings.Join(targets, ", ")
	case "refresh":
		parts := strings.SplitN(value, ";", 2)
		if len(parts) != 2 {
			return redactedHTTPHeader
		}
		attribute := strings.TrimSpace(parts[1])
		if len(attribute) < 4 || !strings.EqualFold(attribute[:4], "url=") {
			return redactedHTTPHeader
		}
		return strings.TrimSpace(parts[0]) + "; url=" + sanitizeHTTPURL(strings.Trim(strings.TrimSpace(attribute[4:]), "\"'"))
	}
	if key == "location" || key == "contentlocation" || key == "referer" || key == "referrer" || key == "origin" ||
		strings.Contains(key, "url") || strings.Contains(key, "uri") {
		return sanitizeHTTPURL(value)
	}
	return value
}

func sanitizeHTTPURL(value string) string {
	if value == redactedHTTPHeader {
		return value
	}
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "%2f") || strings.Contains(lower, "%3a%2f%2f") {
		decoded, err := url.PathUnescape(value)
		if err != nil {
			return redactedHTTPHeader
		}
		value = decoded
	}
	parsed, err := url.Parse(value)
	if err != nil || strings.ContainsAny(value, "\r\n\t\\") ||
		(parsed.Scheme != "" && !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return redactedHTTPHeader
	}
	parsed.User, parsed.RawQuery, parsed.Fragment, parsed.RawFragment, parsed.ForceQuery = nil, "", "", "", false
	return parsed.String()
}

func validHTTPHeaderName(name string) bool {
	if len(name) == 0 || len(name) > maxHTTPHeaderName {
		return false
	}
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c) {
			continue
		}
		return false
	}
	return true
}

func shortHTTPHeaderValue(value string) (string, bool) {
	if len(value) <= maxHTTPHeaderValue {
		return value, false
	}
	const suffix = " [TRUNCATED]"
	end := maxHTTPHeaderValue - len(suffix)
	for !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + suffix, true
}

func captureHTTPHeaders(fields http.Header) ([]HTTPHeader, bool) {
	keys := make([]string, 0, len(fields))
	for name := range fields {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	canonical := make(http.Header)
	truncated := false
	for _, name := range keys {
		if !validHTTPHeaderName(name) {
			truncated = true
			continue
		}
		key := http.CanonicalHeaderKey(name)
		canonical[key] = append(canonical[key], fields[name]...)
	}
	keys = keys[:0]
	for name := range canonical {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	var result []HTTPHeader
	bytes := 0
	for _, name := range keys {
		for _, raw := range canonical[name] {
			value, shortened := shortHTTPHeaderValue(sanitizeHTTPHeaderValue(name, raw))
			// Cutting a structured URI field can make its representation invalid.
			// Prefer an explicit redaction to an unparseable or unsafe partial URI.
			if sanitizeHTTPHeaderValue(name, value) != value {
				value, shortened = redactedHTTPHeader, true
			}
			truncated = truncated || shortened
			if len(result) >= maxHTTPHeaderCount || bytes+len(name)+len(value) > maxHTTPHeaderBytes {
				truncated = true
				continue
			}
			result = append(result, HTTPHeader{Name: name, Value: value})
			bytes += len(name) + len(value)
		}
	}
	return result, truncated
}

func validHTTPHeaderBlock(fields []HTTPHeader) bool {
	if len(fields) > maxHTTPHeaderCount {
		return false
	}
	bytes, last := 0, ""
	for _, field := range fields {
		if !validHTTPHeaderName(field.Name) || http.CanonicalHeaderKey(field.Name) != field.Name || field.Name < last ||
			!utf8.ValidString(field.Value) || len(field.Value) > maxHTTPHeaderValue || sanitizeHTTPHeaderValue(field.Name, field.Value) != field.Value {
			return false
		}
		last = field.Name
		bytes += len(field.Name) + len(field.Value)
		if bytes > maxHTTPHeaderBytes {
			return false
		}
	}
	return true
}

func validHTTPRequestHeaders(request HTTPRequestTiming) bool {
	if !request.HeadersRecorded {
		return request.Method == "" && request.HTTPProtocol == "" && len(request.RequestHeaders) == 0 && len(request.ResponseHeaders) == 0 &&
			!request.RequestHeadersTruncated && !request.ResponseHeadersTruncated
	}
	if !validHTTPHeaderName(request.Method) || len(request.Method) > 32 ||
		!validHTTPHeaderBlock(request.RequestHeaders) || !validHTTPHeaderBlock(request.ResponseHeaders) {
		return false
	}
	if request.HTTPStatus == 0 {
		return request.HTTPProtocol == "" && len(request.ResponseHeaders) == 0 && !request.ResponseHeadersTruncated
	}
	switch request.HTTPProtocol {
	case "HTTP/1.0", "HTTP/1.1", "HTTP/2.0", "HTTP/3.0":
		return true
	default:
		return false
	}
}
