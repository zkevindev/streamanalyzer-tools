package diagnostic

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"math"
	"path/filepath"
	"sort"

	"github.com/pion/rtp"
)

type artifactHashWriter struct {
	h hash.Hash
	n int64
}

func newArtifactHashOnly() (*artifactHashWriter, error) {
	return &artifactHashWriter{h: sha256.New()}, nil
}

func (w *artifactHashWriter) Write(p []byte) (int, error) {
	n, err := w.h.Write(p)
	w.n += int64(n)
	return n, err
}

func (w *artifactHashWriter) artifact() Artifact {
	return Artifact{Bytes: w.n, SHA256: hex.EncodeToString(w.h.Sum(nil))}
}

func replayWHEP(dir string, manifest Manifest) (Report, error) {
	report, _, err := replayWHEPWithEvents(dir, manifest)
	return report, err
}

func replayWHEPWithEvents(dir string, manifest Manifest) (Report, []Event, error) {
	archivePath := filepath.Join(dir, whepStreamName)
	zr, f, err := openEvidenceZIP(archivePath)
	if err != nil {
		return Report{}, nil, fmt.Errorf("%w: WHEP 证据归档不可读", ErrInvalid)
	}
	defer f.Close()

	readEntry := func(name string, limit int64) ([]byte, bool, error) {
		for _, zf := range zr.File {
			if zf.Name != name {
				continue
			}
			rc, err := zf.Open()
			if err != nil {
				return nil, true, err
			}
			defer rc.Close()
			b, err := io.ReadAll(io.LimitReader(rc, limit+1))
			if err != nil {
				return nil, true, err
			}
			if int64(len(b)) > limit {
				return nil, true, fmt.Errorf("%w: WHEP 归档条目超限", ErrInvalid)
			}
			return b, true, nil
		}
		return nil, false, nil
	}

	metaBytes, ok, err := readEntry(whepMetaFileName, 2<<20)
	if err != nil || !ok {
		return Report{}, nil, fmt.Errorf("%w: WHEP 归档缺少 metadata.json", ErrInvalid)
	}
	var meta whepArchiveMetadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return Report{}, nil, fmt.Errorf("%w: WHEP metadata 无效", ErrInvalid)
	}
	offerBytes, _, _ := readEntry(whepOfferFileName, 1<<20)
	answerBytes, _, _ := readEntry(whepAnswerFileName, 1<<20)
	rtpData, _, err := readEntry(whepRTPDataFileName, maxStreamBytes)
	if err != nil {
		return Report{}, nil, err
	}
	rtpIdxBytes, _, err := readEntry(whepRTPIndexFileName, maxTimeline)
	if err != nil {
		return Report{}, nil, err
	}

	var idxEntries []whepRTPIndexEntry
	if len(rtpIdxBytes) > 0 {
		sc := bufio.NewScanner(bytes.NewReader(rtpIdxBytes))
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 {
				continue
			}
			var e whepRTPIndexEntry
			if err := json.Unmarshal(line, &e); err == nil {
				idxEntries = append(idxEntries, e)
			}
		}
	}

	return analyzeWHEPArchive(manifest, meta, string(offerBytes), string(answerBytes), rtpData, idxEntries)
}

type whepTrackAccum struct {
	metrics         WHEPTrackMetrics
	hasSeq          bool
	maxSeq          uint16
	seenSeq         map[uint16]struct{}
	lastArrivalMS   int64
	hasArrival      bool
	hasTransit      bool
	lastTransit     float64
	jitterTicks     float64
	maxJitterTicks  float64
	hasFrameTS      bool
	lastFrameTS     uint32
	lastFrameArrMS  int64
	lastKeyframeMS  int64
	hasKeyframe     bool
	curFrameIsKey   bool
	curFrameStartMS int64
}

func analyzeWHEPArchive(manifest Manifest, meta whepArchiveMetadata, offerSDP, answerSDP string, rtpData []byte, idxEntries []whepRTPIndexEntry) (Report, []Event, error) {
	whepMetrics := &WHEPMetrics{
		Boundary:           "native_whep_webrtc_receive",
		SignalingMode:      meta.SignalingMode,
		HTTPStatus:         meta.HTTPStatus,
		LocationRecorded:   meta.LocationRecorded,
		LocationPath:       meta.LocationPath,
		ETag:               meta.ETag,
		LinkICEServers:     meta.LinkICEServers,
		DeleteStatus:       meta.DeleteStatus,
		ICETransportPolicy: meta.ICETransportPolicy,
		ICEState:           meta.ICEState,
		DTLSState:          meta.DTLSState,
		PeerState:          meta.PeerState,
		GatherMS:           meta.GatherMS,
		ICEConnectMS:       meta.ICEConnectMS,
		DTLSConnectMS:      meta.DTLSConnectMS,
		FirstRTCPMS:        meta.FirstRTCPMS,
		SelectedPair:       meta.SelectedPair,
		LocalCandidates:    meta.LocalCandidates,
		RemoteCandidates:   meta.RemoteCandidates,
		OfferSDP:           parseWHEPSDPDigest(offerSDP),
		AnswerSDP:          parseWHEPSDPDigest(answerSDP),
		TruncatedPackets:   meta.TruncatedPackets,
	}

	trackMap := make(map[int]*whepTrackAccum, len(meta.Tracks))
	for _, tm := range meta.Tracks {
		clockRate := tm.ClockRate
		if clockRate == 0 {
			if tm.Kind == "audio" {
				clockRate = 48000
			} else {
				clockRate = 90000
			}
		}
		trackMap[tm.ID] = &whepTrackAccum{
			metrics: WHEPTrackMetrics{
				ID:                 tm.ID,
				Kind:               tm.Kind,
				Codec:              tm.Codec,
				MimeType:           tm.MimeType,
				PayloadType:        tm.PayloadType,
				ClockRate:          clockRate,
				SSRC:               tm.SSRC,
				PLICount:           tm.PLICount,
				PLIFirstRecoveryMS: tm.PLIFirstRecoveryMS,
				RTCPSRCount:        tm.RTCPSRCount,
				RTCPRRCount:        tm.RTCPRRCount,
			},
			seenSeq: make(map[uint16]struct{}, 256),
		}
	}

	events := make([]Event, 0, min(len(idxEntries), 4096))
	var globalLastRTPMS int64 = -1

	for _, entry := range idxEntries {
		if entry.Offset < 0 || entry.Size <= 0 || entry.Offset+int64(entry.Size) > int64(len(rtpData)) {
			continue
		}
		var pkt rtp.Packet
		if err := pkt.Unmarshal(rtpData[entry.Offset : entry.Offset+int64(entry.Size)]); err != nil {
			continue
		}
		acc := trackMap[entry.TrackID]
		if acc == nil {
			acc = &whepTrackAccum{
				metrics: WHEPTrackMetrics{
					ID:          entry.TrackID,
					Kind:        "other",
					PayloadType: pkt.PayloadType,
					ClockRate:   90000,
					SSRC:        pkt.SSRC,
				},
				seenSeq: make(map[uint16]struct{}, 256),
			}
			trackMap[entry.TrackID] = acc
		}

		tm := &acc.metrics
		tm.Received = true
		tm.Packets++
		tm.Bytes += int64(entry.Size)
		whepMetrics.TotalPackets++
		whepMetrics.TotalBytes += int64(entry.Size)

		if whepMetrics.FirstRTPMS == nil || entry.ArrivalMS < *whepMetrics.FirstRTPMS {
			v := entry.ArrivalMS
			whepMetrics.FirstRTPMS = &v
		}
		if entry.ArrivalMS > globalLastRTPMS {
			globalLastRTPMS = entry.ArrivalMS
		}
		if tm.FirstRTPMS == nil {
			v := entry.ArrivalMS
			tm.FirstRTPMS = &v
		}
		lastV := entry.ArrivalMS
		tm.LastRTPMS = &lastV

		if acc.hasArrival {
			gap := entry.ArrivalMS - acc.lastArrivalMS
			if gap > tm.MaxArrivalGapMS {
				tm.MaxArrivalGapMS = gap
			}
		}
		acc.lastArrivalMS = entry.ArrivalMS
		acc.hasArrival = true

		// Sequence continuity (wrap-safe 16-bit signed delta).
		seq := pkt.SequenceNumber
		if !acc.hasSeq {
			acc.hasSeq = true
			acc.maxSeq = seq
			acc.seenSeq[seq] = struct{}{}
		} else {
			delta := int16(seq - acc.maxSeq)
			if delta == 0 {
				tm.DuplicatePackets++
			} else if delta > 0 {
				if delta > 1 {
					tm.SequenceGaps++
					tm.MissingSequencePackets += int(delta - 1)
				}
				acc.maxSeq = seq
				acc.seenSeq[seq] = struct{}{}
			} else {
				if _, dup := acc.seenSeq[seq]; dup {
					tm.DuplicatePackets++
				} else {
					tm.OutOfOrderPackets++
					if tm.MissingSequencePackets > 0 {
						tm.MissingSequencePackets--
					}
					acc.seenSeq[seq] = struct{}{}
				}
			}
			// Keep bounded dedup window
			if len(acc.seenSeq) > 2048 {
				acc.seenSeq = map[uint16]struct{}{acc.maxSeq: {}}
			}
		}

		// RFC 3550 interarrival jitter
		if tm.ClockRate > 0 {
			arrivalTicks := float64(entry.ArrivalMS) * float64(tm.ClockRate) / 1000.0
			transit := arrivalTicks - float64(pkt.Timestamp)
			if acc.hasTransit {
				d := math.Abs(transit - acc.lastTransit)
				acc.jitterTicks += (d - acc.jitterTicks) / 16.0
				if acc.jitterTicks > acc.maxJitterTicks {
					acc.maxJitterTicks = acc.jitterTicks
				}
			}
			acc.lastTransit = transit
			acc.hasTransit = true
		}

		// Frame aggregation by RTP timestamp changes
		isKey := tm.Kind == "video" && isWHEPRTPKeyframe(tm.Codec, pkt.Payload)
		newFrame := !acc.hasFrameTS || pkt.Timestamp != acc.lastFrameTS
		if newFrame {
			if acc.hasFrameTS {
				gap := entry.ArrivalMS - acc.lastFrameArrMS
				if gap > tm.MaxFrameGapMS {
					tm.MaxFrameGapMS = gap
				}
			}
			acc.hasFrameTS = true
			acc.lastFrameTS = pkt.Timestamp
			acc.lastFrameArrMS = entry.ArrivalMS
			acc.curFrameIsKey = isKey
			acc.curFrameStartMS = entry.ArrivalMS
			tm.Frames++
		} else if isKey {
			acc.curFrameIsKey = true
		}

		if isKey && (!acc.hasKeyframe || acc.curFrameStartMS != acc.lastKeyframeMS) {
			tm.Keyframes++
			if tm.FirstKeyframeMS == nil {
				v := entry.ArrivalMS
				tm.FirstKeyframeMS = &v
			}
			if acc.hasKeyframe {
				gop := entry.ArrivalMS - acc.lastKeyframeMS
				if gop > tm.MaxGOPMS {
					tm.MaxGOPMS = gop
				}
			}
			acc.hasKeyframe = true
			acc.lastKeyframeMS = entry.ArrivalMS
		}

		// Keep bounded timeline events for evidence citations & charts
		if len(events) < maxTags && (newFrame || isKey || tm.Packets <= 64) {
			sCopy := seq
			kindName := "rtp_packet"
			if tm.Kind == "video" {
				kindName = "video"
			} else if tm.Kind == "audio" {
				kindName = "audio"
			}
			tsMS := int64(0)
			if tm.ClockRate > 0 {
				tsMS = int64(pkt.Timestamp) * 1000 / int64(tm.ClockRate)
			}
			events = append(events, Event{
				Offset:      entry.Offset,
				Size:        entry.Size,
				ArrivalMS:   entry.ArrivalMS,
				Kind:        kindName,
				DTS:         tsMS,
				PTS:         tsMS,
				Codec:       tm.Codec,
				Track:       uint8(tm.ID),
				Keyframe:    isKey,
				Artifact:    whepRTPDataFileName,
				RTPSequence: &sCopy,
			})
		}
	}

	// Build sorted Tracks and aggregate into top-level Metrics.
	var trackIDs []int
	for id := range trackMap {
		trackIDs = append(trackIDs, id)
	}
	sort.Ints(trackIDs)

	metrics := Metrics{MediaTimingUnit: "ms"}
	seenVideoCodecs := map[string]struct{}{}
	seenAudioCodecs := map[string]struct{}{}

	for _, id := range trackIDs {
		acc := trackMap[id]
		tm := acc.metrics
		expected := tm.Packets + tm.MissingSequencePackets
		if expected > 0 && tm.MissingSequencePackets > 0 {
			tm.LossRatePct = math.Round(float64(tm.MissingSequencePackets)*10000.0/float64(expected)) / 100.0
		}
		if tm.ClockRate > 0 {
			tm.JitterMS = math.Round(acc.jitterTicks*100000.0/float64(tm.ClockRate)) / 100.0
			tm.MaxJitterMS = math.Round(acc.maxJitterTicks*100000.0/float64(tm.ClockRate)) / 100.0
		}
		if tm.FirstRTPMS != nil && tm.LastRTPMS != nil {
			spanMS := *tm.LastRTPMS - *tm.FirstRTPMS
			if spanMS > 0 {
				tm.BitrateKbps = math.Round(float64(tm.Bytes)*8.0*10.0/float64(spanMS)) / 10.0
			}
		}
		whepMetrics.Tracks = append(whepMetrics.Tracks, tm)

		if tm.Kind == "video" {
			if tm.Codec != "" {
				if _, ok := seenVideoCodecs[tm.Codec]; !ok {
					seenVideoCodecs[tm.Codec] = struct{}{}
					metrics.VideoCodecs = append(metrics.VideoCodecs, tm.Codec)
				}
			}
			metrics.VideoFrames += tm.Frames
			if tm.FirstRTPMS != nil && (metrics.FirstVideoMS == nil || *tm.FirstRTPMS < *metrics.FirstVideoMS) {
				v := *tm.FirstRTPMS
				metrics.FirstVideoMS = &v
			}
			if tm.FirstKeyframeMS != nil && (metrics.FirstKeyframeMS == nil || *tm.FirstKeyframeMS < *metrics.FirstKeyframeMS) {
				v := *tm.FirstKeyframeMS
				metrics.FirstKeyframeMS = &v
			}
			if tm.MaxFrameGapMS > metrics.MaxVideoGapMS {
				metrics.MaxVideoGapMS = tm.MaxFrameGapMS
			}
			if tm.MaxGOPMS > metrics.MaxGOPMS {
				metrics.MaxGOPMS = tm.MaxGOPMS
			}
		} else if tm.Kind == "audio" {
			if tm.Codec != "" {
				if _, ok := seenAudioCodecs[tm.Codec]; !ok {
					seenAudioCodecs[tm.Codec] = struct{}{}
					metrics.AudioCodecs = append(metrics.AudioCodecs, tm.Codec)
				}
			}
			metrics.AudioFrames += tm.Frames
			if tm.FirstRTPMS != nil && (metrics.FirstAudioMS == nil || *tm.FirstRTPMS < *metrics.FirstAudioMS) {
				v := *tm.FirstRTPMS
				metrics.FirstAudioMS = &v
			}
			if tm.MaxFrameGapMS > metrics.MaxAudioGapMS {
				metrics.MaxAudioGapMS = tm.MaxFrameGapMS
			}
		}
	}

	// Detect mid-stream media stop: received RTP packets, then silence until end of sample.
	if globalLastRTPMS >= 0 && manifest.Observation.ElapsedMS > globalLastRTPMS {
		whepMetrics.MediaStoppedGapMS = manifest.Observation.ElapsedMS - globalLastRTPMS
	}

	report := Report{
		RuleVersion: "whep/1",
		Manifest:    manifest,
		Requests:    manifest.Observation.Requests,
		Metrics:     metrics,
		WHEP:        whepMetrics,
		Findings:    []Finding{},
	}
	report.Findings = whepFindings(report)
	report.Assessment = assessReport(report)
	return report, events, nil
}

func walkWHEPThroughput(dir string, fn func(kind string, size int64, arrivalMS int64)) error {
	archivePath := filepath.Join(dir, whepStreamName)
	zr, f, err := openEvidenceZIP(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	var (
		metaBytes []byte
		idxBytes  []byte
	)
	for _, zf := range zr.File {
		switch zf.Name {
		case whepMetaFileName:
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			metaBytes, err = io.ReadAll(io.LimitReader(rc, 2<<20))
			_ = rc.Close()
			if err != nil {
				return err
			}
		case whepRTPIndexFileName:
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			idxBytes, err = io.ReadAll(io.LimitReader(rc, maxTimeline))
			_ = rc.Close()
			if err != nil {
				return err
			}
		}
	}
	kindByTrack := map[int]string{}
	if len(metaBytes) > 0 {
		var meta whepArchiveMetadata
		if json.Unmarshal(metaBytes, &meta) == nil {
			for _, t := range meta.Tracks {
				kindByTrack[t.ID] = t.Kind
			}
		}
	}
	sc := bufio.NewScanner(bytes.NewReader(idxBytes))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e whepRTPIndexEntry
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		k := kindByTrack[e.TrackID]
		if k != "video" && k != "audio" {
			k = "rtp_datagram"
		}
		fn(k, int64(e.Size), e.ArrivalMS)
	}
	return sc.Err()
}
