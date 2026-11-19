package desktop

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func updateResponse(body []byte) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))}
}

func TestDesktopUpdateReleaseSelection(t *testing.T) {
	gotAssets := map[string]string{
		"darwin/arm64":  updateAsset("darwin", "arm64"),
		"darwin/amd64":  updateAsset("darwin", "amd64"),
		"windows/amd64": updateAsset("windows", "amd64"),
		"linux/amd64":   updateAsset("linux", "amd64"),
		"windows/arm64": updateAsset("windows", "arm64"),
	}
	want := map[string]string{
		"darwin/arm64": "StreamAnalyzer-macos-arm64.zip", "darwin/amd64": "StreamAnalyzer-macos-amd64.zip",
		"windows/amd64": "streamanalyzer.exe", "linux/amd64": "streamanalyzer", "windows/arm64": "",
	}
	for platform, name := range gotAssets {
		if name != want[platform] {
			t.Fatalf("wrong %s artifact: %s", platform, name)
		}
	}

	validDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("fixture")))
	for _, tc := range []struct {
		name, tag, current, digest, file string
		prerelease, draft                bool
		status                           int
		found, failure                   bool
	}{
		{name: "higher stable", tag: "v2.10.0", current: "v2.9.0", digest: validDigest, file: want["darwin/arm64"], found: true},
		{name: "same tag", tag: "v2.10.0", current: "v2.10.0", digest: validDigest, file: want["darwin/arm64"]},
		{name: "older tag", tag: "v2.9.1", current: "v2.10.0", digest: validDigest, file: want["darwin/arm64"]},
		{name: "prerelease", tag: "v3.0.0", current: "v2.10.0", digest: validDigest, file: want["darwin/arm64"], prerelease: true},
		{name: "draft", tag: "v3.0.0", current: "v2.10.0", digest: validDigest, file: want["darwin/arm64"], draft: true},
		{name: "only DMG", tag: "v3.0.0", current: "v2.10.0", digest: validDigest, file: "StreamAnalyzer-arm64.dmg", failure: true},
		{name: "missing SHA", tag: "v3.0.0", current: "v2.10.0", file: want["darwin/arm64"], failure: true},
		{name: "bad SHA", tag: "v3.0.0", current: "v2.10.0", digest: "sha256:dead", file: want["darwin/arm64"], failure: true},
		{name: "tag without published release", status: 404, current: "v2.10.0", failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := desktopReleaseProvider{client: &http.Client{Transport: updateTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != releaseAPI || req.Method != http.MethodGet {
					t.Fatal("unexpected release source", req.URL)
				}
				raw := fmt.Appendf(nil, `{"tag_name":%q,"body":"Release details","draft":%t,"prerelease":%t,"assets":[{"name":%q,"size":7,"state":"uploaded","digest":%q}]}`,
					tc.tag, tc.draft, tc.prerelease, tc.file, tc.digest)
				response := updateResponse(raw)
				if tc.status != 0 {
					response.StatusCode = tc.status
				}
				return response, nil
			})}}
			release, err := provider.Check(t.Context(), updater.CheckRequest{CurrentVersion: tc.current, Platform: "darwin", Arch: "arm64"})
			if (err != nil) != tc.failure || (release != nil) != tc.found {
				t.Fatalf("unexpected release: %+v (%v)", release, err)
			}
			if release != nil && (release.Version != tc.tag || release.Notes != "Release details" ||
				release.Artifact.Filename != tc.file || release.Artifact.Size != 7 || len(release.Verification.Digest) != sha256.Size) {
				t.Fatalf("release metadata lost: %+v", release)
			}
		})
	}
}

func TestDesktopUpdateShellRequiresVerifiedPackageAndConfirmation(t *testing.T) {
	name := updateAsset(runtime.GOOS, runtime.GOARCH)
	if name == "" {
		t.Skip("target has no release packages")
	}
	packageBytes := desktopUpdateFixture(t, name)
	sum := sha256.Sum256(packageBytes)
	badPackage := bytes.Clone(packageBytes)
	badPackage[len(badPackage)-1] ^= 1
	body := badPackage
	const current = "v2.0.0"
	const next = "v2.1.0"
	metadata := fmt.Appendf(nil, `{"tag_name":%q,"body":"<script>plain release notes</script>","assets":[{"name":%q,"size":%d,"state":"uploaded","digest":"sha256:%x"}]}`,
		next, name, len(packageBytes), sum)
	provider := desktopReleaseProvider{client: &http.Client{Transport: updateTransport(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case releaseAPI:
			return updateResponse(metadata), nil
		case releaseDownload + next + "/" + name:
			return updateResponse(body), nil
		}
		return nil, fmt.Errorf("unexpected request to %s", req.URL)
	})}}
	restart := make(chan string, 1)
	u := newDesktopUpdates(t.Context(), current, provider, func() { t.Error("test must not quit") }, func(version string) (func(), error) {
		restart <- version
		return nil, errors.New("test prevented the native restart")
	})
	t.Cleanup(u.close)
	shell := Shell{App: &App{updates: u, updateToken: "test-update-token"}}
	req := func(method, path, token, body string, want int) updateView {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set(updateHeader, token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		shell.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s got %d: %s", method, path, w.Code, w.Body)
		}
		var out updateView
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	post := func(action, body string, want int) updateView {
		t.Helper()
		return req("POST", "/desktop-update/"+action, shell.App.updateToken, body, want)
	}
	wait := func(phase string) updateView {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			out := req("GET", "/desktop-update", shell.App.updateToken, "", 200)
			if out.Phase == phase {
				return out
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("did not reach %s: %+v", phase, u.snapshot())
		return updateView{}
	}
	const selectVersion = `{"version":"v2.1.0"}`
	const confirmVersion = `{"version":"v2.1.0","confirmed":true}`
	req("GET", "/desktop-update", "", "", 403)
	req("POST", "/desktop-update/check", "", "", 403)
	req("GET", "/desktop-update/install", shell.App.updateToken, "", 405)
	post("check", "", 202)
	release := wait("available").Release
	if release == nil || release.Version != next || release.Notes != "<script>plain release notes</script>" {
		t.Fatalf("unexpected view: %+v", release)
	}
	post("install", confirmVersion, 409)
	post("download", `{"version":"v2.0.0"}`, 409)
	post("download", selectVersion, 202)
	failed := wait("available")
	if failed.Error == "" || u.engine.DownloadedPath() != "" {
		t.Fatalf("checksum mismatch was not rejected: %+v", failed)
	}
	post("install", confirmVersion, 409)

	body = packageBytes
	post("download", selectVersion, 202)
	staged := wait("ready")
	if staged.Received != int64(len(packageBytes)) || staged.Error != "" || u.engine.DownloadedPath() == "" {
		t.Fatalf("verified package did not become ready: %+v", staged)
	}
	post("check", "", 409)
	post("install", selectVersion, 400)
	req("POST", "/desktop-update/install", "wrong", confirmVersion, 403)
	select {
	case <-restart:
		t.Fatal("install invoked without confirmation")
	default:
	}
	post("install", confirmVersion, 202)
	select {
	case version := <-restart:
		if version != next {
			t.Fatal("confirmation targeted wrong version", version)
		}
	case <-time.After(time.Second):
		t.Fatal("verified and confirmed update did not reach native handoff")
	}
	if wait("ready").Error != "test prevented the native restart" {
		t.Fatal("handoff failure was not reported or could not be retried")
	}
	path := u.engine.DownloadedPath()
	u.close()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unused staging directory remained: %v", err)
	}
}

func desktopUpdateFixture(t *testing.T, name string) []byte {
	t.Helper()
	if filepath.Ext(name) != ".zip" {
		return []byte("test update payload")
	}
	var buf bytes.Buffer
	zipper := zip.NewWriter(&buf)
	header := &zip.FileHeader{Name: "streamanalyzer.app/Contents/MacOS/streamanalyzer", Method: zip.Deflate}
	header.SetMode(0o755)
	f, err := zipper.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(f, "test update payload"); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDesktopUpdateKeepsExplicitDataDirectoryOnce(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "launch.json")
	record := updateRelaunch{Directory: dir, From: "v2.0.0", To: "v2.1.0", Expires: time.Now().Add(time.Minute)}
	if err := writeUpdateRelaunch(marker, record); err != nil {
		t.Fatal(err)
	}
	if got := resumeUpdateDirectory(marker, "v9.0.0"); got != "" {
		t.Fatal("unrelated version consumed restart options")
	}
	if got := resumeUpdateDirectory(marker, record.To); got != dir || resumeUpdateDirectory(marker, record.To) != "" {
		t.Fatalf("restart options not consumed exactly once: %q", got)
	}
	if err := writeUpdateRelaunch(marker, record); err != nil || resumeUpdateDirectory(marker, record.From) != dir {
		t.Fatalf("rollback lost original data directory: %v", err)
	}
}
