package diagnostic

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestPlayerNetworkHTMLProxyBoundaryAndCaseRequests(t *testing.T) {
	zero := int64(0)
	requests := []PlayerNetworkRequest{
		{Number: 1, ResourceID: "index.m3u8", Kind: "playlist", Method: "GET", StartMS: 0, HeaderMS: &zero, FirstBodyMS: &zero, EndMS: 0, HTTPStatus: 200, Bytes: 0, Outcome: "complete"},
		{Number: 2, ResourceID: "r1.m4s", Kind: "media", Method: "GET", StartMS: 10, EndMS: 1200, Bytes: 0, Outcome: "incomplete"},
		{Number: 3, ResourceID: "r2.key", Kind: "key", Method: "GET", StartMS: 20, EndMS: 60, HTTPStatus: 503, Outcome: "http_error"},
		{Number: 4, ResourceID: "r3.mp4", Kind: "init", Method: "HEAD", StartMS: 30, EndMS: 70, Outcome: "timeout"},
		{Number: 5, ResourceID: "r4.ts", Kind: "media", Method: "GET", StartMS: -250, HeaderMS: networkHTMLInt64(-200), FirstBodyMS: networkHTMLInt64(-150), EndMS: -100, HTTPStatus: 206, Bytes: 123, Outcome: "complete"},
	}
	finding := Finding{ID: "S001-E001", Message: "播放请求需要复核"}
	report := Report{
		Manifest:      Manifest{Protocol: ProtocolHLS},
		PlayerNetwork: &PlayerNetworkReport{Version: 1, Boundary: "upstream_proxy", Complete: false, DroppedRequests: 3, Requests: requests},
		Findings:      []Finding{finding},
		StallInvestigations: []StallInvestigation{{
			ID: "S001", Status: "complete", StallIndex: 1,
			Correlation: StallCorrelation{
				Source: "independent_pull", PlayerRequests: requests[:2], Findings: []Finding{finding},
				Requests: []HTTPRequestTiming{{Number: 1, Kind: "hls_playlist", HTTPStatus: 200}},
			},
			Assessment: &StallAssessment{Directions: []StallDirection{{Status: "candidate", Title: "核对实际播放请求", EvidenceIDs: []string{finding.ID}, NextChecks: []string{"复核播放连接的超时"}}}},
		}},
	}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, text := range []string{"HLS 实际播放请求 · 代理观测", "本地代理上游侧", "部分请求快照", "写出完成不代表浏览器已接收、解码或可播放", "偏移从后端采样开始计时", "上游响应头偏移 (ms)", "上游首段响应体偏移 (ms)", "上游读取字节", "代理结束 / 截至偏移 (ms)", "1200（截至）", "未观测", "上游 HTTP 错误", "超时", "另有 3 条请求未写入本记录", "关联实际播放请求", "关联采样 HTTP 请求", "核对实际播放请求", "复核播放连接的超时", `href="#case-S001-E001"`} {
		if !strings.Contains(body, text) {
			t.Errorf("report missing %q", text)
		}
	}
	start := strings.Index(body, `id="player-network-report"`)
	panel := body[start : start+strings.Index(body[start:], "</article>")]
	rowStart := strings.Index(panel, `<tr><th scope="row">#1`)
	row := panel[rowStart : rowStart+strings.Index(panel[rowStart:], "</tr>")]
	if strings.Count(row, "<td>0</td>") != 5 || strings.Contains(row, "未观测") {
		t.Fatal("measured zero offsets or bytes became unknown")
	}
	rowStart = strings.Index(panel, `<tr><th scope="row">#2`)
	row = panel[rowStart : rowStart+strings.Index(panel[rowStart:], "</tr>")]
	if strings.Count(row, "未观测") != 3 || !strings.Contains(row, "1200（截至）") || !strings.Contains(row, "<td>0</td>") {
		t.Fatal("incomplete request lost unknown fields, cutoff or measured zero bytes")
	}
	rowStart = strings.Index(panel, `<tr><th scope="row">#5`)
	row = panel[rowStart : rowStart+strings.Index(panel[rowStart:], "</tr>")]
	for _, value := range []string{"-250", "-200", "-150", "-100"} {
		if !strings.Contains(row, "<td>"+value+"</td>") {
			t.Errorf("pre-capture offset %s was lost or clamped", value)
		}
	}
	for _, id := range []string{finding.ID, "case-" + finding.ID} {
		if strings.Count(body, `id="`+id+`"`) != 1 {
			t.Errorf("missing or duplicate evidence anchor %q", id)
		}
	}
}

func networkHTMLInt64(value int64) *int64 { return &value }

func TestPlayerNetworkHTMLBoundsRowsAndEscapesText(t *testing.T) {
	network := &PlayerNetworkReport{Version: 1, Boundary: "upstream_proxy", Complete: true}
	for number := 1; number <= 202; number++ {
		network.Requests = append(network.Requests, PlayerNetworkRequest{Number: number, ResourceID: fmt.Sprintf("r%d.ts", number), Kind: "media", Method: "GET", Outcome: "complete"})
	}
	network.Requests[0].ResourceID = `<img src=x onerror="alert(1)">`
	report := Report{Manifest: Manifest{Protocol: ProtocolHLS}, PlayerNetwork: network,
		StallInvestigations: []StallInvestigation{{ID: "S001", Correlation: StallCorrelation{PlayerRequests: network.Requests}}}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	if strings.Count(body, `<th scope="row">#200</th>`) != 2 || strings.Contains(body, `<th scope="row">#201</th>`) || strings.Count(body, "另有 2 条已保存记录") != 2 {
		t.Fatal("report or selected case exceeded its display limit or lost omitted notice")
	}
	if strings.Contains(body, `<img src=x`) || !strings.Contains(body, "&lt;img") {
		t.Fatal("resource text became active HTML")
	}
	if !strings.Contains(body, "已结束") || strings.Contains(body, "未写入本记录") {
		t.Fatal("display-only omissions were confused with discarded requests")
	}
}

func TestPlayerNetworkHTMLLegacyAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name    string
		report  Report
		want    string
		notWant string
	}{
		{name: "legacy HLS", report: Report{Manifest: Manifest{Protocol: ProtocolHLS}, Player: &PlayerReport{}}, want: "该记录未采集实际播放请求", notWant: "当前记录未包含实际播放请求"},
		{name: "observed empty", report: Report{Manifest: Manifest{Protocol: ProtocolHLS}, PlayerNetwork: &PlayerNetworkReport{Boundary: "upstream_proxy", Complete: true}}, want: "当前记录未包含实际播放请求", notWant: "该记录未采集实际播放请求"},
		{name: "other protocol", report: Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}}, notWant: "HLS 实际播放请求 · 代理观测"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := RenderHTML(&output, tc.report, nil, ""); err != nil {
				t.Fatal(err)
			}
			body := output.String()
			if !strings.Contains(body, tc.want) || strings.Contains(body, tc.notWant) {
				t.Fatalf("unexpected capability message: want %q, exclude %q", tc.want, tc.notWant)
			}
		})
	}
}
