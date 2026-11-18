package diagnostic

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"streamanalyzer/internal/hlsdelivery/media"
)

func stallTestFLV(t *testing.T, empty bool) (string, Report) {
	t.Helper()
	dir := t.TempDir()
	data := bytes.NewBuffer([]byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0})
	var arrivals []int64
	add := func(kind byte, at int64, key bool) {
		payload := []byte{0xaf, 1, 0x12}
		if kind == 9 {
			frame := byte(0x27)
			if key {
				frame = 0x17
			}
			payload = []byte{frame, 1, 0, 0, 0, 0x65}
		}
		header := make([]byte, 11)
		header[0] = kind
		putFLVUint24(header[1:4], uint32(len(payload)))
		putFLVTimestamp(header[4:8], uint32(at))
		data.Write(header)
		data.Write(payload)
		data.Write(binary.BigEndian.AppendUint32(nil, uint32(len(payload)+11)))
		arrivals = append(arrivals, at)
	}
	if !empty {
		add(9, 0, true)
		add(8, 100, false)
		add(9, 200, false)
		add(9, 4000, true)
		add(8, 4010, false)
		add(9, 4500, false)
		add(9, 6000, true)
		add(8, 6010, false)
	}
	parser, err := openFLV(bytes.NewReader(data.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var timeline bytes.Buffer
	var events []Event
	for index := 0; ; index++ {
		event, err := parser.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		event.ArrivalMS = arrivals[index]
		events = append(events, event)
		if err := json.NewEncoder(&timeline).Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, streamName), data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, timelineName), timeline.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Version: playerSchemaVersion, Protocol: ProtocolHTTPFLV, Stream: Artifact{Bytes: int64(data.Len())}, Timeline: Artifact{Bytes: int64(timeline.Len())}, Observation: Observation{ElapsedMS: 10000, EndReason: "eof"}}
	report := analyze(manifest, events)
	report.Player = &PlayerReport{Source: "same_capture", Revision: 2, Stalls: []PlayerStall{{StartMS: 3500, EndMS: 5000, DurationMS: 1500}}}
	for at := int64(0); at <= 10000; at += 50 {
		report.Player.BufferSamples = append(report.Player.BufferSamples, PlayerBufferSample{OffsetMS: at, CurrentMS: at, BufferMS: max(0, 3500-at), ReadyState: 3})
	}
	return dir, report
}

func TestStallCorrelationLinksReceiveBufferAndRealKeyframes(t *testing.T) {
	dir, report := stallTestFLV(t, false)
	end := int64(10000)
	report.Manifest.Observation.Requests = []HTTPRequestTiming{{Number: 1, StartMS: 0, EndMS: 50, TotalMS: 50, HTTPStatus: 200, BodyEndMS: &end}}
	before, _ := json.Marshal(report)
	got, err := correlateStall(dir, report, "S001", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.WindowStartMS != 500 || got.WindowEndMS != 8000 || len(got.BufferSamples) != 128 || got.BufferSamples[0].OffsetMS != 500 || got.BufferSamples[len(got.BufferSamples)-1].OffsetMS != 8000 {
		t.Fatalf("window or bounded buffer endpoints incorrect: %+v", got)
	}
	if !slices.ContainsFunc(got.Gaps, func(g ReceptionGap) bool {
		return g.Kind == "video" && g.StartMS == 200 && g.EndMS == 4000 && g.DurationMS == 3800 && g.Before != nil && g.After != nil
	}) {
		t.Fatalf("reception gap missing context frames: %+v", got.Gaps)
	}
	if len(got.Keyframes) != 3 || got.Keyframes[0].ArrivalMS != 0 || got.Keyframes[1].ArrivalMS != 4000 || got.Keyframes[2].ArrivalMS != 6000 {
		t.Fatalf("nearest prior or in-window real keyframe omitted: %+v", got.Keyframes)
	}
	if len(got.Requests) != 1 || got.Requests[0].HTTPStatus != 200 {
		t.Fatal("body reception timing not correlated")
	}
	seen := map[string]bool{}
	for _, finding := range got.Findings {
		if seen[finding.ID] || !strings.HasPrefix(finding.ID, "S001-E") {
			t.Fatalf("unstable evidence ID: %+v", finding)
		}
		seen[finding.ID] = true
	}
	again, err := correlateStall(dir, report, "S001", 1)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatalf("correlation is not reproducible offline: %v", err)
	}
	report.Findings = append(report.Findings, got.Findings...)
	again, err = correlateStall(dir, report, "S001", 1)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatal("existing supplemental findings contaminated re-correlation")
	}
	report.Findings = report.Findings[:len(report.Findings)-len(got.Findings)]
	after, _ := json.Marshal(report)
	if !bytes.Equal(before, after) {
		t.Fatal("correlation mutated source report")
	}
}

func TestStallCorrelationMissingMediaAndIndex(t *testing.T) {
	dir, report := stallTestFLV(t, true)
	got, err := correlateStall(dir, report, "S002", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Keyframes) != 0 || len(got.Gaps) != 0 || !strings.Contains(strings.Join(got.Notes, " "), "没有可识别") {
		t.Fatalf("missing media evidence was invented: %+v", got)
	}
	for _, index := range []int{0, 2} {
		if _, err := correlateStall(dir, report, "S002", index); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid stall index accepted: %v", err)
		}
	}
	if _, err := correlateStall(dir, report, "../../x", 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid evidence ID accepted: %v", err)
	}
	if _, err := correlateStall(dir, report, "S009", 1); !errors.Is(err, ErrInvalid) {
		t.Fatal("out-of-budget investigation ID accepted")
	}
}

func TestStallBufferSamplingRetainsShortDepletion(t *testing.T) {
	var samples []PlayerBufferSample
	for n := 0; n < 1000; n++ {
		samples = append(samples, PlayerBufferSample{OffsetMS: int64(n), BufferMS: 1000})
	}
	samples[537].BufferMS = 0
	got := boundedStallSamples(samples)
	if len(got) != maxStallBufferSamples || got[0].OffsetMS != 0 || got[len(got)-1].OffsetMS != 999 || !slices.ContainsFunc(got, func(s PlayerBufferSample) bool { return s.OffsetMS == 537 && s.BufferMS == 0 }) {
		t.Fatal("buffer downsampling concealed a transient empty buffer")
	}
}

func TestStallReceiveGapsRemainBoundedAndTailExplicit(t *testing.T) {
	var events []Event
	for n := 0; n < 60; n++ {
		events = append(events, Event{Kind: "video", ArrivalMS: int64(n) * 400})
	}
	got := stallReceiveGaps(events, 25000, 0, 25000)
	if len(got) != 16 {
		t.Fatalf("unbounded reception intervals: %d", len(got))
	}
	if !slices.ContainsFunc(got, func(g ReceptionGap) bool { return g.After == nil && g.EndMS == 25000 }) {
		t.Fatal("tail gap not distinguished from measured two-event interval")
	}
}

func TestStallTSUsesMediaPESWithoutInventingKeyframes(t *testing.T) {
	standard := media.SyntheticMPEGTS(true)
	copyData := slices.Clone(standard)
	copy(copyData[:188], testPSIWithCRC(standard[:188]))
	copy(copyData[188:376], testPSIWithCRC(standard[188:376]))
	dir, manifest := tsReplayFixture(t, ProtocolHTTPTS, copyData)
	events, notes, err := stallCaptureEvents(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || !slices.ContainsFunc(events, func(e Event) bool { return e.Kind == "video" && e.PID != nil && *e.PID == 0x101 }) {
		t.Fatalf("recognized PES was not observed: %+v", events)
	}
	if slices.ContainsFunc(events, func(e Event) bool { return e.Keyframe }) || !strings.Contains(strings.Join(notes, " "), "PES 起始不视为关键帧") {
		t.Fatal("TS PES was mistaken for keyframe proof")
	}
}

func TestStallHLSKeepsIndependentSourceAndEncryptionLimits(t *testing.T) {
	var calls atomic.Int32
	clear := media.SyntheticMPEGTS(true)
	copy(clear[:188], testPSIWithCRC(clear[:188]))
	copy(clear[188:376], testPSIWithCRC(clear[188:376]))
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/index.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\n/clear.ts\n#EXT-X-KEY:METHOD=AES-128,URI=\"/key\"\n#EXTINF:1,\n/encrypted.ts\n#EXT-X-ENDLIST\n")
		case "/clear.ts":
			time.Sleep(550 * time.Millisecond)
			w.Write(clear)
		case "/encrypted.ts":
			w.Write(bytes.Repeat([]byte{0x99}, 32))
		default:
			t.Error("correlation unexpectedly fetched a key or network resource")
			http.NotFound(w, r)
		}
	}))
	defer source.Close()
	target, _ := url.Parse(source.URL + "/index.m3u8")
	dir := t.TempDir()
	report, err := sampleHLS(t.Context(), captureClient(), dir, target, 2)
	if err != nil {
		t.Fatal(err)
	}
	report.Player = &PlayerReport{Source: "independent_pull", Revision: 1, Stalls: []PlayerStall{{StartMS: 0, EndMS: 500, DurationMS: 500}}}
	source.Close()
	before := calls.Load()
	got, err := correlateStall(dir, report, "S003", 1)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before || got.Source != "independent_pull" {
		t.Fatal("offline correlation refetched media or lost provenance")
	}
	notes := strings.Join(got.Notes, " ")
	if !strings.Contains(notes, "独立连接") || !strings.Contains(notes, "加密分片") || !strings.Contains(notes, "分片下载完成") {
		t.Fatalf("precision or provenance limitations absent: %s", notes)
	}
	if len(got.Keyframes) != 1 || got.Keyframes[0].Artifact == "" {
		t.Fatalf("actual segment keyframe evidence unavailable: %+v", got.Keyframes)
	}
}
