package diagnostic

import (
	"bytes"
	"strings"
	"testing"
)

func networkPhaseHTMLRequest(t *testing.T, body string, number string) string {
	t.Helper()
	start := strings.Index(body, "<summary>#"+number+" · ")
	if start < 0 {
		t.Fatalf("missing phase details for request %s", number)
	}
	end := strings.Index(body[start:], "</details>")
	return body[start : start+end]
}

func TestPlayerNetworkPhasesHTMLRedirectReuseAndTerminalFailure(t *testing.T) {
	zero, hundred := int64(0), int64(100)
	fresh, reused := false, true
	first := PlayerNetworkRequest{Number: 1, ResourceID: "index.m3u8", Kind: "playlist", Method: "GET", StartMS: -100, HeaderMS: &zero, EndMS: 300, HTTPStatus: 200, Outcome: "complete", DroppedHops: 2,
		Hops: []PlayerNetworkHop{
			{Number: 1, StartMS: -100, EndMS: -100, HTTPStatus: 302, TTFBMS: &zero, ConnectionReused: &fresh, Stages: []CaptureStage{
				{Name: "dns", StartMS: -100, EndMS: -100}, {Name: "connect", StartMS: -100, EndMS: -100}, {Name: "tls", StartMS: -100, EndMS: -100},
				{Name: "request_send", StartMS: -100, EndMS: -100}, {Name: "response_first_byte", StartMS: -100, EndMS: -100}, {Name: "response_headers", StartMS: -100, EndMS: -100},
			}},
			{Number: 2, StartMS: -100, EndMS: 300, HTTPStatus: 200, TTFBMS: &hundred, ConnectionReused: &reused, Stages: []CaptureStage{
				{Name: "request_send", StartMS: -100, EndMS: -90}, {Name: "response_first_byte", StartMS: -90, EndMS: 0}, {Name: "response_headers", StartMS: 0, EndMS: 0}, {Name: "receive", StartMS: 0, EndMS: 300},
			}},
		}}
	second := PlayerNetworkRequest{Number: 2, ResourceID: "r1.m4s", Kind: "media", Method: "GET", StartMS: 400, EndMS: 600, Outcome: "transport_error", FailureStage: "tls", FailureCode: "tls_error",
		Hops: []PlayerNetworkHop{{Number: 1, StartMS: 400, EndMS: 600, FailureStage: "tls", FailureCode: "tls_error", Stages: []CaptureStage{{Name: "connect", StartMS: 400, EndMS: 410}, {Name: "connect", StartMS: 420, EndMS: 440}, {Name: "tls", StartMS: 440, EndMS: 600}}}}}
	third := PlayerNetworkRequest{Number: 3, ResourceID: "r2.ts", Kind: "media", Method: "GET", StartMS: 700, EndMS: 1000, HTTPStatus: 200, Outcome: "transport_error", FailureStage: "proxy_write", FailureCode: "proxy_write_error",
		Hops: []PlayerNetworkHop{{Number: 1, StartMS: 700, EndMS: 1000, HTTPStatus: 200, Stages: []CaptureStage{{Name: "receive", StartMS: 730, EndMS: 1000}}}}}
	fourth := PlayerNetworkRequest{Number: 4, ResourceID: "old.ts", Kind: "media", Method: "GET", HTTPStatus: 200, Outcome: "complete"}
	fifth := PlayerNetworkRequest{Number: 5, ResourceID: "r3.key", Kind: "key", Method: "GET", Outcome: "transport_error", FailureStage: "dns", FailureCode: "dns_error"}
	requests := []PlayerNetworkRequest{first, second, third, fourth, fifth}
	finding := Finding{ID: "S001-A001", Message: "对照播放请求的终止阶段"}
	report := Report{Manifest: Manifest{Protocol: ProtocolHLS}, PlayerNetwork: &PlayerNetworkReport{Version: 2, Boundary: "upstream_proxy", Complete: true, Requests: requests, DroppedHops: 2}, Findings: []Finding{finding},
		StallInvestigations: []StallInvestigation{{ID: "S001", Correlation: StallCorrelation{PlayerRequests: requests}, Assessment: &StallAssessment{Findings: []Finding{finding}}, Explanation: &Explanation{Hypotheses: []Hypothesis{{EvidenceIDs: []string{finding.ID}}}}}}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, text := range []string{"逐跳阶段耗时", "HTTP 302 → 200", "TTFB 从本跳开始计时", "不代表浏览器接收完成", "DNS/TCP/TLS 保持未观测", "上游响应体阶段 (ms)", "同阶段多次观测用 / 分列", "另有 2 跳未保存", "共有 2 跳未保存", "另有 1 条已展示请求未包含逐跳阶段记录", "TLS 失败 [tls_error]", "向播放器写出失败 [proxy_write_error]", "DNS 失败 [dns_error]", "此请求未包含可展示的逐跳阶段记录"} {
		if !strings.Contains(body, text) {
			t.Errorf("report missing %q", text)
		}
	}
	firstDetail := networkPhaseHTMLRequest(t, body, "1")
	for _, row := range []string{
		`<th scope="row">#1</th><td>302</td><td>-100</td><td>-100</td><td>未复用</td><td>0</td><td>0</td><td>0</td><td>0</td><td>0</td><td>0</td><td>0</td><td>未观测</td>`,
		`<th scope="row">#2</th><td>200</td><td>-100</td><td>300</td><td>已复用</td><td>100</td><td>未观测</td><td>未观测</td><td>未观测</td><td>10</td><td>90</td><td>0</td><td>300</td>`,
	} {
		if !strings.Contains(firstDetail, row) {
			t.Error("measured zero, missing reused-connection phases or body duration were misrepresented")
		}
	}
	secondDetail := networkPhaseHTMLRequest(t, body, "2")
	if !strings.Contains(secondDetail, "<td>10 / 20</td>") || !strings.Contains(secondDetail, "<td>未观测</td><td>未观测</td>") || !strings.Contains(secondDetail, "请求终止阶段 / 原因") {
		t.Fatal("repeated connection attempts or missing TTFB were lost")
	}
	if strings.Count(body, "HTTP 302 → 200") != 2 || strings.Count(body, `id="case-S001-A001"`) != 1 || !strings.Contains(body, `href="#case-S001-A001"`) {
		t.Fatal("selected case phase details or model evidence anchors were lost")
	}
}

func TestPlayerNetworkPhasesHTMLLegacyUnobserved(t *testing.T) {
	report := Report{Manifest: Manifest{Protocol: ProtocolHLS}, PlayerNetwork: &PlayerNetworkReport{Version: 1, Boundary: "upstream_proxy", Complete: true, Requests: []PlayerNetworkRequest{{Number: 1, ResourceID: "index.m3u8", Method: "GET", HTTPStatus: 200, Outcome: "complete"}}}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	if !strings.Contains(body, "当前请求记录未包含逐跳阶段耗时，重新采样可补充") || strings.Contains(body, `<details class="player-network-phase-detail">`) {
		t.Fatal("legacy request timings were invented or capability note is absent")
	}
}
