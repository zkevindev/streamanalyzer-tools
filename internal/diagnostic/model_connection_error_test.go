package diagnostic

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

func TestModelPostClassifiesHTTPFailuresWithoutLeakingDetails(t *testing.T) {
	const secret = "private-key-sent-by-provider"
	const privateURL = "https://private.example/model"
	for _, tc := range []struct {
		name   string
		status int
		body   string
		code   string
	}{
		{"unauthorized", 401, "request used " + secret, modelAuthentication},
		{"permission", 403, `{"error":{"message":"` + privateURL + `"}}`, modelPermission},
		{"endpoint", 404, "Missing " + privateURL, modelNotFound},
		{"billing", 402, "account balance", modelBilling},
		{"rate limit", 429, "slow down", modelRateLimit},
		{"quota", 429, `{"error":{"code":"insufficient_quota","message":"` + secret + `"}}`, modelQuota},
		{"bad model", 400, `{"error":{"type":"invalid_request_error","code":"model_not_found"}}`, modelNotFound},
		{"Google key", 400, `{"error":{"code":400,"details":[{"reason":"API_KEY_INVALID","metadata":{"apiKey":"` + secret + `"}}]}}`, modelAuthentication},
		{"Google precondition", 400, `{"error":{"status":"FAILED_PRECONDITION"}}`, modelPrecondition},
		{"server error", 503, "temporarily unavailable", modelProviderFailure},
		{"gateway timeout", 504, "timed out", modelTimeout},
		{"JSON format", 200, "<html>" + secret + "</html>", modelInvalidResponse},
		{"wrapped error on success", 200, `{"error":{"code":"invalid_api_key"}}`, modelAuthentication},
		{"flat error on success", 200, `{"reason":"invalid_api_key"}`, modelAuthentication},
		{"error response limit", 401, strings.Repeat("a", 32<<10+1), modelResponseTooLarge},
		{"success response limit", 200, strings.Repeat("a", 256<<10+1), modelResponseTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client, err := newModelClient(ProviderOpenAICompatible, server.URL, "model", secret)
			if err != nil {
				t.Fatal(err)
			}
			var output struct{}
			err = client.post(t.Context(), "/chat/completions", map[string]string{"Authorization": "Bearer " + secret}, struct{}{}, &output)
			var failure *ModelConnectionError
			if !errors.As(err, &failure) || !errors.Is(err, ErrProvider) || failure.Code != tc.code || failure.StatusCode != tc.status {
				t.Fatalf("expected provider error %s with HTTP %d, got %+v", tc.code, tc.status, err)
			}
			for _, message := range []string{err.Error(), ModelConnectionMessage(err)} {
				if strings.Contains(message, secret) || strings.Contains(message, privateURL) || strings.Contains(message, server.URL) {
					t.Fatal("failure exposed a provider response or request detail")
				}
			}
		})
	}
}

func TestModelPostRejectsRedirects(t *testing.T) {
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("model connection followed a redirect")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer redirected.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirected.URL+"/private-endpoint", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := newModelClient(ProviderOpenAICompatible, server.URL, "model", "private-key")
	if err != nil {
		t.Fatal(err)
	}
	var output struct{}
	err = client.post(t.Context(), "/chat/completions", map[string]string{"Authorization": "Bearer private-key"}, struct{}{}, &output)
	var failure *ModelConnectionError
	if !errors.As(err, &failure) || failure.Code != modelRedirect || failure.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("expected redirect failure, got %v", err)
	}
	if strings.Contains(err.Error(), redirected.URL) || strings.Contains(err.Error(), "private-endpoint") {
		t.Fatal("error exposed redirect location")
	}
}

func TestClassifyModelNetworkFailures(t *testing.T) {
	const privateURL = "https://sensitive-endpoint.example/model"
	for _, tc := range []struct {
		cause  error
		status int
		code   string
	}{
		{context.DeadlineExceeded, 0, modelTimeout},
		{context.DeadlineExceeded, 401, modelTimeout},
		{context.Canceled, 0, modelCanceled},
		{&net.DNSError{Err: "name not found", Name: "sensitive-endpoint.example"}, 0, modelDNSFailure},
		{x509.CertificateInvalidError{}, 0, modelTLSFailure},
		{http.ErrSchemeMismatch, 0, modelTLSFailure},
		{syscall.ECONNREFUSED, 0, modelConnectionRefused},
		{io.ErrUnexpectedEOF, 200, modelReadFailure},
		{io.ErrUnexpectedEOF, 0, modelNetworkFailure},
	} {
		err := modelNetworkError(&url.Error{Op: "Post", URL: privateURL, Err: tc.cause}, tc.status)
		if err.Code != tc.code || err.StatusCode != tc.status {
			t.Errorf("expected %s, got %s", tc.code, err.Code)
		}
		if msg := ModelConnectionMessage(err); strings.Contains(msg, privateURL) || strings.Contains(msg, "sensitive-endpoint") {
			t.Error("network failure exposed an endpoint")
		}
	}
	if got := ModelConnectionMessage(context.DeadlineExceeded); !strings.Contains(got, "超时") {
		t.Errorf("desktop deadline message must explain the timeout, got %q", got)
	}
	if got := ModelConnectionMessage(fmt.Errorf("%w: %s", ErrProvider, privateURL)); strings.Contains(got, privateURL) {
		t.Error("unclassified provider failure exposed an endpoint")
	}
}
