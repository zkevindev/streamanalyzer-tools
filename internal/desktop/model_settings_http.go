package desktop

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"streamanalyzer/internal/diagnostic"
)

const settingsHeader = "X-StreamAnalyzer-Settings"

func (s Shell) serveModelSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	settings := s.App.modelSettings
	token := s.App.modelSettingsToken
	if settings == nil || token == "" {
		writeSettingsError(w, http.StatusNotFound, "模型设置仅在桌面端可用")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(settingsHeader)), []byte(token)) != 1 {
		writeSettingsError(w, http.StatusForbidden, "模型设置请求无效")
		return
	}

	isTest := r.URL.Path == "/model-settings/test"
	if r.Method == http.MethodGet && !isTest {
		_ = json.NewEncoder(w).Encode(settings.Current())
		return
	}
	if r.Method != http.MethodPost {
		allow := "GET, POST"
		if isTest {
			allow = http.MethodPost
		}
		w.Header().Set("Allow", allow)
		writeSettingsError(w, http.StatusMethodNotAllowed, "模型设置请求方式无效")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeSettingsError(w, http.StatusUnsupportedMediaType, "模型设置需要 JSON")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	var request modelSettingsRequest
	if err := decoder.Decode(&request); err != nil || decoder.Decode(new(any)) != io.EOF {
		writeSettingsError(w, http.StatusBadRequest, "模型设置格式错误或超出大小上限")
		return
	}

	if isTest {
		testModelSettings(w, r.Context(), settings, request)
		return
	}

	view, err := settings.Save(request, s.App.server.SetDiagnosticModel)
	switch {
	case err == nil:
		_ = json.NewEncoder(w).Encode(view)
	case errors.Is(err, errModelCredentialTooLarge):
		writeSettingsError(w, http.StatusBadRequest, "模型配置超出系统凭据库的容量上限")
	case errors.Is(err, diagnostic.ErrInvalid):
		writeInvalidModelSettings(w, err)
	default:
		writeSettingsError(w, http.StatusServiceUnavailable, "系统凭据库保存失败，请确认已解锁后重试")
	}
}

func testModelSettings(w http.ResponseWriter, ctx context.Context, settings *modelSettings, request modelSettingsRequest) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	start := time.Now()
	err := settings.TestConnection(ctx, request)
	switch {
	case err == nil:
		_ = json.NewEncoder(w).Encode(struct {
			ElapsedMS int64 `json:"elapsed_ms"`
		}{ElapsedMS: time.Since(start).Milliseconds()})
	case errors.Is(err, diagnostic.ErrInvalid):
		writeInvalidModelSettings(w, err)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		writeSettingsError(w, http.StatusGatewayTimeout, diagnostic.ModelConnectionMessage(ctx.Err()))
	default:
		writeSettingsError(w, http.StatusBadGateway, diagnostic.ModelConnectionMessage(err))
	}
}

func writeInvalidModelSettings(w http.ResponseWriter, err error) {
	message := "请检查模型设置"
	if detail, ok := strings.CutPrefix(err.Error(), diagnostic.ErrInvalid.Error()+": "); ok {
		message = detail
	}
	writeSettingsError(w, http.StatusBadRequest, message)
}

func writeSettingsError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
