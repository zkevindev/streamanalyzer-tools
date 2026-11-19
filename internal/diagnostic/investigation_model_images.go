package diagnostic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const maxModelImages = MaxModelImages
const maxModelImageBytes = MaxModelImageBytes

func validModelImageReference(ref ModelImageReference, index int, caseID string) bool {
	return investigationMediaID.MatchString(caseID) && ref.CaseID == caseID && ref.ID == fmt.Sprintf("%s-I%03d", caseID, index+1) &&
		validateStallAsset(DerivedArtifact{Name: ref.Name, Kind: "screenshot", Artifact: ref.Artifact, MediaTimeMS: ref.MediaTimeMS}, caseID) && strings.HasSuffix(ref.Name, ".jpg")
}

// Providers validate again before encoding; the bytes must be the exact saved
// image identified by the trusted service reference, never a remote URL.
func validateModelImages(images []ModelImage) error {
	if len(images) == 0 || len(images) > maxModelImages {
		return fmt.Errorf("%w: 截图数量无效", ErrInvalid)
	}
	total := 0
	seen := map[string]bool{}
	seenIDs := map[string]bool{}
	for _, image := range images {
		ref := image.Reference
		// A report may select one representative from several cases. The
		// original case-local ID is retained even when before/after are omitted.
		validReference := false
		for index := range maxModelImages {
			validReference = validReference || validModelImageReference(ref, index, ref.CaseID)
		}
		if !validReference || seen[ref.Name] || seenIDs[ref.ID] || len(image.JPEG) != int(ref.Bytes) {
			return fmt.Errorf("%w: 截图身份或长度无效", ErrInvalid)
		}
		seen[ref.Name] = true
		seenIDs[ref.ID] = true
		total += len(image.JPEG)
		if total > maxModelImageBytes {
			return fmt.Errorf("%w: 截图总大小超限", ErrInvalid)
		}
		hash := sha256.Sum256(image.JPEG)
		if hex.EncodeToString(hash[:]) != ref.SHA256 {
			return fmt.Errorf("%w: 截图哈希无效", ErrInvalid)
		}
		config, err := jpeg.DecodeConfig(bytes.NewReader(image.JPEG))
		if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || config.Width*config.Height > 8_388_608 {
			return fmt.Errorf("%w: 截图 JPEG 尺寸无效", ErrInvalid)
		}
		decoded, err := jpeg.Decode(bytes.NewReader(image.JPEG))
		if err != nil || decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
			return fmt.Errorf("%w: 截图 JPEG 内容不完整", ErrInvalid)
		}
	}
	return nil
}

func validateModelImagesForReport(report Report, images []ModelImage) error {
	if err := validateModelImages(images); err != nil {
		return err
	}
	// Standalone callers historically supplied numeric-only reports. Whenever
	// case metadata is present, bind every image to its exact canonical asset.
	if len(report.StallInvestigations) == 0 {
		return nil
	}
	for _, image := range images {
		matched := false
		for _, item := range report.StallInvestigations {
			if item.ID != image.Reference.CaseID {
				continue
			}
			assets, err := modelImageAssets(item)
			if err != nil {
				return ErrInvalid
			}
			for index, asset := range assets {
				ref := image.Reference
				if validModelImageReference(ref, index, item.ID) && ref.Name == asset.Name && ref.Artifact == asset.Artifact && reflect.DeepEqual(ref.MediaTimeMS, asset.MediaTimeMS) {
					matched = true
				}
			}
		}
		if !matched {
			return fmt.Errorf("%w: 模型截图与案例产物不一致", ErrInvalid)
		}
	}
	return nil
}

func modelImageAssets(item StallInvestigation) ([]DerivedArtifact, error) {
	var assets []DerivedArtifact
	seen := map[string]bool{}
	for _, asset := range item.Media.Assets {
		if asset.Kind != "screenshot" {
			continue
		}
		if !validateStallAsset(asset, item.ID) || seen[asset.Name] {
			return nil, ErrInvalid
		}
		seen[asset.Name] = true
		assets = append(assets, asset)
	}
	if len(assets) > maxModelImages {
		return nil, ErrInvalid
	}
	rank := func(name string) int {
		switch filepath.Base(name) {
		case "before.jpg":
			return 0
		case "during.jpg":
			return 1
		case "after.jpg":
			return 2
		}
		return 3
	}
	sort.Slice(assets, func(i, j int) bool { return rank(assets[i].Name) < rank(assets[j].Name) })
	return assets, nil
}
func loadStallModelImages(dir string, item StallInvestigation) ([]ModelImage, error) {
	assets, err := modelImageAssets(item)
	if err != nil {
		return nil, err
	}
	if len(assets) == 0 {
		return []ModelImage{}, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, name := range []string{"stalls", "stalls/" + item.ID} {
		info, err := root.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrInvalid
		}
	}
	images := make([]ModelImage, 0, len(assets))
	for index, asset := range assets {
		info, err := root.Lstat(asset.Name)
		if err != nil || !info.Mode().IsRegular() || info.Size() != asset.Bytes {
			return nil, ErrInvalid
		}
		file, err := root.Open(asset.Name)
		if err != nil {
			return nil, err
		}
		opened, statErr := file.Stat()
		if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			file.Close()
			return nil, ErrInvalid
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxDerivedImageBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return nil, ErrInvalid
		}
		ref := ModelImageReference{ID: fmt.Sprintf("%s-I%03d", item.ID, index+1), CaseID: item.ID, Name: asset.Name, Artifact: asset.Artifact, MediaTimeMS: asset.MediaTimeMS}
		images = append(images, ModelImage{Reference: ref, JPEG: data})
	}
	if err := validateModelImages(images); err != nil {
		return nil, err
	}
	return images, nil
}
func modelImageReferences(images []ModelImage) []ModelImageReference {
	refs := make([]ModelImageReference, 0, len(images))
	for _, image := range images {
		ref := image.Reference
		if ref.MediaTimeMS != nil {
			value := *ref.MediaTimeMS
			ref.MediaTimeMS = &value
		}
		refs = append(refs, ref)
	}
	return refs
}

func validModelInputEvidence(evidence *ModelInputEvidence, item StallInvestigation) bool {
	if evidence == nil || evidence.Version != 1 || evidence.CaseID != item.ID || evidence.PlayerRevision != item.PlayerRevision || evidence.PlayerRevision < 1 || evidence.RequestedMode != "auto" && evidence.RequestedMode != "text" {
		return false
	}
	if evidence.Mode == "vision" {
		if evidence.RequestedMode != "auto" || evidence.FallbackCode != "" || len(evidence.Images) == 0 || len(evidence.AttemptedImages) != 0 {
			return false
		}
	} else if evidence.Mode == "text" {
		if len(evidence.Images) != 0 {
			return false
		}
		switch evidence.FallbackCode {
		case "disabled":
			if evidence.RequestedMode != "text" {
				return false
			}
		case "no_images", "images_invalid", "provider_text_only", "model_text_only", "images_unsupported":
			if evidence.RequestedMode != "auto" {
				return false
			}
		default:
			return false
		}
		if (evidence.FallbackCode == "images_unsupported") != (len(evidence.AttemptedImages) > 0) {
			return false
		}
	} else {
		return false
	}
	refs := evidence.Images
	if evidence.Mode == "text" {
		refs = evidence.AttemptedImages
	}
	if len(refs) > maxModelImages {
		return false
	}
	assets, err := modelImageAssets(item)
	if err != nil {
		return len(refs) == 0 && evidence.Mode == "text" && evidence.FallbackCode == "images_invalid"
	}
	if evidence.FallbackCode == "no_images" && len(assets) != 0 {
		return false
	}
	if len(refs) > 0 && len(refs) != len(assets) {
		return false
	}
	for index, ref := range refs {
		if !validModelImageReference(ref, index, item.ID) || ref.Name != assets[index].Name || ref.Artifact != assets[index].Artifact || !reflect.DeepEqual(ref.MediaTimeMS, assets[index].MediaTimeMS) {
			return false
		}
	}
	return true
}
func validateStallModelInput(dir string, report Report, item StallInvestigation, evidence *ModelInputEvidence) error {
	if report.Player == nil || evidence == nil || evidence.PlayerRevision != report.Player.Revision || !validModelInputEvidence(evidence, item) {
		return fmt.Errorf("%w: 模型输入证据与补查版本不一致", ErrInvalid)
	}
	if len(evidence.Images)+len(evidence.AttemptedImages) > 0 {
		images, err := loadStallModelImages(dir, item)
		if err != nil {
			return fmt.Errorf("%w: 模型截图证据已改变", ErrReportChanged)
		}
		expected := evidence.Images
		if evidence.Mode == "text" {
			expected = evidence.AttemptedImages
		}
		if !reflect.DeepEqual(expected, modelImageReferences(images)) {
			return ErrReportChanged
		}
	}
	return nil
}

// Older published ledgers must not acquire audit fields, even explicit null.
func validModelInputLedgerShape(data []byte, ledger investigationLedger) bool {
	var raw struct {
		Items []struct {
			Explanation map[string]json.RawMessage `json:"explanation"`
		} `json:"items"`
	}
	if json.Unmarshal(data, &raw) != nil || len(raw.Items) != len(ledger.Items) {
		return false
	}
	hasAudit := false
	for index, item := range ledger.Items {
		if present, canonical := modelEvidenceFieldPresence(raw.Items[index].Explanation, "report_input_evidence"); present || !canonical || item.Explanation != nil && item.Explanation.ReportInputEvidence != nil {
			return false
		}
		present, canonical := modelEvidenceFieldPresence(raw.Items[index].Explanation, "input_evidence")
		if !canonical {
			return false
		}
		if present && (ledger.Version != 3 || item.Explanation == nil || item.Explanation.InputEvidence == nil) {
			return false
		}
		if item.Explanation != nil && item.Explanation.InputEvidence != nil {
			if ledger.Version != 3 || !present {
				return false
			}
			hasAudit = true
		}
	}
	return ledger.Version != 3 || hasAudit
}

// encoding/json matches struct tags without case sensitivity. Raw presence
// guards must use the same equivalence, while accepting only canonical keys.
func modelEvidenceFieldPresence(fields map[string]json.RawMessage, name string) (bool, bool) {
	present := false
	for key := range fields {
		if strings.EqualFold(key, name) {
			present = true
			if key != name {
				return true, false
			}
		}
	}
	return present, true
}
