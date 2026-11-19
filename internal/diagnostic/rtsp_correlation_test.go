package diagnostic

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/onvif"
)

func TestRTSPReceptionGapKeepsSeparateTracksAndRTPProvenance(t *testing.T) {
	events := []Event{{Kind: "video_rtp", Track: 1, ArrivalMS: 0, Artifact: "udp-datagrams.bin", Offset: 10}, {Kind: "video_rtp", Track: 2, ArrivalMS: 500}, {Kind: "video_rtp", Track: 2, ArrivalMS: 1000}, {Kind: "video_rtp", Track: 1, ArrivalMS: 2000, Artifact: "udp-datagrams.bin", Offset: 50}}
	gaps := rtspReceptionGaps(events, 2200, 0, 2200)
	if len(gaps) < 1 || gaps[0].Before.Track != 1 || gaps[0].DurationMS != 2000 || gaps[0].After == nil || gaps[0].After.Offset != 50 || gaps[0].Before.Kind != "video_rtp" || gaps[0].Before.Keyframe || gaps[0].Before.DTS != 0 {
		t.Fatalf("track gaps lost raw RTP provenance: %+v", gaps)
	}
	if events[0].Kind != "video_rtp" {
		t.Fatal("gap projection modified original events")
	}
}

func TestRTSPPlayerStallInvestigationAndEvidenceImport(t *testing.T) {
	for _, transport := range []string{"tcp", "udp"} {
		t.Run(transport, func(t *testing.T) {
			manager := &onvif.Manager{}
			t.Cleanup(manager.Close)
			camera, err := manager.Start(onvif.Config{BindIP: "127.0.0.1"})
			if err != nil {
				t.Fatal(err)
			}
			service := rtspIntegrationOpen(t)
			run, err := service.Start(SampleRequest{URL: camera.StreamURL, Protocol: ProtocolRTSP, Transport: transport, Seconds: 2, Playback: true})
			if err != nil {
				t.Fatal(err)
			}
			player, err := service.PlayerSession(run.ID, run.Player.Token)
			if err != nil {
				t.Fatal(err)
			}
			_, live, closePlayer, err := player.Subscribe()
			if err != nil {
				t.Fatal(err)
			}
			defer closePlayer()
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				for data := range live {
					_, _ = io.Discard.Write(data)
				}
			}()
			detail := rtspIntegrationAwait(t, service, run.ID)
			<-drained
			record := PlayerRecording{Version: 4, Revision: 1, StartedAt: detail.Report.Manifest.Observation.StartedAt, ElapsedMS: 1500, Complete: true, Engine: "mpegtsjs", Source: "same_capture", Events: []PlayerEvent{{Kind: "start"}, {AtMS: 100, Kind: "playing"}, {AtMS: 120, Kind: "first_frame", Method: "video_frame_callback"}, {AtMS: 400, Kind: "waiting"}, {AtMS: 1000, Kind: "playing"}, {AtMS: 1500, Kind: "stop"}}, Samples: []PlayerSample{{AtMS: 200, ReadyState: 4, BufferMS: 500}, {AtMS: 500, ReadyState: 2, BufferMS: 0}}}
			if _, err := service.SavePlayerRecording(run.ID, run.Player.Token, record); err != nil {
				t.Fatal(err)
			}
			manager.Close()
			if _, err := service.InvestigateStall(run.ID, StallInvestigationRequest{StallIndex: 1}); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			var item StallInvestigation
			for time.Now().Before(deadline) {
				detail, err = service.Detail(run.ID)
				if err != nil {
					t.Fatal(err)
				}
				item = detail.Report.StallInvestigations[0]
				if item.Status != "queued" && item.Status != "running" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if len(item.Correlation.BufferSamples) == 0 || len(item.Correlation.Notes) == 0 || item.Media.Status != "unavailable" || !strings.Contains(item.Media.Reason, "原始 RTP") || len(item.Correlation.Keyframes) != 0 || item.Assessment == nil {
				t.Fatalf("RTSP interval investigation failed: %+v", item)
			}
			if detail.Report.Manifest.Version != rtspPlayerSchemaVersion {
				t.Fatal("stall ledger downgraded the RTSP player manifest")
			}
			rtspIntegrationRoundTrip(t, service, detail)
		})
	}
}

func TestRTSPMediaExtractionExplainsUnavailableWithoutGuessing(t *testing.T) {
	for _, protocol := range []string{ProtocolRTSP, ProtocolRTSPS} {
		media := extractStallMedia(context.Background(), t.TempDir(), Report{Manifest: Manifest{Protocol: protocol}}, StallInvestigation{ID: "S001"})
		if media.Status != "unavailable" || !strings.Contains(media.Reason, "原始 RTP") || len(media.Assets) != 0 {
			t.Fatalf("native RTP was mislabeled as an extracted frame: %+v", media)
		}
	}
}
