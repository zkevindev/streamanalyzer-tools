package diagnostic

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type ShareRequest struct {
	CaseIDs       []string `json:"case_ids,omitempty"`
	IncludeStream bool     `json:"include_stream,omitempty"`
}

type ShareArtifact struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type ShareManifest struct {
	Version       string          `json:"version"`
	CreatedAt     time.Time       `json:"created_at"`
	RunID         string          `json:"run_id"`
	Protocol      string          `json:"protocol"`
	Origin        string          `json:"origin,omitempty"`
	IncludeStream bool            `json:"include_stream"`
	CaseIDs       []string        `json:"case_ids"`
	OfflineReplay bool            `json:"offline_replay"`
	Artifacts     []ShareArtifact `json:"artifacts"`
	PrivacyNotice string          `json:"privacy_notice"`
}

func (s *Service) WriteShareBundle(id string, req ShareRequest, output io.Writer) error {
	if len(req.CaseIDs) > maxStallInvestigations {
		return fmt.Errorf("%w: 分享案例最多 %d 个", ErrInvalid, maxStallInvestigations)
	}
	s.persistence.RLock()
	defer s.persistence.RUnlock()
	detail, err := s.detail(id)
	if err != nil {
		return err
	}
	if detail.Report == nil || detail.Status == "running" {
		return ErrPending
	}
	report := *detail.Report
	selected := make(map[string]bool, len(req.CaseIDs))
	for _, caseID := range req.CaseIDs {
		if selected[caseID] {
			return fmt.Errorf("%w: 分享案例重复", ErrInvalid)
		}
		selected[caseID] = true
	}
	var investigations []StallInvestigation
	for _, item := range report.StallInvestigations {
		if !selected[item.ID] {
			continue
		}
		investigations = append(investigations, item)
		delete(selected, item.ID)
	}
	if len(selected) > 0 {
		return fmt.Errorf("%w: 分享案例不存在", ErrInvalid)
	}
	report.StallInvestigations = investigations
	report.Manifest.Timeline = Artifact{}
	report.Manifest.Player = nil
	report.Manifest.StallEvidence = nil
	report.Manifest.PlayerNetwork = nil
	report.Manifest.AutomaticInvestigation = nil
	report.AutomaticInvestigation = nil
	if !req.IncludeStream {
		report.Manifest.Stream = Artifact{}
	}
	dir := s.runDir(id)
	images := s.StallImages(id, report)
	var html bytes.Buffer
	if err := RenderHTMLWithAssets(&html, report, nil, "", images); err != nil {
		return err
	}
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	manifest := ShareManifest{Version: "share-bundle/1", CreatedAt: time.Now().UTC(), RunID: id, Protocol: report.Manifest.Protocol,
		Origin: report.Manifest.Observation.Origin, IncludeStream: req.IncludeStream, CaseIDs: append([]string{}, req.CaseIDs...), OfflineReplay: false,
		Artifacts: []ShareArtifact{}, PrivacyNotice: "此文件仅用于外发查看，不能作为可重算证据包导入；报告、截图和媒体内容外发前仍需人工核对。"}
	archive := zip.NewWriter(output)
	writeBytes := func(name, kind string, data []byte) error {
		entry, err := archive.Create(name)
		if err != nil {
			return err
		}
		if _, err := entry.Write(data); err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		manifest.Artifacts = append(manifest.Artifacts, ShareArtifact{Name: name, Kind: kind, Bytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:])})
		return nil
	}
	if err := writeBytes(reportName, "report", reportJSON); err != nil {
		return err
	}
	if err := writeBytes(htmlName, "html", html.Bytes()); err != nil {
		return err
	}
	for _, item := range investigations {
		for _, asset := range item.Media.Assets {
			if err := writeShareFile(archive, &manifest, dir, asset.Name, asset.Kind, asset.Artifact); err != nil {
				return err
			}
		}
	}
	if req.IncludeStream {
		name := dumpName(report.Manifest.Protocol)
		if name == "" || report.Manifest.Stream.Bytes == 0 {
			return fmt.Errorf("%w: 当前报告没有可分享的原始流", ErrInvalid)
		}
		if err := writeShareFile(archive, &manifest, dir, name, "raw_stream", report.Manifest.Stream); err != nil {
			return err
		}
	}
	entry, err := archive.Create("share-manifest.json")
	if err != nil {
		return err
	}
	if err := json.NewEncoder(entry).Encode(manifest); err != nil {
		return err
	}
	return archive.Close()
}

func writeShareFile(archive *zip.Writer, manifest *ShareManifest, dir, name, kind string, expected Artifact) error {
	if filepath.Base(name) == name || filepath.IsAbs(name) || name == "" {
		if name != streamName && name != rtmpStreamName && name != hlsStreamName && name != tsStreamName && name != udpStreamName && name != rtpStreamName && name != rtspStreamName {
			return fmt.Errorf("%w: 分享文件名无效", ErrInvalid)
		}
	}
	if stallAssetLimit(name) == 0 && kind != "raw_stream" {
		return fmt.Errorf("%w: 分享文件路径无效", ErrInvalid)
	}
	file, err := os.Open(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	defer file.Close()
	entry, err := archive.Create(name)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(entry, hash), file)
	if err != nil {
		return err
	}
	actual := Artifact{Bytes: written, SHA256: hex.EncodeToString(hash.Sum(nil))}
	if actual != expected {
		return fmt.Errorf("%w: 分享文件未通过 SHA-256 校验", ErrInvalid)
	}
	manifest.Artifacts = append(manifest.Artifacts, ShareArtifact{Name: name, Kind: kind, Bytes: written, SHA256: actual.SHA256})
	return nil
}
