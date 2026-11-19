package diagnostic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type imageStallTestExplainer struct {
	mode        string
	unsupported bool
	vision      func(context.Context, Report, []ModelImage) (Explanation, error)
	text        func(context.Context, Report) (Explanation, error)
}

func (m *imageStallTestExplainer) Identity() ModelIdentity {
	return ModelIdentity{Provider: ProviderOpenAICompatible, Model: "image-test"}
}
func (m *imageStallTestExplainer) ImageInputMode() string {
	if m.mode == "text" {
		return "text"
	}
	return "auto"
}
func (m *imageStallTestExplainer) SupportsImages() bool { return !m.unsupported }
func (m *imageStallTestExplainer) Explain(ctx context.Context, report Report) (Explanation, error) {
	if m.text == nil {
		return Explanation{}, errors.New("unexpected text call")
	}
	return m.text(ctx, report)
}
func (m *imageStallTestExplainer) ExplainWithImages(ctx context.Context, report Report, images []ModelImage) (Explanation, error) {
	if m.vision == nil {
		return Explanation{}, errors.New("unexpected image call")
	}
	return m.vision(ctx, report, images)
}
func imageStallTestAnswer(id string) Explanation {
	return Explanation{Provider: ProviderOpenAICompatible, Model: "image-test", Summary: "核对本次源流截图与时间证据", Hypotheses: []Hypothesis{{Claim: "仅核对静态源画面内容", EvidenceIDs: []string{id}, NextCheck: "结合收流和播放器时间线复查"}}}
}
func openImageStallTestRun(t *testing.T) (*Service, string, StallInvestigation) {
	t.Helper()
	service, id, _ := openStallServiceTestRun(t, nil, false)
	if _, err := service.InvestigateStall(id, StallInvestigationRequest{StallIndex: 1}); err != nil {
		t.Fatal(err)
	}
	item := awaitStallTestItem(t, service, id)
	dir := service.runDir(id)
	if err := os.MkdirAll(filepath.Join(dir, "stalls", item.ID), 0700); err != nil {
		t.Fatal(err)
	}
	item.Media = InvestigationMedia{Status: "ready", Alignment: "player_clock_estimate", Assets: []DerivedArtifact{}}
	for index, name := range []string{"before.jpg", "during.jpg", "after.jpg"} {
		picture := image.NewRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				picture.Set(x, y, color.RGBA{R: uint8(index * 80), G: 80, B: 150, A: 255})
			}
		}
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, picture, nil); err != nil {
			t.Fatal(err)
		}
		name = "stalls/" + item.ID + "/" + name
		artifact := investigationMediaSave(t, dir, name, encoded.Bytes())
		at := int64(400 + index*100)
		item.Media.Assets = append(item.Media.Assets, DerivedArtifact{Name: name, Kind: "screenshot", Artifact: artifact, MediaTimeMS: &at})
	}
	if err := service.saveStallItem(id, item); err != nil {
		t.Fatal(err)
	}
	return service, id, item
}
func TestStallModelImagesVisionEvidenceAndBundleExact(t *testing.T) {
	service, id, _ := openImageStallTestRun(t)
	calls := 0
	service.SetModel(&imageStallTestExplainer{vision: func(_ context.Context, report Report, images []ModelImage) (Explanation, error) {
		calls++
		if err := validateModelImages(images); err != nil {
			t.Fatal(err)
		}
		if len(images) != 3 || len(report.StallInvestigations) != 1 {
			t.Fatal("scope/images missing")
		}
		for index, image := range images {
			if image.Reference.ID != []string{"S001-I001", "S001-I002", "S001-I003"}[index] {
				t.Fatal(image.Reference)
			}
		}
		found := false
		for _, finding := range report.Findings {
			if finding.ID == images[1].Reference.ID {
				found = true
			}
		}
		if !found {
			t.Fatal("trusted image finding absent")
		}
		answer := imageStallTestAnswer(images[1].Reference.ID)
		answer.InputEvidence = &ModelInputEvidence{Version: 99, CaseID: "provider-injected"}
		return answer, nil
	}})
	item, err := service.ExplainStall(t.Context(), id, "S001")
	if err != nil || item.Explanation == nil || calls != 1 {
		t.Fatalf("vision failed: %+v %v", item, err)
	}
	audit := item.Explanation.InputEvidence
	if audit == nil || audit.Mode != "vision" || audit.Version != 1 || audit.CaseID != "S001" || len(audit.Images) != 3 || len(audit.AttemptedImages) != 0 {
		t.Fatalf("untrusted/missing input audit: %+v", audit)
	}
	detail, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	ledgerData, err := readStallLedger(service.runDir(id), *detail.Report.Manifest.StallEvidence)
	if err != nil {
		t.Fatal(err)
	}
	var ledger investigationLedger
	if err := json.Unmarshal(ledgerData, &ledger); err != nil || ledger.Version != 3 {
		t.Fatal("audited explanation did not choose ledger3")
	}
	var bundle bytes.Buffer
	if err := service.WriteBundle(id, &bundle); err != nil {
		t.Fatal(err)
	}
	replay, err := readBundle(t.TempDir(), bytes.NewReader(bundle.Bytes()))
	if err != nil || !reflect.DeepEqual(replay, *detail.Report) {
		t.Fatalf("vision audit did not replay exactly: %v", err)
	}
	for name, mutate := range map[string]func(*investigationLedger){"case": func(l *investigationLedger) { l.Items[0].Explanation.InputEvidence.CaseID = "S002" }, "revision": func(l *investigationLedger) { l.Items[0].Explanation.InputEvidence.PlayerRevision++ }, "hash": func(l *investigationLedger) {
		l.Items[0].Explanation.InputEvidence.Images[0].SHA256 = strings.Repeat("0", 64)
	}, "name": func(l *investigationLedger) {
		l.Items[0].Explanation.InputEvidence.Images[0].Name = "stalls/S002/before.jpg"
	}, "old version": func(l *investigationLedger) { l.Version = 1 }, "injected final text image": func(l *investigationLedger) {
		l.Items[0].Explanation.InputEvidence.Mode = "text"
		l.Items[0].Explanation.InputEvidence.FallbackCode = "no_images"
	}} {
		t.Run(name, func(t *testing.T) {
			modified := assessmentRewriteBundle(t, bundle.Bytes(), mutate)
			if _, err := readBundle(t.TempDir(), bytes.NewReader(modified)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("resigned audit tamper accepted: %v", err)
			}
		})
	}
}

func TestStallModelImagesTextFallbackAndNoRetryOnOtherFailure(t *testing.T) {
	for _, mode := range []string{"disabled", "provider_text_only", "no_images", "images_invalid", "images_unsupported", "auth_failure"} {
		t.Run(mode, func(t *testing.T) {
			service, id, item := openImageStallTestRun(t)
			textCalls, imageCalls := 0, 0
			model := &imageStallTestExplainer{text: func(_ context.Context, report Report) (Explanation, error) {
				textCalls++
				for _, finding := range report.Findings {
					if strings.Contains(finding.ID, "-I") {
						t.Fatal("text fallback retained image evidence IDs")
					}
				}
				return imageStallTestAnswer(report.Findings[0].ID), nil
			}, vision: func(_ context.Context, _ Report, _ []ModelImage) (Explanation, error) {
				imageCalls++
				if mode == "auth_failure" {
					return Explanation{}, ErrProvider
				}
				return Explanation{}, ErrModelImagesUnsupported
			}}
			switch mode {
			case "disabled":
				model.mode = "text"
			case "provider_text_only":
				model.unsupported = true
			case "no_images":
				item.Media.Assets = []DerivedArtifact{}
				item.Media.Status = "unavailable"
				if err := service.saveStallItem(id, item); err != nil {
					t.Fatal(err)
				}
			case "images_invalid":
				if err := os.WriteFile(filepath.Join(service.runDir(id), item.Media.Assets[0].Name), []byte("truncated JPEG"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			service.SetModel(model)
			result, err := service.ExplainStall(t.Context(), id, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "auth_failure" {
				if textCalls != 0 || imageCalls != 1 || result.Explanation != nil || result.ModelState != "failed" {
					t.Fatal("ordinary error retried as text")
				}
				return
			}
			if result.Explanation == nil || result.Explanation.InputEvidence == nil || result.Explanation.InputEvidence.Mode != "text" || result.Explanation.InputEvidence.FallbackCode != mode || textCalls != 1 {
				t.Fatalf("wrong fallback: %+v calls=%d/%d", result, textCalls, imageCalls)
			}
			audit := result.Explanation.InputEvidence
			if len(audit.Images) != 0 {
				t.Fatal("final text result claims images")
			}
			if mode == "images_unsupported" {
				if imageCalls != 1 || len(audit.AttemptedImages) != 3 {
					t.Fatal("rejected image attempt lost")
				}
			} else if imageCalls != 0 || len(audit.AttemptedImages) != 0 {
				t.Fatal("unsent images recorded as attempted")
			}
			if mode != "images_invalid" {
				var bundle bytes.Buffer
				if err := service.WriteBundle(id, &bundle); err != nil {
					t.Fatal(err)
				}
				replay, err := readBundle(t.TempDir(), bytes.NewReader(bundle.Bytes()))
				if err != nil {
					t.Fatalf("text fallback evidence failed import: %v", err)
				}
				got := replay.StallInvestigations[0].Explanation.InputEvidence
				if !reflect.DeepEqual(audit, got) {
					t.Fatal("fallback or attempted-image transmission audit changed on replay")
				}
			}

		})
	}
}

func TestStallModelImagesChangedDuringProviderCannotCommit(t *testing.T) {
	service, id, item := openImageStallTestRun(t)
	started, release := make(chan struct{}), make(chan struct{})
	service.SetModel(&imageStallTestExplainer{vision: func(_ context.Context, _ Report, images []ModelImage) (Explanation, error) {
		close(started)
		<-release
		return imageStallTestAnswer(images[0].Reference.ID), nil
	}})
	done := make(chan error, 1)
	go func() { _, err := service.ExplainStall(t.Context(), id, item.ID); done <- err }()
	<-started
	if err := os.WriteFile(filepath.Join(service.runDir(id), item.Media.Assets[0].Name), []byte("changed while request active"), 0600); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, ErrReportChanged) {
		t.Fatalf("stale pixel response committed: %v", err)
	}
	detail, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	got := detail.Report.StallInvestigations[0]
	if got.Explanation != nil || got.Status == "running" || got.ModelState != "failed" {
		t.Fatalf("stale image job left pending or saved output: %+v", got)
	}
}

func TestStallModelImagesLoaderRejectsTruncatedAndSymlinkInputs(t *testing.T) {
	service, id, item := openImageStallTestRun(t)
	images, err := loadStallModelImages(service.runDir(id), item)
	if err != nil || len(images) != 3 {
		t.Fatal(err)
	}
	truncated := images
	truncated[0].JPEG = append([]byte(nil), truncated[0].JPEG[:len(truncated[0].JPEG)/2]...)
	hash := sha256.Sum256(truncated[0].JPEG)
	truncated[0].Reference.Artifact = Artifact{Bytes: int64(len(truncated[0].JPEG)), SHA256: hex.EncodeToString(hash[:])}
	if err := validateModelImages(truncated); err == nil {
		t.Fatal("valid header but truncated JPEG accepted")
	}
	path := filepath.Join(service.runDir(id), item.Media.Assets[0].Name)
	outside := filepath.Join(t.TempDir(), "outside.jpg")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skip("symlink unavailable")
	}
	if _, err := loadStallModelImages(service.runDir(id), item); err == nil {
		t.Fatal("external symlink screenshot accepted")
	}
}

func TestStallModelImagesOldLedgerRejectsAuditFieldPresence(t *testing.T) {
	for _, version := range []int{1, 2} {
		ledger := investigationLedger{Version: version, Items: []StallInvestigation{{ID: "S001", Explanation: &Explanation{Model: "historical-text", Summary: "历史文本解读"}}}}
		data, err := json.Marshal(ledger)
		if err != nil {
			t.Fatal(err)
		}
		if !validModelInputLedgerShape(data, ledger) {
			t.Fatal("historical text ledger was reclassified")
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		raw["items"].([]any)[0].(map[string]any)["explanation"].(map[string]any)["input_evidence"] = nil
		data, err = json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		if validModelInputLedgerShape(data, ledger) {
			t.Fatalf("legacy ledger%d accepted explicit null audit field", version)
		}
	}
}

func TestStallModelImagesCanceledVisionNeverRetriesText(t *testing.T) {
	service, id, _ := openImageStallTestRun(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	textCalls := 0
	service.SetModel(&imageStallTestExplainer{vision: func(context.Context, Report, []ModelImage) (Explanation, error) {
		cancel()
		return Explanation{}, ErrModelImagesUnsupported
	}, text: func(context.Context, Report) (Explanation, error) {
		textCalls++
		return Explanation{Summary: "unexpected"}, nil
	}})
	item, err := service.ExplainStall(ctx, id, "S001")
	if err != nil {
		t.Fatal(err)
	}
	if textCalls != 0 || item.Explanation != nil || item.Status != "canceled" {
		t.Fatalf("cancelled vision started a text retry: %+v calls%d", item, textCalls)
	}
}

func TestStallModelImagesLegacyAliasesRejectResignedZIP(t *testing.T) {
	fixtures := []struct {
		version int
		path    string
	}{{1, "testdata/rtsp-v4-legacy-stall.zip"}, {2, "testdata/multimodal-legacy-text.zip"}}
	for _, fixture := range fixtures {
		source, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readBundle(t.TempDir(), bytes.NewReader(source)); err != nil {
			t.Fatalf("published ledger%d baseline invalid: %v", fixture.version, err)
		}
		for _, key := range []string{"INPUT_EVIDENCE", "Input_Evidence"} {
			for _, object := range []bool{false, true} {
				t.Run(fmt.Sprintf("ledger%d/%s/object%t", fixture.version, key, object), func(t *testing.T) {
					modified := networkRewriteBundle(t, source, func(files map[string][]byte) {
						var ledger map[string]any
						if err := json.Unmarshal(files[investigationName], &ledger); err != nil {
							t.Fatal(err)
						}
						if ledger["version"] != float64(fixture.version) {
							t.Fatal("wrong published ledger version")
						}
						item := ledger["items"].([]any)[0].(map[string]any)
						explanation, ok := item["explanation"].(map[string]any)
						if !ok {
							explanation = map[string]any{"model": "published-text", "summary": "历史文本解读", "hypotheses": []any{}}
							item["explanation"] = explanation
						}
						var audit any
						if object {
							audit = map[string]any{"version": 1, "case_id": item["id"], "player_revision": item["player_revision"], "requested_mode": "auto", "mode": "text", "fallback_code": "model_text_only", "images": []any{}}
						}
						explanation[key] = audit
						modelImageTestResignLedger(t, files, ledger)
					})
					if _, err := readBundle(t.TempDir(), bytes.NewReader(modified)); !errors.Is(err, ErrInvalid) {
						t.Fatalf("legacy alias was accepted or rewritten: %v", err)
					}
				})
			}
		}
	}
}
func TestStallModelImagesLegacyRTSPAliasesRejectResignedZIP(t *testing.T) {
	source, err := os.ReadFile("testdata/rtsp-v4-legacy-stall.zip")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"RTSP", "RtSp"} {
		for _, object := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/object%t", key, object), func(t *testing.T) {
				modified := networkRewriteBundle(t, source, func(files map[string][]byte) {
					var ledger map[string]any
					if err := json.Unmarshal(files[investigationName], &ledger); err != nil {
						t.Fatal(err)
					}
					item := ledger["items"].([]any)[0].(map[string]any)
					media := item["media"].(map[string]any)
					var proof any
					if object {
						proof = map[string]any{"version": 1}
					}
					media[key] = proof
					modelImageTestResignLedger(t, files, ledger)
				})
				if _, err := readBundle(t.TempDir(), bytes.NewReader(modified)); !errors.Is(err, ErrInvalid) {
					t.Fatalf("legacy RTSP alias accepted: %v", err)
				}
			})
		}
	}
}
func modelImageTestResignLedger(t *testing.T, files map[string][]byte, ledger map[string]any) {
	t.Helper()
	var err error
	files[investigationName], err = json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(files[manifestName], &manifest); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(files[investigationName])
	manifest.StallEvidence = &Artifact{Bytes: int64(len(files[investigationName])), SHA256: hex.EncodeToString(hash[:])}
	files[manifestName], err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
}
