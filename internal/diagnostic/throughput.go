package diagnostic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
)

type ThroughputSample struct {
	Second       int     `json:"second"`
	DurationMS   int64   `json:"duration_ms"`
	NetworkBytes int64   `json:"network_bytes"`
	MediaBytes   int64   `json:"media_bytes"`
	VideoBytes   int64   `json:"video_bytes"`
	AudioBytes   int64   `json:"audio_bytes"`
	NetworkKbps  float64 `json:"network_kbps"`
}

type ThroughputReport struct {
	Version            string             `json:"version"`
	Samples            []ThroughputSample `json:"samples"`
	AverageNetworkKbps float64            `json:"average_network_kbps"`
	AverageMediaKbps   float64            `json:"average_media_kbps"`
	AverageVideoKbps   float64            `json:"average_video_kbps"`
	AverageAudioKbps   float64            `json:"average_audio_kbps"`
	MinimumNetworkKbps float64            `json:"minimum_network_kbps"`
	MaximumNetworkKbps float64            `json:"maximum_network_kbps"`
	Boundary           string             `json:"boundary"`
}

const legacyThroughputBoundary = "网络吞吐按已保存事件的到达字节估算；媒体码率按音视频标签、RTMP 消息、HLS 分片或 TS 包字节统计，均包含容器/协议开销，不是纯编码 payload 码率。"

type throughputEvent struct {
	kind, rendition string
	size            int64
	arrivalMS       int64
}

func buildThroughput(dir string, report Report) (*ThroughputReport, error) {
	observation := report.Manifest.Observation
	seconds := int((observation.ElapsedMS + 999) / 1000)
	if observation.ElapsedMS <= 0 || seconds <= 0 || seconds > maxSeconds {
		return nil, nil
	}
	result := &ThroughputReport{Version: "throughput/1", Samples: make([]ThroughputSample, seconds),
		Boundary: "网络吞吐按已保存事件的到达字节估算；媒体码率按音视频标签、RTMP 消息、RTSP RTP 包、HLS 分片或 TS 包字节统计，均包含容器/协议开销，不是纯编码 payload 码率。"}
	for index := range result.Samples {
		result.Samples[index].Second = index
		result.Samples[index].DurationMS = min(int64(1000), observation.ElapsedMS-int64(index*1000))
	}
	var totalNetwork, totalMedia, totalVideo, totalAudio int64
	account := func(event throughputEvent) {
		second := int(event.arrivalMS / 1000)
		if second < 0 || second >= len(result.Samples) || event.size < 0 {
			return
		}
		sample := &result.Samples[second]
		sample.NetworkBytes += event.size
		totalNetwork += event.size
		switch event.kind {
		case "video", "video_aux", "video_rtp":
			sample.MediaBytes += event.size
			sample.VideoBytes += event.size
			totalMedia += event.size
			totalVideo += event.size
		case "audio", "audio_aux", "audio_rtp":
			sample.MediaBytes += event.size
			sample.AudioBytes += event.size
			totalMedia += event.size
			totalAudio += event.size
		case hlsSegment:
			sample.MediaBytes += event.size
			totalMedia += event.size
			if event.rendition == hlsAudio {
				sample.AudioBytes += event.size
				totalAudio += event.size
			} else {
				sample.VideoBytes += event.size
				totalVideo += event.size
			}
		case "ts_packet", "rtp_datagram":
			sample.MediaBytes += event.size
			totalMedia += event.size
		}
	}
	switch report.Manifest.Protocol {
	case ProtocolHTTPFLV:
		file, err := os.Open(filepath.Join(dir, timelineName))
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(io.LimitReader(file, maxTimeline))
		for {
			var event Event
			if err := decoder.Decode(&event); err != nil {
				if err == io.EOF {
					break
				}
				file.Close()
				return nil, err
			}
			account(throughputEvent{kind: event.Kind, size: int64(event.Size), arrivalMS: event.ArrivalMS})
		}
		file.Close()
	case ProtocolRTMP:
		stamps, err := loadRTMPStamps(dir, report.Manifest)
		if err != nil {
			return nil, err
		}
		file, err := os.Open(filepath.Join(dir, rtmpStreamName))
		if err != nil {
			return nil, err
		}
		events, parseErr := parseRTMPCapture(bufio.NewReaderSize(file, 64<<10), stamps)
		file.Close()
		_ = parseErr
		for _, event := range events {
			account(throughputEvent{kind: event.Kind, size: int64(event.Size), arrivalMS: event.ArrivalMS})
		}
	case ProtocolHLS:
		file, err := os.Open(filepath.Join(dir, timelineName))
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(io.LimitReader(file, maxTimeline))
		scanner.Buffer(make([]byte, 1024), maxHLSRecordBytes)
		for scanner.Scan() {
			var row hlsRecord
			if json.Unmarshal(scanner.Bytes(), &row) != nil {
				file.Close()
				return nil, ErrInvalid
			}
			if row.Result == "ok" {
				account(throughputEvent{kind: row.Kind, rendition: row.Rendition, size: int64(row.Bytes), arrivalMS: row.ArrivalMS})
			}
		}
		err = scanner.Err()
		file.Close()
		if err != nil {
			return nil, err
		}
	case ProtocolHTTPTS, ProtocolUDPTS, ProtocolRTPTS:
		err := walkTSChunks(dir, report.Manifest, func(span tsSpan, data []byte) error {
			kind := "ts_packet"
			if report.Manifest.Protocol == ProtocolRTPTS {
				kind = "rtp_datagram"
			}
			account(throughputEvent{kind: kind, size: int64(len(data)), arrivalMS: span.ArrivalMS})
			return nil
		})
		if err != nil {
			return nil, err
		}
	case ProtocolWHEP:
		if err := walkWHEPThroughput(dir, func(kind string, size int64, arrivalMS int64) {
			account(throughputEvent{kind: kind, size: size, arrivalMS: arrivalMS})
		}); err != nil {
			return nil, err
		}
	case ProtocolDASH:
		if err := walkDASHThroughput(dir, func(kind, rendition string, size int64, arrivalMS int64) {
			account(throughputEvent{kind: kind, rendition: rendition, size: size, arrivalMS: arrivalMS})
		}); err != nil {
			return nil, err
		}
	case ProtocolRTSP, ProtocolRTSPS:
		if err := walkRTSPMediaEvents(dir, report.Manifest, func(event Event) {
			account(throughputEvent{kind: event.Kind, size: int64(event.Size), arrivalMS: event.ArrivalMS})
		}); err != nil {
			return nil, err
		}
	default:
		return nil, nil
	}
	if (report.Manifest.Protocol == ProtocolHTTPFLV || report.Manifest.Protocol == ProtocolRTMP) && report.Manifest.Stream.Bytes > totalNetwork {
		overhead := report.Manifest.Stream.Bytes - totalNetwork
		result.Samples[0].NetworkBytes += overhead
		totalNetwork += overhead
	}
	minimum := float64(0)
	for index := range result.Samples {
		sample := &result.Samples[index]
		if sample.DurationMS > 0 {
			sample.NetworkKbps = float64(sample.NetworkBytes*8) / float64(sample.DurationMS)
		}
		if index == 0 || sample.NetworkKbps < minimum {
			minimum = sample.NetworkKbps
		}
		result.MaximumNetworkKbps = max(result.MaximumNetworkKbps, sample.NetworkKbps)
	}
	result.AverageNetworkKbps = float64(totalNetwork*8) / float64(observation.ElapsedMS)
	result.AverageMediaKbps = float64(totalMedia*8) / float64(observation.ElapsedMS)
	result.AverageVideoKbps = float64(totalVideo*8) / float64(observation.ElapsedMS)
	result.AverageAudioKbps = float64(totalAudio*8) / float64(observation.ElapsedMS)
	result.MinimumNetworkKbps = minimum
	return result, nil
}

func validateStoredThroughput(dir string, stored, replayed Report) (*ThroughputReport, error) {
	if stored.Throughput == nil {
		return nil, nil
	}
	expected, err := buildThroughput(dir, replayed)
	if err != nil || expected == nil {
		return nil, fmt.Errorf("%w: 吞吐与码率报告和时间线不一致", ErrInvalid)
	}
	// Accept the earlier wording for protocols that already emitted throughput/1.
	if stored.Throughput.Boundary == legacyThroughputBoundary {
		switch replayed.Manifest.Protocol {
		case ProtocolHTTPFLV, ProtocolRTMP, ProtocolHLS, ProtocolHTTPTS, ProtocolUDPTS, ProtocolRTPTS:
			expected.Boundary = legacyThroughputBoundary
		}
	}
	if !reflect.DeepEqual(stored.Throughput, expected) {
		return nil, fmt.Errorf("%w: 吞吐与码率报告和时间线不一致", ErrInvalid)
	}
	return stored.Throughput, nil
}
