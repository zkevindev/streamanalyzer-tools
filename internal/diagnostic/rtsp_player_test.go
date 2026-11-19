package diagnostic

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/onvif"
	"streamanalyzer/internal/simulator/testpattern"

	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtpmpeg4audio"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"github.com/pion/rtp"
)

func TestRTSPPreviewH264FragmentedRTPAndInbandParameters(t *testing.T) {
	player := newPlayerSession(t.Context(), ProtocolRTSP, nil, nil)
	defer player.Close()
	preview := newRTSPPreview(player)
	preview.video = &format.H264{PacketizationMode: 1}
	preview.videoDecoder = &rtph264.Decoder{PacketizationMode: 1}
	if err := preview.videoDecoder.Init(); err != nil {
		t.Fatal(err)
	}
	preview.dts.Initialize()
	encoder := &rtph264.Encoder{PayloadType: 96, PacketizationMode: 1, PayloadMaxSize: 300}
	if err := encoder.Init(); err != nil {
		t.Fatal(err)
	}
	var wantAU h264.AnnexB
	if err := wantAU.Unmarshal(testpattern.AccessUnit(0)); err != nil {
		t.Fatal(err)
	}
	for frame := 0; frame < 3; frame++ {
		var au h264.AnnexB
		if err := au.Unmarshal(testpattern.AccessUnit(frame)); err != nil {
			t.Fatal(err)
		}
		packets, err := encoder.Encode(au)
		if err != nil {
			t.Fatal(err)
		}
		if frame == 0 && len(packets) < 2 {
			t.Fatal("fixture must exercise FU-A and aggregation")
		}
		for _, packet := range packets {
			packet.Timestamp = uint32(frame * 6000)
			preview.writeVideo(rtspPreviewPacket{packet: packet, pts: int64(frame * 6000), video: true})
		}
	}
	if preview.report.VideoAUs != 3 || preview.report.State != "ready" || preview.report.PacketErrors != 0 {
		t.Fatalf("H264 remux failed: %+v", preview.report)
	}
	prefix, _, stop, err := player.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	raw := bytes.Join(prefix, nil)
	var bodies [][]byte
	for len(raw) > 0 {
		if len(raw) < 15 {
			t.Fatal("truncated tag")
		}
		size := int(raw[1])<<16 | int(raw[2])<<8 | int(raw[3])
		if size+15 > len(raw) {
			t.Fatal("invalid tag size")
		}
		if binary.BigEndian.Uint32(raw[11+size:]) != uint32(11+size) {
			t.Fatal("invalid FLV previous tag size")
		}
		bodies = append(bodies, raw[11:11+size])
		raw = raw[15+size:]
	}
	if len(bodies) != 4 || bodies[0][1] != 0 || bodies[1][0] != 0x17 || bodies[1][1] != 1 || bodies[2][0] != 0x27 {
		t.Fatalf("missing AVC config/IDR/interframe: %v", bodies)
	}
	var gotAU h264.AVCC
	if err := gotAU.Unmarshal(bodies[1][5:]); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([][]byte(gotAU), [][]byte(wantAU)) {
		t.Fatal("depacketized H264 bytes changed")
	}
}

func TestRTSPPreviewAACMultiAUUsesSharedReceiverClock(t *testing.T) {
	player := newPlayerSession(t.Context(), ProtocolRTSP, nil, nil)
	defer player.Close()
	preview := newRTSPPreview(player)
	preview.audio = &format.MPEG4Audio{Config: &mpeg4audio.AudioSpecificConfig{Type: mpeg4audio.ObjectTypeAACLC, SampleRate: 48000, ChannelConfig: 2}, SizeLength: 13, IndexLength: 3, IndexDeltaLength: 3}
	decoder, err := preview.audio.CreateDecoder()
	if err != nil {
		t.Fatal(err)
	}
	preview.audioDecoder = decoder
	encoder := &rtpmpeg4audio.Encoder{PayloadType: 97, SizeLength: 13, IndexLength: 3, IndexDeltaLength: 3}
	if err := encoder.Init(); err != nil {
		t.Fatal(err)
	}
	aus := [][]byte{{1, 2, 3}, {4, 5, 6}}
	packets, err := encoder.Encode(aus)
	if err != nil {
		t.Fatal(err)
	}
	for _, packet := range packets {
		preview.writeAudio(rtspPreviewPacket{packet: packet, pts: 48000})
	}
	if preview.report.AudioAUs != 2 || preview.report.State != "ready" {
		t.Fatalf("AAC remux failed: %+v", preview.report)
	}
	prefix, _, stop, err := player.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if len(prefix) != 3 {
		t.Fatalf("want ASC + two AUs, got %d", len(prefix))
	}
	for index, want := range []uint32{3000, 3000, 3021} {
		chunk := prefix[index]
		at := uint32(chunk[7])<<24 | uint32(chunk[4])<<16 | uint32(chunk[5])<<8 | uint32(chunk[6])
		if at != want {
			t.Fatalf("AAC AU %d clock %d want %d; must not independently zero audio", index, at, want)
		}
	}
	if !bytes.Equal(prefix[1][13:16], aus[0]) || !bytes.Equal(prefix[2][13:16], aus[1]) {
		t.Fatal("AAC AU bytes changed")
	}
}

func TestRTSPPreviewQueueBoundDoesNotBlockNativeReceiver(t *testing.T) {
	player := newPlayerSession(t.Context(), ProtocolRTSP, nil, nil)
	defer player.Close()
	preview := newRTSPPreview(player)
	packet := &rtp.Packet{Header: rtp.Header{Version: 2}, Payload: []byte{1}}
	start := time.Now()
	for range 1000 {
		preview.enqueue(packet, 0, true)
	}
	if time.Since(start) > time.Second || len(preview.queue) > 128 || preview.report.DroppedPackets != 1 || preview.report.ReasonCode != "queue_limit" || player.PreviewFailure() != "queue_limit" {
		t.Fatalf("preview queue failed to isolate native capture: %+v", preview.report)
	}
}

func TestRTSPPreviewSameCaptureAndOfflinePlayerRoundTrip(t *testing.T) {
	for _, transport := range []string{"tcp", "udp"} {
		t.Run(transport, func(t *testing.T) { rtspPreviewSameCaptureRoundTrip(t, transport) })
	}
}
func rtspPreviewSameCaptureRoundTrip(t *testing.T, transport string) {
	manager := &onvif.Manager{}
	t.Cleanup(manager.Close)
	camera, err := manager.Start(onvif.Config{BindIP: "127.0.0.1", RTSPUsername: "preview-user", RTSPPassword: "preview-private"})
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(camera.StreamURL)
	target.User = url.UserPassword("preview-user", "preview-private")
	service := rtspIntegrationOpen(t)
	run, err := service.Start(SampleRequest{URL: target.String(), Protocol: ProtocolRTSP, Transport: transport, Seconds: 4, Playback: true})
	if err != nil {
		t.Fatal(err)
	}
	if run.Player == nil || run.Player.Transport != "flv" || run.Player.Source != "same_capture" {
		t.Fatalf("missing native preview grant: %+v", run.Player)
	}
	player, err := service.PlayerSession(run.ID, run.Player.Token)
	if err != nil {
		t.Fatal(err)
	}
	prefix, live, stop, err := player.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	consumed := make(chan []byte, 1)
	go func() {
		var raw []byte
		for _, chunk := range prefix {
			raw = append(raw, chunk...)
		}
		for chunk := range live {
			raw = append(raw, chunk...)
		}
		consumed <- raw
	}()
	detail := rtspIntegrationAwait(t, service, run.ID)
	raw := <-consumed
	preview := detail.Report.RTSP.Preview
	if manager.Status().RTSPPlays != 1 || preview == nil || preview.State != "ready" || preview.VideoCodec != "h264" || preview.VideoAUs < 5 || !bytes.HasPrefix(raw, []byte("FLV")) {
		t.Fatalf("preview did not use single native PLAY: preview=%+v plays=%d len=%d", preview, manager.Status().RTSPPlays, len(raw))
	}
	if detail.Report.Metrics.VideoFrames != 0 || detail.Report.Metrics.FirstVideoMS != nil {
		t.Fatal("native RTP statistics fabricated decoded frames")
	}
	record := PlayerRecording{Version: 4, Revision: 1, StartedAt: detail.Report.Manifest.Observation.StartedAt, ElapsedMS: 1500, Complete: true, Engine: "mpegtsjs", Source: "same_capture", Events: []PlayerEvent{{Kind: "start"}, {AtMS: 100, Kind: "playing"}, {AtMS: 120, Kind: "first_frame", Method: "video_frame_callback"}, {AtMS: 400, Kind: "waiting"}, {AtMS: 1000, Kind: "playing"}, {AtMS: 1500, Kind: "stop"}}, Samples: []PlayerSample{{AtMS: 200, ReadyState: 4, BufferMS: 500}, {AtMS: 500, ReadyState: 2, BufferMS: 0}}}
	if _, err := service.SavePlayerRecording(run.ID, run.Player.Token, record); err != nil {
		t.Fatal(err)
	}
	detail, err = service.Detail(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Report.Manifest.Version != rtspPlayerSchemaVersion || detail.Report.Player.FirstScreenMS == nil || detail.Report.Player.StallCount != 1 {
		t.Fatalf("native browser report missing: %+v", detail.Report.Player)
	}
	manager.Close()
	files := rtspIntegrationRoundTrip(t, service, detail)
	var metadata rtspArchiveMetadata
	if err := json.Unmarshal(files[rtspMetadataName], &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Version != rtspPreviewArchiveVersion || !reflect.DeepEqual(metadata.Preview, preview) {
		t.Fatal("native archive lost recorded pipeline result")
	}
	if transport == "udp" {
		if len(files) != 5 || metadata.Transport != "udp" || len(metadata.UDPTransports) != 1 || metadata.ControlPeerIP == "" || detail.Report.RTSP.Transport != "udp" || detail.Report.RTSP.AcceptedDatagrams == 0 {
			t.Fatalf("preview dropped UDP raw/provenance fields: metadata=%+v metrics=%+v", metadata, detail.Report.RTSP)
		}
		changed := map[string][]byte{}
		for name, data := range files {
			changed[name] = bytes.Clone(data)
		}
		metadata.UDPTransports[0].RTPServerPort = metadata.UDPTransports[0].RTPServerPort%65535 + 1
		changed[rtspMetadataName], _ = json.Marshal(metadata)
		rtspUDPTestWriteArchive(t, service.runDir(run.ID), changed)
		if _, err := replayRTSP(service.runDir(run.ID), detail.Report.Manifest); !errors.Is(err, ErrInvalid) {
			t.Fatalf("preview v3 bypassed negotiated UDP peer proof: %v", err)
		}
	} else if len(files) != 3 || metadata.Transport != "" {
		t.Fatal("TCP preview archive gained UDP evidence")
	}
	// The native raw download is still the source archive, never a derived FLV.
	if !bytes.Equal(files[rtspWireName], nil) && bytes.HasPrefix(files[rtspWireName], []byte("FLV")) {
		t.Fatal("raw RTSP dump replaced by FLV")
	}
	data, err := os.ReadFile(filepath.Join(service.runDir(run.ID), playerName))
	if err != nil || !bytes.Contains(data, []byte("first_frame")) {
		t.Fatal("raw browser evidence not saved")
	}
}

func TestRTSPPreviewUnsupportedAndSetupFailureRemainReviewable(t *testing.T) {
	for _, tc := range []struct {
		name, sdp     string
		status        int
		state, reason string
	}{
		{"vp8", rtspIntegrationSDP + "m=video 0 RTP/AVP 96\r\na=rtpmap:96 VP8/90000\r\na=control:trackID=0\r\n", 0, "unsupported", "codec_unsupported"},
		{"setup", rtspIntegrationVideoSDP(), 461, "failed", "capture_setup_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := rtspIntegrationConfig{sdp: tc.sdp, closeAfterPlay: true}
			config.setupStatus = base.StatusCode(tc.status)
			fixture := newRTSPIntegrationFixture(t, config)
			service := rtspIntegrationOpen(t)
			run, err := service.Start(SampleRequest{URL: fixture.address(), Protocol: ProtocolRTSP, Seconds: 1, Playback: true})
			if err != nil {
				t.Fatal(err)
			}
			detail := rtspIntegrationAwait(t, service, run.ID)
			preview := detail.Report.RTSP.Preview
			if preview == nil || preview.State != tc.state || preview.ReasonCode != tc.reason {
				t.Fatalf("wrong controlled preview failure: %+v", preview)
			}
			rtspIntegrationRoundTrip(t, service, detail)
		})
	}
}

// These short parser vectors exercise changing POC order. They do not stand
// for decodable pictures; the full pattern test above preserves real NALUs.
func TestRTSPPreviewPreservesBFramePTSAndMonotonicDTS(t *testing.T) {
	player := newPlayerSession(t.Context(), ProtocolRTSP, nil, nil)
	defer player.Close()
	preview := newRTSPPreview(player)
	preview.video = &format.H264{PacketizationMode: 1}
	preview.videoDecoder = &rtph264.Decoder{PacketizationMode: 1}
	if err := preview.videoDecoder.Init(); err != nil {
		t.Fatal(err)
	}
	preview.dts.Initialize()
	preview.sps = []byte{0x67, 0x64, 0x00, 0x28, 0xac, 0xd9, 0x40, 0x78, 0x02, 0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc6, 0x58}
	preview.pps = []byte{0x68, 0xeb, 0xe3, 0xcb, 0x22, 0xc0}
	frames := [][]byte{{0x65, 0x88, 0x84, 0x00, 0x33, 0xff}, {0x41, 0x9a, 0x21, 0x6c, 0x45, 0xff}, {0x41, 0x9a, 0x42, 0x3c, 0x21, 0x93}, {0x41, 0x9a, 0x63, 0x49, 0xe1, 0x0f}, {0x41, 0x9a, 0x86, 0x49, 0xe1, 0x0f}, {0x41, 0x9e, 0xa5, 0x42, 0x7f, 0xf9}, {0x01, 0x9e, 0xc4, 0x69, 0x13, 0xff}}
	pts := []int64{0, 3000, 6000, 9000, 18000, 15000, 12000}
	for index, frame := range frames {
		packet := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: uint16(index), Timestamp: uint32(pts[index]), Marker: true}, Payload: frame}
		preview.writeVideo(rtspPreviewPacket{packet: packet, pts: pts[index], video: true})
	}
	if preview.report.VideoAUs != len(frames) || preview.report.State != "ready" {
		t.Fatalf("B-frame remux rejected valid POC sequence: %+v", preview.report)
	}
	prefix, _, stop, err := player.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	previous := int64(-1)
	nonzeroCTS := false
	for index, chunk := range prefix[1:] {
		dts := int64(uint32(chunk[7])<<24 | uint32(chunk[4])<<16 | uint32(chunk[5])<<8 | uint32(chunk[6]))
		cts := int64(int32(uint32(chunk[13])<<24|uint32(chunk[14])<<16|uint32(chunk[15])<<8) >> 8)
		if dts < previous {
			t.Fatal("FLV decode time regressed")
		}
		if cts != 0 {
			nonzeroCTS = true
		}
		if dts+cts-(2000+pts[index]/90) < -1 || dts+cts-(2000+pts[index]/90) > 1 {
			t.Fatalf("PTS not retained: frame=%d dts=%d cts=%d pts=%d", index, dts, cts, pts[index])
		}
		previous = dts
	}
	if !nonzeroCTS {
		t.Fatal("B-frame PTS was flattened to DTS")
	}
}

func TestRTSPPreviewMetadataRejectsUncontrolledOrContradictoryCapability(t *testing.T) {
	tracks := []RTSPTrackMetrics{{ID: 1, Kind: "video", Codec: "h264"}}
	valid := RTSPPreviewReport{Version: 1, State: "ready", ClockAlignment: "receiver_rtp", VideoTrackID: 1, VideoCodec: "h264", VideoAUs: 1}
	if !validRTSPPreview(&valid, tracks) {
		t.Fatal("valid preview rejected")
	}
	mutations := []func(*RTSPPreviewReport){func(p *RTSPPreviewReport) { p.ReasonCode = "rtsp://private:secret@host/path" }, func(p *RTSPPreviewReport) { p.VideoTrackID = 2 }, func(p *RTSPPreviewReport) { p.VideoCodec = "h265" }, func(p *RTSPPreviewReport) { p.VideoAUs = 0 }, func(p *RTSPPreviewReport) { p.State = "failed" }, func(p *RTSPPreviewReport) { p.DroppedPackets = -1 }, func(p *RTSPPreviewReport) { p.AudioAUs = 1 }, func(p *RTSPPreviewReport) { p.State = "not_requested" }, func(p *RTSPPreviewReport) { p.State = "unsupported"; p.ReasonCode = "codec_unsupported" }, func(p *RTSPPreviewReport) { p.DroppedPackets = 1 }}
	for index, mutate := range mutations {
		copy := valid
		mutate(&copy)
		if validRTSPPreview(&copy, tracks) {
			t.Fatalf("accepted invalid capability %d: %+v", index, copy)
		}
		model := modelRTSP(&RTSPMetrics{Preview: &copy, Tracks: tracks})
		if model.Preview != nil {
			t.Fatal("unvalidated preview entered model")
		}
	}
}

func TestRTSPPreviewPacketizationZeroAndTimestampLimits(t *testing.T) {
	player := newPlayerSession(t.Context(), ProtocolRTSP, nil, nil)
	defer player.Close()
	preview := newRTSPPreview(player)
	preview.video = &format.H264{PacketizationMode: 0}
	preview.videoDecoder = &rtph264.Decoder{PacketizationMode: 1}
	if err := preview.videoDecoder.Init(); err != nil {
		t.Fatal(err)
	}
	preview.dts.Initialize()
	var au h264.AnnexB
	if err := au.Unmarshal(testpattern.AccessUnit(0)); err != nil {
		t.Fatal(err)
	}
	for index, nalu := range au {
		preview.writeVideo(rtspPreviewPacket{packet: &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: uint16(index), Timestamp: 0, Marker: index == len(au)-1}, Payload: nalu}, pts: 0, video: true})
	}
	if preview.report.VideoAUs != 1 || preview.report.State != "ready" {
		t.Fatalf("RFC6184 single NAL mode rejected: %+v", preview.report)
	}
	if preview.tag(9, 2000+maxPlayerElapsedMS+1, []byte{0x17, 1, 0, 0, 0}) || preview.report.ReasonCode != "timestamp_unsupported" {
		t.Fatal("unbounded RTP media clock was forwarded to player")
	}
	if player.PreviewFailure() != "timestamp_unsupported" {
		t.Fatal("native pipeline error lost its controlled category")
	}
}
