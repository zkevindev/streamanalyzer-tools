package diagnostic

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

func TestFLVCodecEvidenceAndV1Replay(t *testing.T) {
	// Older bundles treated the start of AV1 CodedFrames as an AVC-style
	// composition offset and left G.711 unnamed in the timeline.
	dump := bytes.NewBuffer([]byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0})
	appendTag := func(kind byte, payload []byte) Event {
		offset := dump.Len()
		dump.Write([]byte{kind, 0, 0, byte(len(payload)), 0, 3, 232, 0, 0, 0, 0})
		dump.Write(payload)
		dump.Write(binary.BigEndian.AppendUint32(nil, uint32(11+len(payload))))
		return Event{Offset: int64(offset), Size: dump.Len() - offset, DTS: 1000, PTS: 1000}
	}
	video := appendTag(9, []byte{0x91, 'a', 'v', '0', '1', 0, 0, 16, 0x10})
	video.Kind, video.Codec, video.Keyframe, video.PTS = "video", "av01", true, 1016
	audio := appendTag(8, []byte{0x72, 0x55})
	audio.Kind = "audio"
	opus := appendTag(8, []byte{0x91, 'O', 'p', 'u', 's', 0x11})
	opus.Kind, opus.Codec = "audio", "Opus"

	parser, err := openFLV(bytes.NewReader(dump.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	for {
		event, err := parser.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if len(events) != 3 || events[0].PTS != 1000 || events[0].Kind != "video" {
		t.Fatalf("incorrect enhanced video timestamp or packet type: %+v", events)
	}

	dir := t.TempDir()
	save := func(name string, body []byte) Artifact {
		t.Helper()
		file, err := newArtifact(dir, name)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.Write(body)
		if err := errors.Join(writeErr, file.close()); err != nil {
			t.Fatal(err)
		}
		return file.artifact()
	}
	var timeline bytes.Buffer
	for _, event := range []Event{video, audio, opus} {
		if err := json.NewEncoder(&timeline).Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	manifest := Manifest{
		Version: legacyFLVSchemaVersion, Protocol: ProtocolHTTPFLV,
		Observation: Observation{Origin: "http://origin.example", StartedAt: time.Now().UTC(), RequestedSeconds: 1, EndReason: "eof"},
		Stream:      save(streamName, dump.Bytes()), Timeline: save(timelineName, timeline.Bytes()),
	}
	original := Report{Manifest: manifest}
	if err := persistReport(dir, original); err != nil {
		t.Fatal(err)
	}
	var bundle bytes.Buffer
	if err := writeBundle(&bundle, dir, original); err != nil {
		t.Fatal(err)
	}
	replayed, err := readBundle(t.TempDir(), &bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, report := range []Report{replayed, analyze(manifest, events)} {
		if !reflect.DeepEqual(report.Metrics.VideoCodecs, []string{"av1"}) ||
			!reflect.DeepEqual(report.Metrics.AudioCodecs, []string{"g711-alaw", "opus"}) {
			t.Fatalf("incorrect codec evidence: %+v", report.Metrics)
		}
	}
}
