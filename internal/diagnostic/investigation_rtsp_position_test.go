package diagnostic

import (
	"bytes"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

// Content, not just a decodable JPEG, must follow the saved media clock. At
// 1.5 s the source is blue; using the 2.5 s stall/capture clock would be green.
func TestRTSPStallScreenshotContentFollowsMediaClock(t *testing.T) {
	tool := rtspG711TestTool(t)
	source := filepath.Join(t.TempDir(), "colors.h264")
	args := []string{"-hide_banner", "-loglevel", "error"}
	for _, color := range []string{"red", "blue", "lime"} {
		args = append(args, "-f", "lavfi", "-i", "color=c="+color+":s=96x64:r=10:d=1")
	}
	args = append(args, "-filter_complex", "[0:v][1:v][2:v]concat=n=3:v=1:a=0,format=yuv420p", "-c:v", "libx264", "-preset", "ultrafast", "-x264-params", "keyint=10:min-keyint=10:scenecut=0:aud=1", "-threads", "1", "-an", "-f", "h264", "-y", source)
	if err := investigationMediaRun(t.Context(), tool, args, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	var frames [][][]byte
	positions := []int{0}
	first := true
	for offset := 0; offset+4 < len(raw); {
		next := bytes.Index(raw[offset:], []byte{0, 0, 1, 9})
		if next < 0 {
			break
		}
		position := offset + next
		start := position
		if start > 0 && raw[start-1] == 0 {
			start--
		}
		if !first {
			positions = append(positions, start)
		}
		first = false
		offset = position + 4
	}
	positions = append(positions, len(raw))
	for index := 0; index < len(positions)-1; index++ {
		var nalus h264.AnnexB
		if err := nalus.Unmarshal(raw[positions[index]:positions[index+1]]); err != nil {
			t.Fatal(err)
		}
		frames = append(frames, nalus)
	}
	if len(frames) != 30 {
		t.Fatalf("unexpected color source length: %d", len(frames))
	}
	encoder := &rtph264.Encoder{PayloadType: 96, PacketizationMode: 1}
	if err := encoder.Init(); err != nil {
		t.Fatal(err)
	}
	var packets []rtspIntegrationPacket
	sequence := uint16(100)
	for index, frame := range frames {
		encoded, err := encoder.Encode(frame)
		if err != nil {
			t.Fatal(err)
		}
		for _, packet := range encoded {
			packet.Version, packet.SSRC, packet.SequenceNumber, packet.Timestamp = 2, 7, sequence, uint32(index*9000)
			sequence++
			data, err := packet.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			packets = append(packets, rtspIntegrationPacket{raw: data, delay: time.Millisecond})
		}
	}
	dir, report, item := investigationRTSPCapturePackets(t, rtspIntegrationVideoSDP(), packets)
	report.Player.BufferSamples[0].CurrentMS = 1500
	media := extractStallMedia(t.Context(), dir, report, item)
	if (media.Status != "ready" && media.Status != "partial") || len(media.Assets) != 4 || media.RTSP == nil || media.RTSP.AnchorCurrentMS != 1500 || media.RTSP.AnchorSampleOffsetMS != 2400 {
		t.Fatalf("color source supplementation failed: %+v", media)
	}
	for _, tc := range []struct {
		name    string
		channel int
	}{{"before.jpg", 0}, {"during.jpg", 2}, {"after.jpg", 1}} {
		file, err := os.Open(filepath.Join(dir, "stalls", item.ID, tc.name))
		if err != nil {
			t.Fatal(err)
		}
		image, err := jpeg.Decode(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		bounds := image.Bounds()
		r, g, b, _ := image.At(bounds.Dx()/2, bounds.Dy()/2).RGBA()
		channels := []uint32{r, g, b}
		if channels[tc.channel] < 40_000 || channels[(tc.channel+1)%3] > 10_000 || channels[(tc.channel+2)%3] > 10_000 {
			t.Errorf("%s picked the wrong source content: RGB=%v", tc.name, channels)
		}
	}
}
