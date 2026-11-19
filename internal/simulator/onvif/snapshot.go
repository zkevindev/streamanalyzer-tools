package onvif

import (
	"bytes"
	"context"
	_ "embed"
	"image"
	"image/jpeg"
	"math"
	"net/http"
	"os/exec"
	"time"

	"streamanalyzer/internal/simulator/mediafile"
)

//go:generate ffmpeg -hide_banner -loglevel error -y -i ../testpattern/pattern.h264 -frames:v 1 -q:v 3 -update 1 main.jpg
//go:embed main.jpg
var mainSnapshot []byte

//go:generate ffmpeg -hide_banner -loglevel error -y -i sub.h264 -frames:v 1 -q:v 3 -update 1 sub.jpg
//go:embed sub.jpg
var subSnapshot []byte

func loadSnapshot(ctx context.Context, p *profileVideo, builtin []byte, local bool) error {
	data := builtin
	if local {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		ffmpeg, err := mediafile.FindTool("ffmpeg", "")
		if err != nil {
			return err
		}
		first, err := p.frames[0].Marshal()
		if err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin", "-f", "h264", "-i", "pipe:0",
			"-frames:v", "1", "-threads", "1", "-c:v", "mjpeg", "-q:v", "3", "-f", "image2pipe", "pipe:1")
		cmd.Stdin = bytes.NewReader(first)
		data, err = cmd.Output()
		if err != nil {
			return err
		}
	}
	frame, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	p.still, p.jpeg = frame, data
	return nil
}

func (c *camera) serveSnapshot(w http.ResponseWriter, r *http.Request) {
	var profile *profileVideo
	for _, p := range c.profiles {
		if p.Token == r.PathValue("profile") {
			profile = p
			break
		}
	}
	if profile == nil {
		http.NotFound(w, r)
		return
	}

	c.ptzMu.Lock()
	profile.ptz.advance()
	pose := profile.ptz.position
	c.ptzMu.Unlock()

	data := profile.jpeg
	if pose != (ptzPose{}) {
		var out bytes.Buffer
		if err := jpeg.Encode(&out, cropSnapshot(profile.still, pose), &jpeg.Options{Quality: 90}); err != nil {
			http.Error(w, "Unable to render snapshot", http.StatusInternalServerError)
			return
		}
		data = out.Bytes()
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

func cropSnapshot(src image.Image, pose ptzPose) *image.RGBA {
	b := src.Bounds()
	width, height := b.Dx(), b.Dy()
	scale := 1 / (1 + 3*max(pose.zoom, math.Max(math.Abs(pose.pan), math.Abs(pose.tilt))/4))
	cw, ch := max(1, int(float64(width)*scale)), max(1, int(float64(height)*scale))
	x0 := b.Min.X + int(float64(width-cw)*(1+pose.pan)/2)
	y0 := b.Min.Y + int(float64(height-ch)*(1-pose.tilt)/2)
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			out.Set(x, y, src.At(x0+x*cw/width, y0+y*ch/height))
		}
	}
	return out
}
