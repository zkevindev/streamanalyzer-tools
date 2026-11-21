package diagnostic

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

const (
	whepMetaFileName     = "metadata.json"
	whepOfferFileName    = "offer.sdp"
	whepAnswerFileName   = "answer.sdp"
	whepRTPDataFileName  = "rtp-packets.bin"
	whepRTPIndexFileName = "rtp-index.ndjson"
	whepGatherTimeout    = 3 * time.Second
	whepConnectTimeout   = 10 * time.Second
)

type whepSampleOptions struct {
	ICETransportPolicy string
	ICEServers         []WHEPICEServer
}

type whepTrackRecorder struct {
	mu                 sync.Mutex
	meta               whepTrackMeta
	firstRTPMS         *int64
	firstKeyMS         *int64
	lastPLISentMS      int64
	sawKeyframe        bool
	lastSeq            uint16
	hasSeq             bool
}

func sampleWHEP(ctx context.Context, client *http.Client, dir string, target *url.URL, seconds int, opts whepSampleOptions) (Report, error) {
	startedAt := time.Now().UTC()
	start := time.Now()
	nowMS := func() int64 { return max(0, time.Since(start).Milliseconds()) }

	limit := captureLimit(ctx)
	if limit <= 0 {
		limit = maxStreamBytes
	}

	sampleCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()

	obs := Observation{
		Origin:           origin(target),
		StartedAt:        startedAt,
		RequestedSeconds: seconds,
		EndReason:        "duration_limit",
	}
	meta := whepArchiveMetadata{
		Version:            1,
		SignalingMode:      "client_offer",
		ICETransportPolicy: normalizeICEPolicy(opts.ICETransportPolicy),
		ICEState:           "new",
		DTLSState:          "new",
		PeerState:          "new",
	}

	var stagesMu sync.Mutex
	addStage := func(name string, sMS, eMS int64) {
		if eMS < sMS {
			eMS = sMS
		}
		stagesMu.Lock()
		obs.Stages = append(obs.Stages, CaptureStage{Name: name, StartMS: sMS, EndMS: eMS})
		stagesMu.Unlock()
	}

	// Build Pion MediaEngine with common WebRTC video/audio codecs.
	var me webrtc.MediaEngine
	if err := me.RegisterDefaultCodecs(); err != nil {
		return Report{}, fmt.Errorf("register webrtc codecs: %w", err)
	}
	// Also register H.265/HEVC on an unused dynamic payload type (avoid colliding with default H.264 PT=102).
	_ = me.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeH265,
			ClockRate: 90000,
		},
		PayloadType: 120,
	}, webrtc.RTPCodecTypeVideo)

	var se webrtc.SettingEngine
	se.SetIncludeLoopbackCandidate(true)
	api := webrtc.NewAPI(webrtc.WithMediaEngine(&me), webrtc.WithSettingEngine(se))
	pcCfg := buildWebRTCConfiguration(opts)

	var metaMu sync.Mutex
	createPCAndOffer := func(includeAudio bool) (*webrtc.PeerConnection, string, error) {
		conn, err := api.NewPeerConnection(pcCfg)
		if err != nil {
			return nil, "", fmt.Errorf("create peer connection: %w", err)
		}
		if _, err := conn.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		}); err != nil {
			_ = conn.Close()
			return nil, "", fmt.Errorf("add video transceiver: %w", err)
		}
		if includeAudio {
			if _, err := conn.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionRecvonly,
			}); err != nil {
				_ = conn.Close()
				return nil, "", fmt.Errorf("add audio transceiver: %w", err)
			}
		}
		metaMu.Lock()
		meta.LocalCandidates = 0
		metaMu.Unlock()
		conn.OnICECandidate(func(cand *webrtc.ICECandidate) {
			if cand != nil {
				metaMu.Lock()
				meta.LocalCandidates++
				metaMu.Unlock()
			}
		})
		gStart := nowMS()
		offer, err := conn.CreateOffer(nil)
		if err != nil {
			_ = conn.Close()
			return nil, "", fmt.Errorf("create whep offer: %w", err)
		}
		gatherDone := webrtc.GatheringCompletePromise(conn)
		if err := conn.SetLocalDescription(offer); err != nil {
			_ = conn.Close()
			return nil, "", fmt.Errorf("set local description: %w", err)
		}
		gatherTimer := time.NewTimer(whepGatherTimeout)
		select {
		case <-gatherDone:
		case <-gatherTimer.C:
		case <-sampleCtx.Done():
		}
		gatherTimer.Stop()
		gEnd := nowMS()
		addStage("whep_ice_gather", gStart, gEnd)
		gMS := gEnd - gStart
		meta.GatherMS = &gMS
		sdp := offer.SDP
		if ld := conn.LocalDescription(); ld != nil && ld.SDP != "" {
			sdp = ld.SDP
		}
		return conn, sdp, nil
	}

	// 1. Create local Offer and gather ICE candidates (non-trickle WHEP POST).
	pc, localSDP, err := createPCAndOffer(true)
	if err != nil {
		return Report{}, err
	}
	defer func() {
		if pc != nil {
			_ = pc.Close()
		}
	}()

	// Prepare temporary RTP capture files inside run dir (archived into stream.whep.zip later).
	rtpBinPath := filepath.Join(dir, ".whep-rtp.bin")
	rtpIdxPath := filepath.Join(dir, ".whep-idx.ndjson")
	defer os.Remove(rtpBinPath)
	defer os.Remove(rtpIdxPath)

	rtpBinFile, err := os.Create(rtpBinPath)
	if err != nil {
		return Report{}, err
	}
	defer rtpBinFile.Close()
	rtpIdxFile, err := os.Create(rtpIdxPath)
	if err != nil {
		return Report{}, err
	}
	defer rtpIdxFile.Close()
	rtpIdxBuf := bufio.NewWriterSize(rtpIdxFile, 32<<10)

	var (
		writeMu      sync.Mutex
		rtpOffset    int64
		answerSDP    string
		locationURL  string
		tracksMu     sync.Mutex
		tracks       []*whepTrackRecorder
		trackWG      sync.WaitGroup
		iceStartMS   int64
		iceDoneCh    = make(chan struct{})
		iceDoneOnce  sync.Once
		iceFailedCh  = make(chan string, 2)
		dtlsStartMS  int64
		dtlsDoneOnce sync.Once
	)

	// 2. Send WHEP HTTP POST with observed HTTP timings and headers.
	postOffer := func(sdp string) (*http.Response, error) {
		sigStart := nowMS()
		httpReq, err := http.NewRequestWithContext(sampleCtx, http.MethodPost, target.String(), strings.NewReader(sdp))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/sdp")
		httpReq.Header.Set("Accept", "application/sdp")
		applyCaptureHeaders(sampleCtx, httpReq)
		r, err := doObservedRequest(client, httpReq, &obs, start)
		sigEnd := nowMS()
		addStage("whep_signaling", sigStart, sigEnd)
		return r, err
	}

	resp, reqErr := postOffer(localSDP)
	// Fallback to video-only Offer when a video-only WHEP endpoint rejects audio+video with HTTP 400.
	if reqErr == nil && resp != nil && resp.StatusCode == http.StatusBadRequest && sampleCtx.Err() == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
		_ = pc.Close()
		pc = nil
		obs.FailureCode = ""
		obs.FailureStage = ""
		if retryPC, retrySDP, retryErr := createPCAndOffer(false); retryErr == nil {
			pc = retryPC
			localSDP = retrySDP
			resp, reqErr = postOffer(localSDP)
		}
	}

	signalingOK := false
	if reqErr != nil {
		obs.EndReason = "connect_error"
		if obs.FailureCode == "" {
			obs.FailureStage = "whep_signaling"
			obs.FailureCode = "whep_signaling_error"
		}
		if ctx.Err() != nil {
			obs.EndReason = "stopped"
			obs.FailureCode = "canceled"
		}
	} else {
		defer resp.Body.Close()
		obs.HTTPStatus = resp.StatusCode
		meta.HTTPStatus = resp.StatusCode
		meta.ETag = strings.TrimSpace(resp.Header.Get("ETag"))
		meta.LinkICEServers = parseLinkICEServers(resp.Header.Values("Link"))

		if loc := strings.TrimSpace(resp.Header.Get("Location")); loc != "" {
			if resolved, rerr := resolveWHEPLocation(target, loc); rerr == nil {
				locationURL = resolved.String()
				meta.LocationRecorded = true
				meta.LocationPath = resolved.Path
			}
		}

		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			answerSDP = string(bodyBytes)
			if strings.Contains(answerSDP, "v=0") && strings.Contains(answerSDP, "m=") {
				signalingOK = true
			} else {
				obs.EndReason = "connect_error"
				obs.FailureStage = "whep_signaling"
				obs.FailureCode = "whep_sdp_error"
			}
		} else {
			obs.EndReason = "http_error"
			obs.FailureStage = "whep_signaling"
			obs.FailureCode = "http_status"
		}
	}

	// Ensure remote WHEP session is cleaned up via DELETE Location (runs once before archiving, with defer safety net).
	var deleteOnce sync.Once
	deleteSession := func() {
		deleteOnce.Do(func() {
			if locationURL == "" {
				return
			}
			delCtx, delCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer delCancel()
			delReq, derr := http.NewRequestWithContext(delCtx, http.MethodDelete, locationURL, nil)
			if derr != nil {
				return
			}
			applyCaptureHeaders(ctx, delReq)
			if delResp, derr := client.Do(delReq); derr == nil {
				metaMu.Lock()
				meta.DeleteStatus = delResp.StatusCode
				metaMu.Unlock()
				_, _ = io.Copy(io.Discard, io.LimitReader(delResp.Body, 4096))
				_ = delResp.Body.Close()
			}
		})
	}
	defer deleteSession()

	if signalingOK {
		var closing atomic.Bool
		// Attach ICE/DTLS/Peer state callbacks before SetRemoteDescription.
		iceStartMS = nowMS()
		pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
			if closing.Load() {
				return
			}
			t := nowMS()
			metaMu.Lock()
			meta.ICEState = state.String()
			metaMu.Unlock()
			switch state {
			case webrtc.ICEConnectionStateConnected, webrtc.ICEConnectionStateCompleted:
				iceDoneOnce.Do(func() {
					addStage("whep_ice", iceStartMS, t)
					metaMu.Lock()
					v := t - iceStartMS
					meta.ICEConnectMS = &v
					dtlsStartMS = t
					metaMu.Unlock()
					captureSelectedPair(pc, &metaMu, &meta)
					close(iceDoneCh)
				})
			case webrtc.ICEConnectionStateFailed:
				select {
				case iceFailedCh <- "ice_failed":
				default:
				}
			}
		})

		pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
			if closing.Load() {
				return
			}
			t := nowMS()
			metaMu.Lock()
			meta.PeerState = state.String()
			if state == webrtc.PeerConnectionStateConnected {
				meta.DTLSState = "connected"
			} else if state == webrtc.PeerConnectionStateFailed {
				if meta.DTLSState != "connected" {
					meta.DTLSState = "failed"
				}
			}
			metaMu.Unlock()
			if state == webrtc.PeerConnectionStateConnected {
				dtlsDoneOnce.Do(func() {
					metaMu.Lock()
					ds := dtlsStartMS
					if ds == 0 {
						ds = iceStartMS
					}
					v := max(0, t-ds)
					meta.DTLSConnectMS = &v
					metaMu.Unlock()
					addStage("whep_dtls", ds, t)
					captureSelectedPair(pc, &metaMu, &meta)
				})
			} else if state == webrtc.PeerConnectionStateFailed {
				select {
				case iceFailedCh <- "dtls_failed":
				default:
				}
			}
		})

		pc.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			codecParams := remote.Codec()
			kind := strings.ToLower(remote.Kind().String())
			codecName := normalizeWHEPCodec(codecParams.MimeType)
			ssrc := uint32(remote.SSRC())

			tracksMu.Lock()
			trackID := len(tracks) + 1
			rec := &whepTrackRecorder{
				meta: whepTrackMeta{
					ID:          trackID,
					Kind:        kind,
					Codec:       codecName,
					MimeType:    codecParams.MimeType,
					PayloadType: uint8(remote.PayloadType()),
					ClockRate:   codecParams.ClockRate,
					SSRC:        ssrc,
				},
			}
			tracks = append(tracks, rec)
			tracksMu.Unlock()

			// Read RTCP reports in parallel for SR/RR statistics.
			trackWG.Add(1)
			go func() {
				defer trackWG.Done()
				for {
					pkts, _, rerr := receiver.ReadRTCP()
					if rerr != nil {
						return
					}
					at := nowMS()
					metaMu.Lock()
					if meta.FirstRTCPMS == nil {
						v := at
						meta.FirstRTCPMS = &v
					}
					metaMu.Unlock()
					rec.mu.Lock()
					for _, p := range pkts {
						switch p.(type) {
						case *rtcp.SenderReport:
							rec.meta.RTCPSRCount++
						case *rtcp.ReceiverReport:
							rec.meta.RTCPRRCount++
						}
					}
					rec.mu.Unlock()
				}
			}()

			// Read RTP packets and archive them into stream.whep.zip.
			trackWG.Add(1)
			go func() {
				defer trackWG.Done()
				sendPLI := func(at int64) {
					if kind != "video" {
						return
					}
					rec.mu.Lock()
					if at-rec.lastPLISentMS < 1000 {
						rec.mu.Unlock()
						return
					}
					rec.lastPLISentMS = at
					rec.meta.PLICount++
					if rec.meta.PLIFirstSentMS == nil {
						v := at
						rec.meta.PLIFirstSentMS = &v
					}
					rec.mu.Unlock()
					_ = pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}})
				}

				for {
					pkt, _, rerr := remote.ReadRTP()
					if rerr != nil {
						return
					}
					at := nowMS()
					markFirstPayload(&obs, start)

					rawPkt, merr := pkt.Marshal()
					if merr != nil {
						continue
					}

					rec.mu.Lock()
					if rec.firstRTPMS == nil {
						v := at
						rec.firstRTPMS = &v
					}
					isKey := kind == "video" && isWHEPRTPKeyframe(codecName, pkt.Payload)
					if isKey {
						if !rec.sawKeyframe {
							rec.sawKeyframe = true
							v := at
							rec.firstKeyMS = &v
							if rec.meta.PLIFirstSentMS != nil && rec.meta.PLIFirstRecoveryMS == nil {
								rv := max(0, at-*rec.meta.PLIFirstSentMS)
								rec.meta.PLIFirstRecoveryMS = &rv
							}
						}
					} else if kind == "video" && !rec.sawKeyframe && rec.firstRTPMS != nil && at-*rec.firstRTPMS >= 600 {
						// No keyframe yet after 600ms of RTP -> request PLI.
						rec.mu.Unlock()
						sendPLI(at)
						rec.mu.Lock()
					}
					if rec.hasSeq {
						diff := int16(pkt.SequenceNumber - rec.lastSeq)
						if diff > 1 && kind == "video" {
							rec.mu.Unlock()
							sendPLI(at)
							rec.mu.Lock()
						}
						if diff > 0 {
							rec.lastSeq = pkt.SequenceNumber
						}
					} else {
						rec.lastSeq = pkt.SequenceNumber
						rec.hasSeq = true
					}
					rec.mu.Unlock()

					writeMu.Lock()
					if rtpOffset+int64(len(rawPkt)) <= limit {
						off := rtpOffset
						if n, werr := rtpBinFile.Write(rawPkt); werr == nil {
							rtpOffset += int64(n)
							entry := whepRTPIndexEntry{
								TrackID:   trackID,
								SSRC:      ssrc,
								ArrivalMS: at,
								Offset:    off,
								Size:      n,
							}
							if eb, jerr := json.Marshal(entry); jerr == nil {
								_, _ = rtpIdxBuf.Write(eb)
								_ = rtpIdxBuf.WriteByte('\n')
							}
						}
					} else {
						metaMu.Lock()
						meta.TruncatedPackets++
						metaMu.Unlock()
					}
					writeMu.Unlock()
				}
			}()
		})

		if err := pc.SetRemoteDescription(webrtc.SessionDescription{
			Type: webrtc.SDPTypeAnswer,
			SDP:  answerSDP,
		}); err != nil {
			obs.EndReason = "connect_error"
			obs.FailureStage = "whep_signaling"
			obs.FailureCode = "whep_sdp_error"
		} else {
			recvStart := nowMS()
			// Wait for ICE connection or timeout / failure / sample duration end.
			connTimeout := min(whepConnectTimeout, time.Duration(seconds)*time.Second)
			connTimer := time.NewTimer(connTimeout)
			connected := false
			select {
			case <-iceDoneCh:
				connected = true
			case reason := <-iceFailedCh:
				obs.EndReason = "connect_error"
				if reason == "dtls_failed" {
					obs.FailureStage = "whep_dtls"
					obs.FailureCode = "whep_dtls_failed"
				} else {
					obs.FailureStage = "whep_ice"
					obs.FailureCode = "whep_ice_failed"
				}
			case <-connTimer.C:
				obs.EndReason = "connect_error"
				obs.FailureStage = "whep_ice"
				obs.FailureCode = "timeout"
				addStage("whep_ice", iceStartMS, nowMS())
			case <-sampleCtx.Done():
				if ctx.Err() != nil {
					obs.EndReason = "stopped"
					obs.FailureCode = "canceled"
				} else {
					obs.EndReason = "connect_error"
					obs.FailureStage = "whep_ice"
					obs.FailureCode = "timeout"
				}
			}
			connTimer.Stop()

			if connected {
				// Stay connected and record RTP until duration expires or user cancels.
				select {
				case <-sampleCtx.Done():
					if ctx.Err() != nil {
						obs.EndReason = "canceled"
					} else {
						obs.EndReason = "duration_limit"
					}
				case reason := <-iceFailedCh:
					obs.EndReason = "stream_error"
					if reason == "dtls_failed" {
						obs.FailureStage = "whep_dtls"
						obs.FailureCode = "whep_dtls_failed"
					} else {
						obs.FailureStage = "whep_ice"
						obs.FailureCode = "whep_ice_failed"
					}
				}
				addStage("whep_receive", recvStart, nowMS())
			}
		}
		closing.Store(true)
	}

	// Close peer connection to unblock track read goroutines before flushing files.
	_ = pc.Close()
	trackWG.Wait()
	deleteSession()

	// Finalize metadata snapshot.
	tracksMu.Lock()
	for _, tr := range tracks {
		tr.mu.Lock()
		meta.Tracks = append(meta.Tracks, tr.meta)
		tr.mu.Unlock()
	}
	tracksMu.Unlock()

	if answerSDP != "" {
		ansDigest := parseWHEPSDPDigest(answerSDP)
		meta.RemoteCandidates = ansDigest.CandidateCount
	}

	writeMu.Lock()
	_ = rtpIdxBuf.Flush()
	_ = rtpBinFile.Sync()
	_ = rtpIdxFile.Sync()
	writeMu.Unlock()

	obs.ElapsedMS = max(1, nowMS())
	// Clamp any stage end times to ElapsedMS to satisfy strict observation validation.
	for i := range obs.Stages {
		if obs.Stages[i].EndMS > obs.ElapsedMS {
			obs.Stages[i].EndMS = obs.ElapsedMS
		}
		if obs.Stages[i].StartMS > obs.Stages[i].EndMS {
			obs.Stages[i].StartMS = obs.Stages[i].EndMS
		}
	}

	// Archive stream.whep.zip
	streamArtifact, err := writeWHEPArchive(dir, meta, localSDP, answerSDP, rtpBinPath, rtpIdxPath)
	if err != nil {
		return Report{}, err
	}

	manifest := Manifest{
		Version:          multiProtocolSchemaVersion,
		Protocol:         ProtocolWHEP,
		StreamLimitBytes: limit,
		Observation:      obs,
		Stream:           streamArtifact,
	}

	// Replay from the freshly created archive so live & imported paths are 100% deterministic.
	report, events, err := replayWHEPWithEvents(dir, manifest)
	if err != nil {
		return Report{}, err
	}

	tlArtifact, err := writeTimelineArtifact(dir, events)
	if err != nil {
		return Report{}, err
	}
	report.Manifest.Timeline = tlArtifact
	return report, nil
}

func writeWHEPArchive(dir string, meta whepArchiveMetadata, offerSDP, answerSDP, rtpBinPath, rtpIdxPath string) (Artifact, error) {
	zipPath := filepath.Join(dir, whepStreamName)
	f, err := os.Create(zipPath)
	if err != nil {
		return Artifact{}, err
	}
	zw := zip.NewWriter(f)

	addBytes := func(name string, data []byte) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	addFile := func(name, path string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		return err
	}

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		_ = zw.Close()
		_ = f.Close()
		return Artifact{}, err
	}
	if err := addBytes(whepMetaFileName, metaBytes); err != nil {
		_ = zw.Close()
		_ = f.Close()
		return Artifact{}, err
	}
	if offerSDP != "" {
		if err := addBytes(whepOfferFileName, []byte(sanitizeSDPForArchive(offerSDP))); err != nil {
			_ = zw.Close()
			_ = f.Close()
			return Artifact{}, err
		}
	}
	if answerSDP != "" {
		if err := addBytes(whepAnswerFileName, []byte(sanitizeSDPForArchive(answerSDP))); err != nil {
			_ = zw.Close()
			_ = f.Close()
			return Artifact{}, err
		}
	}
	if err := addFile(whepRTPDataFileName, rtpBinPath); err != nil {
		_ = zw.Close()
		_ = f.Close()
		return Artifact{}, err
	}
	if err := addFile(whepRTPIndexFileName, rtpIdxPath); err != nil {
		_ = zw.Close()
		_ = f.Close()
		return Artifact{}, err
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		return Artifact{}, err
	}
	if err := f.Close(); err != nil {
		return Artifact{}, err
	}
	return hashFileArtifact(zipPath)
}

func writeTimelineArtifact(dir string, events []Event) (Artifact, error) {
	tl, err := newArtifact(dir, timelineName)
	if err != nil {
		return Artifact{}, err
	}
	for _, ev := range events {
		b, err := json.Marshal(ev)
		if err != nil {
			_ = tl.close()
			return Artifact{}, err
		}
		b = append(b, '\n')
		if _, err := tl.Write(b); err != nil {
			_ = tl.close()
			return Artifact{}, err
		}
	}
	if err := tl.close(); err != nil {
		return Artifact{}, err
	}
	return tl.artifact(), nil
}

func hashFileArtifact(path string) (Artifact, error) {
	f, err := os.Open(path)
	if err != nil {
		return Artifact{}, err
	}
	defer f.Close()
	w, err := newArtifactHashOnly()
	if err != nil {
		return Artifact{}, err
	}
	if _, err := io.Copy(w, f); err != nil {
		return Artifact{}, err
	}
	return w.artifact(), nil
}

func buildWebRTCConfiguration(opts whepSampleOptions) webrtc.Configuration {
	cfg := webrtc.Configuration{}
	if normalizeICEPolicy(opts.ICETransportPolicy) == "relay" {
		cfg.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}
	for _, srv := range opts.ICEServers {
		if len(srv.URLs) == 0 {
			continue
		}
		cfg.ICEServers = append(cfg.ICEServers, webrtc.ICEServer{
			URLs:       append([]string(nil), srv.URLs...),
			Username:   srv.Username,
			Credential: srv.Credential,
		})
	}
	return cfg
}

func normalizeICEPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "relay":
		return "relay"
	default:
		return "all"
	}
}

func captureSelectedPair(pc *webrtc.PeerConnection, mu *sync.Mutex, meta *whepArchiveMetadata) {
	if pc == nil || pc.SCTP() == nil || pc.SCTP().Transport() == nil || pc.SCTP().Transport().ICETransport() == nil {
		// Also check via RTPReceiver's Transport if SCTP transport is absent.
		for _, r := range pc.GetReceivers() {
			if r != nil && r.RTPTransceiver() != nil && r.RTPTransceiver().Receiver() != nil {
				if dtls := r.RTPTransceiver().Receiver().Transport(); dtls != nil && dtls.ICETransport() != nil {
					if pair, err := dtls.ICETransport().GetSelectedCandidatePair(); err == nil && pair != nil {
						setSelectedPair(pair, mu, meta)
						return
					}
				}
			}
		}
		return
	}
	if pair, err := pc.SCTP().Transport().ICETransport().GetSelectedCandidatePair(); err == nil && pair != nil {
		setSelectedPair(pair, mu, meta)
	}
}

func setSelectedPair(pair *webrtc.ICECandidatePair, mu *sync.Mutex, meta *whepArchiveMetadata) {
	if pair == nil {
		return
	}
	cp := &WHEPCandidatePair{State: "succeeded"}
	if pair.Local != nil {
		cp.LocalType = pair.Local.Typ.String()
		cp.LocalProtocol = strings.ToLower(pair.Local.Protocol.String())
	}
	if pair.Remote != nil {
		cp.RemoteType = pair.Remote.Typ.String()
		cp.RemoteProtocol = strings.ToLower(pair.Remote.Protocol.String())
	}
	mu.Lock()
	meta.SelectedPair = cp
	mu.Unlock()
}

func resolveWHEPLocation(base *url.URL, loc string) (*url.URL, error) {
	ref, err := url.Parse(strings.TrimSpace(loc))
	if err != nil {
		return nil, err
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return nil, errors.New("unsupported location scheme")
	}
	return resolved, nil
}

// parseLinkICEServers extracts STUN/TURN URLs from RFC 9725 Link headers (rel="ice-server").
func parseLinkICEServers(values []string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if !strings.Contains(strings.ToLower(part), "ice-server") {
				continue
			}
			lt := strings.IndexByte(part, '<')
			gt := strings.IndexByte(part, '>')
			if lt < 0 || gt <= lt+1 {
				continue
			}
			u := strings.TrimSpace(part[lt+1 : gt])
			low := strings.ToLower(u)
			if !strings.HasPrefix(low, "stun:") && !strings.HasPrefix(low, "stuns:") &&
				!strings.HasPrefix(low, "turn:") && !strings.HasPrefix(low, "turns:") {
				continue
			}
			// Strip query params to keep archive clean
			if q := strings.IndexByte(u, '?'); q > 0 {
				u = u[:q]
			}
			if _, ok := seen[u]; !ok {
				seen[u] = struct{}{}
				out = append(out, u)
			}
		}
	}
	return out
}

func sanitizeSDPForArchive(raw string) string {
	var out strings.Builder
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		low := strings.ToLower(line)
		// Redact ice-pwd to avoid persisting session secrets in shared bundles.
		if strings.HasPrefix(low, "a=ice-pwd:") {
			out.WriteString("a=ice-pwd:[redacted]\r\n")
			continue
		}
		out.WriteString(line)
		out.WriteString("\r\n")
	}
	return out.String()
}

func parseWHEPSDPDigest(raw string) *WHEPSDPDigest {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var parsed sdp.SessionDescription
	if err := parsed.UnmarshalString(raw); err != nil {
		return nil
	}
	d := &WHEPSDPDigest{}
	for _, attr := range parsed.Attributes {
		if attr.Key == "ice-lite" {
			d.ICELite = true
		}
		if attr.Key == "setup" && d.SetupRole == "" {
			d.SetupRole = attr.Value
		}
	}
	seenV := map[string]struct{}{}
	seenA := map[string]struct{}{}
	for _, md := range parsed.MediaDescriptions {
		kind := strings.ToLower(md.MediaName.Media)
		if kind == "video" {
			d.HasVideo = true
		} else if kind == "audio" {
			d.HasAudio = true
		}
		for _, attr := range md.Attributes {
			switch attr.Key {
			case "candidate":
				d.CandidateCount++
			case "setup":
				if d.SetupRole == "" {
					d.SetupRole = attr.Value
				}
			case "rtpmap":
				// format: "<pt> <encoding>/<clock>[/<params>]"
				fields := strings.Fields(attr.Value)
				if len(fields) >= 2 {
					enc := strings.Split(fields[1], "/")[0]
					norm := normalizeWHEPCodec(kind + "/" + enc)
					if norm == "" || norm == "rtx" || norm == "red" || norm == "ulpfec" || norm == "flexfec" || norm == "telephone-event" {
						continue
					}
					if kind == "video" {
						if _, ok := seenV[norm]; !ok {
							seenV[norm] = struct{}{}
							d.VideoCodecs = append(d.VideoCodecs, norm)
						}
					} else if kind == "audio" {
						if _, ok := seenA[norm]; !ok {
							seenA[norm] = struct{}{}
							d.AudioCodecs = append(d.AudioCodecs, norm)
						}
					}
				}
			}
		}
	}
	return d
}

func normalizeWHEPCodec(mime string) string {
	s := strings.ToLower(strings.TrimSpace(mime))
	if slash := strings.LastIndexByte(s, '/'); slash >= 0 {
		s = s[slash+1:]
	}
	switch s {
	case "h264", "avc":
		return "h264"
	case "h265", "hevc":
		return "h265"
	case "vp8":
		return "vp8"
	case "vp9":
		return "vp9"
	case "av1", "av1x":
		return "av1"
	case "opus":
		return "opus"
	case "pcma":
		return "pcma"
	case "pcmu":
		return "pcmu"
	case "g722":
		return "g722"
	case "aac", "mpeg4-generic", "mp4a-latm":
		return "aac"
	default:
		return s
	}
}

func isWHEPRTPKeyframe(codecName string, payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	switch codecName {
	case "h264":
		nt := payload[0] & 0x1F
		if nt == 5 || nt == 7 {
			return true
		}
		if nt == 24 && len(payload) >= 4 { // STAP-A
			off := 1
			for off+2 <= len(payload) {
				sz := int(payload[off])<<8 | int(payload[off+1])
				off += 2
				if sz <= 0 || off+sz > len(payload) {
					break
				}
				inner := payload[off] & 0x1F
				if inner == 5 || inner == 7 {
					return true
				}
				off += sz
			}
		}
		if nt == 28 && len(payload) >= 2 { // FU-A
			fu := payload[1]
			start := (fu & 0x80) != 0
			inner := fu & 0x1F
			return start && (inner == 5 || inner == 7)
		}
	case "h265":
		if len(payload) < 2 {
			return false
		}
		nt := (payload[0] >> 1) & 0x3F
		if (nt >= 16 && nt <= 21) || nt == 32 || nt == 33 {
			return true
		}
		if nt == 49 && len(payload) >= 3 { // FU
			fu := payload[2]
			start := (fu & 0x80) != 0
			inner := fu & 0x3F
			return start && ((inner >= 16 && inner <= 21) || inner == 32 || inner == 33)
		}
	case "vp8":
		// RFC 7741: skip payload descriptor, bit 0 of frame tag == 0 means keyframe.
		off := 1
		if (payload[0] & 0x80) != 0 { // X bit
			if off >= len(payload) {
				return false
			}
			ext := payload[off]
			off++
			if (ext & 0x80) != 0 { // I bit (PictureID)
				if off >= len(payload) {
					return false
				}
				if (payload[off] & 0x80) != 0 {
					off += 2
				} else {
					off++
				}
			}
			if (ext & 0x40) != 0 { // L
				off++
			}
			if (ext & 0x30) != 0 { // T or K
				off++
			}
		}
		if off < len(payload) && (payload[0]&0x10) != 0 { // S=1 start of partition
			return (payload[off] & 0x01) == 0
		}
	case "vp9":
		// RFC 8741: B=1 (bit 3) and P=0 (bit 6) means inter-picture predicted = false (keyframe).
		return (payload[0]&0x08) != 0 && (payload[0]&0x40) == 0
	case "av1":
		// AV1 RTP aggregation header: N bit (bit 3, 0x08) marks new coded video sequence (keyframe).
		return (payload[0] & 0x08) != 0
	}
	return false
}

// Keep a compile-time reference to rtp.Packet used by replay.
var _ = rtp.Packet{}
