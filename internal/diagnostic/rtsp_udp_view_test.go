package diagnostic

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRTSPUDPViewsKeepTransportAndZeroMeasurements(t *testing.T) {
	zero := int64(0)
	report := Report{Manifest: Manifest{Protocol: ProtocolRTSP}, RTSP: &RTSPMetrics{Transport: "udp", RequestedTransport: "udp", FirstRTCPMS: &zero}, Findings: []Finding{{ID: "F001", Evidence: []Event{{Artifact: "udp-datagrams.bin", Kind: "rtp_packet", Offset: 100, Size: 30}}}}}
	var html bytes.Buffer
	if err := RenderHTML(&html, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"UDP 单播 RTP/RTCP", "udp-index.ndjson", "udp-datagrams.bin", "<dt>收到 UDP 数据报</dt><dd>0</dd>", "<dt>首个 RTCP 到达</dt><dd>0 ms</dd>", "源端漏发、链路丢包"} {
		if !strings.Contains(html.String(), value) {
			t.Errorf("UDP HTML lost %s", value)
		}
	}
	input, err := modelInput(report)
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		RTSP struct {
			Transport string        `json:"transport"`
			UDP       *modelRTSPUDP `json:"udp_receive"`
		} `json:"rtsp"`
	}
	if err := json.Unmarshal(input, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.RTSP.Transport != "udp" || summary.RTSP.UDP == nil || summary.RTSP.UDP.Datagrams != 0 {
		t.Fatalf("model lost measured UDP zeros: %s", input)
	}
}
