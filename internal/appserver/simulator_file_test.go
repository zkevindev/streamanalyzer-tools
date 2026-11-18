package appserver

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/gb28181"
)

func TestGB28181LocalFilePlayback(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("local video playback requires FFmpeg")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("local video playback requires ffprobe")
	}

	clearDiagnosticModelEnv(t)
	fixtures := t.TempDir()
	h264File := filepath.Join(fixtures, "av.mp4")
	if output, err := exec.CommandContext(t.Context(), ffmpeg, "-v", "error",
		"-f", "lavfi", "-i", "testsrc2=size=160x96:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=700:sample_rate=48000",
		"-t", "0.8", "-c:v", "libx264", "-g", "10", "-bf", "2", "-c:a", "aac", h264File).CombinedOutput(); err != nil {
		t.Fatalf("generate H.264 and AAC fixture: %v: %s", err, output)
	}
	videoOnlyFile := filepath.Join(fixtures, "video.mp4")
	if output, err := exec.CommandContext(t.Context(), ffmpeg, "-v", "error",
		"-f", "lavfi", "-i", "testsrc2=size=160x96:rate=25", "-t", "0.8",
		"-c:v", "mpeg4", "-an", videoOnlyFile).CombinedOutput(); err != nil {
		t.Fatalf("generate video-only fixture: %v: %s", err, output)
	}

	staging := t.TempDir()
	t.Setenv("TMPDIR", staging)
	t.Setenv("TMP", staging)
	t.Setenv("TEMP", staging)
	platform := openSimulatorUDP(t)
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/simulators/gb28181"
	const deviceID = "34020000001320000001"
	const channelID = "34020000001320000002"
	const platformID = "34020000002000000001"

	start := func(t *testing.T, name, audio string, video io.Reader, expected int) gb28181.Status {
		t.Helper()
		config := gb28181.Config{
			PlatformHost: "127.0.0.1", PlatformPort: platform.LocalAddr().(*net.UDPAddr).Port,
			PlatformID: platformID, DeviceID: deviceID, ChannelID: channelID, AudioCodec: audio,
		}
		data, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		if err := form.WriteField("config", string(data)); err != nil {
			t.Fatal(err)
		}
		part, err := form.CreateFormFile("video", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(part, video); err != nil {
			t.Fatal(err)
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		res, err := http.Post(server.URL()+api+"/start-file", form.FormDataContentType(), &body)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != expected {
			t.Fatalf("start-file returned %d: %s (%v)", res.StatusCode, out, err)
		}
		if res.StatusCode != http.StatusAccepted {
			return gb28181.Status{}
		}
		return decodeDiagnostic[gb28181.Status](t, out)
	}
	assertStagingEmpty := func(t *testing.T) {
		t.Helper()
		left, err := filepath.Glob(filepath.Join(staging, "streamanalyzer-gb28181-*.video"))
		if err != nil || len(left) != 0 {
			t.Fatalf("simulator left staged media behind: %v (%v)", left, err)
		}
	}

	start(t, "invalid.mp4", "g711a", bytes.NewBufferString("not a video"), http.StatusBadRequest)
	assertStagingEmpty(t)

	for _, test := range []struct {
		id, file, audio, output, mode, codec string
		audioPSM                             byte
	}{
		{"alaw", h264File, "", "g711a", "copy", "h264", 0x90},
		{"mulaw", h264File, "g711u", "g711u", "copy", "h264", 0x91},
		{"video-only", videoOnlyFile, "g711a", "none", "transcode", "mpeg4", 0},
	} {
		t.Run(test.id, func(t *testing.T) {
			file, err := os.Open(test.file)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			status := start(t, filepath.Base(test.file), test.audio, file, http.StatusAccepted)
			source := status.MediaSource
			if source.Filename != filepath.Base(test.file) || source.VideoCodec != test.codec ||
				source.VideoMode != test.mode || source.AudioCodec != test.output {
				t.Fatalf("unexpected media selection: %+v", source)
			}

			register, device := receiveSimulatorSIP(t, platform, "REGISTER ", 2*time.Second)
			sendSimulatorSIP(t, platform, device, register.response("200 OK", ""))
			waitSimulator(t, server, api, "registered")

			media := openSimulatorUDP(t)
			channel := "sip:" + channelID + "@" + device.String()
			from := "<sip:" + platformID + "@3402000000>;tag=mock-platform"
			offer := fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 127.0.0.1\r\ns=Play\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n"+
				"m=video %d RTP/AVP 96\r\na=rtpmap:96 PS/90000\r\ny=0100000001\r\n", platformID, media.LocalAddr().(*net.UDPAddr).Port)
			sendSimulatorRequest(t, platform, device, from, "INVITE", channel, "<"+channel+">", test.id, test.id+"-invite", "application/sdp", offer, 1)
			answer, _ := receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
			media.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			if _, _, err := media.ReadFromUDP(make([]byte, 1500)); err == nil {
				t.Fatal("file media started before ACK")
			}
			sendSimulatorRequest(t, platform, device, from, "ACK", channel, answer.headers["to"], test.id, test.id+"-ack", "", "", 1)

			media.SetReadDeadline(time.Now().Add(5 * time.Second))
			clocks := collectLocalSimulatorMedia(t, media, test.audioPSM)
			if test.mode == "copy" && !clocks.reordered {
				t.Fatal("copied video lost distinct PTS and DTS")
			}
			if test.audioPSM != 0 && !clocks.audio20ms {
				t.Fatal("missing G.711 audio at 20 ms cadence")
			}
			status = waitSimulatorMedia(t, server, api, "streaming", time.Second)
			if status.VideoFrames < 20 || (test.audioPSM != 0) != (status.AudioPackets > 0) {
				t.Fatalf("workbench did not count both tracks correctly: %+v", status)
			}

			sendSimulatorRequest(t, platform, device, from, "BYE", channel, answer.headers["to"], test.id, test.id+"-bye", "", "", 2)
			receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
			waitSimulatorMedia(t, server, api, "idle", time.Second)
			diagnosticRequest(t, server, http.MethodPost, api+"/stop", http.StatusAccepted, nil)
			unregister, _ := receiveSimulatorSIP(t, platform, "REGISTER ", 2*time.Second)
			sendSimulatorSIP(t, platform, device, unregister.response("200 OK", ""))
			waitSimulator(t, server, api, "stopped")
			assertStagingEmpty(t)
		})
	}
}

type simulatorFileClocks struct {
	reordered bool
	audio20ms bool
}

func collectLocalSimulatorMedia(t *testing.T, receiver *net.UDPConn, audioType byte) simulatorFileClocks {
	t.Helper()
	var clocks simulatorFileClocks
	packet := make([]byte, 1500)
	var ps []byte
	var audioTime, videoTime uint64
	var frames, audioPackets int
	var mappedAudio byte
	var mappedVideo bool
	for frames < 27 || audioType != 0 && audioPackets < 42 {
		n, _, err := receiver.ReadFromUDP(packet)
		if err != nil {
			t.Fatalf("read RTP for local video (%d frames, %d audio packets): %v", frames, audioPackets, err)
		}
		if n < 12 || packet[0] != 0x80 || packet[1]&0x7f != 96 {
			t.Fatalf("unexpected RTP header: %x", packet[:n])
		}
		ps = append(ps, packet[12:n]...)
		if packet[1]&0x80 == 0 {
			continue
		}

		if len(ps) < 14 || !bytes.HasPrefix(ps, []byte{0, 0, 1, 0xba}) {
			t.Fatal("PS pack header missing")
		}
		for data := ps[14:]; len(data) > 0; {
			if len(data) < 6 || !bytes.HasPrefix(data, []byte{0, 0, 1}) {
				t.Fatal("invalid PS start code")
			}
			length := int(binary.BigEndian.Uint16(data[4:6]))
			if length == 0 || len(data) < 6+length {
				t.Fatal("invalid PS length")
			}
			stream, part := data[3], data[6:6+length]
			data = data[6+length:]
			switch stream {
			case 0xbc:
				info := int(binary.BigEndian.Uint16(part[2:4]))
				esSize := int(binary.BigEndian.Uint16(part[4+info : 6+info]))
				for es := part[6+info : 6+info+esSize]; len(es) >= 4; {
					if es[1] == 0xe0 {
						mappedVideo = es[0] == 0x1b
					}
					if es[1] == 0xc0 {
						mappedAudio = es[0]
					}
					skip := int(binary.BigEndian.Uint16(es[2:4]))
					es = es[4+skip:]
				}
			case 0xe0:
				if part[1]&0x80 == 0 {
					continue // Continuation PES.
				}
				dts := decodeSimulatorPTS(part[3:8])
				if part[1]&0x40 != 0 {
					pts := dts
					dts = decodeSimulatorPTS(part[8:13])
					clocks.reordered = clocks.reordered || pts != dts
				}
				if frames > 0 && dts <= videoTime {
					t.Fatalf("video DTS regressed across the loop: %d after %d", dts, videoTime)
				}
				videoTime, frames = dts, frames+1
			case 0xc0:
				pts := decodeSimulatorPTS(part[3:8])
				if audioPackets > 0 {
					if pts <= audioTime {
						t.Fatalf("audio timestamp regressed across the loop: %d after %d", pts, audioTime)
					}
					if pts-audioTime == 1800 && len(part[3+int(part[2]):]) == 160 {
						clocks.audio20ms = true
					}
				}
				audioTime, audioPackets = pts, audioPackets+1
			}
		}
		ps = ps[:0]
	}
	if !mappedVideo || mappedAudio != audioType || (audioType != 0) != (audioPackets > 0) {
		t.Fatalf("unexpected PS streams: H.264 %v audio type 0x%02x and %d packets", mappedVideo, mappedAudio, audioPackets)
	}
	return clocks
}

func decodeSimulatorPTS(data []byte) uint64 {
	return uint64(data[0]&0x0e)<<29 | uint64(data[1])<<22 | uint64(data[2]&0xfe)<<14 |
		uint64(data[3])<<7 | uint64(data[4]>>1)
}
