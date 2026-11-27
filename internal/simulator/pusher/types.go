package pusher

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	MaxActiveTasks   = 16
	MaxUploadBytes   = 512 << 20 // 512 MiB
	maxRecentLogs    = 25
	defaultBitrate   = 1200
	defaultFPS       = 25
	defaultRes       = "1280x720"
)

var (
	ErrInvalid       = errors.New("推流参数无效")
	ErrNotFound      = errors.New("推流任务不存在")
	ErrLimitReached  = errors.New("同时运行的推流任务已达上限（16 路）")
	ErrClosed        = errors.New("推流器已关闭")
	ErrUploadTooBig  = errors.New("上传录像文件超过 512 MiB 上限")
	ErrTaskRunning   = errors.New("任务仍在推流中，请先停止再删除")
	resolutionRegexp = regexp.MustCompile(`^[1-9][0-9]{1,3}x[1-9][0-9]{1,3}$`)
)

type Protocol string

const (
	ProtocolRTMP Protocol = "rtmp"
	ProtocolRTSP Protocol = "rtsp"
	ProtocolWHIP Protocol = "whip"
)

type SourceKind string

const (
	SourcePattern SourceKind = "pattern"
	SourceFile    SourceKind = "file"
	SourceUpload  SourceKind = "upload"
	SourceBrowser SourceKind = "browser"
)

type TaskStatus string

const (
	StatusStarting TaskStatus = "starting"
	StatusPushing  TaskStatus = "pushing"
	StatusStopped  TaskStatus = "stopped"
	StatusFailed   TaskStatus = "failed"
)

type Config struct {
	Name             string     `json:"name,omitempty"`
	Protocol         Protocol   `json:"protocol"`
	TargetURL        string     `json:"target_url"`
	BearerToken      string     `json:"bearer_token,omitempty"`
	RTSPTransport    string     `json:"rtsp_transport,omitempty"` // tcp / udp
	SourceKind       SourceKind `json:"source_kind"`
	FilePath         string     `json:"file_path,omitempty"`
	SourceName       string     `json:"source_name,omitempty"`
	Loop             *bool      `json:"loop,omitempty"`
	LoopCount        int        `json:"loop_count,omitempty"` // 0 = infinite when Loop is true
	VideoMode        string     `json:"video_mode,omitempty"` // copy / h264
	AudioMode        string     `json:"audio_mode,omitempty"` // copy / aac / opus / none
	VideoBitrateKbps int        `json:"video_bitrate_kbps,omitempty"`
	FPS              int        `json:"fps,omitempty"`
	Resolution       string     `json:"resolution,omitempty"`
}

func (c *Config) normalize() error {
	c.Protocol = Protocol(strings.ToLower(strings.TrimSpace(string(c.Protocol))))
	c.SourceKind = SourceKind(strings.ToLower(strings.TrimSpace(string(c.SourceKind))))
	c.TargetURL = strings.TrimSpace(c.TargetURL)
	c.FilePath = strings.TrimSpace(c.FilePath)
	c.Name = strings.TrimSpace(c.Name)
	c.VideoMode = strings.ToLower(strings.TrimSpace(c.VideoMode))
	c.AudioMode = strings.ToLower(strings.TrimSpace(c.AudioMode))
	c.RTSPTransport = strings.ToLower(strings.TrimSpace(c.RTSPTransport))
	c.Resolution = strings.ToLower(strings.TrimSpace(c.Resolution))

	switch c.Protocol {
	case ProtocolRTMP, ProtocolRTSP, ProtocolWHIP:
	default:
		return fmt.Errorf("%w：协议必须为 rtmp、rtsp 或 whip", ErrInvalid)
	}

	u, err := url.Parse(c.TargetURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%w：请输入合法的目标推流地址", ErrInvalid)
	}
	scheme := strings.ToLower(u.Scheme)
	switch c.Protocol {
	case ProtocolRTMP:
		if scheme != "rtmp" && scheme != "rtmps" {
			return fmt.Errorf("%w：RTMP 推流地址需以 rtmp:// 或 rtmps:// 开头", ErrInvalid)
		}
	case ProtocolRTSP:
		if scheme != "rtsp" && scheme != "rtsps" {
			return fmt.Errorf("%w：RTSP 推流地址需以 rtsp:// 或 rtsps:// 开头", ErrInvalid)
		}
		if c.RTSPTransport == "" {
			c.RTSPTransport = "tcp"
		}
		if c.RTSPTransport != "tcp" && c.RTSPTransport != "udp" {
			return fmt.Errorf("%w：RTSP 传输方式仅支持 tcp 或 udp", ErrInvalid)
		}
	case ProtocolWHIP:
		if scheme != "http" && scheme != "https" {
			return fmt.Errorf("%w：WHIP 地址需以 http:// 或 https:// 开头", ErrInvalid)
		}
	}

	switch c.SourceKind {
	case SourcePattern, SourceFile, SourceUpload, SourceBrowser:
	default:
		return fmt.Errorf("%w：推流源必须为 pattern、file、upload 或 browser", ErrInvalid)
	}

	if c.SourceKind == SourceFile {
		if c.FilePath == "" {
			return fmt.Errorf("%w：请填写本机录像文件路径", ErrInvalid)
		}
		st, err := os.Stat(c.FilePath)
		if err != nil || st.IsDir() {
			return fmt.Errorf("%w：本机录像文件不存在或不可读：%s", ErrInvalid, c.FilePath)
		}
		if c.SourceName == "" {
			c.SourceName = filepath.Base(c.FilePath)
		}
	}

	if c.Loop == nil {
		t := true
		c.Loop = &t
	}
	if c.LoopCount < 0 {
		c.LoopCount = 0
	}

	if c.VideoMode == "" {
		if c.SourceKind == SourcePattern || c.SourceKind == SourceBrowser || c.Protocol == ProtocolWHIP {
			c.VideoMode = "h264"
		} else {
			c.VideoMode = "copy"
		}
	}
	if c.VideoMode != "copy" && c.VideoMode != "h264" {
		return fmt.Errorf("%w：视频编码模式仅支持 copy 或 h264", ErrInvalid)
	}
	if c.SourceKind == SourcePattern || c.SourceKind == SourceBrowser {
		c.VideoMode = "h264"
	}

	if c.AudioMode == "" {
		switch c.Protocol {
		case ProtocolWHIP:
			c.AudioMode = "opus"
		default:
			if c.SourceKind == SourcePattern || c.SourceKind == SourceBrowser {
				c.AudioMode = "aac"
			} else {
				c.AudioMode = "copy"
			}
		}
	}
	switch c.AudioMode {
	case "copy", "aac", "opus", "none":
	default:
		return fmt.Errorf("%w：音频编码模式仅支持 copy、aac、opus 或 none", ErrInvalid)
	}
	if c.Protocol == ProtocolWHIP && c.AudioMode != "none" {
		c.AudioMode = "opus"
	}
	if (c.SourceKind == SourcePattern || c.SourceKind == SourceBrowser) && c.AudioMode == "copy" {
		if c.Protocol == ProtocolWHIP {
			c.AudioMode = "opus"
		} else {
			c.AudioMode = "aac"
		}
	}

	if c.VideoBitrateKbps <= 0 {
		c.VideoBitrateKbps = defaultBitrate
	}
	if c.VideoBitrateKbps > 50000 {
		c.VideoBitrateKbps = 50000
	}
	if c.FPS <= 0 {
		c.FPS = defaultFPS
	}
	if c.FPS > 120 {
		c.FPS = 120
	}
	if c.Resolution == "" {
		c.Resolution = defaultRes
	}
	if !resolutionRegexp.MatchString(c.Resolution) {
		return fmt.Errorf("%w：分辨率格式应为 宽x高（如 1280x720）", ErrInvalid)
	}

	if c.SourceName == "" {
		switch c.SourceKind {
		case SourcePattern:
			c.SourceName = fmt.Sprintf("测试画面 (%s@%dfps)", c.Resolution, c.FPS)
		case SourceBrowser:
			c.SourceName = "浏览器采集"
		default:
			c.SourceName = filepath.Base(c.FilePath)
		}
	}
	if c.Name == "" {
		c.Name = strings.ToUpper(string(c.Protocol)) + " · " + c.SourceName
	}
	return nil
}

type TaskSnapshot struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Protocol      Protocol   `json:"protocol"`
	TargetURL     string     `json:"target_url"`
	RTSPTransport string     `json:"rtsp_transport,omitempty"`
	SourceKind    SourceKind `json:"source_kind"`
	SourceName    string     `json:"source_name"`
	Loop          bool       `json:"loop"`
	LoopCount     int        `json:"loop_count"`
	VideoMode     string     `json:"video_mode"`
	AudioMode     string     `json:"audio_mode"`
	Status        TaskStatus `json:"status"`
	ICEState      string     `json:"ice_state,omitempty"`
	FramesSent    int64      `json:"frames_sent"`
	BytesSent     int64      `json:"bytes_sent"`
	FPS           float64    `json:"fps"`
	BitrateKbps   float64    `json:"bitrate_kbps"`
	OutTimeMS     int64      `json:"out_time_ms"`
	ElapsedMS     int64      `json:"elapsed_ms"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    time.Time  `json:"finished_at,omitempty"`
	Error         string     `json:"error,omitempty"`
	RecentLogs    []string   `json:"recent_logs,omitempty"`
}

func redactTargetURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.User != nil {
		if _, hasPass := u.User.Password(); hasPass {
			u.User = url.UserPassword(u.User.Username(), "***")
		}
	}
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			low := strings.ToLower(k)
			if strings.Contains(low, "token") || strings.Contains(low, "key") ||
				strings.Contains(low, "secret") || strings.Contains(low, "pass") ||
				strings.Contains(low, "auth") {
				q.Set(k, "***")
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}
