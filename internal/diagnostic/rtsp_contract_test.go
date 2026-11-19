package diagnostic

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestRTSPTargetCredentialsStayOutOfOrigin(t *testing.T) {
	for _, protocol := range []string{ProtocolRTSP, ProtocolRTSPS} {
		target, err := parseSampleTarget(protocol+"://camera-user:private-password@camera.example:8554/profile?token=private-token#fragment", protocol)
		if err != nil || target.User == nil {
			t.Fatalf("RTSP authentication input rejected: %v", err)
		}
		if origin(target) != protocol+"://camera.example:8554" || target.Fragment != "" {
			t.Fatalf("RTSP origin leaked source details: %s", origin(target))
		}
		other := ProtocolRTSP
		if protocol == ProtocolRTSP {
			other = ProtocolRTSPS
		}
		if _, err := parseSampleTarget(target.String(), other); !errors.Is(err, ErrInvalid) {
			t.Fatal("protocol/scheme mismatch accepted")
		}
	}
	for _, protocol := range []string{ProtocolHTTPFLV, ProtocolHLS, ProtocolHTTPTS, ProtocolRTMP} {
		scheme := "http"
		if protocol == ProtocolRTMP {
			scheme = "rtmp"
		}
		if _, err := parseSampleTarget(scheme+"://user:password@camera.example/app/stream", protocol); !errors.Is(err, ErrInvalid) {
			t.Fatalf("userinfo policy unexpectedly changed for %s", protocol)
		}
	}
}

func TestRTSPManifestRejectsUnsupportedBrowserRecording(t *testing.T) {
	for _, protocol := range []string{ProtocolRTSP, ProtocolRTSPS} {
		manifest := Manifest{Version: multiProtocolSchemaVersion, Protocol: protocol, Observation: Observation{
			Origin: protocol + "://camera.example:8554", StartedAt: time.Now().UTC(), RequestedSeconds: 1,
			ElapsedMS: 1000, EndReason: "duration_limit",
		}}
		read := func(value Manifest) error {
			var buffer bytes.Buffer
			writer := zip.NewWriter(&buffer)
			entry, err := writer.Create(manifestName)
			if err != nil {
				return err
			}
			if err := json.NewEncoder(entry).Encode(value); err != nil {
				return err
			}
			if err := writer.Close(); err != nil {
				return err
			}
			archive, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
			if err != nil {
				return err
			}
			_, err = readManifest(archive.File[0])
			return err
		}
		if err := read(manifest); err != nil {
			t.Fatal(err)
		}
		manifest.Version = playerSchemaVersion
		manifest.Player = &Artifact{}
		if err := read(manifest); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unsupported RTSP player recording accepted: %v", err)
		}
	}
}
