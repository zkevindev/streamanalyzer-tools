package diagnostic

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/yutopp/go-rtmp/message"
)

func amf0TestString(target *[]byte, value string) {
	*target = append(*target, 0, byte(len(value)>>8), byte(len(value)))
	*target = append(*target, value...)
}

func TestRTMPControlPayloadsExposeOnlyControlledFields(t *testing.T) {
	chunk := make([]byte, 4)
	binary.BigEndian.PutUint32(chunk, 4096)
	if msg, ok := parseRTMPControlMessage(message.TypeIDSetChunkSize, chunk); !ok || msg.Value != 4096 || msg.Type != "set_chunk_size" {
		t.Fatalf("incorrect chunk size message: %+v", msg)
	}
	ping := []byte{0, 6, 0, 0, 0, 100}
	if msg, ok := parseRTMPControlMessage(message.TypeIDUserCtrl, ping); !ok || msg.Name != "ping_request" || msg.Value != 100 {
		t.Fatalf("incorrect ping message: %+v", msg)
	}
	var command []byte
	amf0TestString(&command, "onStatus")
	command = append(command, 1, 0, 0, 0, 0, 0, 0, 0, 0, 5, 3)
	command = append(command, 0, 5)
	command = append(command, "level"...)
	amf0TestString(&command, "error")
	command = append(command, 0, 4)
	command = append(command, "code"...)
	amf0TestString(&command, "NetStream.Play.StreamNotFound")
	command = append(command, 0, 11)
	command = append(command, "description"...)
	amf0TestString(&command, "private-stream-name-and-token")
	msg, ok := parseRTMPControlMessage(message.TypeIDCommandMessageAMF0, command)
	encoded := msg.Level + msg.Code + msg.Name
	if !ok || msg.Level != "error" || msg.Code != "NetStream.Play.StreamNotFound" || strings.Contains(encoded, "private") {
		t.Fatalf("onStatus was not safely reduced: %+v", msg)
	}
	for _, payload := range [][]byte{nil, {0}, {0, 0, 100}, {0, 0, 1, 'x'}} {
		_, _ = parseRTMPControlMessage(message.TypeIDCommandMessageAMF0, payload)
	}
}
