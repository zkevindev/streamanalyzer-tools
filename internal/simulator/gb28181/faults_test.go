package gb28181

import (
	"bytes"
	"encoding/binary"
	"net"
	"reflect"
	"testing"
	"time"
)

type capturePacketConn struct {
	net.Conn
	packets [][]byte
}

func (c *capturePacketConn) SetWriteDeadline(time.Time) error { return nil }

func (c *capturePacketConn) Write(p []byte) (int, error) {
	c.packets = append(c.packets, bytes.Clone(p))
	return len(p), nil
}

func TestMediaFaultsPacketCadence(t *testing.T) {
	conn := &capturePacketConn{}
	trace := sessionEvidence{fleet: &fleet{}, SessionReport: SessionReport{CaptureEnabled: true, Start: time.Now()}}
	seq := uint16(65534)
	rules := packetFaults{dropEvery: 3, reorderEvery: 1}
	sent, err := sendRTP(conn, mediaOffer{transport: mediaUDP, payload: 96}, bytes.Repeat([]byte{1}, 4000), 90, &seq,
		func(packet []byte) { trace.capture(packet, false) }, &rules)
	if err != nil || sent != 3 || rules.dropped != 1 || rules.reordered != 2 || seq != 2 {
		t.Fatalf("wrong packet effects: %d %d %+v %v", sent, seq, rules, err)
	}
	var order []uint16
	for _, packet := range conn.packets {
		order = append(order, binary.BigEndian.Uint16(packet[2:4]))
	}
	if !reflect.DeepEqual(order, []uint16{65535, 65534, 1}) {
		t.Fatalf("unexpected packet order: %v", order)
	}
	review, err := reviewRTP(trace.packets)
	if err != nil || review.SequenceGaps != 1 || review.OutOfOrder != 1 {
		t.Fatalf("faults did not appear in offline evidence: %+v %v", review, err)
	}
}

func TestMediaFaultsTimestampAndAudioWindow(t *testing.T) {
	faults := mediaFaults{config: &FaultConfig{StartMS: 100, WindowMS: 40, MuteAudio: true, TimestampJumpMS: -50}}
	var marks []string
	mark := func(stage, detail string) { marks = append(marks, stage) }
	before := mediaUnit{dts: 10000, pts: 10000, stream: audioStreamID}
	if inside, mute := faults.apply(&before, mark); inside || mute {
		t.Fatal("fault before start")
	}
	during := mediaUnit{dts: 19000, pts: 19900, stream: audioStreamID}
	if inside, mute := faults.apply(&during, mark); !inside || !mute || during.dts != 14500 || during.pts != 15400 {
		t.Fatalf("bad audio/timestamp fault: %+v", during)
	}
	after := mediaUnit{dts: 22600, pts: 22600, stream: audioStreamID}
	if inside, mute := faults.apply(&after, mark); inside || mute || after.pts != 18100 || len(marks) != 1 {
		t.Fatalf("fault did not end cleanly: %+v, %v", after, marks)
	}
}
