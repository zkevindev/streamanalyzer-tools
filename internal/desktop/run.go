package desktop

import (
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

func Run() {
	updater.HandleHelperMode()
	dataDir := flag.String("data-dir", "", "data directory (defaults to the system application data directory)")
	flag.Parse()

	executable, executableErr := os.Executable()
	customDir := *dataDir
	if customDir == "" && executableErr == nil {
		if path, err := relaunchFile(executable); err == nil {
			customDir = resumeUpdateDirectory(path, desktopVersion)
		}
	}
	desktop := NewApp()
	dir, err := dataDirectory(customDir)
	if err == nil {
		err = desktop.Startup(dir)
	}
	if err != nil {
		log.Printf("Start desktop service: %v", err)
		desktop.startupError = fmt.Sprintf("启动本地服务失败：%v", err)
	}

	var window *application.WebviewWindow
	var app *application.App
	explicitDir := ""
	if customDir != "" {
		explicitDir = dir
	}
	desktop.updateToken = rand.Text()
	desktop.updates = newDesktopUpdates(desktop.ctx, desktopVersion, newDesktopReleaseProvider(), func() { app.Quit() }, func(version string) (func(), error) {
		if executableErr != nil {
			return nil, executableErr
		}
		return preserveUpdateLaunch(executable, explicitDir, version)
	})
	app = application.New(application.Options{
		Name: "Stream Analyzer",
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Assets: application.AssetOptions{Handler: Shell{
			App: desktop,
			SaveFile: func(filename string) (string, error) {
				path, err := app.Dialog.SaveFile().SetMessage("保存排障产物").SetFilename(filename).
					CanCreateDirectories(true).AttachToWindow(window).PromptForSingleSelection()
				// Wails exposes Windows dialog cancellation through an internal error sentinel.
				if runtime.GOOS == "windows" && path == "" && err != nil && err.Error() == "cancelled by user" {
					return "", nil
				}
				return path, err
			},
		}},
		OnShutdown: desktop.Shutdown,
	})
	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Stream Analyzer",
		URL:              "/",
		Width:            1280,
		Height:           840,
		MinWidth:         760,
		MinHeight:        620,
		BackgroundColour: application.NewRGB(242, 245, 249),
	})
	if err := app.Run(); err != nil {
		desktop.Shutdown()
		log.Fatal(err)
	}
}

func dataDirectory(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
	case "linux":
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".local", "share")
		}
	}
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "Stream Analyzer"), nil
}
