package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

type updateReleaseView struct {
	Version string    `json:"version"`
	Notes   string    `json:"notes"`
	Date    time.Time `json:"date"`
	Size    int64     `json:"size"`
}

type updateView struct {
	Current  string             `json:"current"`
	Phase    string             `json:"phase"`
	Checked  time.Time          `json:"checked_at,omitzero"`
	Release  *updateReleaseView `json:"release,omitempty"`
	Received int64              `json:"received"`
	Error    string             `json:"error,omitempty"`
}

type desktopUpdates struct {
	ctx            context.Context
	cancel         context.CancelFunc
	engine         *updater.Updater
	quit           func()
	preserveLaunch func(string) (func(), error)

	mu     sync.Mutex
	view   updateView
	closed bool
	done   chan struct{}
}

func newDesktopUpdates(parent context.Context, version string, provider updater.Provider, quit func(), preserveLaunch func(string) (func(), error)) *desktopUpdates {
	ctx, cancel := context.WithCancel(parent)
	u := &desktopUpdates{
		ctx: ctx, cancel: cancel, quit: quit, preserveLaunch: preserveLaunch,
		view: updateView{Current: version, Phase: "idle"},
	}
	if _, ok := parseStableVersion(version); !ok {
		u.view.Phase = "disabled"
		u.view.Error = "开发构建仅在打包时注入正式版本号后支持版本检查"
		return u
	}
	if updateAsset(runtime.GOOS, runtime.GOARCH) == "" {
		u.view.Phase = "disabled"
		u.view.Error = "当前平台无在线升级安装包"
		return u
	}
	u.engine = updater.New(u)
	if err := u.engine.Init(updater.Config{
		CurrentVersion: version, Providers: []updater.Provider{provider}, Window: updater.WindowNone,
	}); err != nil {
		u.engine = nil
		u.view.Phase, u.view.Error = "disabled", "无法初始化在线升级"
	}
	return u
}

func (u *desktopUpdates) snapshot() updateView {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.view
}

func (u *desktopUpdates) begin(action, version string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.engine == nil || u.closed {
		return errors.New("当前构建无法在线升级")
	}
	if u.done != nil || u.view.Phase == "restarting" {
		return errors.New("更新任务正在进行")
	}
	switch action {
	case "check":
		if u.view.Phase == "ready" {
			return errors.New("已校验的更新等待安装")
		}
		u.view.Release, u.view.Phase = nil, "checking"
	case "download", "install":
		if u.view.Release == nil || version != u.view.Release.Version {
			return errors.New("所选版本已变化，请重新检查")
		}
		phase := "available"
		if action == "install" {
			phase = "ready"
		}
		if u.view.Phase != phase {
			return errors.New("当前更新尚未到达该步骤")
		}
		if action == "download" {
			u.view.Phase, u.view.Received = "downloading", 0
		} else {
			u.view.Phase = "restarting"
		}
	default:
		return errors.New("未知更新操作")
	}
	u.view.Error = ""
	u.done = make(chan struct{})
	go u.run(action, version, u.done)
	return nil
}

func (u *desktopUpdates) run(action, version string, done chan struct{}) {
	switch action {
	case "check":
		release, err := u.engine.Check(u.ctx)
		u.mu.Lock()
		u.view.Checked = time.Now()
		switch {
		case err != nil:
			u.view.Phase, u.view.Error = "error", err.Error()
		case release == nil:
			u.view.Phase = "current"
		default:
			u.view.Phase = "available"
			u.view.Release = &updateReleaseView{Version: release.Version, Notes: release.Notes, Date: release.PublishedAt, Size: release.Artifact.Size}
		}
	case "download":
		err := u.engine.DownloadAndInstall(u.ctx) // Wails stages the verified file. Restart alone replaces the running app.
		u.mu.Lock()
		if err != nil {
			u.view.Phase, u.view.Error, u.view.Received = "available", err.Error(), 0
		} else {
			u.view.Phase, u.view.Received = "ready", u.view.Release.Size
			if u.closed {
				u.removeStaged()
			}
		}
	case "install":
		err := u.restart(version)
		u.mu.Lock()
		if err != nil {
			u.view.Phase, u.view.Error = "ready", err.Error()
			if u.closed {
				u.removeStaged()
			}
		}
	}
	u.done = nil
	close(done)
	u.mu.Unlock()
}

func (u *desktopUpdates) restart(version string) error {
	var undo func()
	if u.preserveLaunch != nil {
		var err error
		undo, err = u.preserveLaunch(version)
		if err != nil {
			return err
		}
	}
	if err := u.engine.Restart(u.ctx); err != nil {
		if undo != nil {
			undo()
		}
		return err
	}
	return nil
}

func (u *desktopUpdates) close() {
	u.mu.Lock()
	u.closed = true
	u.cancel()
	done, restarting := u.done, u.view.Phase == "restarting"
	if u.view.Phase == "ready" {
		u.removeStaged()
	}
	u.mu.Unlock()
	// Restart invokes the native Quit callback within its worker. Wails owns
	// the staged package once the installer has been handed off.
	if done != nil && !restarting {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		}
	}
}

func (u *desktopUpdates) removeStaged() {
	if path := u.engine.DownloadedPath(); path != "" {
		parent := filepath.Dir(path)
		if filepath.IsAbs(parent) && strings.HasPrefix(filepath.Base(parent), "wails-update-") {
			_ = os.RemoveAll(parent)
		}
	}
}

// The shell renders the updater UI. Translate the staging lifecycle without
// exposing local file paths or any of the release provider's internal metadata.
func (u *desktopUpdates) Emit(event string, data ...any) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	switch event {
	case updater.EventDownloadProgress:
		if progress, ok := data[0].(updater.Progress); ok {
			u.view.Received = progress.Written
		}
	case updater.EventVerifying:
		u.view.Phase = "verifying"
	case updater.EventInstalling:
		u.view.Phase = "preparing"
	}
	return true
}

func (u *desktopUpdates) Quit()                                               { u.quit() }
func (*desktopUpdates) OnEvent(string, func(any)) func()                      { return func() {} }
func (*desktopUpdates) OpenWindow(updater.WindowOptions) updater.WindowHandle { return nil }
