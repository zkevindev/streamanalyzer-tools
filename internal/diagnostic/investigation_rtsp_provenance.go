package diagnostic

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func rtspMediaArtifactShape(value Artifact, limit int64) bool {
	if value.Bytes <= 0 || value.Bytes > limit || len(value.SHA256) != 64 {
		return false
	}
	_, err := hex.DecodeString(value.SHA256)
	return err == nil
}

// Shape validation also protects ad-hoc model projections. Import additionally
// recomputes the complete numeric proof from the persisted source and player.
func validRTSPMediaEvidenceShape(value *RTSPMediaEvidence) bool {
	if value == nil || value.Version != 1 || value.PlayerRevision < 1 ||
		!rtspMediaArtifactShape(value.SourceArtifact, maxStreamBytes) || !rtspMediaArtifactShape(value.TimelineArtifact, maxTimeline) ||
		!rtspMediaArtifactShape(value.PlayerArtifact, maxPlayerBytes) || !rtspMediaArtifactShape(value.RemuxArtifact, maxStreamBytes) ||
		value.VideoTrackID < 0 || value.VideoTrackID > maxRTSPTracks || value.AudioTrackID < 0 || value.AudioTrackID > maxRTSPTracks ||
		value.VideoTrackID == 0 && value.AudioTrackID == 0 || value.VideoTrackID != 0 && value.VideoTrackID == value.AudioTrackID {
		return false
	}
	if value.VideoTrackID == 0 {
		if value.VideoCodec != "" || value.VideoAccessUnits != 0 || len(value.Keyframes) != 0 {
			return false
		}
	} else if value.VideoCodec != "h264" && value.VideoCodec != "h265" {
		return false
	}
	if value.AudioTrackID == 0 {
		if value.AudioCodec != "" || value.AudioTransform != "" || value.AudioAccessUnits != 0 {
			return false
		}
	} else {
		switch value.AudioCodec {
		case "aac", "opus":
			if value.AudioTransform != "native_remux" {
				return false
			}
		case "pcma", "pcmu":
			if value.AudioTransform != "g711_to_pcm" {
				return false
			}
		default:
			return false
		}
	}
	if value.AnchorSampleOffsetMS < -60_000 || value.AnchorSampleOffsetMS > maxPlayerElapsedMS || value.AnchorCurrentMS < 0 || value.AnchorCurrentMS > 86_400_000 ||
		value.MediaOriginMS < -60_000 || value.MediaOriginMS > maxPlayerElapsedMS+2000 || value.PreviewOriginMS < -60_000 || value.PreviewOriginMS > maxPlayerElapsedMS+2000 ||
		value.DurationMS <= 0 || value.DurationMS > maxPlayerElapsedMS+2000 || value.PacketCount < 1 || value.PacketCount > maxTags ||
		value.DroppedPackets < 0 || value.DroppedPackets > value.PacketCount || value.IncompleteAccessUnits < 0 || value.IncompleteAccessUnits > value.PacketCount ||
		value.VideoAccessUnits < 0 || value.VideoAccessUnits > maxTags || value.AudioAccessUnits < 0 || value.AudioAccessUnits > 1_000_000_000 || value.VideoAccessUnits+value.AudioAccessUnits == 0 || len(value.Keyframes) > 16 {
		return false
	}
	anchor := value.AnchorCurrentMS + value.PreviewOriginMS - value.MediaOriginMS
	if anchor < 0 || anchor > value.DurationMS {
		return false
	}
	for _, event := range value.Keyframes {
		if event.Kind != "video" || !event.Keyframe || int(event.Track) != value.VideoTrackID || event.Codec != value.VideoCodec ||
			(event.Artifact != rtspWireName && event.Artifact != rtspUDPDataName) || event.Offset < 0 || event.Size < 12 || event.Size > 65535 || event.Offset > maxStreamBytes-int64(event.Size) ||
			event.ArrivalMS < 0 || event.ArrivalMS > 3_600_000 || event.DTS < 0 || event.DTS > maxPlayerElapsedMS+2000 || event.PTS < -60_000 || event.PTS > maxPlayerElapsedMS+2000 ||
			event.RTPSequence == nil || event.Rendition != "" || event.Sequence != nil || event.HTTPStatus != 0 || event.DurationMS != 0 || event.PID != nil {
			return false
		}
	}
	return true
}

func validateRTSPMediaEvidence(dir string, report Report, item StallInvestigation) error {
	value := item.Media.RTSP
	if !isRTSPProtocol(report.Manifest.Protocol) || report.Player == nil || report.Player.Source != "same_capture" || report.Manifest.Player == nil ||
		item.Media.Alignment != "receiver_rtp_estimate" || !validRTSPMediaEvidenceShape(value) || value.SourceArtifact != report.Manifest.Stream || value.TimelineArtifact != report.Manifest.Timeline ||
		value.PlayerArtifact != *report.Manifest.Player || value.PlayerRevision != item.PlayerRevision || value.PlayerRevision != report.Player.Revision {
		return fmt.Errorf("%w: RTSP 媒体重组证据与源流或播放器版本不一致", ErrInvalid)
	}
	if !sourceArtifactMatches(dir, playerName, value.PlayerArtifact, maxPlayerBytes) {
		return fmt.Errorf("%w: RTSP 媒体重组的播放器时钟校验失败", ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	expected, err := recomputeRTSPMediaEvidence(ctx, dir, report, item)
	if err != nil {
		return fmt.Errorf("%w: 无法从原始 RTP 重建媒体证据", ErrInvalid)
	}
	before, _ := json.Marshal(expected)
	after, _ := json.Marshal(value)
	if !bytes.Equal(before, after) {
		return fmt.Errorf("%w: RTSP 媒体重组元数据与原始 RTP 不一致", ErrInvalid)
	}
	if item.Media.StartMS != nil || item.Media.EndMS != nil || len(item.Media.Assets) > 0 {
		if item.Media.StartMS == nil || item.Media.EndMS == nil || *item.Media.StartMS < 0 || *item.Media.EndMS <= *item.Media.StartMS || *item.Media.EndMS > value.DurationMS {
			return fmt.Errorf("%w: RTSP 派生媒体时间范围无效", ErrInvalid)
		}
		for _, asset := range item.Media.Assets {
			if asset.MediaTimeMS == nil || *asset.MediaTimeMS < *item.Media.StartMS || *asset.MediaTimeMS > *item.Media.EndMS {
				return fmt.Errorf("%w: RTSP 派生产物缺少对应媒体时间", ErrInvalid)
			}
			if strings.HasSuffix(asset.Name, ".mp4") && (asset.DurationMS == nil || *asset.DurationMS > *item.Media.EndMS-*item.Media.StartMS+250) {
				return fmt.Errorf("%w: RTSP 派生片段时长与所选范围不一致", ErrInvalid)
			}
		}
	}
	return nil
}

func validRTSPStallLedgerShape(data []byte, ledger investigationLedger) bool {
	var raw struct {
		Items []struct {
			Media map[string]json.RawMessage `json:"media"`
		} `json:"items"`
	}
	if json.Unmarshal(data, &raw) != nil || len(raw.Items) != len(ledger.Items) {
		return false
	}
	hasProof := false
	for index, item := range ledger.Items {
		_, present := raw.Items[index].Media["rtsp"]
		if present && (ledger.Version != 2 || item.Media.RTSP == nil) {
			return false
		}
		hasProof = hasProof || item.Media.RTSP != nil
	}
	return ledger.Version == 1 && !hasProof || ledger.Version == 2 && hasProof
}
