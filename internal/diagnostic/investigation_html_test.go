package diagnostic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/jpeg"
	"strings"
	"testing"
)

func investigationTestJPEG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func investigationTestImage(name string, data []byte) DerivedArtifact {
	hash := sha256.Sum256(data)
	return DerivedArtifact{Name: name, Kind: "screenshot", Artifact: Artifact{Bytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}}
}

func TestStallInvestigationHTMLContainsEvidenceAndEmbeddedScreenshots(t *testing.T) {
	data := investigationTestJPEG(t)
	asset := investigationTestImage("stalls/S001/during.jpg", data)
	start, end := int64(100), int64(1200)
	report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}, StallInvestigations: []StallInvestigation{{
		ID: "S001", StallIndex: 1, Status: "partial", Stall: PlayerStall{StartMS: 1500, EndMS: 2500, DurationMS: 1000},
		Correlation: StallCorrelation{Source: "independent_pull", WindowStartMS: 0, WindowEndMS: 5000,
			Notes: []string{`<img src=x onerror="alert(1)">`}, Gaps: []ReceptionGap{{Kind: "video", StartMS: 1000, EndMS: 2600, DurationMS: 1600}},
			BufferSamples: []PlayerBufferSample{{OffsetMS: 1000, BufferMS: 900}, {OffsetMS: 1500, BufferMS: 0}},
			Findings:      []Finding{{ID: "S001-E001", Message: "播放器缓冲耗尽", StartMS: 1500, EndMS: 2500, NextCheck: "比较源端媒体发送记录"}}},
		Media: InvestigationMedia{Status: "partial", Reason: "部分截图不可用", Alignment: "arrival_estimate", StartMS: &start, EndMS: &end,
			Assets: []DerivedArtifact{asset, {Name: "stalls/S001/clip.mp4", Kind: "clip", Artifact: Artifact{Bytes: 1024}}}},
		ModelState: "complete", Explanation: &Explanation{Summary: "需验证源端发送间隔", Hypotheses: []Hypothesis{{Claim: "媒体接收间隔异常", EvidenceIDs: []string{"S001-E001"}, NextCheck: "对照发送端日志"}}},
	}}}
	report.Findings = append([]Finding(nil), report.StallInvestigations[0].Correlation.Findings...)
	var output bytes.Buffer
	if err := RenderHTMLWithAssets(&output, report, nil, "/diagnostics#run123", map[string][]byte{asset.Name: data}); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, text := range []string{"卡顿证据补查", "第 1 次卡顿", "保留部分结果", "1600", "采样使用独立连接", "S001-E001", "源流截图", "data:image/jpeg;base64,", "按接收时序估算", "需验证源端发送间隔", "/api/diagnostics/runs/run123/stall-assets/S001/clip.mp4", "&lt;img"} {
		if !strings.Contains(body, text) {
			t.Errorf("report missing %q", text)
		}
	}
	if strings.Contains(body, `<img src=x`) || strings.Contains(body, `<script`) {
		t.Fatal("evidence text became active HTML")
	}
	for _, id := range []string{"S001-E001", "case-S001-E001"} {
		if strings.Count(body, `id="`+id+`"`) != 1 {
			t.Fatalf("duplicate or missing evidence anchor %s", id)
		}
	}
	if !strings.Contains(body, `href="#case-S001-E001"`) {
		t.Fatal("case advice link did not target inline case evidence")
	}
	output.Reset()
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "data:image/jpeg") || !strings.Contains(output.String(), `href="stalls/S001/clip.mp4"`) {
		t.Fatal("legacy renderer cannot link bundled assets without embedding")
	}
	report.Findings = nil
	output.Reset()
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `id="case-S001-E001"`) || !strings.Contains(output.String(), `href="#case-S001-E001"`) {
		t.Fatal("case-only report has a dangling advice anchor")
	}
}

func TestStallInvestigationHTMLRejectsUntrustedImageDataAndPaths(t *testing.T) {
	data := investigationTestJPEG(t)
	asset := investigationTestImage("stalls/S001/during.jpg", data)
	bad := asset
	bad.Name = "javascript:alert(1)"
	wrong := asset
	wrong.SHA256 = strings.Repeat("0", 64)
	items := []StallInvestigation{{ID: "S001", Media: InvestigationMedia{Assets: []DerivedArtifact{bad, wrong}}}}
	views := investigationHTMLItems(items, "/diagnostics#javascript:alert(1)", map[string][]byte{bad.Name: data, asset.Name: data})
	if len(views) != 1 || len(views[0].Images) != 0 || len(views[0].Clips) != 0 || len(views[0].OtherImages) != 0 {
		t.Fatal("untrusted media entered exported HTML")
	}
}

func TestStallInvestigationHTMLBoundsEmbeddedImages(t *testing.T) {
	data := make([]byte, maxDerivedImageBytes)
	copy(data, investigationTestJPEG(t))
	var items []StallInvestigation
	assets := make(map[string][]byte)
	for _, id := range []string{"S001", "S002", "S003", "S004", "S005", "S006", "S007", "S008"} {
		item := StallInvestigation{ID: id}
		for _, name := range []string{"before.jpg", "during.jpg", "after.jpg"} {
			asset := investigationTestImage("stalls/"+id+"/"+name, data)
			item.Media.Assets = append(item.Media.Assets, asset)
			assets[asset.Name] = data
		}
		items = append(items, item)
	}
	views := investigationHTMLItems(items, "", assets)
	embedded, omitted := 0, 0
	for _, view := range views {
		embedded += len(view.Images)
		omitted += view.ImagesOmitted
	}
	if embedded != 16 || omitted != 8 {
		t.Fatalf("unexpected screenshot budget: embedded=%d omitted=%d", embedded, omitted)
	}
}
