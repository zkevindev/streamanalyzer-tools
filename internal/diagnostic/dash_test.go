package diagnostic

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSampleDASHBundleRoundTripAndFindings(t *testing.T) {
	mpdBody := `<?xml version="1.0" encoding="UTF-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT6S" minBufferTime="PT2S">
  <Period id="p0">
    <AdaptationSet contentType="video" mimeType="video/mp4" frameRate="30">
      <SegmentTemplate timescale="1000" initialization="init-$RepresentationID$.m4s" media="seg-$RepresentationID$-$Number$.m4s" startNumber="1">
        <SegmentTimeline>
          <S t="0" d="2000"/>
          <S t="4000" d="2000"/>
        </SegmentTimeline>
      </SegmentTemplate>
      <Representation id="v720" bandwidth="1500000" width="1280" height="720" codecs="avc1.4d401f"/>
    </AdaptationSet>
  </Period>
</MPD>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.mpd":
			w.Header().Set("Content-Type", "application/dash+xml")
			_, _ = w.Write([]byte(mpdBody))
		case "/init-v720.m4s":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(bytes.Repeat([]byte("I"), 512))
		case "/seg-v720-1.m4s":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(bytes.Repeat([]byte("M"), 2048))
		case "/seg-v720-2.m4s":
			http.Error(w, "missing segment", http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer svc.Close()

	run, err := svc.Start(SampleRequest{
		Protocol: ProtocolDASH,
		URL:      srv.URL + "/manifest.mpd",
		Seconds:  5,
	})
	if err != nil {
		t.Fatalf("Start DASH: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	var detail Detail
	for time.Now().Before(deadline) {
		detail, err = svc.Detail(run.ID)
		if err == nil && detail.ReportReady {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !detail.ReportReady || detail.Report == nil || detail.Report.DASH == nil {
		t.Fatalf("expected completed DASH report, got %+v", detail)
	}

	dm := detail.Report.DASH
	if dm.MPDType != "static" || dm.RepresentationsCount != 1 {
		t.Fatalf("unexpected DASH metrics: %+v", dm)
	}
	if dm.InitSegmentsCaptured != 1 || dm.SegmentsCaptured != 1 || dm.SegmentsFailed != 1 {
		t.Fatalf("unexpected segment counts: init=%d ok=%d fail=%d", dm.InitSegmentsCaptured, dm.SegmentsCaptured, dm.SegmentsFailed)
	}
	if dm.TimelineGaps != 1 {
		t.Fatalf("expected 1 SegmentTimeline gap, got %d", dm.TimelineGaps)
	}

	var hasGap, hasHTTPErr bool
	for _, f := range detail.Report.Findings {
		if f.Code == "dash_timeline_gap" {
			hasGap = true
		}
		if f.Code == "dash_segment_http_error" {
			hasHTTPErr = true
		}
	}
	if !hasGap || !hasHTTPErr {
		t.Fatalf("expected dash_timeline_gap and dash_segment_http_error findings, got %+v", detail.Report.Findings)
	}

	// Verify HTML rendering
	var htmlBuf bytes.Buffer
	if err := RenderHTML(&htmlBuf, *detail.Report, nil, "/diagnostics"); err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}
	if !strings.Contains(htmlBuf.String(), "MPEG-DASH 清单与分片指标") || !strings.Contains(htmlBuf.String(), "v720") {
		t.Fatalf("rendered HTML missing DASH section")
	}

	// Verify bundle export and re-import round-trip
	var bundleBuf bytes.Buffer
	if err := svc.WriteBundle(run.ID, &bundleBuf); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	imported, err := svc.Import(bytes.NewReader(bundleBuf.Bytes()))
	if err != nil {
		t.Fatalf("Import DASH bundle: %v", err)
	}
	if imported.Protocol != ProtocolDASH {
		t.Fatalf("expected imported protocol dash, got %q", imported.Protocol)
	}
	impDetail, err := svc.Detail(imported.ID)
	if err != nil || impDetail.Report == nil || impDetail.Report.DASH == nil {
		t.Fatalf("expected imported DASH report, got err=%v detail=%+v", err, impDetail)
	}
	if impDetail.Report.DASH.SegmentsCaptured != 1 || impDetail.Report.DASH.TimelineGaps != 1 {
		t.Fatalf("imported DASH metrics mismatch: %+v", impDetail.Report.DASH)
	}
}
