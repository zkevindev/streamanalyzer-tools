package diagnostic

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var stallAssetPattern = regexp.MustCompile(`^stalls/S00[1-8]/(clip\.mp4|before\.jpg|during\.jpg|after\.jpg)$`)

func validStallAssetName(name string) bool { return stallAssetPattern.MatchString(name) }

func stallAssetLimit(name string) uint64 {
	if !validStallAssetName(name) {
		return 0
	}
	if strings.HasSuffix(name, ".mp4") {
		return maxDerivedClipBytes
	}
	return maxDerivedImageBytes
}

func validateStallAsset(asset DerivedArtifact, caseID string) bool {
	limit := stallAssetLimit(asset.Name)
	if limit == 0 || !strings.HasPrefix(asset.Name, "stalls/"+caseID+"/") || asset.Bytes <= 0 || uint64(asset.Bytes) > limit || len(asset.SHA256) != 64 {
		return false
	}
	if _, err := hex.DecodeString(asset.SHA256); err != nil {
		return false
	}
	if strings.HasSuffix(asset.Name, ".mp4") {
		if asset.Kind != "clip" {
			return false
		}
	} else if asset.Kind != "screenshot" {
		return false
	}
	return (asset.MediaTimeMS == nil || *asset.MediaTimeMS >= 0 && *asset.MediaTimeMS <= 86_400_000) && (asset.DurationMS == nil || *asset.DurationMS > 0 && *asset.DurationMS <= 30_000)
}

func validateStallAssetFile(dir string, asset DerivedArtifact) error {
	path := filepath.Join(dir, filepath.FromSlash(asset.Name))
	stat, err := os.Lstat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != asset.Bytes {
		return fmt.Errorf("%w: 卡顿派生产物长度无效", ErrInvalid)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, asset.Bytes+1)); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return fmt.Errorf("%w: 卡顿派生产物未通过 SHA-256 校验", ErrInvalid)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if asset.Kind == "screenshot" {
		config, err := jpeg.DecodeConfig(file)
		if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || config.Width*config.Height > 8_388_608 {
			return fmt.Errorf("%w: 卡顿截图不是有效的限尺寸 JPEG", ErrInvalid)
		}
	} else {
		var header [12]byte
		if _, err := io.ReadFull(file, header[:]); err != nil || string(header[4:8]) != "ftyp" {
			return fmt.Errorf("%w: 卡顿片段不是有效的 MP4", ErrInvalid)
		}
	}
	return nil
}

func sourceArtifactMatches(dir, name string, expected Artifact, limit int64) bool {
	if expected.Bytes < 0 || expected.Bytes > limit {
		return false
	}
	file, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return false
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != expected.Bytes {
		return false
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, limit+1))
	return err == nil && n == expected.Bytes && hex.EncodeToString(hash.Sum(nil)) == expected.SHA256
}

func importedStallEvidence(dir string, report *Report, entries map[string]*zip.File) error {
	artifact := report.Manifest.StallEvidence
	if artifact == nil {
		for name := range entries {
			if name == investigationName || strings.HasPrefix(name, "stalls/") {
				return fmt.Errorf("%w: 卡顿补查产物未在清单中声明", ErrInvalid)
			}
		}
		return nil
	}
	if entries[investigationName] == nil || report.Player == nil || artifact.Bytes < 0 || artifact.Bytes > maxInvestigationBytes {
		return fmt.Errorf("%w: 卡顿补查清单不完整", ErrInvalid)
	}
	if err := extractArtifact(dir, entries[investigationName], *artifact); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, investigationName))
	if err != nil {
		return err
	}
	var ledger investigationLedger
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&ledger) != nil || decoder.Decode(new(any)) != io.EOF || ledger.Version != 1 || len(ledger.Items) == 0 || len(ledger.Items) > maxStallInvestigations {
		return fmt.Errorf("%w: 卡顿补查记录无效", ErrInvalid)
	}
	seenIDs, seenStalls, seenAssets := map[string]bool{}, map[int]bool{}, map[string]bool{}
	for _, item := range ledger.Items {
		if item.ID < "S001" || item.ID > "S008" || len(item.ID) != 4 || seenIDs[item.ID] || seenStalls[item.StallIndex] ||
			item.StallIndex < 1 || item.StallIndex > len(report.Player.Stalls) || item.PlayerRevision != report.Player.Revision || item.Stall != report.Player.Stalls[item.StallIndex-1] ||
			item.CreatedAt.IsZero() || item.UpdatedAt.Before(item.CreatedAt) || len(item.ModelError) > 1024 || len(item.Media.Reason) > 1024 || len(item.Media.Assets) > 4 {
			return fmt.Errorf("%w: 卡顿补查范围或版本无效", ErrInvalid)
		}
		seenIDs[item.ID], seenStalls[item.StallIndex] = true, true
		switch item.Status {
		case "queued", "running", "complete", "partial", "failed", "canceled":
		default:
			return ErrInvalid
		}
		switch item.ModelState {
		case "pending", "complete", "unconfigured", "failed", "changed":
		default:
			return ErrInvalid
		}
		switch item.Media.Status {
		case "ready", "partial", "unavailable", "failed":
		default:
			return ErrInvalid
		}
		switch item.Media.Alignment {
		case "", "player_clock_estimate", "arrival_estimate", "independent_capture_reference":
		default:
			return ErrInvalid
		}
		if len(item.Correlation.Findings) > 0 {
			expected, err := correlateStall(dir, *report, item.ID, item.StallIndex)
			if err != nil {
				return err
			}
			before, _ := json.Marshal(expected)
			after, _ := json.Marshal(item.Correlation)
			if !bytes.Equal(before, after) {
				return fmt.Errorf("%w: 卡顿关联证据与原始采样不一致", ErrInvalid)
			}
		} else if item.Status == "complete" || item.Status == "partial" || len(item.Media.Assets) > 0 || !emptyStallCorrelation(item.Correlation) || item.ModelState == "complete" {
			return fmt.Errorf("%w: 卡顿补查缺少关联证据", ErrInvalid)
		}
		if item.Explanation != nil && (!validExplanation(*item.Explanation, item.Correlation.Findings) || len(item.Explanation.Provider) > 128 || len(item.Explanation.Model) > 256) {
			return fmt.Errorf("%w: 卡顿模型建议的证据引用无效", ErrInvalid)
		}
		if item.ModelState == "complete" && item.Explanation == nil {
			return ErrInvalid
		}
		for _, asset := range item.Media.Assets {
			if !validateStallAsset(asset, item.ID) || seenAssets[asset.Name] || entries[asset.Name] == nil {
				return fmt.Errorf("%w: 卡顿派生产物清单无效", ErrInvalid)
			}
			seenAssets[asset.Name] = true
			if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(asset.Name))), 0o700); err != nil {
				return err
			}
			if err := extractArtifact(dir, entries[asset.Name], asset.Artifact); err != nil {
				return err
			}
			if err := validateStallAssetFile(dir, asset); err != nil {
				return err
			}
		}
	}
	for name := range entries {
		if strings.HasPrefix(name, "stalls/") && !seenAssets[name] {
			return fmt.Errorf("%w: 卡顿派生产物未被记录引用", ErrInvalid)
		}
	}
	report.StallInvestigations = ledger.Items
	for i := range report.StallInvestigations {
		item := &report.StallInvestigations[i]
		if item.Status == "queued" || item.Status == "running" {
			item.Status = "canceled"
			item.ModelState = "failed"
			item.ModelError = "导入时补查尚未完成，可重新补查或解读"
		}
	}
	return persistStallReport(dir, report)
}

func emptyStallCorrelation(value StallCorrelation) bool {
	return value.Source == "" && value.WindowStartMS == 0 && value.WindowEndMS == 0 && len(value.BufferSamples) == 0 && len(value.Gaps) == 0 &&
		len(value.Keyframes) == 0 && len(value.Requests) == 0 && len(value.Findings) == 0 && len(value.Notes) == 0
}

func loadStallImages(dir string, report Report) map[string][]byte {
	result := make(map[string][]byte)
	remaining := 8 << 20
	for _, item := range report.StallInvestigations {
		for _, asset := range item.Media.Assets {
			if asset.Kind != "screenshot" || !validateStallAsset(asset, item.ID) || asset.Bytes > int64(remaining) {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(asset.Name)))
			if err != nil || int64(len(data)) != asset.Bytes {
				continue
			}
			hash := sha256.Sum256(data)
			if hex.EncodeToString(hash[:]) != asset.SHA256 {
				continue
			}
			result[asset.Name] = data
			remaining -= len(data)
		}
	}
	return result
}

func (s *Service) StallImages(runID string, report Report) map[string][]byte {
	return loadStallImages(s.runDir(runID), report)
}
