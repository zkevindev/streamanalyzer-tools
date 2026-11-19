package mediafile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func FindTool(name, alongside string) (string, error) {
	dirs := []string{}
	if alongside != "" {
		dirs = append(dirs, alongside)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	dirs = append(dirs, "") // Resolve through PATH.
	if runtime.GOOS == "darwin" {
		// LaunchServices apps typically don't inherit the terminal PATH.
		dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin")
	}
	for _, dir := range dirs {
		if resolved, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("未找到 %s，请将工具放在程序旁或加入 PATH", name)
}
