package diagnostic

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"syscall"
)

const (
	modelTimeout           = "timeout"
	modelCanceled          = "canceled"
	modelDNSFailure        = "dns_error"
	modelTLSFailure        = "tls_error"
	modelConnectionRefused = "connection_refused"
	modelNetworkFailure    = "network_error"
	modelReadFailure       = "response_read_error"
	modelRedirect          = "redirect"
	modelAuthentication    = "authentication"
	modelPermission        = "permission"
	modelNotFound          = "not_found"
	modelQuota             = "quota"
	modelRateLimit         = "rate_limit"
	modelBilling           = "billing"
	modelPrecondition      = "precondition"
	modelProviderFailure   = "server_error"
	modelRejected          = "request_rejected"
	modelResponseTooLarge  = "response_too_large"
	modelInvalidResponse   = "invalid_response"
)

const (
	winsockTimedOut          syscall.Errno = 10060
	winsockConnectionRefused syscall.Errno = 10061
)

// ModelConnectionError exposes the classified failure and HTTP status without
// retaining the request URL, authentication data, or provider response text.
type ModelConnectionError struct {
	Code       string
	StatusCode int
}

func (e *ModelConnectionError) Error() string {
	return ErrProvider.Error() + ": " + e.Message()
}

func (e *ModelConnectionError) Unwrap() error { return ErrProvider }

func (e *ModelConnectionError) Message() string {
	var message string
	switch e.Code {
	case modelTimeout:
		message = "模型服务请求超时，请检查网络连接或稍后重试"
	case modelCanceled:
		message = "模型服务请求已取消"
	case modelDNSFailure:
		message = "DNS 解析失败，请检查 Base URL 的域名和本机 DNS 配置"
	case modelTLSFailure:
		message = "TLS 握手或证书校验失败，请检查 Base URL 和服务证书"
	case modelConnectionRefused:
		message = "连接被拒绝，请检查 Base URL、端口和服务状态"
	case modelNetworkFailure:
		message = "无法连接模型服务，请检查 Base URL 和网络连接"
	case modelReadFailure:
		message = "模型服务响应读取失败，请检查网络连接或稍后重试"
	case modelRedirect:
		message = "模型服务返回重定向，请将 Base URL 指向最终服务地址"
	case modelAuthentication:
		message = "身份验证失败，请检查 API Key 是否有效"
	case modelPermission:
		message = "访问被拒绝，请检查 API Key 是否有权调用该模型"
	case modelNotFound:
		message = "找不到模型或服务接口，请检查模型名和 Base URL"
	case modelQuota:
		message = "可用额度不足，请检查模型服务配额或套餐"
	case modelRateLimit:
		message = "请求频率或用量超限，请检查用量或稍后重试"
	case modelBilling:
		message = "模型服务需要付费或余额不足，请检查账单和余额"
	case modelPrecondition:
		message = "当前账户或地区不满足模型服务要求，请检查计费状态和服务地区"
	case modelProviderFailure:
		message = "模型服务出现错误，请检查服务状态或稍后重试"
	case modelRejected:
		message = "模型服务拒绝请求，请核对厂商、Base URL 和模型名"
	case modelResponseTooLarge:
		message = "模型服务响应超过大小上限"
	default:
		message = "模型服务返回的响应格式无效，请核对厂商、Base URL 和模型名"
	}
	if e.StatusCode > 0 && e.StatusCode != http.StatusOK {
		return fmt.Sprintf("HTTP %d，%s", e.StatusCode, message)
	}
	return message
}

// ModelConnectionMessage provides a user-visible diagnosis without rendering
// arbitrary error text from a provider or network client.
func ModelConnectionMessage(err error) string {
	if err == nil {
		return ""
	}
	var failure *ModelConnectionError
	if errors.As(err, &failure) {
		return failure.Message()
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return modelNetworkError(err, 0).Message()
	}
	return (&ModelConnectionError{Code: modelInvalidResponse}).Message()
}

func modelNetworkError(err error, status int) *ModelConnectionError {
	var networkError net.Error
	var dnsError *net.DNSError
	code := modelNetworkFailure
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &networkError) && networkError.Timeout(),
		runtime.GOOS == "windows" && errors.Is(err, winsockTimedOut):
		code = modelTimeout
	case errors.Is(err, context.Canceled):
		code = modelCanceled
	case errors.As(err, &dnsError):
		code = modelDNSFailure
	case errors.Is(err, syscall.ECONNREFUSED), runtime.GOOS == "windows" && errors.Is(err, winsockConnectionRefused):
		code = modelConnectionRefused
	case modelTLSError(err):
		code = modelTLSFailure
	case status > 0:
		code = modelReadFailure
	}
	return &ModelConnectionError{Code: code, StatusCode: status}
}

func modelTLSError(err error) bool {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		err = urlError.Err
	}
	var verification *tls.CertificateVerificationError
	var header tls.RecordHeaderError
	var alert tls.AlertError
	var hostname x509.HostnameError
	var authority x509.UnknownAuthorityError
	var invalidCertificate x509.CertificateInvalidError
	return errors.Is(err, http.ErrSchemeMismatch) || errors.As(err, &verification) ||
		errors.As(err, &header) || errors.As(err, &alert) || errors.As(err, &hostname) ||
		errors.As(err, &authority) || errors.As(err, &invalidCertificate) ||
		strings.HasPrefix(err.Error(), "tls: ") || strings.HasPrefix(err.Error(), "remote error: tls: ")
}

func modelHTTPError(status int, payload []byte) *ModelConnectionError {
	failure := &ModelConnectionError{StatusCode: status}
	switch {
	case status >= 300 && status < 400:
		failure.Code = modelRedirect
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		failure.Code = modelTimeout
	case status >= 500:
		failure.Code = modelProviderFailure
	default:
		failure.Code = modelDeclaredError(payload)
		if failure.Code == "" {
			failure.Code = modelStatusCode(status)
		}
	}
	return failure
}

func modelStatusCode(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return modelAuthentication
	case http.StatusForbidden:
		return modelPermission
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return modelNotFound
	case http.StatusTooManyRequests:
		return modelRateLimit
	case http.StatusPaymentRequired:
		return modelBilling
	default:
		return modelRejected
	}
}

type modelErrorFields struct {
	Code    json.RawMessage
	Type    string
	Status  string
	Reason  string
	Details []struct{ Reason string }
}

func modelDeclaredError(payload []byte) string {
	var body struct {
		modelErrorFields
		Error json.RawMessage
	}
	if json.Unmarshal(payload, &body) != nil {
		return ""
	}
	var nested modelErrorFields
	if len(body.Error) > 0 && json.Unmarshal(body.Error, &nested) == nil {
		if code := nested.classify(); code != "" {
			return code
		}
	}
	return body.modelErrorFields.classify()
}

func (fields modelErrorFields) classify() string {
	for _, detail := range fields.Details {
		if code := modelDeclaredCode(detail.Reason); code != "" {
			return code
		}
	}
	var responseCode string
	if len(fields.Code) > 0 {
		_ = json.Unmarshal(fields.Code, &responseCode)
	}
	for _, value := range []string{fields.Reason, responseCode, fields.Type, fields.Status} {
		if code := modelDeclaredCode(value); code != "" {
			return code
		}
	}
	return ""
}

func modelDeclaredCode(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "api_key_invalid", "invalid_api_key", "authentication_error", "unauthenticated", "invalid_token":
		return modelAuthentication
	case "permission_error", "permission_denied", "forbidden", "insufficient_permissions", "api_key_service_blocked", "api_key_http_referrer_blocked", "api_key_ip_address_blocked":
		return modelPermission
	case "model_not_found", "not_found", "not_found_error", "unknown_model", "model_not_supported":
		return modelNotFound
	case "insufficient_quota", "quota_exceeded", "resource_quota_exceeded", "daily_limit_exceeded":
		return modelQuota
	case "rate_limit_error", "rate_limit_exceeded", "resource_exhausted", "too_many_requests":
		return modelRateLimit
	case "billing_error", "billing_not_active", "billing_hard_limit_reached", "insufficient_balance", "payment_required":
		return modelBilling
	case "failed_precondition":
		return modelPrecondition
	default:
		return ""
	}
}
