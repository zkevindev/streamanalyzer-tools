package desktop

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

const updateHeader = "X-StreamAnalyzer-Update"

func (s Shell) serveUpdates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	u := s.App.updates
	if u == nil || s.App.updateToken == "" {
		writeSettingsError(w, http.StatusNotFound, "在线升级仅在桌面端可用")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(updateHeader)), []byte(s.App.updateToken)) != 1 {
		writeSettingsError(w, http.StatusForbidden, "更新请求无效")
		return
	}
	if r.URL.Path == "/desktop-update" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeSettingsError(w, http.StatusMethodNotAllowed, "更新请求方式无效")
			return
		}
		_ = json.NewEncoder(w).Encode(u.snapshot())
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/desktop-update/")
	if action != "check" && action != "download" && action != "install" {
		writeSettingsError(w, http.StatusNotFound, "更新操作不存在")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeSettingsError(w, http.StatusMethodNotAllowed, "更新请求方式无效")
		return
	}
	var input struct {
		Version   string `json:"version"`
		Confirmed bool   `json:"confirmed"`
	}
	if action != "check" {
		format, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || format != "application/json" {
			writeSettingsError(w, http.StatusUnsupportedMediaType, "更新请求需要 JSON")
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || decoder.Decode(new(any)) != io.EOF ||
			input.Version == "" || (action == "install" && !input.Confirmed) {
			writeSettingsError(w, http.StatusBadRequest, "更新请求格式无效或尚未确认安装")
			return
		}
	}
	if err := u.begin(action, input.Version); err != nil {
		writeSettingsError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(u.snapshot())
}
