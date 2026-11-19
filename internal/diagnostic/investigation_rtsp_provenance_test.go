package diagnostic

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rtspProvenanceCase(t *testing.T) (string, Report, StallInvestigation) {
	t.Helper()
	_ = rtspG711TestTool(t)
	dir, report, _ := investigationRTSPCaptureFixture(t, "h264")
	record := PlayerRecording{Version: 1, Revision: 1, StartedAt: report.Manifest.Observation.StartedAt, ElapsedMS: 4000, Complete: true, Engine: "mpegtsjs", Source: "same_capture",
		Events:  []PlayerEvent{{Kind: "start"}, {AtMS: 100, Kind: "playing"}, {AtMS: 120, Kind: "first_frame", Method: "video_frame_callback"}, {AtMS: 2500, Kind: "waiting"}, {AtMS: 3300, Kind: "playing"}, {AtMS: 4000, Kind: "stop"}},
		Samples: []PlayerSample{{AtMS: 2400, CurrentMS: 3800, BufferMS: 10, ReadyState: 4}, {AtMS: 2700, CurrentMS: 3800, ReadyState: 2}, {AtMS: 3400, CurrentMS: 3800, BufferMS: 1000, ReadyState: 4}}}
	if err := writePlayerRecording(dir, record); err != nil {
		t.Fatal(err)
	}
	if err := attachPlayerArtifact(dir, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Player.Stalls) != 1 {
		t.Fatal("fixture has no selected stall")
	}
	now := time.Now().UTC()
	item := StallInvestigation{ID: "S001", StallIndex: 1, PlayerRevision: 1, Stall: report.Player.Stalls[0], CreatedAt: now, UpdatedAt: now, Status: "partial", ModelState: "unconfigured"}
	var err error
	item.Correlation, err = correlateStall(dir, report, item.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	item.Assessment = assessStall(item)
	item.Media = extractStallMedia(t.Context(), dir, report, item)
	if item.Media.RTSP == nil || item.Media.Status != "ready" || len(item.Media.Assets) != 4 {
		t.Fatalf("real near-end RTSP extraction incomplete: %+v", item.Media)
	}
	report.StallInvestigations = []StallInvestigation{item}
	if err := persistStallReport(dir, &report); err != nil {
		t.Fatal(err)
	}
	return dir, report, item
}

func TestRTSPDerivedMediaNearEndKeepsActualFramesAndOfflineProof(t *testing.T) {
	dir, report, item := rtspProvenanceCase(t)
	proof := item.Media.RTSP
	if !validRTSPMediaEvidenceShape(proof) || item.Media.EndMS == nil || *item.Media.EndMS > proof.DurationMS {
		t.Fatalf("invalid source extent: %+v", item.Media)
	}
	for _, asset := range item.Media.Assets {
		if asset.MediaTimeMS == nil || *asset.MediaTimeMS < *item.Media.StartMS || *asset.MediaTimeMS > *item.Media.EndMS {
			t.Fatal("screenshot claimed encoder padding as source content")
		}
		if err := validateStallAssetFile(dir, asset); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateRTSPMediaEvidence(dir, report, item); err != nil {
		t.Fatal(err)
	}
	var bundle bytes.Buffer
	if err := writeBundle(&bundle, dir, report); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "") // Import must not launch FFmpeg/ffprobe or reconnect.
	imported, err := readBundle(t.TempDir(), bytes.NewReader(bundle.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(report)
	after, _ := json.Marshal(imported)
	if !bytes.Equal(before, after) {
		t.Fatal("source/derived report changed during offline import")
	}
	model, err := modelInput(imported)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Cases []struct {
			Pixels bool               `json:"image_pixels_provided"`
			Proof  *RTSPMediaEvidence `json:"rtsp_media"`
			Assets []DerivedArtifact  `json:"assets"`
		} `json:"stall_investigations"`
	}
	if json.Unmarshal(model, &payload) != nil || len(payload.Cases) != 1 || payload.Cases[0].Pixels || payload.Cases[0].Proof == nil || len(payload.Cases[0].Assets) != 4 {
		t.Fatal("text model lost source proof or claimed screenshot pixels")
	}
	if bytes.Contains(model, []byte("private-sdp-fixture-title")) || bytes.Contains(model, []byte("rtsp://")) || bytes.Contains(model, []byte("/9j/")) {
		t.Fatal("model received private raw SDP/address or image pixels")
	}
}

func TestRTSPMediaProofRejectsChangedInputsAndRawLocations(t *testing.T) {
	dir, report, item := rtspProvenanceCase(t)
	for name, mutate := range map[string]func(*RTSPMediaEvidence){
		"source":          func(p *RTSPMediaEvidence) { p.SourceArtifact.SHA256 = strings.Repeat("a", 64) },
		"player_revision": func(p *RTSPMediaEvidence) { p.PlayerRevision++ },
		"sample":          func(p *RTSPMediaEvidence) { p.AnchorSampleOffsetMS-- },
		"origin":          func(p *RTSPMediaEvidence) { p.PreviewOriginMS++ },
		"remux":           func(p *RTSPMediaEvidence) { p.RemuxArtifact.SHA256 = strings.Repeat("b", 64) },
		"track":           func(p *RTSPMediaEvidence) { p.VideoTrackID++ },
		"keyframe":        func(p *RTSPMediaEvidence) { p.Keyframes[0].Offset++ },
	} {
		t.Run(name, func(t *testing.T) {
			value := *item.Media.RTSP
			value.Keyframes = append([]Event(nil), value.Keyframes...)
			copy := item
			copy.Media.RTSP = &value
			mutate(&value)
			if err := validateRTSPMediaEvidence(dir, report, copy); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted changed provenance: %v", err)
			}
		})
	}
	private := *item.Media.RTSP
	private.VideoCodec = "rtsp://user:secret@example/private"
	if modelRTSPMedia(&private) != nil {
		t.Fatal("uncontrolled codec text entered model")
	}
	projected := modelRTSPMedia(item.Media.RTSP)
	if projected == nil || len(projected.Keyframes) == 0 {
		t.Fatal("valid provenance lost model keyframes")
	}
	before := *item.Media.RTSP.Keyframes[0].RTPSequence
	*projected.Keyframes[0].RTPSequence++
	if *item.Media.RTSP.Keyframes[0].RTPSequence != before {
		t.Fatal("model projection mutated source RTP reference")
	}
}

func TestRTSPMediaBundleRejectsResignedProvenanceTampering(t *testing.T) {
	dir, report, _ := rtspProvenanceCase(t)
	var archive bytes.Buffer
	if err := writeBundle(&archive, dir, report); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	for name, mutate := range map[string]func(*investigationLedger){
		"player_clock":  func(l *investigationLedger) { l.Items[0].Media.RTSP.AnchorCurrentMS-- },
		"raw_reference": func(l *investigationLedger) { l.Items[0].Media.RTSP.Keyframes[0].Offset++ },
		"remux_hash":    func(l *investigationLedger) { l.Items[0].Media.RTSP.RemuxArtifact.SHA256 = strings.Repeat("a", 64) },
		"source_extent": func(l *investigationLedger) {
			end := l.Items[0].Media.RTSP.DurationMS + 1
			l.Items[0].Media.EndMS = &end
		},
		"stripped_proof":    func(l *investigationLedger) { l.Items[0].Media.RTSP = nil },
		"version_downgrade": func(l *investigationLedger) { l.Version = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			modified := assessmentRewriteBundle(t, archive.Bytes(), mutate)
			if _, err := readBundle(t.TempDir(), bytes.NewReader(modified)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("re-signed RTP provenance tamper accepted: %v", err)
			}
		})
	}
}

func TestRTSPStallLedgerVersionAndExplicitNullAreStrict(t *testing.T) {
	for _, test := range []struct {
		version int
		media   string
		proof   bool
		valid   bool
	}{
		{1, `{"assets":[]}`, false, true}, {1, `{"assets":[],"rtsp":null}`, false, false},
		{2, `{"assets":[]}`, false, false}, {2, `{"assets":[],"rtsp":null}`, false, false},
		{2, `{"assets":[],"rtsp":{"version":1}}`, true, true}, {1, `{"assets":[],"rtsp":{"version":1}}`, true, false},
	} {
		ledger := investigationLedger{Version: test.version, Items: []StallInvestigation{{}}}
		if test.proof {
			ledger.Items[0].Media.RTSP = &RTSPMediaEvidence{Version: 1}
		}
		data := []byte(`{"version":` + string(rune('0'+test.version)) + `,"items":[{"media":` + test.media + `}]}`)
		if validRTSPStallLedgerShape(data, ledger) != test.valid {
			t.Fatalf("bad ledger version/null decision: %s", data)
		}
	}
}

func TestRTSPStallFrameExportKeepsTCPAndUDPRawLocations(t *testing.T) {
	packet := rtspIntegrationRTP(96, 17, 0, 123, []byte{0x65, 0x88}, true)
	for _, transport := range []string{"tcp", "udp"} {
		t.Run(transport, func(t *testing.T) {
			dir := t.TempDir()
			var report Report
			var err error
			if transport == "udp" {
				target, _ := rtspUDPTestServer(t, []rtspUDPTestPacket{{data: packet}, {data: packet, foreign: true}}, false)
				report, err = sampleRTSPTransport(t.Context(), dir, target, 1, ProtocolRTSP, "udp")
			} else {
				source := newRTSPIntegrationFixture(t, rtspIntegrationConfig{packets: []rtspIntegrationPacket{{raw: packet}}, closeAfterPlay: true})
				target, _ := parseProtocolURL(source.address(), ProtocolRTSP)
				report, err = sampleRTSP(t.Context(), dir, target, 1, ProtocolRTSP)
			}
			if err != nil {
				t.Fatal(err)
			}
			var refs []Event
			export := &rtspWireExport{packet: func(media int, data []byte, event Event) error {
				if media != 1 || !bytes.Equal(data, packet) {
					t.Fatal("export changed original RTP")
				}
				refs = append(refs, event)
				return nil
			}}
			if _, err := readRTSPWire(dir, report.Manifest, nil, export); err != nil {
				t.Fatal(err)
			}
			if len(refs) != 1 || refs[0].Kind != "video_rtp" || refs[0].Track != 1 || refs[0].RTPSequence == nil || *refs[0].RTPSequence != 17 {
				t.Fatalf("bad accepted packet refs: %+v", refs)
			}
			raw, err := os.ReadFile(filepath.Join(dir, rtspStreamName))
			if err != nil {
				t.Fatal(err)
			}
			entries := rtspIntegrationZIP(t, raw)
			event := refs[0]
			if !bytes.Equal(entries[event.Artifact][event.Offset:event.Offset+int64(event.Size)], packet) {
				t.Fatal("packet reference lost exact padding/extensions")
			}
		})
	}
}
