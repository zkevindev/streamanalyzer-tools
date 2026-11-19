package diagnostic

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph265"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
	"github.com/pion/rtp"
)

// Generated from testsrc2, 320x180/15fps, libx265 Main, closed 15-frame GOP,
// three B-frames, AUD/repeated parameter sets. The fixture needs no FFmpeg at
// test runtime. PTS offsets below retain the original decode-order reordering.
func hevcPreviewFixture(t *testing.T) ([][][]byte, []int64, []byte, []byte, []byte) {
	t.Helper()
	raw, err := os.ReadFile("testdata/preview-h265.h265")
	if err != nil {
		t.Fatal(err)
	}

	var frames [][][]byte
	var vps, sps, pps []byte
	positions := []int{0}
	firstAUD := true
	for offset := 0; offset+5 < len(raw); {
		relative := bytes.Index(raw[offset:], []byte{0, 0, 1, 0x46, 1})
		if relative < 0 {
			break
		}
		position := offset + relative
		start := position
		if start > 0 && raw[start-1] == 0 {
			start--
		}
		if !firstAUD {
			positions = append(positions, start)
		}
		firstAUD = false
		offset = position + 5
	}
	positions = append(positions, len(raw))
	for index := 0; index < len(positions)-1; index++ {
		var nalus h264.AnnexB
		if err := nalus.Unmarshal(raw[positions[index]:positions[index+1]]); err != nil {
			t.Fatal(err)
		}
		frames = append(frames, [][]byte(nalus))
		for _, nalu := range nalus {
			if len(nalu) < 2 {
				t.Fatal("short HEVC fixture NALU")
			}
			switch (nalu[0] >> 1) & 63 {
			case 32:
				vps = nalu
			case 33:
				sps = nalu
			case 34:
				pps = nalu
			}
		}
	}
	ptsUnits := []int64{0, 4, 2, 1, 3, 8, 6, 5, 7, 12, 10, 9, 11, 14, 13, 15, 19, 17, 16, 18, 23, 21, 20, 22, 27, 25, 24, 26, 29, 28}
	pts := make([]int64, len(ptsUnits))
	for i, v := range ptsUnits {
		pts[i] = v * 6000
	}
	if len(frames) != len(pts) {
		t.Fatalf("fixture AU count=%d want%d", len(frames), len(pts))
	}
	return frames, pts, vps, sps, pps
}
func hevcPreviewPackets(t *testing.T, frames [][][]byte, pts []int64) []*rtp.Packet {
	t.Helper()
	encoder := &rtph265.Encoder{PayloadType: 96, PayloadMaxSize: 512}
	if err := encoder.Init(); err != nil {
		t.Fatal(err)
	}
	var result []*rtp.Packet
	for index, frame := range frames {
		packets, err := encoder.Encode(frame)
		if err != nil {
			t.Fatal(err)
		}
		for _, packet := range packets {
			packet.Timestamp = uint32(pts[index])
			result = append(result, packet)
		}
	}
	return result
}
func hevcPreviewSDP(vps, sps, pps []byte) string {
	return rtspIntegrationSDP + "m=video 0 RTP/AVP 96\r\na=rtpmap:96 H265/90000\r\na=fmtp:96 sprop-vps=" + base64.StdEncoding.EncodeToString(vps) + ";sprop-sps=" + base64.StdEncoding.EncodeToString(sps) + ";sprop-pps=" + base64.StdEncoding.EncodeToString(pps) + "\r\na=control:trackID=0\r\n"
}

func TestRTSPHEVCEnhancedFLVPreservesBFramePTSAndParameterNALUs(t *testing.T) {
	frames, pts, vps, sps, pps := hevcPreviewFixture(t)
	player := newPlayerSession(t.Context(), ProtocolRTSP, nil, nil)
	defer player.Close()
	preview := newRTSPPreview(player)
	if !preview.selectHEVC(&format.H265{VPS: vps, SPS: sps, PPS: pps}) {
		t.Fatal("H265 decoder unavailable")
	}
	player.publish([]byte{'F', 'L', 'V', 1, 1, 0, 0, 0, 9, 0, 0, 0, 0})
	for _, packet := range hevcPreviewPackets(t, frames, pts) {
		preview.writeVideo(rtspPreviewPacket{packet: packet, pts: int64(packet.Timestamp), video: true})
	}
	if preview.report.State != "ready" || preview.report.VideoAUs != 30 || preview.report.PacketErrors != 0 {
		t.Fatalf("HEVC depay/remux failed: %+v", preview.report)
	}
	prefix, _, stop, err := player.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	raw := bytes.Join(prefix, nil)
	if target := os.Getenv("STREAMANALYZER_TEST_HEVC_FLV"); target != "" {
		if err := os.WriteFile(target, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	parser := raw[13:]
	previousDTS := int64(-1)
	frameIndex, configs, compositionOffsets := 0, 0, 0
	for len(parser) > 0 {
		if len(parser) < 15 {
			t.Fatal("short FLV tag")
		}
		size := int(parser[1])<<16 | int(parser[2])<<8 | int(parser[3])
		if size+15 > len(parser) {
			t.Fatal("bad FLV tag size")
		}
		body := parser[11 : 11+size]
		dts := int64(parser[4])<<16 | int64(parser[5])<<8 | int64(parser[6]) | int64(parser[7])<<24
		if dts < previousDTS {
			t.Fatal("HEVC decode time regressed")
		}
		previousDTS = dts
		if len(body) < 5 || body[0]&0x80 == 0 || string(body[1:5]) != "hvc1" {
			t.Fatal("not Enhanced FLV HEVC")
		}
		if body[0]&15 == 0 {
			configs++
			config, err := hevcConfigurationRecord(vps, sps, pps)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(body[5:], config) {
				t.Fatal("configuration record changed")
			}
			for _, nalu := range [][]byte{vps, sps, pps} {
				if !bytes.Contains(config, nalu) {
					t.Fatal("parameter NALU changed")
				}
			}
		} else {
			if body[0]&15 != 1 {
				t.Fatal("missing signed composition offset")
			}
			cts := int64(int32(uint32(body[5])<<24|uint32(body[6])<<16|uint32(body[7])<<8) >> 8)
			if cts != 0 {
				compositionOffsets++
			}
			if diff := dts + cts - (2000 + pts[frameIndex]/90); diff < -1 || diff > 1 {
				t.Fatalf("HEVC PTS changed frame%d: DTS%d CTS%d PTS%d", frameIndex, dts, cts, pts[frameIndex])
			}
			var got h264.AVCC
			if err := got.Unmarshal(body[8:]); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual([][]byte(got), frames[frameIndex]) {
				t.Fatalf("HEVC access unit%d bytes changed", frameIndex)
			}
			frameIndex++
		}
		if binary.BigEndian.Uint32(parser[11+size:]) != uint32(11+size) {
			t.Fatal("bad previous tag size")
		}
		parser = parser[15+size:]
	}
	if frameIndex != 30 || configs != 1 || compositionOffsets == 0 {
		t.Fatalf("HEVC frames/config/Bframe CTS missing: %d/%d/%d", frameIndex, configs, compositionOffsets)
	}
	// Configuration profile/constraints must come from RBSP, including streams
	// whose encoded SPS inserts 03 emulation prevention between zero bytes.
	if !bytes.Contains(sps, []byte{0, 0, 3}) {
		t.Fatal("fixture must exercise SPS emulation prevention")
	}
	var parsed h265.SPS
	if parsed.Unmarshal(sps) != nil {
		t.Fatal("invalid fixture SPS")
	}
	config, _ := hevcConfigurationRecord(vps, sps, pps)
	if config[1]&31 != parsed.ProfileTierLevel.GeneralProfileIdc || config[12] != parsed.ProfileTierLevel.GeneralLevelIdc {
		t.Fatal("HVCC profile or level wrong")
	}
}

func TestRTSPHEVCNativeSameCaptureAndOfflineRoundTrip(t *testing.T) {
	frames, pts, vps, sps, pps := hevcPreviewFixture(t)
	packets := hevcPreviewPackets(t, frames, pts)
	sdp := hevcPreviewSDP(vps, sps, pps)
	for _, transport := range []string{"tcp", "udp"} {
		t.Run(transport, func(t *testing.T) {
			service := rtspIntegrationOpen(t)
			var address string
			if transport == "tcp" {
				var wire []rtspIntegrationPacket
				previous := uint32(0xffffffff)
				for _, packet := range packets {
					raw, err := packet.Marshal()
					if err != nil {
						t.Fatal(err)
					}
					pause := time.Duration(0)
					if previous != packet.Timestamp {
						pause = 20 * time.Millisecond
						previous = packet.Timestamp
					}
					wire = append(wire, rtspIntegrationPacket{raw: raw, delay: pause})
				}
				fixture := newRTSPIntegrationFixture(t, rtspIntegrationConfig{sdp: sdp, packets: wire, closeAfterPlay: true})
				address = fixture.address()
			} else {
				var datagrams []rtspUDPTestPacket
				for _, packet := range packets {
					raw, _ := packet.Marshal()
					datagrams = append(datagrams, rtspUDPTestPacket{data: raw})
				}
				target, _ := rtspUDPTestServer(t, datagrams, false, sdp)
				address = target.String()
			}
			run, err := service.Start(SampleRequest{Protocol: ProtocolRTSP, Transport: transport, URL: address, Seconds: 2, Playback: true})
			if err != nil {
				t.Fatal(err)
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
			done := make(chan []byte, 1)
			go func() {
				raw := bytes.Join(prefix, nil)
				for chunk := range live {
					raw = append(raw, chunk...)
				}
				done <- raw
			}()
			detail := rtspIntegrationAwait(t, service, run.ID)
			raw := <-done
			p := detail.Report.RTSP.Preview
			if p == nil || p.Version != 2 || p.CodecCapabilitiesVersion != rtspPreviewCodecCapabilitiesVersion || p.State != "ready" || p.VideoCodec != "h265" || p.VideoOutputCodec != "h265" || p.VideoTransform != "native_remux" || p.VideoAUs != 30 || p.PacketErrors != 0 || !bytes.HasPrefix(raw, []byte("FLV")) {
				t.Fatalf("native HEVC preview incomplete: %+v", p)
			}
			if detail.Report.RuleVersion != "rtsp/4" || detail.Report.Metrics.VideoFrames != 0 || detail.Report.RTSP.RTPPackets != len(packets) {
				t.Fatal("native RTP stats fabricated frames or lost packets")
			}
			files := rtspIntegrationRoundTrip(t, service, detail)
			var metadata rtspArchiveMetadata
			_ = json.Unmarshal(files[rtspMetadataName], &metadata)
			if metadata.Version != 4 || !reflect.DeepEqual(metadata.Preview, p) {
				t.Fatal("raw4 pipeline proof not persisted")
			}
		})
	}
}

func TestRTSPHEVCConfigurationAndCapabilityLimits(t *testing.T) {
	_, _, vps, sps, pps := hevcPreviewFixture(t)
	for _, broken := range [][][]byte{{nil, sps, pps}, {vps, []byte{0x42, 1}, pps}, {vps, sps, []byte{0x44, 1}}, {bytes.Repeat([]byte{1}, 65536), sps, pps}} {
		if _, err := hevcConfigurationRecord(broken[0], broken[1], broken[2]); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid HEVC parameter record accepted")
		}
	}
	p := newRTSPPreview(newPlayerSession(t.Context(), ProtocolRTSP, nil, nil))
	if p.selectHEVC(&format.H265{MaxDONDiff: 1}) {
		t.Fatal("unsupported DON packetization accepted")
	}
	valid := RTSPPreviewReport{Version: 2, CodecCapabilitiesVersion: 1, State: "ready", ClockAlignment: "receiver_rtp", VideoTrackID: 1, VideoCodec: "h265", VideoOutputCodec: "h265", VideoTransform: "native_remux", VideoAUs: 1}
	tracks := []RTSPTrackMetrics{{ID: 1, Kind: "video", Codec: "h265"}}
	if !validRTSPPreview(&valid, tracks) {
		t.Fatal("valid HEVC v2 rejected")
	}
	for _, mutate := range []func(*RTSPPreviewReport){func(p *RTSPPreviewReport) { p.Version = 1 }, func(p *RTSPPreviewReport) { p.CodecCapabilitiesVersion = rtspPreviewCodecCapabilitiesVersion + 1 }, func(p *RTSPPreviewReport) { p.VideoOutputCodec = "h264" }, func(p *RTSPPreviewReport) { p.VideoTransform = "ffmpeg_transcode" }} {
		copy := valid
		mutate(&copy)
		if validRTSPPreview(&copy, tracks) {
			t.Fatalf("invalid codec proof accepted: %+v", copy)
		}
	}
}

func TestRTSPHEVCBLAAnchorsAndInvalidNALHeaders(t *testing.T) {
	frames, _, vps, sps, pps := hevcPreviewFixture(t)
	var idr []byte
	for _, nalu := range frames[0] {
		kind := (nalu[0] >> 1) & 63
		if kind == 19 || kind == 20 {
			idr = nalu
			break
		}
	}
	if idr == nil {
		t.Fatal("fixture must include IDR")
	}
	for _, kind := range []byte{16, 17, 18} {
		t.Run(string(rune('A'+kind-16)), func(t *testing.T) {
			player := newPlayerSession(t.Context(), ProtocolRTSP, nil, nil)
			defer player.Close()
			preview := newRTSPPreview(player)
			codec := &format.H265{VPS: vps, SPS: sps, PPS: pps}
			if !preview.selectHEVC(codec) {
				t.Fatal("HEVC unavailable")
			}
			// A short parser vector checks BLA anchor classification and byte retention;
			// it does not claim that changing a picture header creates a decodable BLA.
			vector := bytes.Clone(idr)
			vector[0] = vector[0]&0x81 | kind<<1
			for _, payload := range [][]byte{vector, append([]byte{49 << 1, 1, 0x80 | kind}, vector[2:10]...), append([]byte{48 << 1, 1, byte(len(vector) >> 8), byte(len(vector))}, vector...)} {
				if !preview.hevc.clockTrack.PTSEqualsDTS(&rtp.Packet{Payload: payload}) {
					t.Fatal("BLA single/FU/AP did not establish shared clock")
				}
			}
			preview.writeVideo(rtspPreviewPacket{packet: &rtp.Packet{Header: rtp.Header{Version: 2, Marker: true, SequenceNumber: 1}, Payload: vector}, pts: 0, video: true})
			if preview.report.State != "ready" || preview.report.VideoAUs != 1 {
				t.Fatalf("BLA timing anchor omitted: %+v", preview.report)
			}
			prefix, _, stop, err := player.Subscribe()
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			body := prefix[len(prefix)-1][11:]
			if body[0] != 0x91 || !bytes.Contains(body, vector) {
				t.Fatal("BLA output type or original NALU bytes changed")
			}
		})
	}
	for _, invalid := range [][]byte{{0xc2, 1}, {0x42, 0}, {0x42}} {
		if validHEVCNALUHeader(invalid) {
			t.Fatal("invalid forbidden_zero_bit/temporal_id_plus1 accepted")
		}
		player := newPlayerSession(t.Context(), ProtocolRTSP, nil, nil)
		defer player.Close()
		preview := newRTSPPreview(player)
		preview.selectHEVC(&format.H265{VPS: vps, SPS: sps, PPS: pps})
		preview.writeVideo(rtspPreviewPacket{packet: &rtp.Packet{Header: rtp.Header{Version: 2, Marker: true}, Payload: invalid}, video: true})
		if preview.report.VideoAUs > 0 || preview.report.State == "ready" {
			t.Fatal("invalid header was emitted as ready")
		}
	}
}

func TestRTSPHEVCV2RejectsLegacyFieldPresence(t *testing.T) {
	data, err := os.ReadFile("testdata/rtsp-v3-h264-preview.zip")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	report, err := readBundle(dir, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, rtspStreamName))
	if err != nil {
		t.Fatal(err)
	}
	files := rtspIntegrationZIP(t, raw)
	for _, key := range []string{"codec_capabilities_version", "video_output_codec", "audio_output_codec", "video_transform", "audio_transform"} {
		t.Run(key, func(t *testing.T) {
			changed := map[string][]byte{}
			for name, value := range files {
				changed[name] = bytes.Clone(value)
			}
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(changed[rtspMetadataName], &fields)
			var preview map[string]json.RawMessage
			_ = json.Unmarshal(fields["preview"], &preview)
			preview[key] = json.RawMessage("null")
			fields["preview"], _ = json.Marshal(preview)
			changed[rtspMetadataName], _ = json.Marshal(fields)
			rtspUDPTestWriteArchive(t, dir, changed)
			if _, err := replayRTSP(dir, report.Manifest); !errors.Is(err, ErrInvalid) {
				t.Fatalf("raw3 accepted v2 field %s:null: %v", key, err)
			}
		})
	}
}
