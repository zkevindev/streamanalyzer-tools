package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

const (
	releaseRepository = "zkevindev/streamanalyzer-tools"
	releaseAPI        = "https://api.github.com/repos/" + releaseRepository + "/releases/latest"
	releaseDownload   = "https://github.com/" + releaseRepository + "/releases/download/"
	maxUpdateBytes    = 256 << 20
)

// Tagged desktop packages set this via the linker.
var desktopVersion = "dev"

type stableVersion [3]uint64

func parseStableVersion(tag string) (stableVersion, bool) {
	var version stableVersion
	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(parts) != len(version) {
		return version, false
	}
	for i, part := range parts {
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil || strconv.FormatUint(number, 10) != part {
			return version, false
		}
		version[i] = number
	}
	return version, true
}

func (v stableVersion) newerThan(other stableVersion) bool {
	for i, number := range v {
		if number != other[i] {
			return number > other[i]
		}
	}
	return false
}

func updateAsset(platform, arch string) string {
	switch {
	case platform == "darwin" && (arch == "arm64" || arch == "amd64"):
		return "StreamAnalyzer-macos-" + arch + ".zip"
	case platform == "windows" && arch == "amd64":
		return "streamanalyzer.exe"
	case platform == "linux" && arch == "amd64":
		return "streamanalyzer"
	default:
		return ""
	}
}

type desktopReleaseProvider struct{ client *http.Client }

func newDesktopReleaseProvider() desktopReleaseProvider {
	return desktopReleaseProvider{client: &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			host := req.URL.Host
			if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil ||
				(host != "github.com" && host != "release-assets.githubusercontent.com" && host != "objects.githubusercontent.com") {
				return errors.New("发行地址重定向无效")
			}
			return nil
		},
	}}
}

func (p desktopReleaseProvider) Name() string { return "github" }

func (p desktopReleaseProvider) Check(ctx context.Context, request updater.CheckRequest) (*updater.Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	current, ok := parseStableVersion(request.CurrentVersion)
	if !ok {
		return nil, errors.New("当前构建没有正式版本号")
	}
	name := updateAsset(request.Platform, request.Arch)
	if name == "" {
		return nil, errors.New("当前平台无在线升级安装包")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := p.client.Do(req)
	if err != nil {
		return nil, errors.New("无法连接 GitHub 发布服务")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub 版本查询返回 HTTP %d", res.StatusCode)
	}
	var release struct {
		Tag         string    `json:"tag_name"`
		Body        string    `json:"body"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
		Assets      []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
			State  string `json:"state"`
			Size   int64  `json:"size"`
		} `json:"assets"`
	}
	const metadataLimit = 1 << 20
	body, err := io.ReadAll(io.LimitReader(res.Body, metadataLimit+1))
	if err != nil || len(body) > metadataLimit || json.Unmarshal(body, &release) != nil {
		return nil, errors.New("无法读取 GitHub 发行信息")
	}
	next, valid := parseStableVersion(release.Tag)
	if !valid || release.Draft || release.Prerelease || !strings.HasPrefix(release.Tag, "v") || !next.newerThan(current) {
		return nil, nil
	}
	for _, asset := range release.Assets {
		if asset.Name != name || asset.State != "uploaded" {
			continue
		}
		value, hasDigest := strings.CutPrefix(asset.Digest, "sha256:")
		digest, err := hex.DecodeString(value)
		if err != nil || !hasDigest || len(digest) != sha256.Size || asset.Size <= 0 || asset.Size > maxUpdateBytes {
			return nil, errors.New("新版安装包缺少有效的 SHA-256 或文件大小")
		}
		notes := strings.TrimSpace(release.Body)
		if len(notes) > 32<<10 {
			notes = strings.ToValidUTF8(notes[:32<<10], "") + "\n…"
		}
		return &updater.Release{
			Version: release.Tag, Notes: notes, PublishedAt: release.PublishedAt,
			Artifact:     updater.Artifact{Filename: name, Size: asset.Size, Platform: request.Platform, Arch: request.Arch},
			Verification: &updater.Verification{DigestAlgo: "sha256", Digest: digest},
		}, nil
	}
	return nil, errors.New("最新版尚未提供当前平台的升级包")
}

func (p desktopReleaseProvider) Download(ctx context.Context, release *updater.Release, dst io.Writer, progress func(int64, int64)) error {
	asset := release.Artifact
	_, valid := parseStableVersion(release.Version)
	if !valid || !strings.HasPrefix(release.Version, "v") || asset.Filename == "" || asset.Filename != updateAsset(asset.Platform, asset.Arch) ||
		asset.Size <= 0 || asset.Size > maxUpdateBytes || release.Verification == nil ||
		release.Verification.DigestAlgo != "sha256" || len(release.Verification.Digest) != sha256.Size {
		return errors.New("升级包元数据无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseDownload+release.Version+"/"+asset.Filename, nil)
	if err != nil {
		return err
	}
	res, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("无法下载安装包")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("安装包下载返回 HTTP %d", res.StatusCode)
	}
	if res.ContentLength >= 0 && res.ContentLength != asset.Size {
		return errors.New("安装包长度与发行信息不符")
	}
	target := &updateProgressWriter{dst: dst, total: asset.Size, progress: progress}
	progress(0, asset.Size)
	n, err := io.Copy(target, io.LimitReader(res.Body, asset.Size))
	if err != nil {
		return err
	}
	_, end := io.ReadFull(res.Body, make([]byte, 1))
	if n != asset.Size || end != io.EOF {
		return errors.New("安装包长度与发行信息不符")
	}
	return nil
}

type updateProgressWriter struct {
	dst            io.Writer
	written, total int64
	progress       func(int64, int64)
}

func (w *updateProgressWriter) Write(data []byte) (int, error) {
	n, err := w.dst.Write(data)
	w.written += int64(n)
	w.progress(w.written, w.total)
	return n, err
}
