package diagnostic

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mixedReportImageFixture(t *testing.T) (Report, []ModelImage) {
	t.Helper()
	report := Report{Findings: []Finding{{ID: "F001", Code: "test_buffer", Message: "多次卡顿时序"}}}
	images := providerImageFixture(t)
	for index := range 4 {
		id := fmt.Sprintf("S%03d", index+1)
		item := StallInvestigation{ID: id, PlayerRevision: 4, Stall: PlayerStall{StartMS: int64(index * 3000), EndMS: int64(index*3000 + 1500), DurationMS: 1500}}
		if index < len(images) {
			ref := &images[index].Reference
			ref.ID, ref.CaseID, ref.Name = id+"-I002", id, "stalls/"+id+"/during.jpg"
			// Keep the canonical I002 identity: one representative is selected from
			// a full case with before/during/after metadata.
			for assetIndex, name := range []string{"before.jpg", "during.jpg", "after.jpg"} {
				asset := DerivedArtifact{Name: "stalls/" + id + "/" + name, Kind: "screenshot", Artifact: ref.Artifact, MediaTimeMS: ref.MediaTimeMS}
				if assetIndex != 1 {
					asset.SHA256 = strings.Repeat(fmt.Sprintf("%x", assetIndex+1), 64)
				}
				item.Media.Assets = append(item.Media.Assets, asset)
			}
		}
		report.StallInvestigations = append(report.StallInvestigations, item)
	}
	report.Findings = append(report.Findings, modelImageFindings(modelImageReferences(images))...)
	return report, images
}

// Collect native image values without interpreting captions as pixels.
func reportProviderImageValues(value any) []string {
	var result []string
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if text, ok := child.(string); ok && (key == "data" || key == "url") {
				result = append(result, strings.TrimPrefix(text, "data:image/jpeg;base64,"))
			} else {
				result = append(result, reportProviderImageValues(child)...)
			}
		}
	case []any:
		for _, child := range v {
			result = append(result, reportProviderImageValues(child)...)
		}
	}
	return result
}

func TestReportModelProvidersSendMixedCaseRepresentatives(t *testing.T) {
	report, images := mixedReportImageFixture(t)
	for _, name := range []string{"openai", "anthropic", "gemini"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				var envelope any
				if json.Unmarshal(body, &envelope) != nil {
					t.Error("bad provider envelope")
					return
				}
				values := reportProviderImageValues(envelope)
				if len(values) != 3 {
					t.Errorf("want3nativeimages, got%d", len(values))
				}
				for _, image := range images {
					found := false
					for _, value := range values {
						data, err := base64.StdEncoding.DecodeString(value)
						found = found || err == nil && bytes.Equal(data, image.JPEG)
					}
					if !found {
						t.Errorf("missing actual JPEG %s", image.Reference.ID)
					}
					if !bytes.Contains(body, []byte(image.Reference.ID)) {
						t.Error("lost original per-case image ID")
					}
				}
				// Numeric JSON remains a text block in each native payload. Both the
				// unpictured fourth case and the selected cases must reach the model.
				if !bytes.Contains(body, []byte("S004")) || !bytes.Contains(body, []byte("analysis_scope")) || !bytes.Contains(body, []byte("report")) {
					t.Error("report scope or unpictured case timeline omitted")
				}
				providerImageResponse(w, name)
			}))
			defer server.Close()
			provider := newTestImageProvider(t, name, server.URL)
			result, err := provider.ExplainWithImages(t.Context(), report, images)
			if err != nil || result.Summary == "" || result.InputEvidence != nil || result.ReportInputEvidence != nil || calls != 1 {
				t.Fatalf("mixed-case image call: %+v %v calls=%d", result, err, calls)
			}
		})
	}
}

func TestReportModelInputMarksOnlyPicturedCases(t *testing.T) {
	report, images := mixedReportImageFixture(t)
	data, err := modelInputWithImages(report, images)
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Scope string `json:"analysis_scope"`
		Cases []struct {
			ID     string `json:"id"`
			Pixels bool   `json:"image_pixels_provided"`
		} `json:"stall_investigations"`
	}
	if json.Unmarshal(data, &input) != nil || input.Scope != "report" || len(input.Cases) != 4 {
		t.Fatalf("wrong overall scope: %s", data)
	}
	for index, item := range input.Cases {
		if item.Pixels != (index < 3) {
			t.Errorf("case%s hasfalsepictureclaim", item.ID)
		}
	}
	images[0].Reference.ID = "S001-I001"
	if _, err := modelInputWithImages(report, images); err == nil {
		t.Fatal("unselected/noncanonical image identity accepted")
	}
}

func TestReportModelPrioritiesValidatedAndOrdered(t *testing.T) {
	client := modelClient{identity: ModelIdentity{Provider: ProviderGemini, Model: "model"}}
	report := Report{Findings: []Finding{{ID: "F001"}}}
	answer := `{"summary":"整体结论","hypotheses":[{"priority":"P3","claim":"条件检查","evidence_ids":["F001"],"next_check":"对照"},{"priority":"P1","claim":"优先检查","evidence_ids":["F001"],"next_check":"复测"}],"report_input_evidence":{"version":1,"mode":"vision","images":[{"id":"forged"}]}}`
	result, err := client.parse(answer, report)
	if err != nil || result.ReportInputEvidence != nil || len(result.Hypotheses) != 2 || result.Hypotheses[0].Priority != "P1" {
		t.Fatalf("priority or forged audit: %+v %v", result, err)
	}
	for _, priority := range []string{"P0", "urgent", "<script>", "p1"} {
		invalid := strings.Replace(answer, `"P3"`, fmt.Sprintf("%q", priority), 1)
		if _, err := client.parse(invalid, report); err == nil {
			t.Fatalf("invalid priority accepted:%s", priority)
		}
	}
	legacy := `{"summary":"旧文字结果","hypotheses":[{"claim":"旧建议","evidence_ids":["F001"],"next_check":"复测"}]}`
	if _, err := client.parse(legacy, report); err != nil {
		t.Fatalf("legacy result invalidated: %v", err)
	}
}
