package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type updateRelaunch struct {
	Directory string    `json:"directory"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Expires   time.Time `json:"expires"`
}

func relaunchFile(executable string) (string, error) {
	base, err := dataDirectory("")
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(filepath.Clean(executable)))
	return filepath.Join(base, ".update-relaunch-"+hex.EncodeToString(key[:12])+".json"), nil
}

// Wails relaunches without command-line arguments. Preserve an explicit
// -data-dir once, including when the helper restores and relaunches the old app.
func preserveUpdateLaunch(executable, directory, version string) (func(), error) {
	ancestor := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	if runtime.GOOS == "darwin" && filepath.Ext(ancestor) != ".app" {
		return nil, errors.New("请从已安装的 Stream Analyzer.app 启动后再升级")
	}
	if directory == "" {
		return nil, nil
	}
	path, err := relaunchFile(executable)
	if err != nil {
		return nil, err
	}
	record := updateRelaunch{Directory: directory, From: desktopVersion, To: version, Expires: time.Now().Add(5 * time.Minute)}
	if err := writeUpdateRelaunch(path, record); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}

func writeUpdateRelaunch(path string, record updateRelaunch) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".update-relaunch-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encodeErr := json.NewEncoder(file).Encode(record)
	closeErr := file.Close()
	if err := errors.Join(encodeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func resumeUpdateDirectory(path, version string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	var record updateRelaunch
	decoder := json.NewDecoder(io.LimitReader(file, 32<<10))
	decodeErr := decoder.Decode(&record)
	_ = file.Close() // Windows requires the handle to be closed before removal.
	if decodeErr != nil || !record.Expires.After(time.Now()) || !filepath.IsAbs(record.Directory) {
		_ = os.Remove(path)
		return ""
	}
	if version != record.From && version != record.To {
		return ""
	}
	if os.Remove(path) != nil {
		return ""
	}
	return record.Directory
}
