package appserver

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/gb28181"
)

func TestGB28181RecordingAcrossGap(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip(err)
	}
	clip, err := os.ReadFile("../simulator/hlsorigin/media/segment-01.ts")
	if err != nil {
		t.Fatal(err)
	}
	server := testDiagnosticServer(t, t.TempDir())
	pbx, media := openSimulatorUDP(t), openSimulatorUDP(t)
	const api = "/api/simulators/gb28181"
	const platformID = "34020000002000000001"
	const deviceID = "34020000001320000001"
	const channelID = "34020000001320000002"
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("config", fmt.Sprintf(`{"platform_host":"127.0.0.1","platform_port":%d,"platform_id":"%s","device_id":"%s","channel_id":"%s","recording_start":"2026-01-01T00:00:00Z","recording_count":2,"recording_gap_seconds":4}`, pbx.LocalAddr().(*net.UDPAddr).Port, platformID, deviceID, channelID)); err != nil {
		t.Fatal(err)
	}
	file, err := form.CreateFormFile("video", "repeat.ts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(clip); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Post(server.URL()+api+"/start-file", form.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusAccepted {
		t.Fatalf("start recording: %s %v", raw, err)
	}
	status := decodeDiagnostic[gb28181.Status](t, raw)
	if len(status.Recordings) != 2 || status.Recordings[1].Start.Sub(status.Recordings[0].End) != 4*time.Second {
		t.Fatalf("wrong camera archive: %+v", status.Recordings)
	}
	first, second := status.Recordings[0], status.Recordings[1]
	reg, device := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
	sendSimulatorSIP(t, pbx, device, reg.response("200 OK", ""))
	waitSimulator(t, server, api, "registered")
	uri := "sip:" + channelID + "@" + device.String()
	to := "<" + uri + ">"
	from := "<sip:" + platformID + "@3402000000>;tag=archive-platform"
	send := func(method, to, id, branch, kind, body string, seq int) {
		t.Helper()
		sendSimulatorRequest(t, pbx, device, from, method, uri, to, id, branch, kind, body, seq)
	}
	query := func(begin, end time.Time, id string, total int) {
		t.Helper()
		xml := fmt.Sprintf("<Query><CmdType>RecordInfo</CmdType><SN>61</SN><DeviceID>%s</DeviceID><StartTime>%s</StartTime><EndTime>%s</EndTime></Query>", channelID, begin.Format(time.RFC3339), end.Format(time.RFC3339))
		send("MESSAGE", to, id, id, "Application/MANSCDP+xml", xml, 1)
		receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
		for i := 0; i < max(total, 1); i++ {
			reply, _ := receiveSimulatorSIP(t, pbx, "MESSAGE ", time.Second)
			if !strings.Contains(reply.body, fmt.Sprintf("<SumNum>%d</SumNum>", total)) ||
				total == 2 && !strings.Contains(reply.body, fmt.Sprintf("<Name>Local Recording %d</Name>", i+1)) {
				t.Fatalf("wrong RecordInfo response: %s", reply.body)
			}
			sendSimulatorSIP(t, pbx, device, reply.response("200 OK", ""))
		}
	}
	query(first.Start, second.End, "both-records", 2)
	query(first.End, second.Start, "gap-only", 0)
	inviteBody := func(begin, end time.Time) string {
		return fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 127.0.0.1\r\ns=Playback\r\nc=IN IP4 127.0.0.1\r\nt=%d %d\r\nm=video %d RTP/AVP 96\r\na=rtpmap:96 PS/90000\r\na=recvonly\r\ny=0100000001\r\n", platformID, begin.Unix(), end.Unix(), media.LocalAddr().(*net.UDPAddr).Port)
	}
	send("INVITE", to, "gap-play", "gap-play", "application/sdp", inviteBody(first.End, second.Start), 1)
	receiveSimulatorSIP(t, pbx, "SIP/2.0 488 ", time.Second)
	send("INVITE", to, "full-play", "full-play", "application/sdp", inviteBody(first.Start, second.End), 1)
	answer, _ := receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
	send("ACK", answer.headers["to"], "full-play", "full-ack", "", "", 1)
	done, _ := receiveSimulatorSIP(t, pbx, "MESSAGE ", 8*time.Second)
	if !strings.Contains(done.body, "<NotifyType>121</NotifyType>") {
		t.Fatalf("missing completion notification: %s", done.body)
	}
	sendSimulatorSIP(t, pbx, device, done.response("200 OK", ""))
	status = waitSimulator(t, server, api, "registered")
	marks := status.Sessions[0].Timeline
	if !hasGBPreviewStage(marks, "playback_gap") || !hasGBPreviewStage(marks, "playback_done") || status.VideoFrames < 40 {
		t.Fatalf("cross-gap playback did not reach both archives: %+v", status)
	}
	count := 0
	for _, mark := range marks {
		if mark.Stage == "playback_recording" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("want one start event per archive: %+v", marks)
	}
	send("BYE", answer.headers["to"], "full-play", "full-bye", "", "", 2)
	receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
}
