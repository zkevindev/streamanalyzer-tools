package offline

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/yutopp/go-flv"
	"github.com/yutopp/go-flv/tag"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
)

func TestRunFLVProfileAndEnhancedAVC(t *testing.T) {
	sps := baselineSPS(320, 240)
	meta, err := codec.ParseH264SPS(sps)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Width != 320 || meta.Height != 240 || meta.ProfileName != "Baseline" || meta.Level != 31 {
		t.Fatalf("sps meta: %+v", meta)
	}
	pps := []byte{0x68, 0xce, 0x06, 0xe2}
	avcc := buildAVCC(sps, pps)

	dir := t.TempDir()
	input := filepath.Join(dir, "sample.flv")
	if err := writeLegacyFLV(input, avcc); err != nil {
		t.Fatal(err)
	}

	summary := &models.OfflineSummary{}
	mgr := NewManager(dir)
	if err := mgr.runFLV(&models.OfflineTask{InputPath: input}, filepath.Join(dir, "out"), summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Flows) != 1 {
		t.Fatalf("flows: %+v", summary.Flows)
	}
	flow := summary.Flows[0]
	if flow.VideoCodec != "h264" || flow.AudioCodec != "aac" {
		t.Fatalf("codecs video=%s audio=%s", flow.VideoCodec, flow.AudioCodec)
	}
	if flow.Profile == nil || flow.Profile.Width != 320 || flow.Profile.Height != 240 || flow.Profile.LevelName != "3.1" {
		t.Fatalf("profile: %+v", flow.Profile)
	}
	if flow.Profile.AudioSampleRate != 44100 || flow.Profile.AudioChannels != 2 {
		t.Fatalf("audio: %+v", flow.Profile)
	}
	if flow.Profile.VideoFrames != 2 || flow.Profile.AudioFrames != 1 || flow.Profile.Keyframes != 1 {
		t.Fatalf("frames: %+v", flow.Profile)
	}
	if flow.Profile.DurationMS != 40 {
		t.Fatalf("duration %d", flow.Profile.DurationMS)
	}
	if flow.VideoPath == "" || flow.AudioPath == "" {
		t.Fatalf("missing elementary streams: %+v", flow)
	}

	enhancedDir := filepath.Join(dir, "enhanced")
	if err := os.MkdirAll(enhancedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ext := &flvExtractor{outDir: enhancedDir, avccNALUSize: 4, hvcNALUSize: 4}
	var header bytes.Buffer
	header.WriteByte(0x90)
	header.WriteString("avc1")
	header.Write(avcc)
	var video tag.VideoData
	if err := tag.DecodeVideoData(&header, &video); err != nil {
		t.Fatal(err)
	}
	if _, err := ext.handleVideo(0, &video); err != nil {
		t.Fatal(err)
	}
	if ext.videoCodec != "h264" || ext.videoMeta == nil || ext.videoMeta.Width != 320 {
		t.Fatalf("enhanced sequence: codec=%s meta=%+v", ext.videoCodec, ext.videoMeta)
	}

	var coded bytes.Buffer
	coded.WriteByte(0x91)
	coded.WriteString("avc1")
	coded.Write([]byte{0, 0, 0})
	nal := []byte{0x65, 0x88, 0x84, 0x00}
	coded.Write([]byte{0, 0, 0, byte(len(nal))})
	coded.Write(nal)
	var frame tag.VideoData
	if err := tag.DecodeVideoData(&coded, &frame); err != nil {
		t.Fatal(err)
	}
	detail, err := ext.handleVideo(40, &frame)
	if err != nil || detail == nil || detail.MediaType != "video" || detail.DTS != 40 {
		t.Fatalf("enhanced frame: %+v %v", detail, err)
	}
}

func TestRunFLVAndTSFixtures(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	flvSummary := &models.OfflineSummary{}
	if err := mgr.runFLV(&models.OfflineTask{InputPath: filepath.Join("..", "simulator", "mediasource", "fixtures", "regular.flv")}, filepath.Join(dir, "flv"), flvSummary); err != nil {
		t.Fatal(err)
	}
	if len(flvSummary.Flows) != 1 || flvSummary.Flows[0].Profile == nil || flvSummary.Flows[0].Profile.VideoFrames == 0 {
		t.Fatalf("flv fixture: %+v", flvSummary.Flows)
	}
	if flvSummary.Flows[0].Profile.Width == 0 || flvSummary.Flows[0].VideoCodec == "" {
		t.Fatalf("flv fixture profile: %+v codec=%s", flvSummary.Flows[0].Profile, flvSummary.Flows[0].VideoCodec)
	}
	tsSummary := &models.OfflineSummary{}
	if err := mgr.runTS(&models.OfflineTask{InputPath: filepath.Join("..", "simulator", "mediasource", "fixtures", "regular.ts")}, filepath.Join(dir, "ts"), tsSummary); err != nil {
		t.Fatal(err)
	}
	if len(tsSummary.Flows) != 1 || tsSummary.Flows[0].Profile == nil {
		t.Fatalf("ts fixture: %+v", tsSummary.Flows)
	}
	profile := tsSummary.Flows[0].Profile
	if profile.VideoFrames == 0 || tsSummary.Flows[0].VideoCodec == "" || profile.DurationMS <= 0 {
		t.Fatalf("ts profile codec=%s %+v", tsSummary.Flows[0].VideoCodec, profile)
	}
	if profile.Width != 160 || profile.AudioSampleRate != 48000 {
		t.Fatalf("ts codec parameters: %+v", profile)
	}
	if tsSummary.Flows[0].VideoPIDCount == 0 || tsSummary.Flows[0].PATCount == 0 {
		t.Fatalf("ts structure: %+v", tsSummary.Flows[0])
	}

	mp4Summary := &models.OfflineSummary{}
	err := mgr.runMP4(&models.OfflineTask{InputPath: filepath.Join("..", "simulator", "hlsorigin", "avmedia", "video", "init.mp4")}, filepath.Join(dir, "mp4"), mp4Summary)
	if err != nil {
		t.Fatal(err)
	}
	if len(mp4Summary.Flows) != 1 || mp4Summary.Flows[0].Profile == nil || mp4Summary.Flows[0].Profile.Width == 0 {
		t.Fatalf("mp4 init: err=%v flow=%+v", err, mp4Summary.Flows)
	}
	if mp4Summary.Flows[0].Profile.Profile != "Baseline" || mp4Summary.Flows[0].VideoCodec != "h264" {
		t.Fatalf("mp4 profile: %+v", mp4Summary.Flows[0].Profile)
	}
}

func writeLegacyFLV(path string, avcc []byte) error {
	buf := &bytes.Buffer{}
	enc, err := flv.NewEncoder(buf, flv.FlagsAudio|flv.FlagsVideo)
	if err != nil {
		return err
	}
	if err := enc.Encode(&tag.FlvTag{TagType: tag.TagTypeVideo, Timestamp: 0, Data: &tag.VideoData{
		FrameType:     tag.FrameTypeKeyFrame,
		CodecID:       tag.CodecIDAVC,
		AVCPacketType: tag.AVCPacketTypeSequenceHeader,
		Data:          bytes.NewReader(avcc),
	}}); err != nil {
		return err
	}
	for _, ts := range []uint32{0, 40} {
		frameType := tag.FrameTypeInterFrame
		nal := []byte{0x41, 0x9a, 0x00, 0x01}
		if ts == 0 {
			frameType = tag.FrameTypeKeyFrame
			nal = []byte{0x65, 0x88, 0x84, 0x21}
		}
		payload := append([]byte{0, 0, 0, byte(len(nal))}, nal...)
		if err := enc.Encode(&tag.FlvTag{TagType: tag.TagTypeVideo, Timestamp: ts, Data: &tag.VideoData{
			FrameType:     frameType,
			CodecID:       tag.CodecIDAVC,
			AVCPacketType: tag.AVCPacketTypeNALU,
			Data:          bytes.NewReader(payload),
		}}); err != nil {
			return err
		}
	}
	asc := []byte{0x12, 0x10}
	if err := enc.Encode(&tag.FlvTag{TagType: tag.TagTypeAudio, Timestamp: 0, Data: &tag.AudioData{
		SoundFormat:   tag.SoundFormatAAC,
		AACPacketType: tag.AACPacketTypeSequenceHeader,
		Data:          bytes.NewReader(asc),
	}}); err != nil {
		return err
	}
	if err := enc.Encode(&tag.FlvTag{TagType: tag.TagTypeAudio, Timestamp: 0, Data: &tag.AudioData{
		SoundFormat:   tag.SoundFormatAAC,
		AACPacketType: tag.AACPacketTypeRaw,
		Data:          bytes.NewReader([]byte{0x11, 0x22, 0x33, 0x44}),
	}}); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func buildAVCC(sps, pps []byte) []byte {
	out := []byte{1, sps[1], sps[2], sps[3], 0xff, 0xe1, byte(len(sps) >> 8), byte(len(sps))}
	out = append(out, sps...)
	out = append(out, 1, byte(len(pps)>>8), byte(len(pps)))
	return append(out, pps...)
}

type bitBuf struct {
	buf []byte
	n   int
}

func (b *bitBuf) write(v uint64, n int) {
	for i := n - 1; i >= 0; i-- {
		if b.n%8 == 0 {
			b.buf = append(b.buf, 0)
		}
		if ((v >> uint(i)) & 1) == 1 {
			b.buf[len(b.buf)-1] |= 1 << (7 - uint(b.n%8))
		}
		b.n++
	}
}

func (b *bitBuf) ue(v uint64) {
	v++
	width := 0
	for x := v; x > 0; x >>= 1 {
		width++
	}
	b.write(0, width-1)
	b.write(v, width)
}

func baselineSPS(width, height int) []byte {
	var b bitBuf
	b.write(0x67, 8)
	b.write(66, 8)
	b.write(0, 8)
	b.write(31, 8)
	b.ue(0)
	b.ue(0)
	b.ue(0)
	b.ue(0)
	b.ue(0)
	b.write(0, 1)
	b.ue(uint64(width/16 - 1))
	b.ue(uint64(height/16 - 1))
	b.write(1, 1)
	b.write(1, 1)
	b.write(0, 1)
	b.write(0, 1)
	b.write(1, 1)
	for b.n%8 != 0 {
		b.write(0, 1)
	}
	return b.buf
}
