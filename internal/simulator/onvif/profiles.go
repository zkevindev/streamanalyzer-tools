package onvif

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"streamanalyzer/internal/simulator/mediafile"
	"streamanalyzer/internal/simulator/testpattern"

	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

//go:generate ffmpeg -hide_banner -loglevel error -y -f lavfi -i testsrc2=size=160x90:rate=10 -t 2 -c:v libx264 -threads 2 -pix_fmt yuv420p -preset fast -crf 33 -x264-params aud=1:repeat-headers=1:keyint=20:scenecut=0:bframes=0 -f h264 sub.h264
//go:embed sub.h264
var subPattern []byte

const MaxVideoBytes = 64 << 20
const maxAnnexBBytes = 32 << 20

type LocalVideo struct {
	Name   string
	Reader io.Reader
}

type Profile struct {
	Token     string `json:"token"`
	Name      string `json:"name"`
	StreamURL string `json:"stream_url"`
	Filename  string `json:"filename,omitempty"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	FPS       int    `json:"fps"`
}

type profileVideo struct {
	Profile
	path   string
	number string
	frames []h264.AnnexB
	avc    *format.H264
}

func prepareProfiles(ctx context.Context, files [2]LocalVideo) ([]*profileVideo, error) {
	main := &profileVideo{Profile: Profile{Token: "profile-1", Name: "Main Stream", Width: testpattern.Width, Height: testpattern.Height, FPS: testpattern.FPS}, path: "/live", number: "1"}
	sub := &profileVideo{Profile: Profile{Token: "profile-2", Name: "Sub Stream", Width: 160, Height: 90, FPS: 10}, path: "/sub", number: "2"}
	profiles := []*profileVideo{main, sub}
	var builtin bytes.Buffer
	for i := range testpattern.Len() {
		builtin.Write(testpattern.AccessUnit(i))
	}
	patterns := [][]byte{builtin.Bytes(), subPattern}
	for i, profile := range profiles {
		raw := patterns[i]
		if file := files[i]; file.Reader != nil {
			var err error
			raw, err = loadVideo(ctx, file.Reader, profile)
			if err != nil {
				return nil, fmt.Errorf("%w，%s：%v", ErrConfig, profile.Name, err)
			}
			profile.Filename = filepath.Base(strings.ReplaceAll(file.Name, "\\", "/"))
		}
		if err := profile.parse(raw); err != nil {
			return nil, fmt.Errorf("%w，%s：%v", ErrConfig, profile.Name, err)
		}
	}
	return profiles, nil
}

func loadVideo(parent context.Context, input io.Reader, p *profileVideo) ([]byte, error) {
	ffmpeg, err := mediafile.FindTool("ffmpeg", "")
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp("", "streamanalyzer-onvif-*.video")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	n, err := io.Copy(file, io.LimitReader(input, MaxVideoBytes+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil || n < 1 || n > MaxVideoBytes {
		return nil, fmt.Errorf("所选视频必须可读且不超过 64 MiB")
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	scale := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=%d", p.Width, p.Height, p.Width, p.Height, p.FPS)
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin", "-protocol_whitelist", "file",
		"-format_whitelist", "mov,matroska,webm,flv,mpegts,avi,mpeg", "-i", file.Name(), "-map", "0:V:0",
		"-an", "-vf", scale, "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", "-profile:v", "high", "-preset", "fast",
		"-crf", "32", "-x264-params", "aud=1:repeat-headers=1:scenecut=0:bframes=0:keyint="+strconv.Itoa(p.FPS*2), "-f", "h264", "pipe:1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(out, maxAnnexBBytes+1))
	if readErr != nil || len(raw) > maxAnnexBBytes {
		cancel()
	}
	waitErr := cmd.Wait()
	if len(raw) > maxAnnexBBytes {
		return nil, fmt.Errorf("转码后的视频超过 32 MiB")
	}
	if readErr != nil || waitErr != nil || len(raw) == 0 {
		return nil, fmt.Errorf("FFmpeg 未能生成完整视频，请检查格式或文件大小")
	}
	return raw, nil
}

func (p *profileVideo) parse(raw []byte) error {
	aud := []byte{0, 0, 0, 1, 9}
	if !bytes.HasPrefix(raw, aud) {
		return fmt.Errorf("H.264 视频缺少访问单元边界")
	}
	p.avc = &format.H264{PayloadTyp: 96, PacketizationMode: 1}
	for len(raw) != 0 {
		end := len(raw)
		if offset := bytes.Index(raw[len(aud):], aud); offset >= 0 {
			end = offset + len(aud)
		}
		var frame h264.AnnexB
		if err := frame.Unmarshal(raw[:end]); err != nil {
			return err
		}
		p.frames = append(p.frames, frame)
		raw = raw[end:]
	}
	for _, nalu := range p.frames[0] {
		switch h264.NALUType(nalu[0] & 31) {
		case h264.NALUTypeSPS:
			p.avc.SPS = nalu
		case h264.NALUTypePPS:
			p.avc.PPS = nalu
		}
	}
	if len(p.avc.SPS) == 0 || len(p.avc.PPS) == 0 {
		return fmt.Errorf("H.264 视频缺少 SPS 或 PPS")
	}
	return nil
}
