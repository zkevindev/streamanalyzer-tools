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
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/gb28181"
)

func TestGB28181RecordingPlayback(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("recording playback needs FFmpeg")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("recording playback needs ffprobe")
	}
	clearDiagnosticModelEnv(t)
	platform, media := openSimulatorUDP(t), openSimulatorUDP(t)
	server := testDiagnosticServer(t, t.TempDir())
	file := filepath.Join(t.TempDir(), "recording.mp4")
	if out, err := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x96:rate=20",
		"-t", "3", "-c:v", "libx264", "-g", "10", "-bf", "0", file).CombinedOutput(); err != nil {
		t.Fatalf("generate recording: %v %s", err, out)
	}
	video, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	const deviceID = "34020000001320000001"
	const channelID = "34020000001320000002"
	const platformID = "34020000002000000001"
	const api = "/api/simulators/gb28181"
	start := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	cfg, err := json.Marshal(gb28181.Config{PlatformHost: "127.0.0.1", PlatformPort: platform.LocalAddr().(*net.UDPAddr).Port,
		PlatformID: platformID, DeviceID: deviceID, ChannelID: channelID, RecordingStart: start.Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("config", string(cfg)); err != nil {
		t.Fatal(err)
	}
	part, err := form.CreateFormFile("video", "recording.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(video); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL()+api+"/start-file", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start recording source: %s %v", data, err)
	}
	status := decodeDiagnostic[gb28181.Status](t, data)
	if status.Recording == nil || !status.Recording.Start.Equal(start) || status.Recording.DurationMS != 3000 {
		t.Fatalf("invalid recording metadata: %+v", status.Recording)
	}

	register, device := receiveSimulatorSIP(t, platform, "REGISTER ", 2*time.Second)
	sendSimulatorSIP(t, platform, device, register.response("200 OK", ""))
	waitSimulator(t, server, api, "registered")
	channel := "sip:" + channelID + "@" + device.String()
	from := "<sip:" + platformID + "@3402000000>;tag=mock-platform"
	query := func(id string, begin, end time.Time, count string) {
		t.Helper()
		xml := fmt.Sprintf("<Query><CmdType>RecordInfo</CmdType><SN>44</SN><DeviceID>%s</DeviceID><StartTime>%s</StartTime><EndTime>%s</EndTime><Type>all</Type></Query>",
			channelID, begin.Format(time.RFC3339), end.Format(time.RFC3339))
		sendSimulatorRequest(t, platform, device, from, "MESSAGE", channel, "<"+channel+">", id, id, "Application/MANSCDP+xml", xml, 1)
		receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
		answer, _ := receiveSimulatorSIP(t, platform, "MESSAGE ", time.Second)
		if !strings.Contains(answer.body, "<CmdType>RecordInfo</CmdType>") || !strings.Contains(answer.body, "<SN>44</SN>") ||
			!strings.Contains(answer.body, "<SumNum>"+count+"</SumNum>") {
			t.Fatalf("unexpected RecordInfo: %s", answer.body)
		}
		sendSimulatorSIP(t, platform, device, answer.response("200 OK", ""))
	}
	query("outside", start.Add(-3*time.Second), start.Add(-time.Second), "0")
	query("overlap", start.Add(time.Second), start.Add(2*time.Second), "1")

	offer := fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 127.0.0.1\r\ns=Playback\r\nc=IN IP4 127.0.0.1\r\nt=%d %d\r\n"+
		"m=video %d RTP/AVP 96\r\na=rtpmap:96 PS/90000\r\ny=1000000001\r\n", platformID, start.Unix(), start.Add(3*time.Second).Unix(), media.LocalAddr().(*net.UDPAddr).Port)
	sendSimulatorRequest(t, platform, device, from, "INVITE", channel, "<"+channel+">", "record-play", "record-invite", "application/sdp", offer, 1)
	answer, _ := receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
	if !strings.Contains(answer.body, "s=Playback\r\n") || !strings.Contains(answer.body, fmt.Sprintf("t=%d %d\r\n", start.Unix(), start.Add(3*time.Second).Unix())) {
		t.Fatalf("invalid playback negotiation: %s", answer.body)
	}
	dialog := func(method, branch, payload string, sequence int) {
		t.Helper()
		contentType := ""
		if payload != "" {
			contentType = "Application/MANSRTSP"
		}
		sendSimulatorRequest(t, platform, device, from, method, channel, answer.headers["to"], "record-play", branch, contentType, payload, sequence)
	}
	dialog("ACK", "record-ack", "", 1)
	media.SetReadDeadline(time.Now().Add(2 * time.Second))
	before := readRecordingUnit(t, media)
	dialog("INFO", "pause", "PAUSE MANSRTSP/1.0\r\nCSeq: 2\r\nPauseTime: now\r\n\r\n", 2)
	receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
	waitSimulatorMedia(t, server, api, "paused", time.Second)
	media.SetReadDeadline(time.Now().Add(120 * time.Millisecond))
	for {
		if _, _, err := media.ReadFromUDP(make([]byte, 1500)); err != nil {
			break
		}
	}
	paused := waitSimulatorMedia(t, server, api, "paused", time.Second)
	time.Sleep(100 * time.Millisecond)
	if again := waitSimulatorMedia(t, server, api, "paused", time.Second); again.RTPPackets != paused.RTPPackets {
		t.Fatal("pause did not stop the media clock")
	}

	dialog("INFO", "resume", "PLAY MANSRTSP/1.0\r\nCSeq: 3\r\nRange: npt=now-\r\n\r\n", 3)
	receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
	media.SetReadDeadline(time.Now().Add(time.Second))
	resumed := readRecordingUnit(t, media)
	if int32(resumed-before) <= 0 || (resumed-before)%4500 != 0 {
		t.Fatalf("resume changed the source position: %d to %d", before, resumed)
	}

	dialog("INFO", "seek", "PLAY MANSRTSP/1.0\r\nCSeq: 4\r\nRange: npt=1-2\r\nScale: 2.0\r\n\r\n", 4)
	receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
	media.SetReadDeadline(time.Now().Add(2 * time.Second))
	previous := resumed
	faster := false
	for range 6 {
		next := readRecordingUnit(t, media)
		if int32(next-previous) <= 0 {
			t.Fatalf("seek broke RTP continuity: %d to %d", previous, next)
		}
		if next-previous == 2250 {
			faster = true
			break
		}
		previous = next
	}
	if !faster {
		t.Fatal("2x playback did not produce 2250-tick frames")
	}
	done, _ := receiveSimulatorSIP(t, platform, "MESSAGE ", 3*time.Second)
	if !strings.Contains(done.body, "<NotifyType>121</NotifyType>") {
		t.Fatalf("no replay end notification: %s", done.body)
	}
	sendSimulatorSIP(t, platform, device, done.response("200 OK", ""))
	status = waitSimulatorMedia(t, server, api, "playback_done", time.Second)
	if len(status.Sessions) != 1 || status.Sessions[0].Mode != "playback" {
		t.Fatalf("missing replay evidence: %+v", status.Sessions)
	}
	for _, stage := range []string{"playback_pause", "playback_play", "playback_seek", "playback_done"} {
		found := false
		for _, mark := range status.Sessions[0].Timeline {
			if mark.Stage == stage {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s in playback evidence", stage)
		}
	}
	dialog("BYE", "record-bye", "", 5)
	receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
	waitSimulatorMedia(t, server, api, "idle", time.Second)
	diagnosticRequest(t, server, http.MethodPost, api+"/stop", http.StatusAccepted, nil)
	unregister, _ := receiveSimulatorSIP(t, platform, "REGISTER ", time.Second)
	sendSimulatorSIP(t, platform, device, unregister.response("200 OK", ""))
	waitSimulator(t, server, api, "stopped")
}

func readRecordingUnit(t *testing.T, media *net.UDPConn) uint32 {
	t.Helper()
	packet := make([]byte, 1500)
	for {
		n, _, err := media.ReadFromUDP(packet)
		if err != nil || n < 12 {
			t.Fatalf("read recording RTP: %d %v", n, err)
		}
		if packet[1]&0x80 != 0 {
			return binary.BigEndian.Uint32(packet[4:8])
		}
	}
}
