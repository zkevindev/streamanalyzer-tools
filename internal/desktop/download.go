package desktop

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"streamanalyzer/internal/diagnostic"
)

func (s Shell) download(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var request struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "下载请求无效", http.StatusBadRequest)
		return
	}
	id, err := hex.DecodeString(request.ID)
	if err != nil || len(id) != 16 {
		http.Error(w, "排障记录无效", http.StatusBadRequest)
		return
	}

	switch request.Name {
	case "evidence.zip", "report.json", "dump", "stream.flv":
	default:
		http.Error(w, "产物类型无效", http.StatusBadRequest)
		return
	}
	server := s.App.server.URL()
	if server == "" {
		http.Error(w, "本地服务尚未启动", http.StatusConflict)
		return
	}
	source := server + "/api/diagnostics/runs/" + request.ID + "/" + request.Name
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, source, nil)
	if err != nil {
		http.Error(w, "无法读取本地排障产物", http.StatusInternalServerError)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "无法读取本地排障产物", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "排障产物不可用", http.StatusBadGateway)
		return
	}
	filename := request.Name
	switch request.Name {
	case "evidence.zip":
		filename = "stream-evidence.zip"
	case "dump":
		disposition, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
		filename = params["filename"]
		if err != nil || disposition != "attachment" || !isDiagnosticDumpName(filename) {
			http.Error(w, "无法识别原始流文件名", http.StatusBadGateway)
			return
		}
	}
	target, err := s.SaveFile(filename)
	switch {
	case err != nil:
		http.Error(w, "无法打开保存对话框", http.StatusInternalServerError)
		return
	case target == "":
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := saveArtifact(target, resp.Body); err != nil {
		http.Error(w, "保存失败，请检查目标目录和磁盘空间", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func isDiagnosticDumpName(filename string) bool {
	switch filename {
	case "stream.flv", "stream.rtmp", "stream.hls.zip":
		return true
	default:
		return false
	}
}

func saveArtifact(target string, source io.Reader) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".streamanalyzer-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	copied, err := io.Copy(tmp, io.LimitReader(source, diagnostic.MaxBundleBytes+1))
	if err := errors.Join(err, tmp.Close()); err != nil {
		return err
	}
	if copied > diagnostic.MaxBundleBytes {
		return errors.New("artifact exceeded download limit")
	}
	return os.Rename(tmp.Name(), target)
}
