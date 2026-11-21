package diagnostic

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeMediaTool(t *testing.T, dir, name, version string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body := "#!/bin/sh\ncase \"$1\" in\n-version) echo '" + version + "' ;;\n-encoders) echo ' AAC aac\n V libx264\n V libx265\n A libopus' ;;\n-filters) echo ' blackdetect\n freezedetect\n silencedetect' ;;\nesac\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMediaToolSettingsPersistAndShowCapabilities(t *testing.T) {
	mediaToolEnv(MediaToolSettings{})
	t.Cleanup(func() { mediaToolEnv(MediaToolSettings{}) })
	dir := t.TempDir()
	ffmpeg := fakeMediaTool(t, dir, "ffmpeg", "ffmpeg test-version")
	ffprobe := fakeMediaTool(t, dir, "ffprobe", "ffprobe test-version")
	service, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.SaveMediaTools(MediaToolSettings{FFmpegPath: ffmpeg, FFprobePath: ffprobe})
	if err != nil {
		t.Fatal(err)
	}
	if !status.FFmpeg.Available || !status.FFprobe.Available || !status.FFmpeg.AAC || !status.FFmpeg.H264 || !status.FFmpeg.HEVC ||
		!status.FFmpeg.Opus || !status.FFmpeg.Blackdetect || !status.FFmpeg.Freezedetect || !status.FFmpeg.Silencedetect ||
		status.FFmpeg.Version != "ffmpeg test-version" || status.FFprobe.Version != "ffprobe test-version" {
		t.Fatalf("unexpected media tool capabilities: %+v", status)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded := reopened.MediaTools()
	if loaded.Settings.FFmpegPath != ffmpeg || loaded.Settings.FFprobePath != ffprobe || !loaded.FFmpeg.Available {
		t.Fatalf("media tool settings did not survive restart: %+v", loaded)
	}
	if _, err := reopened.SaveMediaTools(MediaToolSettings{FFmpegPath: filepath.Join(dir, "missing")}); err == nil {
		t.Fatal("saved an invalid executable path")
	}
	if got := reopened.MediaTools().Settings.FFmpegPath; got != ffmpeg {
		t.Fatalf("invalid save replaced the working path: %q", got)
	}
}
