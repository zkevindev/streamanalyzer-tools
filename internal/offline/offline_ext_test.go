package offline

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"streamanalyzer/internal/models"
)

// ---- Helpers to build synthetic MPEG-PS, WebM/EBML, FLV, MPEG-TS, PCAP ----

func buildTestH264SPS() []byte {
	// Valid Baseline 16x16 H.264 SPS NALU (from standard test vectors)
	return []byte{0x67, 0x42, 0x00, 0x0a, 0xf8, 0x41, 0xa2}
}

func buildTestH264PPS() []byte {
	return []byte{0x68, 0xce, 0x38, 0x80}
}

func buildTestH264IDR() []byte {
	return []byte{0x65, 0x88, 0x84, 0x00, 0x10, 0xff, 0xfe, 0x01, 0x02, 0x03}
}

func buildTestH264P() []byte {
	return []byte{0x41, 0x9a, 0x24, 0x01, 0x02, 0x03, 0x04}
}

func appendAnnexB(dst []byte, nals ...[]byte) []byte {
	for _, n := range nals {
		dst = append(dst, 0x00, 0x00, 0x00, 0x01)
		dst = append(dst, n...)
	}
	return dst
}

func encodePTS5(pts90k int64, prefix byte) []byte {
	out := make([]byte, 5)
	out[0] = (prefix << 4) | byte(((pts90k>>30)&0x07)<<1) | 0x01
	out[1] = byte((pts90k >> 22) & 0xFF)
	out[2] = byte(((pts90k>>15)&0x7F)<<1) | 0x01
	out[3] = byte((pts90k >> 7) & 0xFF)
	out[4] = byte((pts90k&0x7F)<<1) | 0x01
	return out
}

func buildPESPacket(streamID byte, pts90k int64, payload []byte) []byte {
	ptsBytes := encodePTS5(pts90k, 0x02)
	var hdr bytes.Buffer
	hdr.Write([]byte{0x00, 0x00, 0x01, streamID})
	pesLen := 3 + len(ptsBytes) + len(payload)
	hdr.WriteByte(byte(pesLen >> 8))
	hdr.WriteByte(byte(pesLen))
	hdr.WriteByte(0x80) // MPEG-2 PES flags
	hdr.WriteByte(0x80) // PTS only
	hdr.WriteByte(byte(len(ptsBytes)))
	hdr.Write(ptsBytes)
	hdr.Write(payload)
	return hdr.Bytes()
}

func buildTestMPEGPS() []byte {
	var buf bytes.Buffer
	// Pack Header (14 bytes MPEG-2 PS)
	buf.Write([]byte{0x00, 0x00, 0x01, 0xBA, 0x44, 0x00, 0x04, 0x00, 0x04, 0x01, 0x01, 0x89, 0xC3, 0xF8})
	// PSM (0xBC): video 0xE0 = H.264 (0x1B), audio 0xC0 = G.711 A-law (0x90)
	var psmBody bytes.Buffer
	psmBody.Write([]byte{0xE1, 0xFF})                   // current_next + reserved
	psmBody.Write([]byte{0x00, 0x00})                   // program_stream_info_length = 0
	psmBody.Write([]byte{0x00, 0x08})                   // elementary_stream_map_length = 8
	psmBody.Write([]byte{0x1B, 0xE0, 0x00, 0x00})       // H.264 video
	psmBody.Write([]byte{0x90, 0xC0, 0x00, 0x00})       // G.711 A-law audio
	psmBody.Write([]byte{0x00, 0x00, 0x00, 0x00})       // CRC32 placeholder
	buf.Write([]byte{0x00, 0x00, 0x01, 0xBC})
	buf.WriteByte(byte(psmBody.Len() >> 8))
	buf.WriteByte(byte(psmBody.Len()))
	buf.Write(psmBody.Bytes())

	// 3 video frames (IDR + 2 P) + 3 G.711 audio packets
	v0 := appendAnnexB(nil, buildTestH264SPS(), buildTestH264PPS(), buildTestH264IDR())
	v1 := appendAnnexB(nil, buildTestH264P())
	v2 := appendAnnexB(nil, buildTestH264P())
	buf.Write(buildPESPacket(0xE0, 0, v0))
	buf.Write(buildPESPacket(0xC0, 0, bytes.Repeat([]byte{0x55}, 160)))
	buf.Write(buildPESPacket(0xE0, 3600, v1)) // 40ms
	buf.Write(buildPESPacket(0xC0, 1800, bytes.Repeat([]byte{0xAA}, 160)))
	buf.Write(buildPESPacket(0xE0, 7200, v2)) // 80ms
	buf.Write(buildPESPacket(0xC0, 3600, bytes.Repeat([]byte{0x55}, 160)))
	return buf.Bytes()
}

// EBML builder helpers for WebM/MKV tests
func ebmlVINTSize(n int) []byte {
	if n < 0x7F {
		return []byte{byte(0x80 | n)}
	}
	if n < 0x3FFF {
		return []byte{byte(0x40 | (n >> 8)), byte(n)}
	}
	return []byte{byte(0x20 | (n >> 16)), byte(n >> 8), byte(n)}
}

func ebmlElem(id []byte, payload []byte) []byte {
	var b bytes.Buffer
	b.Write(id)
	b.Write(ebmlVINTSize(len(payload)))
	b.Write(payload)
	return b.Bytes()
}

func buildTestAV1OBUForWebM(w, h int) []byte {
	var bits []uint8
	push := func(val uint64, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, uint8((val>>uint(i))&1))
		}
	}
	push(0, 3)  // Main
	push(1, 1)  // still
	push(1, 1)  // reduced
	push(8, 5)  // level 4.0
	push(10, 4) // 11 bits
	push(10, 4) // 11 bits
	push(uint64(w-1), 11)
	push(uint64(h-1), 11)
	push(0, 1) // sb
	push(0, 1) // fi
	push(0, 1) // ief
	push(0, 1) // sr
	push(0, 1) // cdef
	push(0, 1) // rest
	push(0, 1) // high_bd
	push(0, 1) // mono
	push(0, 1) // color_desc
	push(0, 1) // color_range
	push(0, 2) // chroma_pos
	push(0, 1) // uv_delta
	push(0, 1) // film_grain
	push(1, 1) // trailing
	for len(bits)%8 != 0 {
		bits = append(bits, 0)
	}
	p := make([]byte, len(bits)/8)
	for i, bit := range bits {
		if bit != 0 {
			p[i/8] |= 1 << uint(7-(i%8))
		}
	}
	return append([]byte{0x0A, byte(len(p))}, p...)
}

func buildTestWebM() []byte {
	ebmlHdr := ebmlElem([]byte{0x1A, 0x45, 0xDF, 0xA3},
		ebmlElem([]byte{0x42, 0x82}, []byte("webm")),
	)
	// Info: TimestampScale = 1000000 (1ms)
	info := ebmlElem([]byte{0x15, 0x49, 0xA9, 0x66},
		ebmlElem([]byte{0x2A, 0xD7, 0xB1}, []byte{0x0F, 0x42, 0x40}),
	)
	// Track 1: Video V_AV1 with CodecPrivate = av1C + seq hdr OBU
	obu := buildTestAV1OBUForWebM(1280, 720)
	av1c := append([]byte{0x81, 0x08, 0x0C, 0x00}, obu...)
	videoInfo := ebmlElem([]byte{0xE0}, append(
		ebmlElem([]byte{0xB0}, []byte{0x05, 0x00}), // 1280
		ebmlElem([]byte{0xBA}, []byte{0x02, 0xD0})..., // 720
	))
	track1 := ebmlElem([]byte{0xAE}, bytes.Join([][]byte{
		ebmlElem([]byte{0xD7}, []byte{1}),
		ebmlElem([]byte{0x83}, []byte{1}), // video
		ebmlElem([]byte{0x86}, []byte("V_AV1")),
		ebmlElem([]byte{0x63, 0xA2}, av1c),
		videoInfo,
	}, nil))
	// Track 2: Audio A_OPUS
	audioInfo := ebmlElem([]byte{0xE1}, append(
		ebmlElem([]byte{0x9F}, []byte{2}),
		ebmlElem([]byte{0xB5}, []byte{0x47, 0x3B, 0x80, 0x00})..., // float32 48000.0
	))
	track2 := ebmlElem([]byte{0xAE}, bytes.Join([][]byte{
		ebmlElem([]byte{0xD7}, []byte{2}),
		ebmlElem([]byte{0x83}, []byte{2}), // audio
		ebmlElem([]byte{0x86}, []byte("A_OPUS")),
		audioInfo,
	}, nil))
	tracks := ebmlElem([]byte{0x16, 0x54, 0xAE, 0x6B}, append(track1, track2...))

	// SimpleBlock helper: trackVINT(1B) + relTS(int16 BE) + flags(1B) + data
	makeSimpleBlock := func(track byte, relTS int16, key bool, payload []byte) []byte {
		var b bytes.Buffer
		b.WriteByte(0x80 | track)
		_ = binary.Write(&b, binary.BigEndian, relTS)
		flags := byte(0)
		if key {
			flags = 0x80
		}
		b.WriteByte(flags)
		b.Write(payload)
		return ebmlElem([]byte{0xA3}, b.Bytes())
	}
	cluster := ebmlElem([]byte{0x1F, 0x43, 0xB6, 0x75}, bytes.Join([][]byte{
		ebmlElem([]byte{0xE7}, []byte{0}), // cluster timestamp 0
		makeSimpleBlock(1, 0, true, obu),
		makeSimpleBlock(2, 0, true, []byte{0xFC, 0x01, 0x02, 0x03}),
		makeSimpleBlock(1, 40, false, []byte{0x32, 0x04, 0x01, 0x02, 0x03, 0x04}),
		makeSimpleBlock(2, 20, true, []byte{0xFC, 0x04, 0x05, 0x06}),
		makeSimpleBlock(1, 80, false, []byte{0x32, 0x04, 0x05, 0x06, 0x07, 0x08}),
	}, nil))

	seg := ebmlElem([]byte{0x18, 0x53, 0x80, 0x67}, bytes.Join([][]byte{info, tracks, cluster}, nil))
	return append(ebmlHdr, seg...)
}

func buildTestFLVBytes() []byte {
	var buf bytes.Buffer
	// FLV header (video only) + PrevTagSize0
	buf.Write([]byte{'F', 'L', 'V', 0x01, 0x01, 0x00, 0x00, 0x00, 0x09, 0, 0, 0, 0})
	writeTag := func(tagType byte, ts uint32, body []byte) {
		buf.WriteByte(tagType)
		sz := len(body)
		buf.Write([]byte{byte(sz >> 16), byte(sz >> 8), byte(sz)})
		buf.Write([]byte{byte(ts >> 16), byte(ts >> 8), byte(ts), byte(ts >> 24)})
		buf.Write([]byte{0, 0, 0})
		buf.Write(body)
		_ = binary.Write(&buf, binary.BigEndian, uint32(11+sz))
	}
	// AVCDecoderConfigurationRecord
	sps := buildTestH264SPS()
	pps := buildTestH264PPS()
	var avcc bytes.Buffer
	avcc.Write([]byte{0x17, 0x00, 0x00, 0x00, 0x00})
	avcc.Write([]byte{0x01, sps[1], sps[2], sps[3], 0xFF, 0xE1})
	_ = binary.Write(&avcc, binary.BigEndian, uint16(len(sps)))
	avcc.Write(sps)
	avcc.WriteByte(1)
	_ = binary.Write(&avcc, binary.BigEndian, uint16(len(pps)))
	avcc.Write(pps)
	writeTag(9, 0, avcc.Bytes())

	// IDR video tag
	idr := buildTestH264IDR()
	var v1 bytes.Buffer
	v1.Write([]byte{0x17, 0x01, 0x00, 0x00, 0x00})
	_ = binary.Write(&v1, binary.BigEndian, uint32(len(idr)))
	v1.Write(idr)
	writeTag(9, 0, v1.Bytes())

	// P frame
	p := buildTestH264P()
	var v2 bytes.Buffer
	v2.Write([]byte{0x27, 0x01, 0x00, 0x00, 0x00})
	_ = binary.Write(&v2, binary.BigEndian, uint32(len(p)))
	v2.Write(p)
	writeTag(9, 40, v2.Bytes())
	return buf.Bytes()
}

func buildTestTSPacket(pid uint16, pusi bool, cc byte, payload []byte) []byte {
	pkt := make([]byte, 188)
	pkt[0] = 0x47
	pkt[1] = byte((pid >> 8) & 0x1F)
	if pusi {
		pkt[1] |= 0x40
	}
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = 0x10 | (cc & 0x0F)
	for i := 4; i < 188; i++ {
		pkt[i] = 0xFF
	}
	n := len(payload)
	if n > 184 {
		n = 184
	}
	copy(pkt[4:4+n], payload[:n])
	return pkt
}

func mpeg2CRC32(data []byte) uint32 {
	var crc uint32 = 0xFFFFFFFF
	for _, b := range data {
		crc ^= uint32(b) << 24
		for i := 0; i < 8; i++ {
			if (crc & 0x80000000) != 0 {
				crc = (crc << 1) ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func buildPSISection(bodyNoCRC []byte) []byte {
	sec := append([]byte{0x00}, bodyNoCRC...) // pointer_field = 0
	crc := mpeg2CRC32(bodyNoCRC)
	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], crc)
	return append(sec, crcBuf[:]...)
}

func buildTestTSBytes() []byte {
	var out bytes.Buffer
	// Valid PAT (PID 0) -> PMT PID 0x1000
	pat := buildPSISection([]byte{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xF0, 0x00})
	out.Write(buildTestTSPacket(0x0000, true, 0, pat))
	// Valid PMT (PID 0x1000) -> video PID 0x0100 stream_type 0x1B (H.264)
	pmt := buildPSISection([]byte{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00})
	out.Write(buildTestTSPacket(0x1000, true, 0, pmt))
	// 2 video PES packets
	v0 := appendAnnexB(nil, buildTestH264SPS(), buildTestH264PPS(), buildTestH264IDR())
	v1 := appendAnnexB(nil, buildTestH264P())
	out.Write(buildTestTSPacket(0x0100, true, 0, buildPESPacket(0xE0, 0, v0)))
	out.Write(buildTestTSPacket(0x0100, true, 1, buildPESPacket(0xE0, 3600, v1)))
	return out.Bytes()
}

func waitOfflineDone(t *testing.T, mgr *Manager, taskID string) (*models.OfflineTask, *models.OfflineSummary) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		task, err := mgr.GetTask(taskID)
		if err != nil {
			t.Fatalf("GetTask(%s) err: %v", taskID, err)
		}
		if task.Status != models.OfflineStatusRunning {
			if task.Status != models.OfflineStatusDone {
				t.Fatalf("task %s finished with status=%s err=%s", taskID, task.Status, task.Error)
			}
			b, err := os.ReadFile(task.SummaryPath)
			if err != nil {
				t.Fatalf("read summary: %v", err)
			}
			var sum models.OfflineSummary
			if err := json.Unmarshal(b, &sum); err != nil {
				t.Fatalf("unmarshal summary: %v", err)
			}
			return task, &sum
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %s timed out", taskID)
	return nil, nil
}

func runLocalOfflineTask(t *testing.T, mgr *Manager, filePath string, mode models.OfflineMode) (*models.OfflineTask, *models.OfflineSummary) {
	t.Helper()
	task, err := mgr.CreateLocalTask(&models.OfflineTaskRequest{Mode: mode}, filePath)
	if err != nil {
		t.Fatalf("CreateLocalTask: %v", err)
	}
	prep, err := mgr.PrepareTask(task.ID, false)
	if err != nil {
		t.Fatalf("PrepareTask: %v", err)
	}
	if err := mgr.StartTask(prep.ID); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	return waitOfflineDone(t, mgr, prep.ID)
}

func TestOfflineMPEGPSAndG711WAV(t *testing.T) {
	tmp := t.TempDir()
	psPath := filepath.Join(tmp, "gb28181_sample.ps")
	if err := os.WriteFile(psPath, buildTestMPEGPS(), 0o644); err != nil {
		t.Fatal(err)
	}

	mode, reason := DetectMode(psPath, "gb28181_sample.ps")
	if mode != models.OfflineModePS {
		t.Fatalf("expected DetectMode=ps, got %s (%s)", mode, reason)
	}

	mgr := NewManager(tmp)
	task, sum := runLocalOfflineTask(t, mgr, psPath, models.OfflineModeAuto)
	if task.Mode != models.OfflineModePS {
		t.Fatalf("expected task.Mode=ps, got %s", task.Mode)
	}
	if len(sum.Flows) != 1 {
		t.Fatalf("expected 1 PS flow, got %d", len(sum.Flows))
	}
	f := sum.Flows[0]
	if f.VideoCodec != "h264" || f.AudioCodec != "pcma" {
		t.Fatalf("unexpected codecs: video=%s audio=%s", f.VideoCodec, f.AudioCodec)
	}
	var wavPath string
	for _, ap := range f.ArtifactPaths {
		if strings.HasSuffix(ap, ".wav") {
			wavPath = ap
			break
		}
	}
	if wavPath == "" {
		t.Fatalf("expected G.711 audio exported as WAV in artifact_paths, got %v (audio_path=%s)", f.ArtifactPaths, f.AudioPath)
	}
	wavData, err := os.ReadFile(wavPath)
	if err != nil || len(wavData) < 44 || string(wavData[:4]) != "RIFF" {
		t.Fatalf("invalid exported WAV: err=%v len=%d", err, len(wavData))
	}
	if f.Profile == nil || f.Profile.VideoFrames != 3 || f.Profile.AudioFrames != 3 {
		t.Fatalf("unexpected PS profile: %+v", f.Profile)
	}

	// Verify frame inspector works on both exported PS video frame and G.711 audio frame
	vidIdx, audIdx := -1, -1
	for idx, fd := range f.FrameDetails {
		if fd.MediaType == "video" && vidIdx < 0 {
			vidIdx = idx
		}
		if fd.MediaType == "audio" && audIdx < 0 {
			audIdx = idx
		}
	}
	if vidIdx < 0 || audIdx < 0 {
		t.Fatalf("expected both video and audio frame details, got %d frames", len(f.FrameDetails))
	}
	insV, err := mgr.InspectFrame(task.ID, 0, vidIdx, 0, 256)
	if err != nil {
		t.Fatalf("InspectFrame on PS video failed: %v", err)
	}
	if len(insV.Inspection.NALUs) < 2 {
		t.Fatalf("expected NALUs in PS keyframe inspection, got %+v", insV.Inspection)
	}
	insA, err := mgr.InspectFrame(task.ID, 0, audIdx, 0, 256)
	if err != nil {
		t.Fatalf("InspectFrame on PS audio failed: %v", err)
	}
	if insA.Inspection.AudioHeader == nil || insA.Inspection.AudioHeader.SampleRate != 8000 {
		t.Fatalf("expected G.711 audio header inspection, got %+v", insA.Inspection)
	}
}

func TestOfflineWebMAndAV1(t *testing.T) {
	tmp := t.TempDir()
	webmPath := filepath.Join(tmp, "av1_demo.webm")
	if err := os.WriteFile(webmPath, buildTestWebM(), 0o644); err != nil {
		t.Fatal(err)
	}

	mode, reason := DetectMode(webmPath, "av1_demo.webm")
	if mode != models.OfflineModeWebM {
		t.Fatalf("expected DetectMode=webm, got %s (%s)", mode, reason)
	}

	mgr := NewManager(tmp)
	task, sum := runLocalOfflineTask(t, mgr, webmPath, models.OfflineModeAuto)
	if task.Mode != models.OfflineModeWebM {
		t.Fatalf("expected task.Mode=webm, got %s", task.Mode)
	}
	if len(sum.Flows) != 1 {
		t.Fatalf("expected 1 WebM flow, got %d", len(sum.Flows))
	}
	f := sum.Flows[0]
	if f.VideoCodec != "av1" || f.AudioCodec != "opus" {
		t.Fatalf("unexpected WebM codecs: video=%s audio=%s", f.VideoCodec, f.AudioCodec)
	}
	if f.Profile == nil || f.Profile.Width != 1280 || f.Profile.Height != 720 {
		t.Fatalf("expected 1280x720 from AV1/WebM, got %+v", f.Profile)
	}
	if f.Profile.VideoFrames != 3 || f.Profile.AudioFrames != 2 {
		t.Fatalf("unexpected WebM frame counts: %+v", f.Profile)
	}
}

func TestOfflinePCAPExtendedProtocols(t *testing.T) {
	tmp := t.TempDir()
	pcapPath := filepath.Join(tmp, "mixed_rtp_flv_ts.pcap")

	f, err := os.Create(pcapPath)
	if err != nil {
		t.Fatal(err)
	}
	w := pcapgo.NewWriter(f)
	if err := w.WriteFileHeader(65535, layers.LinkTypeEthernet); err != nil {
		t.Fatal(err)
	}

	baseTime := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	pktIdx := 0
	writeUDP := func(srcIP, dstIP string, srcPort, dstPort uint16, payload []byte, deltaMS int) {
		eth := &layers.Ethernet{
			SrcMAC:       net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
			DstMAC:       net.HardwareAddr{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb},
			EthernetType: layers.EthernetTypeIPv4,
		}
		ip := &layers.IPv4{
			Version: 4, IHL: 5, TTL: 64, Protocol: layers.IPProtocolUDP,
			SrcIP: net.ParseIP(srcIP), DstIP: net.ParseIP(dstIP),
		}
		udp := &layers.UDP{SrcPort: layers.UDPPort(srcPort), DstPort: layers.UDPPort(dstPort)}
		_ = udp.SetNetworkLayerForChecksum(ip)
		buf := gopacket.NewSerializeBuffer()
		opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
		if err := gopacket.SerializeLayers(buf, opts, eth, ip, udp, gopacket.Payload(payload)); err != nil {
			t.Fatal(err)
		}
		data := buf.Bytes()
		ci := gopacket.CaptureInfo{
			Timestamp:     baseTime.Add(time.Duration(deltaMS) * time.Millisecond),
			CaptureLength: len(data),
			Length:        len(data),
		}
		if err := w.WritePacket(ci, data); err != nil {
			t.Fatal(err)
		}
		pktIdx++
	}

	writeTCP := func(srcIP, dstIP string, srcPort, dstPort uint16, seq uint32, payload []byte, deltaMS int) {
		eth := &layers.Ethernet{
			SrcMAC:       net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
			DstMAC:       net.HardwareAddr{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb},
			EthernetType: layers.EthernetTypeIPv4,
		}
		ip := &layers.IPv4{
			Version: 4, IHL: 5, TTL: 64, Protocol: layers.IPProtocolTCP,
			SrcIP: net.ParseIP(srcIP), DstIP: net.ParseIP(dstIP),
		}
		tcp := &layers.TCP{
			SrcPort: layers.TCPPort(srcPort), DstPort: layers.TCPPort(dstPort),
			Seq: seq, ACK: true, PSH: true, Window: 65535,
		}
		_ = tcp.SetNetworkLayerForChecksum(ip)
		buf := gopacket.NewSerializeBuffer()
		opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
		if err := gopacket.SerializeLayers(buf, opts, eth, ip, tcp, gopacket.Payload(payload)); err != nil {
			t.Fatal(err)
		}
		data := buf.Bytes()
		ci := gopacket.CaptureInfo{
			Timestamp:     baseTime.Add(time.Duration(deltaMS) * time.Millisecond),
			CaptureLength: len(data),
			Length:        len(data),
		}
		if err := w.WritePacket(ci, data); err != nil {
			t.Fatal(err)
		}
	}

	// 1. UDP RTP H.264 stream (SSRC 0x12345678, PT 96) with 1 lost (seq 103), 1 duplicate (seq 101), 1 out-of-order (seq 105 before 104) + 1 RTCP SR
	ssrc := uint32(0x12345678)
	makeRTP := func(seq uint16, ts uint32, marker bool, payload []byte) []byte {
		p := &rtp.Packet{
			Header: rtp.Header{
				Version: 2, PayloadType: 96, SequenceNumber: seq,
				Timestamp: ts, SSRC: ssrc, Marker: marker,
			},
			Payload: payload,
		}
		b, err := p.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	writeUDP("10.0.0.1", "10.0.0.2", 5004, 6004, makeRTP(100, 90000, false, buildTestH264SPS()), 0)
	writeUDP("10.0.0.1", "10.0.0.2", 5004, 6004, makeRTP(101, 90000, false, buildTestH264PPS()), 1)
	writeUDP("10.0.0.1", "10.0.0.2", 5004, 6004, makeRTP(101, 90000, false, buildTestH264PPS()), 2) // duplicate
	writeUDP("10.0.0.1", "10.0.0.2", 5004, 6004, makeRTP(102, 90000, true, buildTestH264IDR()), 5)
	// seq 103 lost!
	writeUDP("10.0.0.1", "10.0.0.2", 5004, 6004, makeRTP(105, 97200, true, buildTestH264P()), 45)
	writeUDP("10.0.0.1", "10.0.0.2", 5004, 6004, makeRTP(104, 93600, true, buildTestH264P()), 48) // out-of-order

	// RTCP SR on port+1
	sr := &rtcp.SenderReport{SSRC: ssrc, PacketCount: 6, OctetCount: 512}
	srBytes, _ := sr.Marshal()
	writeUDP("10.0.0.1", "10.0.0.2", 5005, 6005, srBytes, 50)

	// 2. TCP HTTP-FLV stream on port 8080
	httpFLV := append([]byte("HTTP/1.1 200 OK\r\nContent-Type: video/x-flv\r\n\r\n"), buildTestFLVBytes()...)
	writeTCP("10.0.1.10", "10.0.1.20", 8080, 51234, 1000, httpFLV, 60)

	// 3. UDP MPEG-TS stream on port 1234 (chunk real fixture into standard 7*188=1316B UDP datagrams)
	tsRaw, err := os.ReadFile(filepath.Join("..", "simulator", "mediasource", "fixtures", "regular.ts"))
	if err != nil {
		t.Fatal(err)
	}
	const udpTSChunk = 7 * 188
	for off, idx := 0, 0; off < len(tsRaw); off, idx = off+udpTSChunk, idx+1 {
		end := off + udpTSChunk
		if end > len(tsRaw) {
			end = len(tsRaw)
		}
		writeUDP("10.0.2.1", "239.0.0.1", 4000, 1234, tsRaw[off:end], 70+idx*5)
	}

	_ = f.Close()

	mgr := NewManager(tmp)
	task, sum := runLocalOfflineTask(t, mgr, pcapPath, models.OfflineModePCAP)
	if task.Mode != models.OfflineModePCAP {
		t.Fatalf("unexpected mode: %s", task.Mode)
	}

	var foundRTP, foundHTTPFLV, foundUDPTS bool
	for _, fl := range sum.Flows {
		switch fl.Protocol {
		case "rtp":
			foundRTP = true
			if fl.RTPStats == nil {
				t.Fatalf("RTP flow missing RTPStats")
			}
			if fl.RTPStats.SSRC != ssrc {
				t.Fatalf("unexpected SSRC: %x", fl.RTPStats.SSRC)
			}
			if fl.RTPStats.Lost < 1 || fl.RTPStats.Duplicates < 1 || fl.RTPStats.OutOfOrder < 1 {
				t.Fatalf("expected lost>=1, dup>=1, ooo>=1, got %+v", fl.RTPStats)
			}
			if fl.RTPStats.RTCPCount < 1 {
				t.Fatalf("expected RTCP SR matched to SSRC, got %+v", fl.RTPStats)
			}
			if fl.VideoCodec != "h264" || fl.Profile == nil || fl.Profile.VideoFrames < 2 {
				t.Fatalf("expected H.264 frames reconstructed from RTP, got codec=%s profile=%+v", fl.VideoCodec, fl.Profile)
			}
		case "http-flv":
			foundHTTPFLV = true
			if fl.VideoCodec != "h264" || fl.Profile == nil || fl.Profile.VideoFrames < 2 {
				t.Fatalf("expected HTTP-FLV parsed frames, got codec=%s profile=%+v", fl.VideoCodec, fl.Profile)
			}
		case "mpeg-ts":
			if fl.Direction == "udp-ts" {
				foundUDPTS = true
			}
			if fl.Profile == nil || fl.Profile.VideoFrames < 1 {
				t.Fatalf("expected UDP MPEG-TS parsed, got profile=%+v", fl.Profile)
			}
		}
	}
	if !foundRTP || !foundHTTPFLV || !foundUDPTS {
		t.Fatalf("missing expected extended PCAP flows: rtp=%v http-flv=%v udp-ts=%v (total flows=%d)",
			foundRTP, foundHTTPFLV, foundUDPTS, len(sum.Flows))
	}
}

func TestOfflineCompareAndReports(t *testing.T) {
	tmp := t.TempDir()
	psPath := filepath.Join(tmp, "sample_a.ps")
	webmPath := filepath.Join(tmp, "sample_b.webm")
	if err := os.WriteFile(psPath, buildTestMPEGPS(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(webmPath, buildTestWebM(), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(tmp)
	taskA, _ := runLocalOfflineTask(t, mgr, psPath, models.OfflineModeAuto)
	taskB, _ := runLocalOfflineTask(t, mgr, webmPath, models.OfflineModeAuto)

	cmp, err := mgr.CompareTasks(taskA.ID, 0, taskB.ID, 0)
	if err != nil {
		t.Fatalf("CompareTasks failed: %v", err)
	}
	if cmp.ChangedCount == 0 || len(cmp.Rows) < 5 {
		t.Fatalf("expected meaningful diff rows, got changed=%d rows=%d", cmp.ChangedCount, len(cmp.Rows))
	}
	if len(cmp.SummaryNotes) == 0 {
		t.Fatalf("expected non-empty SummaryNotes")
	}

	// Markdown report
	mdBytes, mdName, err := mgr.RenderMarkdownReport(taskA.ID, 0)
	if err != nil {
		t.Fatalf("RenderMarkdownReport failed: %v", err)
	}
	if !strings.HasSuffix(mdName, ".md") || !bytes.Contains(mdBytes, []byte("离线媒体诊断报告")) {
		t.Fatalf("unexpected markdown report: name=%s len=%d", mdName, len(mdBytes))
	}

	// Self-contained HTML report (zero external CDN links)
	htmlBytes, htmlName, err := mgr.RenderHTMLReport(taskA.ID, 0)
	if err != nil {
		t.Fatalf("RenderHTMLReport failed: %v", err)
	}
	htmlStr := string(htmlBytes)
	if !strings.HasSuffix(htmlName, ".html") || !strings.Contains(strings.ToLower(htmlStr), "<!doctype html>") {
		t.Fatalf("unexpected html report: name=%s", htmlName)
	}
	if strings.Contains(htmlStr, "http://") || strings.Contains(htmlStr, "https://") {
		t.Fatalf("HTML report should be self-contained with zero external URLs")
	}
}

func TestOfflineFFmpegThumbAuditAndClip(t *testing.T) {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed in PATH, skipping FFmpeg integration test")
	}

	tmp := t.TempDir()
	mp4Path := filepath.Join(tmp, "black_and_bars.mp4")
	// Generate a 2.5s MP4: 1.5s black video + 1s color bars, with silent audio
	cmd := exec.Command(ffmpegBin,
		"-y",
		"-f", "lavfi", "-i", "color=c=black:s=320x240:d=1.5:r=25",
		"-f", "lavfi", "-i", "testsrc=s=320x240:d=1.0:r=25",
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo:d=2.5",
		"-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0[v]",
		"-map", "[v]", "-map", "2:a",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-g", "25",
		"-c:a", "aac", "-shortest",
		mp4Path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg test clip generation unavailable (%v): %s", err, string(out))
	}

	mgr := NewManager(tmp)
	task, _ := runLocalOfflineTask(t, mgr, mp4Path, models.OfflineModeMP4)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Thumbnail extraction
	jpegBytes, err := mgr.ExtractFrameThumbnail(ctx, task.ID, 0, 1800)
	if err != nil {
		t.Fatalf("ExtractFrameThumbnail failed: %v", err)
	}
	if len(jpegBytes) < 100 || jpegBytes[0] != 0xFF || jpegBytes[1] != 0xD8 {
		t.Fatalf("expected valid JPEG bytes, got len=%d head=%x", len(jpegBytes), jpegBytes[:min(4, len(jpegBytes))])
	}

	// 2. Decode Audit (black + silence detection)
	audit, err := mgr.RunDecodeAudit(ctx, task.ID, 0, true)
	if err != nil {
		t.Fatalf("RunDecodeAudit failed: %v", err)
	}
	if len(audit.BlackFrames) == 0 {
		t.Fatalf("expected at least 1 black frame interval detected, got %+v", audit)
	}
	if len(audit.SilenceRanges) == 0 {
		t.Fatalf("expected at least 1 silence interval detected, got %+v", audit)
	}

	// 3. Lossless clip export (-c copy)
	relPath, sz, err := mgr.ExportClip(ctx, task.ID, 0, 0.5, 2.0)
	if err != nil {
		t.Fatalf("ExportClip failed: %v", err)
	}
	if sz <= 0 || !strings.HasPrefix(relPath, "clips/") {
		t.Fatalf("unexpected clip export result: rel=%s size=%d", relPath, sz)
	}
	fullClip := filepath.Join(tmp, "offline", task.ID, filepath.FromSlash(relPath))
	if st, err := os.Stat(fullClip); err != nil || st.Size() != sz {
		t.Fatalf("exported clip missing or size mismatch: err=%v", err)
	}
}
