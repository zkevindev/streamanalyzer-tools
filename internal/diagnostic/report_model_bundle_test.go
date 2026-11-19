package diagnostic

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func reportModelBundleFixture(t *testing.T, fallback bool) (*Service, string, Explanation, []byte) {
	t.Helper()
	service, id, _ := openImageStallTestRun(t)
	service.SetModel(&imageStallTestExplainer{
		vision: func(_ context.Context, report Report, images []ModelImage) (Explanation, error) {
			if fallback {
				return Explanation{}, ErrModelImagesUnsupported
			}
			answer := imageStallTestAnswer(images[0].Reference.ID)
			answer.Summary = "整体结论：先核对源流与缓冲时间线，再定位实际等待阶段"
			answer.Hypotheses[0].Priority = "P1"
			return answer, nil
		},
		text: func(_ context.Context, report Report) (Explanation, error) {
			answer := imageStallTestAnswer(report.Findings[0].ID)
			answer.Summary = "整体文本结论：按时序证据优先检查收流间隔"
			answer.Hypotheses[0].Priority = "P2"
			return answer, nil
		},
	})
	explanation, err := service.RefreshExplanation(t.Context(), id)
	if err != nil || explanation.ReportInputEvidence == nil {
		t.Fatalf("missing report input audit: %+v %v", explanation, err)
	}
	var archive bytes.Buffer
	if err := service.WriteBundle(id, &archive); err != nil {
		t.Fatal(err)
	}
	return service, id, explanation, archive.Bytes()
}

func reportExplanationZIPEntry(t *testing.T, source []byte, name string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range reader.File {
		if entry.Name != name {
			continue
		}
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	t.Fatalf("missing %s", name)
	return nil
}

func rewriteReportExplanationZIP(t *testing.T, source, replacement []byte) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for _, entry := range reader.File {
		writer, err := archive.Create(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name == explanationName {
			if _, err := writer.Write(replacement); err != nil {
				t.Fatal(err)
			}
			continue
		}
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		_, copyErr := io.Copy(writer, file)
		file.Close()
		if copyErr != nil {
			t.Fatal(copyErr)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestReportModelBundleSummaryAndFallbackReplayOffline(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "vision", true: "rejected_images_text"}[fallback], func(t *testing.T) {
			service, id, expected, archive := reportModelBundleFixture(t, fallback)
			before, err := service.Detail(id)
			if err != nil {
				t.Fatal(err)
			}
			var saved Explanation
			if err := json.Unmarshal(reportExplanationZIPEntry(t, archive, explanationName), &saved); err != nil || !reflect.DeepEqual(expected, saved) {
				t.Fatal("export did not retain the exact report explanation", err)
			}
			html := string(reportExplanationZIPEntry(t, archive, htmlName))
			if !strings.Contains(html, expected.Summary) || !strings.Contains(html, expected.Hypotheses[0].Priority) {
				t.Fatal("portable HTML omitted overall conclusion or priority")
			}
			// Import has no reason to use a model, source connection or media tool.
			service.SetModel(&imageStallTestExplainer{text: func(context.Context, Report) (Explanation, error) {
				t.Fatal("offline replay called a model")
				return Explanation{}, ErrProvider
			}, vision: func(context.Context, Report, []ModelImage) (Explanation, error) {
				t.Fatal("offline replay sent images")
				return Explanation{}, ErrProvider
			}})
			t.Setenv("PATH", "")
			imported, err := service.Import(bytes.NewReader(archive))
			if err != nil {
				t.Fatal(err)
			}
			after, err := service.Detail(imported.ID)
			if err != nil || after.Explanation == nil || !reflect.DeepEqual(expected, *after.Explanation) || !reflect.DeepEqual(before.Report, after.Report) {
				t.Fatalf("offline replay changed report, inputs or priorities: %v", err)
			}
			if fallback && (after.Explanation.ReportInputEvidence.Mode != "text" || len(after.Explanation.ReportInputEvidence.Images) != 0 || len(after.Explanation.ReportInputEvidence.AttemptedImages) == 0) {
				t.Fatal("replay confused attempted images with final text inputs")
			}
			var again bytes.Buffer
			if err := service.WriteBundle(imported.ID, &again); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(reportExplanationZIPEntry(t, archive, explanationName), reportExplanationZIPEntry(t, again.Bytes(), explanationName)) {
				t.Fatal("re-export rewrote the explanation")
			}
		})
	}
}

func TestReportModelBundleRejectsUnboundSummaryAndAmbiguousJSON(t *testing.T) {
	_, _, _, archive := reportModelBundleFixture(t, false)
	source := reportExplanationZIPEntry(t, archive, explanationName)
	mutations := map[string]func(map[string]any){
		"audit_version":  func(value map[string]any) { value["report_input_evidence"].(map[string]any)["version"] = 99 },
		"wrong_revision": func(value map[string]any) { value["report_input_evidence"].(map[string]any)["player_revision"] = 999 },
		"wrong_case": func(value map[string]any) {
			value["report_input_evidence"].(map[string]any)["cases"].([]any)[0].(map[string]any)["case_id"] = "S008"
		},
		"missing_timing_cases": func(value map[string]any) { value["report_input_evidence"].(map[string]any)["cases"] = []any{} },
		"wrong_image_hash": func(value map[string]any) {
			value["report_input_evidence"].(map[string]any)["images"].([]any)[0].(map[string]any)["sha256"] = strings.Repeat("0", 64)
		},
		"wrong_image_case": func(value map[string]any) {
			value["report_input_evidence"].(map[string]any)["images"].([]any)[0].(map[string]any)["case_id"] = "S008"
		},
		"unselected_image_citation": func(value map[string]any) {
			value["hypotheses"].([]any)[0].(map[string]any)["evidence_ids"] = []any{"S008-I001"}
		},
		"invalid_priority":  func(value map[string]any) { value["hypotheses"].([]any)[0].(map[string]any)["priority"] = "RUN_NOW" },
		"null_report_audit": func(value map[string]any) { value["report_input_evidence"] = nil },
		"null_case_audit":   func(value map[string]any) { value["input_evidence"] = nil },
		"mixed_scope_audit": func(value map[string]any) { value["input_evidence"] = map[string]any{"case_id": "S001"} },
		"casefold_report_audit": func(value map[string]any) {
			value["REPORT_INPUT_EVIDENCE"] = value["report_input_evidence"]
			delete(value, "report_input_evidence")
		},
		"casefold_nested_images": func(value map[string]any) {
			audit := value["report_input_evidence"].(map[string]any)
			audit["IMAGES"] = audit["images"]
			delete(audit, "images")
		},
		"casefold_case_audit_null": func(value map[string]any) { value["INPUT_EVIDENCE"] = nil },
		"text_cannot_cite_images": func(value map[string]any) {
			audit := value["report_input_evidence"].(map[string]any)
			audit["requested_mode"], audit["mode"], audit["fallback_code"] = "text", "text", "disabled"
			audit["images"], audit["omitted_image_cases"] = []any{}, 0
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(source, &value); err != nil {
				t.Fatal(err)
			}
			mutate(value)
			modified, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			changed := rewriteReportExplanationZIP(t, archive, modified)
			if _, err := readBundle(t.TempDir(), bytes.NewReader(changed)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("re-signed summary tamper accepted: %v", err)
			}
		})
	}
	for name, modified := range map[string][]byte{
		"trailing_json":   append(append([]byte(nil), source...), []byte(` {}`)...),
		"duplicate_audit": []byte(strings.Replace(string(source), `"report_input_evidence":`, `"report_input_evidence":null,"report_input_evidence":`, 1)),
		"oversized":       append(append([]byte(nil), source...), bytes.Repeat([]byte(" "), maxReportExplanationBytes)...),
	} {
		t.Run(name, func(t *testing.T) {
			changed := rewriteReportExplanationZIP(t, archive, modified)
			if _, err := readBundle(t.TempDir(), bytes.NewReader(changed)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("ambiguous/oversized JSON accepted: %v", err)
			}
		})
	}
}

func TestReportModelBundleLegacyTextSummaryRetainsUnknownImageHistory(t *testing.T) {
	service, id, _ := openImageStallTestRun(t)
	legacy := Explanation{Provider: ProviderDeepSeek, Model: "old-text-model", Summary: "旧版整体文字结论"}
	if err := writeJSONFile(service.runDir(id), explanationName, legacy); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := service.WriteBundle(id, &archive); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := readBundle(dir, bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, explanationName))
	if err != nil {
		t.Fatal(err)
	}
	var actual Explanation
	if err := json.Unmarshal(data, &actual); err != nil || !reflect.DeepEqual(legacy, actual) || actual.ReportInputEvidence != nil {
		t.Fatal("legacy summary was rewritten or acquired invented image audit", err)
	}
}

func TestReportModelBundleRejectsReportAuditInPublishedCaseLedgers(t *testing.T) {
	for _, fixture := range []string{"testdata/rtsp-v4-legacy-stall.zip", "testdata/multimodal-legacy-text.zip"} {
		source, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"report_input_evidence", "REPORT_INPUT_EVIDENCE", "Report_Input_Evidence"} {
			for _, object := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/object%t", filepath.Base(fixture), key, object), func(t *testing.T) {
					changed := networkRewriteBundle(t, source, func(files map[string][]byte) {
						var ledger map[string]any
						if err := json.Unmarshal(files[investigationName], &ledger); err != nil {
							t.Fatal(err)
						}
						item := ledger["items"].([]any)[0].(map[string]any)
						explanation, ok := item["explanation"].(map[string]any)
						if !ok {
							explanation = map[string]any{"model": "old-text", "summary": "原有文字记录", "hypotheses": []any{}}
							item["explanation"] = explanation
						}
						var audit any
						if object {
							audit = map[string]any{"version": 1, "mode": "vision"}
						}
						explanation[key] = audit
						modelImageTestResignLedger(t, files, ledger)
					})
					if _, err := readBundle(t.TempDir(), bytes.NewReader(changed)); !errors.Is(err, ErrInvalid) {
						t.Fatalf("case ledger accepted report audit: %v", err)
					}
				})
			}
		}
	}
}

func TestReportModelBundleSnapshotsSummaryDuringSlowDownload(t *testing.T) {
	service, id, expected, _ := reportModelBundleFixture(t, false)
	writer := &blockingPlayerDownload{started: make(chan struct{}), resume: make(chan struct{})}
	download := make(chan error, 1)
	go func() { download <- service.WriteBundle(id, writer) }()
	select {
	case <-writer.started:
	case <-time.After(3 * time.Second):
		close(writer.resume)
		t.Fatal("download did not begin")
	}
	service.SetModel(testExplainer{
		identity: ModelIdentity{Provider: ProviderDeepSeek, Model: "new-summary"},
		explain:  func(context.Context) (string, error) { return "下载期间生成的新整体结论", nil },
	})
	updated := make(chan error, 1)
	go func() { _, err := service.RefreshExplanation(t.Context(), id); updated <- err }()
	select {
	case err := <-updated:
		if err != nil {
			close(writer.resume)
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		close(writer.resume)
		t.Fatal("slow export blocked model persistence")
	}
	close(writer.resume)
	if err := <-download; err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := readBundle(dir, bytes.NewReader(writer.Bytes())); err != nil {
		t.Fatal(err)
	}
	saved, err := readJSONFile[Explanation](dir, explanationName)
	if err != nil || !reflect.DeepEqual(saved, expected) {
		t.Fatal("download mixed the old report snapshot with a new summary", err)
	}
}
