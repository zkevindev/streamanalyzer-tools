package diagnostic

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"streamanalyzer/internal/hlsdelivery/m3u8"
	"streamanalyzer/internal/rtmpraw"
)

func investigationMediaFixture(t *testing.T) (string, string, string) {
	t.Helper()
	ffmpeg, err := investigationMediaTool("ffmpeg", "")
	if err != nil {
		t.Skip("FFmpeg not installed")
	}
	ffprobe, err := investigationMediaTool("ffprobe", filepath.Dir(ffmpeg))
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	path := filepath.Join(t.TempDir(), "source.flv")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=20", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "6", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-g", "20", "-c:a", "aac", "-f", "flv", "-y", path}
	if err := investigationMediaRun(ctx, ffmpeg, args, nil); err != nil {
		t.Skipf("fixture encoders unavailable: %v", err)
	}
	return ffmpeg, ffprobe, path
}

func investigationMediaSave(t *testing.T, dir, name string, data []byte) Artifact {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	return Artifact{Bytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}
}

func investigationMediaTestReport(protocol string) (Report, StallInvestigation) {
	report := Report{Manifest: Manifest{Protocol: protocol, Observation: Observation{StartedAt: time.Now().UTC(), ElapsedMS: 6000, RequestedSeconds: 6, EndReason: "eof"}}, Player: &PlayerReport{Source: "same_capture", BufferSamples: []PlayerBufferSample{{OffsetMS: 900, CurrentMS: 4000, BufferMS: 500}}}}
	item := StallInvestigation{ID: "S001", StallIndex: 1, Stall: PlayerStall{StartMS: 1000, EndMS: 1800, DurationMS: 800}, Correlation: StallCorrelation{WindowStartMS: 0, WindowEndMS: 4800}}
	return report, item
}

func TestStallMediaOfflineAcrossCapturedProtocols(t *testing.T) {
	ffmpeg, ffprobe, source := investigationMediaFixture(t)
	flv, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	tsFile := filepath.Join(t.TempDir(), "source.ts")
	if err := investigationMediaRun(t.Context(), ffmpeg, []string{"-hide_banner", "-loglevel", "error", "-i", source, "-c", "copy", "-f", "mpegts", "-y", tsFile}, nil); err != nil {
		t.Fatal(err)
	}
	ts, err := os.ReadFile(tsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{ProtocolHTTPFLV, ProtocolRTMP, ProtocolHTTPTS, ProtocolUDPTS, ProtocolRTPTS} {
		t.Run(protocol, func(t *testing.T) {
			dir := t.TempDir()
			report, item := investigationMediaTestReport(protocol)
			payload := flv
			switch protocol {
			case ProtocolRTMP:
				payload = investigationMediaTestRTMP(t, flv)
			case ProtocolHTTPTS:
				payload = ts
			case ProtocolUDPTS, ProtocolRTPTS:
				var wire, timeline bytes.Buffer
				for index, offset := 0, 0; offset < len(ts); index++ {
					end := min(len(ts), offset+7*188)
					data := ts[offset:end]
					if protocol == ProtocolRTPTS {
						packet := []byte{0x80, 33, byte(index >> 8), byte(index), 0, 0, 0, 0, 0, 0, 0, 1}
						data = append(packet, data...)
					}
					span := tsSpan{Offset: int64(wire.Len()), Size: len(data), ArrivalMS: int64(index)}
					wire.Write(data)
					if err := json.NewEncoder(&timeline).Encode(span); err != nil {
						t.Fatal(err)
					}
					offset = end
				}
				payload = wire.Bytes()
				report.Manifest.Timeline = investigationMediaSave(t, dir, timelineName, timeline.Bytes())
			}
			report.Manifest.Stream = investigationMediaSave(t, dir, dumpName(protocol), payload)
			got := extractStallMedia(t.Context(), dir, report, item)
			if got.Status != "ready" || got.Alignment != "player_clock_estimate" || len(got.Assets) != 4 || got.StartMS == nil || *got.StartMS != 1000 {
				t.Fatalf("media extraction failed or used capture clock: %+v", got)
			}
			investigationMediaCheckArtifacts(t, ffprobe, dir, got)
			after, _ := os.ReadFile(filepath.Join(dir, dumpName(protocol)))
			if !bytes.Equal(after, payload) {
				t.Fatal("media extraction changed the original dump")
			}
		})
	}
}

func investigationMediaCheckArtifacts(t *testing.T, ffprobe, dir string, result InvestigationMedia) {
	t.Helper()
	for _, asset := range result.Assets {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(asset.Name)))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		if asset.Bytes != int64(len(data)) || asset.SHA256 != hex.EncodeToString(hash[:]) {
			t.Fatalf("derived artifact hash does not match: %+v", asset)
		}
		if asset.Kind == "clip" {
			probe, err := investigationMediaInspect(t.Context(), ffprobe, filepath.Join(dir, filepath.FromSlash(asset.Name)))
			if err != nil || !probe.hasVideo || !probe.hasAudio || probe.durationMS <= 0 || probe.durationMS > 16000 {
				t.Fatalf("clip is not playable: %+v %v", probe, err)
			}
		} else {
			config, err := jpeg.DecodeConfig(bytes.NewReader(data))
			if err != nil || config.Width > 1280 || config.Height > 720 {
				t.Fatalf("source screenshot is invalid: %+v %v", config, err)
			}
		}
	}
}

func investigationMediaTestRTMP(t *testing.T, flv []byte) []byte {
	t.Helper()
	raw := make([]byte, rtmpraw.DefaultHandshakeSkipBytes)
	raw[0] = 3
	for offset := int(binary.BigEndian.Uint32(flv[5:9])) + 4; offset+15 <= len(flv); {
		size := int(readUint24(flv[offset+1 : offset+4]))
		end := offset + 11 + size + 4
		if end > len(flv) {
			t.Fatal("truncated FLV fixture")
		}
		stamp := readUint24(flv[offset+4:offset+7]) | uint32(flv[offset+7])<<24
		header := make([]byte, 12)
		header[0] = 6
		putFLVUint24(header[1:4], stamp)
		putFLVUint24(header[4:7], uint32(size))
		header[7] = flv[offset]
		header[8] = 1
		raw = append(raw, header...)
		payload := flv[offset+11 : end-4]
		for len(payload) > 0 {
			n := min(128, len(payload))
			raw = append(raw, payload[:n]...)
			payload = payload[n:]
			if len(payload) > 0 {
				raw = append(raw, 0xc6)
			}
		}
		offset = end
	}
	return raw
}

func TestStallMediaHLSUsesLocalCapturedReferences(t *testing.T) {
	ffmpeg, ffprobe, source := investigationMediaFixture(t)
	var requests atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "must not fetch", http.StatusInternalServerError)
	}))
	defer remote.Close()
	for _, fmp4 := range []bool{false, true} {
		t.Run(fmt.Sprint("fmp4=", fmp4), func(t *testing.T) {
			generated := t.TempDir()
			playlist := filepath.Join(generated, "source.m3u8")
			args := []string{"-hide_banner", "-loglevel", "error", "-i", source, "-c", "copy", "-hls_time", "1", "-hls_list_size", "0", "-hls_segment_filename", filepath.Join(generated, "segment-%03d.ts")}
			if fmp4 {
				args = append(args, "-hls_segment_type", "fmp4", "-hls_fmp4_init_filename", "init.mp4")
			}
			args = append(args, "-y", playlist)
			if err := investigationMediaRun(t.Context(), ffmpeg, args, nil); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			report, item := investigationMediaTestReport(ProtocolHLS)
			report.Player.Source = "independent_pull"
			data, _ := os.ReadFile(playlist)
			parsed, err := decodeHLSPlaylist(data)
			if err != nil {
				t.Fatal(err)
			}
			var packed, timeline bytes.Buffer
			archive := zip.NewWriter(&packed)
			add := func(name string, content []byte) {
				entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write(content); err != nil {
					t.Fatal(err)
				}
			}
			// Every playlist URI points to a live endpoint; extraction must ignore
			// those URIs and rebuild references from captured artifact IDs alone.
			remotePlaylist := string(data)
			for _, segment := range parsed.Segments {
				remotePlaylist = strings.ReplaceAll(remotePlaylist, segment.URI, remote.URL+"/private/"+segment.URI+"?token=never-send")
			}
			if parsed.Map != nil {
				remotePlaylist = strings.ReplaceAll(remotePlaylist, parsed.Map.URI, remote.URL+"/private-init?token=never-send")
			}
			add("playlist-0001.m3u8", []byte(remotePlaylist))
			encode := func(row hlsRecord) {
				if err := json.NewEncoder(&timeline).Encode(row); err != nil {
					t.Fatal(err)
				}
			}
			encode(hlsRecord{Kind: hlsPlaylist, Rendition: hlsMain, Result: "ok", Artifact: "playlist-0001.m3u8", Bytes: len(remotePlaylist), HTTPStatus: 200})
			id, initID := 1, 0
			if fmp4 {
				id++
				initID = id
				body, err := os.ReadFile(filepath.Join(generated, "init.mp4"))
				if err != nil {
					t.Fatal(err)
				}
				name := hlsArtifactName(hlsInit, id)
				add(name, body)
				sequence := parsed.Segments[0].Sequence
				encode(hlsRecord{Kind: hlsInit, Rendition: hlsMain, Result: "ok", Artifact: name, Bytes: len(body), Playlist: 1, Sequence: &sequence, HTTPStatus: 200})
			}
			for index, segment := range parsed.Segments {
				id++
				body, err := os.ReadFile(filepath.Join(generated, segment.URI))
				if err != nil {
					t.Fatal(err)
				}
				name := hlsArtifactName(hlsSegment, id)
				add(name, body)
				sequence := segment.Sequence
				encode(hlsRecord{Kind: hlsSegment, Rendition: hlsMain, Result: "ok", Artifact: name, Bytes: len(body), Playlist: 1, Sequence: &sequence, Init: initID, HTTPStatus: 200, ArrivalMS: int64(index*900 + 100)})
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			report.Manifest.Stream = investigationMediaSave(t, dir, hlsStreamName, packed.Bytes())
			report.Manifest.Timeline = investigationMediaSave(t, dir, timelineName, timeline.Bytes())
			got := extractStallMedia(t.Context(), dir, report, item)
			if got.Status != "ready" || got.Alignment != "independent_capture_reference" || len(got.Assets) != 4 {
				t.Fatalf("HLS local extraction failed: %+v", got)
			}
			investigationMediaCheckArtifacts(t, ffprobe, dir, got)
			if requests.Load() != 0 {
				t.Fatal("HLS extraction requested an original remote URI")
			}
		})
	}
}

func TestStallMediaAudioOnlyAndInterruptedPublicationRecovery(t *testing.T) {
	ffmpeg, ffprobe, source := investigationMediaFixture(t)
	audio := filepath.Join(t.TempDir(), "audio.flv")
	if err := investigationMediaRun(t.Context(), ffmpeg, []string{"-hide_banner", "-loglevel", "error", "-i", source, "-vn", "-c:a", "copy", "-f", "flv", "-y", audio}, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(audio)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	report, item := investigationMediaTestReport(ProtocolHTTPFLV)
	report.Manifest.Stream = investigationMediaSave(t, dir, streamName, data)
	orphan := filepath.Join(dir, "stalls", item.ID)
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"clip.mp4", "during.jpg"} {
		if err := os.WriteFile(filepath.Join(orphan, name), []byte("interrupted"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := extractStallMedia(t.Context(), dir, report, item)
	if got.Status != "partial" || len(got.Assets) != 1 || !strings.Contains(got.Reason, "只有音频") {
		t.Fatalf("audio-only extraction invented screenshots: %+v", got)
	}
	probe, err := investigationMediaInspect(t.Context(), ffprobe, filepath.Join(dir, filepath.FromSlash(got.Assets[0].Name)))
	if err != nil || probe.hasVideo || !probe.hasAudio || probe.durationMS <= 0 {
		t.Fatalf("audio-only clip invalid: %+v %v", probe, err)
	}
	entries, _ := os.ReadDir(orphan)
	if len(entries) != 1 || entries[0].Name() != "clip.mp4" {
		t.Fatal("unreferenced interrupted files survived publication")
	}
}

func TestStallMediaUnavailableAndCanceledPreserveEvidence(t *testing.T) {
	dir := t.TempDir()
	report, item := investigationMediaTestReport(ProtocolHTTPFLV)
	report.Manifest.Stream = investigationMediaSave(t, dir, streamName, []byte("not a media file"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := extractStallMedia(ctx, dir, report, item); got.Status != "failed" || len(got.Assets) != 0 || !strings.Contains(got.Reason, "取消") {
		t.Fatalf("canceled extraction created artifacts: %+v", got)
	}
	report.Player.BufferSamples = nil
	if input, err := investigationMediaPrepare(t.Context(), dir, t.TempDir(), report, item); err == nil || input.path != "" {
		t.Fatal("capture time was guessed as media time without a player clock")
	}
	if err := os.MkdirAll(filepath.Join(dir, "stalls", "S001"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stalls", "S001", "unknown.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := investigationMediaRemoveOrphans(filepath.Join(dir, "stalls", "S001")); err == nil {
		t.Fatal("orphan cleanup removed an unknown file")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, streamName)); string(data) != "not a media file" {
		t.Fatal("failure changed raw evidence")
	}
}

func TestStallMediaEncryptedHLSDoesNotReadKeys(t *testing.T) {
	stage := t.TempDir()
	segments := []investigationMediaHLSSegment{{segment: m3u8.Segment{Key: &m3u8.Key{Method: "AES-128", URI: "https://private.example/key"}}}}
	if err := investigationMediaWriteHLS(t.Context(), stage, "main", segments, map[string]*zip.File{}, ""); err == nil || !strings.Contains(err.Error(), "加密") {
		t.Fatalf("encrypted HLS handling: %v", err)
	}
	entries, _ := os.ReadDir(stage)
	if len(entries) != 0 {
		t.Fatal("encrypted extraction produced media files")
	}
}

// Ensure source timing normalization cannot silently choose a distant region.
func TestStallMediaWindowClampsToMediaDuration(t *testing.T) {
	start, end := investigationMediaWindow(7000, 5000, 6000)
	if start != 4000 || end != 6000 {
		t.Fatalf("incorrect clipped media window: %d %d", start, end)
	}
	start, end = investigationMediaWindow(60000, 1000, 6000)
	if end-start >= 100 {
		t.Fatal("out-of-range player time selected an unrelated clip")
	}
}
