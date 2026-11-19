package appserver

import (
	"encoding/json"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/mediasource"
)

func TestMediaSimulatorReportsControlledFaults(t *testing.T) {
	const api = "/api/simulators/media"
	for _, test := range []struct {
		protocol, fault, code string
	}{
		{diagnostic.ProtocolHTTPFLV, "long_gop", "long_gop"},
		{diagnostic.ProtocolHTTPFLV, "dts_backwards", "video_dts_backwards"},
		{diagnostic.ProtocolHTTPTS, "continuity", "ts_continuity_error"},
		{diagnostic.ProtocolHTTPTS, "pcr_jump", "ts_pcr_backwards"},
	} {
		t.Run(test.fault, func(t *testing.T) {
			t.Parallel()
			server := testDiagnosticServer(t, t.TempDir())
			page := diagnosticRequest(t, server, "GET", "/simulators/media", http.StatusOK, nil)
			if !strings.Contains(string(page), "HTTP-FLV / MPEG-TS 模拟器") {
				t.Fatal("media simulator entry missing")
			}
			diagnosticRequest(t, server, "POST", api+"/start", http.StatusBadRequest, []byte(`{"protocol":"http-flv","fault":"continuity"}`))
			raw, err := json.Marshal(mediasource.Config{Protocol: test.protocol, Fault: test.fault})
			if err != nil {
				t.Fatal(err)
			}
			started := decodeDiagnostic[mediasource.Status](t, diagnosticRequest(t, server, "POST", api+"/start", http.StatusCreated, raw))
			if !started.Active || started.URL == "" || started.DurationMS < 5000 {
				t.Fatalf("invalid media source: %+v", started)
			}
			diagnosticRequest(t, server, "HEAD", started.URL, http.StatusOK, nil)
			req, err := json.Marshal(diagnostic.SampleRequest{Protocol: test.protocol, Seconds: 16, URL: server.URL() + started.URL})
			if err != nil {
				t.Fatal(err)
			}
			created := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", "/api/diagnostics/runs", http.StatusAccepted, req))
			runPath := "/api/diagnostics/runs/" + created.ID
			var result diagnostic.Detail
			deadline := time.Now().Add(19 * time.Second)
			for time.Now().Before(deadline) {
				result = decodeDiagnostic[diagnostic.Detail](t, diagnosticRequest(t, server, "GET", runPath, http.StatusOK, nil))
				if result.Status != "running" {
					break
				}
				time.Sleep(80 * time.Millisecond)
			}
			if result.Report == nil || result.Status != "done" {
				t.Fatalf("diagnostic source did not finish: %+v", result)
			}
			if !slices.ContainsFunc(result.Report.Findings, func(f diagnostic.Finding) bool {
				return f.Code == test.code && len(f.Evidence) > 0
			}) {
				t.Fatalf("report omitted %s with evidence: %+v", test.code, result.Report.Findings)
			}
			if test.fault == "pcr_jump" && !slices.ContainsFunc(result.Report.Findings, func(f diagnostic.Finding) bool {
				return f.Code == "ts_pcr_gap"
			}) {
				t.Fatal("PCR jump did not report both edges")
			}
			state := decodeDiagnostic[mediasource.Status](t, diagnosticRequest(t, server, "GET", api, http.StatusOK, nil))
			if state.Requests != 1 || state.FaultHits != 1 || state.LastFault == nil {
				t.Fatalf("incorrect source trigger evidence: %+v", state)
			}
			diagnosticRequest(t, server, "POST", api+"/stop", http.StatusOK, nil)
			diagnosticRequest(t, server, "GET", started.URL, http.StatusNotFound, nil)
		})
	}
}

func TestMediaSimulatorDatagramFaults(t *testing.T) {
	for _, tc := range []struct {
		protocol, fault string
		multicast       bool
	}{
		{"rtp-ts", "rtp_drop", false}, {"rtp-ts", "rtp_reorder", false}, {"rtp-ts", "rtp_duplicate", false}, {"rtp-ts", "pmt_pid_switch", false}, {"udp-ts", "continuity", true},
	} {
		t.Run(tc.fault, func(t *testing.T) {
			t.Parallel()
			const api = "/api/simulators/media"
			server := testDiagnosticServer(t, t.TempDir())
			reservation := openSimulatorUDP(t)
			port := reservation.LocalAddr().(*net.UDPAddr).Port
			reservation.Close()
			cfg := mediasource.Config{Protocol: tc.protocol, Fault: tc.fault, TargetIP: "127.0.0.1", TargetPort: port}
			if tc.multicast {
				nics, err := net.Interfaces()
				if err != nil {
					t.Fatal(err)
				}
				for _, nic := range nics {
					if nic.Flags&net.FlagLoopback != 0 {
						cfg.Interface = nic.Name
						break
					}
				}
				if cfg.Interface == "" {
					t.Skip("loopback interface unavailable")
				}
				cfg.TargetIP = "239.255.103.220"
			}
			config, _ := json.Marshal(cfg)
			source := decodeDiagnostic[mediasource.Status](t, diagnosticRequest(t, server, "POST", api+"/start", http.StatusCreated, config))
			seconds := 4
			if tc.fault == "pmt_pid_switch" {
				seconds = 5
			}
			request, _ := json.Marshal(diagnostic.SampleRequest{Protocol: tc.protocol, URL: source.URL, Seconds: seconds})
			run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", "/api/diagnostics/runs", http.StatusAccepted, request))
			time.Sleep(150 * time.Millisecond)
			diagnosticRequest(t, server, "POST", api+"/send", http.StatusAccepted, nil)
			report := waitDiagnostic(t, server, "/api/diagnostics/runs/"+run.ID).Report
			if report == nil || report.MPEGTS == nil || report.MPEGTS.Packets == 0 {
				t.Fatal("MPEG-TS packets did not reach diagnostic capture")
			}
			m := report.MPEGTS
			switch tc.fault {
			case "rtp_drop":
				if m.RTPMissing != 1 {
					t.Fatalf("wrong RTP missing count: %+v", m)
				}
			case "rtp_reorder":
				if m.RTPOutOfOrder != 1 || m.RTPMissing != 0 {
					t.Fatalf("wrong RTP reorder count: %+v", m)
				}
			case "rtp_duplicate":
				if m.RTPDuplicates != 1 {
					t.Fatalf("wrong RTP duplicates: %+v", m)
				}
			case "pmt_pid_switch":
				if m.RTPMissing != 0 || m.RTPOutOfOrder != 0 || m.RTPDuplicates != 0 || m.ContinuityErrors != 0 || m.PSIErrors != 0 ||
					len(m.ProgramMaps) != 1 {
					t.Fatalf("PMT/PID switch introduced a transport fault or lacked map evidence: %+v", m)
				}
				change := m.ProgramMaps[0]
				if change.Before.Version == change.After.Version || change.Before.PCRPID != 256 || change.After.PCRPID != 300 || len(change.After.Streams) != 2 {
					t.Fatalf("wrong program map: %+v", change)
				}
				for i, after := range change.After.Streams {
					if after.PID != uint16(300+i) || after.FirstPacketMS == nil || *after.FirstPacketMS < change.AtMS {
						t.Fatalf("missing first packet after switching PID: %+v", after)
					}
				}
				html := diagnosticRequest(t, server, "GET", "/api/diagnostics/runs/"+run.ID+"/report.html", http.StatusOK, nil)
				if !strings.Contains(string(html), "PMT 与媒体 PID 切换") {
					t.Fatal("report does not render the PID switch")
				}
				bundle := diagnosticRequest(t, server, "GET", "/api/diagnostics/runs/"+run.ID+"/evidence.zip", http.StatusOK, nil)
				replay := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", "/api/diagnostics/import", http.StatusCreated, bundle))
				offline := waitDiagnostic(t, server, "/api/diagnostics/runs/"+replay.ID)
				beforeJSON, _ := json.Marshal(m.ProgramMaps)
				afterJSON, _ := json.Marshal(offline.Report.MPEGTS.ProgramMaps)
				if string(beforeJSON) != string(afterJSON) {
					t.Fatalf("offline replay lost program map evidence: %s vs %s", beforeJSON, afterJSON)
				}
			case "continuity":
				if m.ContinuityErrors != 1 {
					t.Fatalf("wrong TS continuity errors: %+v", m)
				}
			}
			sent := decodeDiagnostic[mediasource.Status](t, diagnosticRequest(t, server, "GET", api, http.StatusOK, nil))
			if sent.SentDatagrams == 0 || sent.FaultHits != 1 || sent.SendError != "" {
				t.Fatalf("wrong sender evidence: %+v", sent)
			}
			diagnosticRequest(t, server, "POST", api+"/stop", http.StatusOK, nil)
		})
	}
}
