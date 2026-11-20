package diagnostic

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/yutopp/go-rtmp"
	"github.com/yutopp/go-rtmp/message"
)

type RTMPControlMessage struct {
	ArrivalMS int64  `json:"arrival_ms"`
	Type      string `json:"type"`
	Name      string `json:"name,omitempty"`
	Value     uint32 `json:"value,omitempty"`
	Event     uint16 `json:"event,omitempty"`
	Level     string `json:"level,omitempty"`
	Code      string `json:"code,omitempty"`
}

type RTMPControlReport struct {
	Version              string               `json:"version"`
	TotalMessages        int                  `json:"total_messages"`
	ChunkSizeChanges     int                  `json:"chunk_size_changes"`
	Acknowledgements     int                  `json:"acknowledgements"`
	WindowUpdates        int                  `json:"window_updates"`
	PeerBandwidthUpdates int                  `json:"peer_bandwidth_updates"`
	UserControlEvents    int                  `json:"user_control_events"`
	CommandMessages      int                  `json:"command_messages"`
	Messages             []RTMPControlMessage `json:"messages"`
	Truncated            int                  `json:"truncated"`
	Boundary             string               `json:"boundary"`
}

func analyzeRTMPControl(dir string, manifest Manifest) (*RTMPControlReport, error) {
	if manifest.Protocol != ProtocolRTMP {
		return nil, nil
	}
	stamps, err := loadRTMPStamps(dir, manifest)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(dir, rtmpStreamName))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var handshake [1536]byte
	reader := bufio.NewReaderSize(file, 64<<10)
	if _, err := io.ReadFull(reader, handshake[:]); err != nil {
		return &RTMPControlReport{Version: "rtmp-control/1", Messages: []RTMPControlMessage{}, Boundary: "原始 RTMP 握手不完整，未获得控制消息。"}, nil
	}
	streamer := rtmp.NewChunkStreamer(reader, io.Discard, &rtmp.StreamControlStateConfig{MaxChunkStreams: 128})
	defer streamer.Close()
	result := &RTMPControlReport{Version: "rtmp-control/1", Messages: []RTMPControlMessage{}, Boundary: "仅展示受控协议字段和 onStatus level/code，不包含 connect/play 参数、URL、description 或其他 AMF 属性。"}
	stampIndex := 0
	for index := 0; index < 2*maxTags; index++ {
		packet, err := streamer.ReadPayload()
		if err != nil {
			break
		}
		end := int64(packet.EndOffset) + int64(len(handshake))
		for stampIndex < len(stamps) && stamps[stampIndex].EndOffset < end {
			stampIndex++
		}
		if stampIndex >= len(stamps) {
			break
		}
		if msg, ok := parseRTMPControlMessage(packet.TypeID, packet.Payload); ok {
			msg.ArrivalMS = stamps[stampIndex].ArrivalMS
			result.TotalMessages++
			switch packet.TypeID {
			case message.TypeIDSetChunkSize:
				result.ChunkSizeChanges++
			case message.TypeIDAck:
				result.Acknowledgements++
			case message.TypeIDWinAckSize:
				result.WindowUpdates++
			case message.TypeIDSetPeerBandwidth:
				result.PeerBandwidthUpdates++
			case message.TypeIDUserCtrl:
				result.UserControlEvents++
			case message.TypeIDCommandMessageAMF0, message.TypeIDCommandMessageAMF3:
				result.CommandMessages++
			}
			if len(result.Messages) < 64 {
				result.Messages = append(result.Messages, msg)
			} else {
				result.Truncated++
			}
		}
	}
	return result, nil
}

func parseRTMPControlMessage(typeID message.TypeID, payload []byte) (RTMPControlMessage, bool) {
	value := func() uint32 {
		if len(payload) < 4 {
			return 0
		}
		return binary.BigEndian.Uint32(payload[:4])
	}
	switch typeID {
	case message.TypeIDSetChunkSize:
		return RTMPControlMessage{Type: "set_chunk_size", Value: value() & 0x7fffffff}, len(payload) == 4
	case message.TypeIDAbortMessage:
		return RTMPControlMessage{Type: "abort", Value: value()}, len(payload) == 4
	case message.TypeIDAck:
		return RTMPControlMessage{Type: "acknowledgement", Value: value()}, len(payload) == 4
	case message.TypeIDWinAckSize:
		return RTMPControlMessage{Type: "window_ack_size", Value: value()}, len(payload) == 4
	case message.TypeIDSetPeerBandwidth:
		if len(payload) != 5 {
			return RTMPControlMessage{}, false
		}
		return RTMPControlMessage{Type: "set_peer_bandwidth", Value: value(), Event: uint16(payload[4])}, true
	case message.TypeIDUserCtrl:
		if len(payload) < 6 {
			return RTMPControlMessage{}, false
		}
		event := binary.BigEndian.Uint16(payload[:2])
		names := map[uint16]string{0: "stream_begin", 1: "stream_eof", 2: "stream_dry", 3: "set_buffer_length", 4: "stream_is_recorded", 6: "ping_request", 7: "ping_response"}
		return RTMPControlMessage{Type: "user_control", Name: names[event], Event: event, Value: binary.BigEndian.Uint32(payload[2:6])}, true
	case message.TypeIDCommandMessageAMF0, message.TypeIDCommandMessageAMF3:
		data := payload
		if typeID == message.TypeIDCommandMessageAMF3 && len(data) > 0 && data[0] == 0 {
			data = data[1:]
		}
		name, ok := amf0FirstString(data)
		if !ok {
			return RTMPControlMessage{}, false
		}
		msg := RTMPControlMessage{Type: "command", Name: name}
		if name == "onStatus" {
			msg.Level = amf0StringProperty(data, "level")
			msg.Code = amf0StringProperty(data, "code")
			if msg.Level != "status" && msg.Level != "error" && msg.Level != "warning" {
				msg.Level = ""
			}
			if !strings.HasPrefix(msg.Code, "NetStream.") || len(msg.Code) > 128 {
				msg.Code = ""
			}
		}
		return msg, true
	default:
		return RTMPControlMessage{}, false
	}
}

func amf0FirstString(data []byte) (string, bool) {
	if len(data) < 3 || data[0] != 0 {
		return "", false
	}
	length := int(binary.BigEndian.Uint16(data[1:3]))
	if length < 1 || length > 64 || len(data) < 3+length {
		return "", false
	}
	value := string(data[3 : 3+length])
	for _, r := range value {
		if r < 0x20 || r > 0x7e {
			return "", false
		}
	}
	return value, true
}

func amf0StringProperty(data []byte, key string) string {
	for index := 0; index+len(key)+5 <= len(data); index++ {
		if string(data[index:index+len(key)]) != key || data[index+len(key)] != 0 {
			continue
		}
		at := index + len(key) + 1
		length := int(binary.BigEndian.Uint16(data[at : at+2]))
		if length > 128 || at+2+length > len(data) {
			return ""
		}
		return string(data[at+2 : at+2+length])
	}
	return ""
}

func validateStoredRTMPControl(dir string, stored, replayed Report) (*RTMPControlReport, error) {
	if stored.RTMPControl == nil {
		return nil, nil
	}
	expected, err := analyzeRTMPControl(dir, replayed.Manifest)
	if err != nil || !reflect.DeepEqual(stored.RTMPControl, expected) {
		return nil, fmt.Errorf("%w: RTMP 控制面报告与原始消息不一致", ErrInvalid)
	}
	return stored.RTMPControl, nil
}
