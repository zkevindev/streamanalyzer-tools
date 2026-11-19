package diagnostic

import (
	"strings"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/sdp"
)

func rtspDescription(data []byte) (*description.Session, []RTSPTrackMetrics, error) {
	if len(data) == 0 {
		return nil, []RTSPTrackMetrics{}, nil
	}
	var raw sdp.SessionDescription
	if len(data) > maxRTSPSDPBytes || raw.Unmarshal(data) != nil || len(raw.MediaDescriptions) > maxRTSPTracks {
		return nil, nil, ErrInvalid
	}
	var session description.Session
	if session.Unmarshal(&raw) != nil {
		return nil, nil, ErrInvalid
	}
	tracks := []RTSPTrackMetrics{}
	for mediaIndex, media := range session.Medias {
		kind := "other"
		if media.Type == description.MediaTypeAudio {
			kind = "audio"
		} else if media.Type == description.MediaTypeVideo {
			kind = "video"
		}
		for _, codec := range media.Formats {
			if len(tracks) >= maxRTSPTracks {
				return nil, nil, ErrInvalid
			}
			track := RTSPTrackMetrics{ID: len(tracks) + 1, MediaID: mediaIndex + 1, Kind: kind, Codec: rtspCodec(codec), PayloadType: int(codec.PayloadType()), ClockRate: codec.ClockRate()}
			switch value := codec.(type) {
			case *format.G711:
				track.Channels = value.ChannelCount
			case *format.Opus:
				track.Channels = value.ChannelCount
			case *format.MPEG4Audio:
				if value.Config != nil {
					track.Channels = value.Config.ChannelCount
				}
			}
			tracks = append(tracks, track)
		}
	}
	return &session, tracks, nil
}

func rtspCodec(codec format.Format) string {
	if value, ok := codec.(*format.G711); ok {
		if value.MULaw {
			return "pcmu"
		}
		return "pcma"
	}
	switch strings.ToLower(codec.Codec()) {
	case "h264":
		return "h264"
	case "h265":
		return "h265"
	case "mpeg-4 audio", "mpeg4audio", "mpeg-4 audio latm":
		return "aac"
	case "opus", "av1", "vp8", "vp9", "g722", "lpcm", "ac3", "speex":
		return strings.ToLower(codec.Codec())
	case "mjpeg", "jpeg":
		return "jpeg"
	case "mpeg-1/2 video", "mpeg1video":
		return "mpeg2video"
	case "mpeg-4 video":
		return "mpeg4video"
	case "mpeg-1/2 audio", "mpeg1audio":
		return "mpeg1audio"
	}
	return "unknown"
}
