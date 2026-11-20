package diagnostic

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

const maxManifestBytes = 256 << 10

type artifactWriter struct {
	file   *os.File
	buffer *bufio.Writer
	hash   hash.Hash
	bytes  int64
	err    error
}

func newArtifact(dir, name string) (*artifactWriter, error) {
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	return &artifactWriter{file: file, buffer: bufio.NewWriterSize(file, 64<<10), hash: sha256.New()}, nil
}

func (a *artifactWriter) Write(p []byte) (int, error) {
	n, err := a.buffer.Write(p)
	a.bytes += int64(n)
	_, _ = a.hash.Write(p[:n])
	a.err = err
	return n, err
}

func (a *artifactWriter) close() error {
	return errors.Join(a.buffer.Flush(), a.file.Close())
}

func (a *artifactWriter) artifact() Artifact {
	return Artifact{Bytes: a.bytes, SHA256: hex.EncodeToString(a.hash.Sum(nil))}
}

func writeBundle(w io.Writer, dir string, report Report) error {
	return writeBundleWithPlayer(w, dir, report, nil)
}

func writeBundleWithPlayer(w io.Writer, dir string, report Report, player io.Reader) error {
	var ledger []byte
	if report.Manifest.StallEvidence != nil {
		var err error
		ledger, err = readStallLedger(dir, *report.Manifest.StallEvidence)
		if err != nil {
			return err
		}
	}
	return writeBundleSnapshot(w, dir, report, player, ledger)
}

func writeBundleSnapshot(w io.Writer, dir string, report Report, player io.Reader, ledger []byte, networkSnapshot ...[]byte) error {
	explanation, err := readReportBundleExplanation(dir, report)
	if err != nil {
		return err
	}
	return writeBundleSnapshotExplanation(w, dir, report, player, ledger, explanation, networkSnapshot...)
}

func writeBundleSnapshotExplanation(w io.Writer, dir string, report Report, player io.Reader, ledger []byte, explanation *Explanation, networkSnapshot ...[]byte) error {
	var playerSnapshot []byte
	if report.Manifest.Player != nil {
		var err error
		if player != nil {
			playerSnapshot, err = io.ReadAll(io.LimitReader(player, maxPlayerBytes+1))
		} else if report.Player != nil {
			playerSnapshot, err = os.ReadFile(filepath.Join(dir, playerVersionName(report.Player.Revision)))
		} else {
			return ErrInvalid
		}
		if err != nil || len(playerSnapshot) > maxPlayerBytes {
			return fmt.Errorf("%w: 播放器快照无效", ErrInvalid)
		}
		player = bytes.NewReader(playerSnapshot)
	}
	var automatic []byte
	if report.Manifest.AutomaticInvestigation != nil {
		var err error
		automatic, err = readAutomaticInvestigation(dir, *report.Manifest.AutomaticInvestigation)
		if err != nil {
			return err
		}
	}
	return writeBundleSnapshotAutomatic(w, dir, report, player, ledger, explanation, automatic, playerSnapshot, networkSnapshot...)
}

func writeBundleSnapshotAutomatic(w io.Writer, dir string, report Report, player io.Reader, ledger []byte, explanation *Explanation, automatic, playerSnapshot []byte, networkSnapshot ...[]byte) error {
	if err := validateAutomaticSnapshot(dir, report, automatic, playerSnapshot); err != nil {
		return err
	}
	if err := validateAutomaticSummary(report, explanation); err != nil {
		return err
	}
	if explanation != nil {
		if err := validateReportBundleExplanation(dir, report, *explanation); err != nil {
			return err
		}
	}
	var network []byte
	if report.Manifest.PlayerNetwork != nil {
		if len(networkSnapshot) > 0 {
			network = networkSnapshot[0]
		} else {
			var err error
			network, err = readPlayerNetworkArtifact(dir, *report.Manifest.PlayerNetwork)
			if err != nil {
				return err
			}
		}
		if len(network) == 0 {
			return ErrInvalid
		}
	}
	name := dumpName(report.Manifest.Protocol)
	if name == "" {
		return fmt.Errorf("%w: 不支持的采样协议", ErrInvalid)
	}
	archive := zip.NewWriter(w)
	names := []string{manifestName, name, timelineName, reportName}
	if report.Manifest.AutomaticInvestigation != nil {
		names = append(names, automaticInvestigationName)
	}
	if explanation != nil {
		names = append(names, explanationName)
	}
	if report.Manifest.Player != nil {
		names = append(names, playerName)
	}
	if report.Manifest.PlayerNetwork != nil {
		names = append(names, playerNetworkName)
	}
	if report.Manifest.StallEvidence != nil {
		names = append(names, investigationName)
		for _, item := range report.StallInvestigations {
			for _, asset := range item.Media.Assets {
				names = append(names, asset.Name)
			}
		}
	}
	for _, filename := range names {
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: filename, Method: zip.Store})
		if err != nil {
			return err
		}
		switch filename {
		case automaticInvestigationName:
			if _, err := entry.Write(automatic); err != nil {
				return err
			}
		case explanationName:
			if err := json.NewEncoder(entry).Encode(explanation); err != nil {
				return err
			}
		case playerNetworkName:
			if _, err := entry.Write(network); err != nil {
				return err
			}
		case investigationName:
			if len(ledger) == 0 {
				return fmt.Errorf("%w: 卡顿补查记录缺失", ErrInvalid)
			}
			if _, err := entry.Write(ledger); err != nil {
				return err
			}
		case manifestName:
			if err := json.NewEncoder(entry).Encode(report.Manifest); err != nil {
				return err
			}
		case reportName:
			if err := json.NewEncoder(entry).Encode(report); err != nil {
				return err
			}
		case playerName:
			if report.Player == nil {
				return fmt.Errorf("%w: 播放器证据缺少版本", ErrInvalid)
			}
			if player != nil {
				if _, err := io.Copy(entry, player); err != nil {
					return err
				}
			} else {
				if err := copyFile(entry, filepath.Join(dir, playerVersionName(report.Player.Revision))); err != nil {
					return err
				}
			}
		default:
			if err := copyFile(entry, filepath.Join(dir, filename)); err != nil {
				return err
			}
		}
	}
	entry, err := archive.Create(htmlName)
	if err != nil {
		return err
	}
	if err := RenderHTMLWithAssets(entry, report, explanation, "", loadStallImages(dir, report)); err != nil {
		return err
	}
	return archive.Close()
}

func copyFile(w io.Writer, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(w, file)
	return err
}

func readBundle(dir string, source io.Reader) (Report, error) {
	upload, err := os.CreateTemp(dir, "upload-*.zip")
	if err != nil {
		return Report{}, err
	}
	defer os.Remove(upload.Name())
	size, err := io.Copy(upload, io.LimitReader(source, MaxBundleBytes+1))
	if closeErr := upload.Close(); closeErr != nil {
		return Report{}, closeErr
	}
	if err != nil || size > MaxBundleBytes {
		return Report{}, fmt.Errorf("%w: 证据包上传中断或超出大小上限", ErrInvalid)
	}

	archive, file, err := openEvidenceZIP(upload.Name())
	if err != nil {
		return Report{}, fmt.Errorf("%w: 无法读取 ZIP 证据包", ErrInvalid)
	}
	defer file.Close()
	entries, err := bundleEntries(archive.File)
	if err != nil {
		return Report{}, err
	}
	manifest, err := readManifest(entries[manifestName])
	if err != nil {
		return Report{}, err
	}
	name := dumpName(manifest.Protocol)
	if entries[name] == nil {
		return Report{}, fmt.Errorf("%w: 缺少协议对应的原始流", ErrInvalid)
	}
	for _, other := range []string{streamName, rtmpStreamName, hlsStreamName, tsStreamName, udpStreamName, rtpStreamName, rtspStreamName} {
		if other != name && entries[other] != nil {
			return Report{}, fmt.Errorf("%w: 证据包包含其他协议的原始流", ErrInvalid)
		}
	}
	if err := extractArtifact(dir, entries[name], manifest.Stream); err != nil {
		return Report{}, err
	}
	if err := extractArtifact(dir, entries[timelineName], manifest.Timeline); err != nil {
		return Report{}, err
	}
	if manifest.Player != nil {
		if entries[playerName] == nil {
			return Report{}, fmt.Errorf("%w: 缺少播放器证据", ErrInvalid)
		}
		if err := extractArtifact(dir, entries[playerName], *manifest.Player); err != nil {
			return Report{}, err
		}
	} else if entries[playerName] != nil {
		return Report{}, fmt.Errorf("%w: 播放器证据未在清单中声明", ErrInvalid)
	}
	var report Report
	switch manifest.Protocol {
	case ProtocolHTTPFLV:
		events, err := replay(dir, manifest)
		if err != nil {
			return Report{}, err
		}
		report = analyze(manifest, events)
	case ProtocolRTMP:
		report, err = replayRTMP(dir, manifest)
	case ProtocolHLS:
		report, err = replayHLS(dir, manifest)
	case ProtocolHTTPTS, ProtocolUDPTS, ProtocolRTPTS:
		report, err = replayTS(dir, manifest)
	case ProtocolRTSP, ProtocolRTSPS:
		report, err = replayRTSP(dir, manifest)
	default:
		return Report{}, fmt.Errorf("%w: 不支持的采样协议", ErrInvalid)
	}
	if err != nil {
		return Report{}, err
	}
	if manifest.Player != nil {
		if err := attachPlayerArtifact(dir, &report); err != nil {
			return Report{}, err
		}
	}
	if err := importedPlayerNetwork(dir, &report, entries); err != nil {
		return Report{}, err
	}
	if err := importedStallEvidence(dir, &report, entries); err != nil {
		return Report{}, err
	}
	if err := importedAutomaticInvestigation(dir, &report, entries); err != nil {
		return Report{}, err
	}
	if err := importedReportExplanation(dir, report, entries[explanationName]); err != nil {
		return Report{}, err
	}
	if err := validateImportedAutomaticSummary(dir, report); err != nil {
		return Report{}, err
	}
	return report, nil
}

// The evidence archives use short entry names and a bounded number of files.
// Limit directory reads during zip.NewReader, before validating its entries.
func openEvidenceZIP(path string) (*zip.Reader, *os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	source := &evidenceZIPReader{file: file, directoryBytes: 512 << 10, opening: true}
	reader, err := zip.NewReader(source, stat.Size())
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	source.opening = false
	return reader, file, nil
}

type evidenceZIPReader struct {
	file           *os.File
	directoryBytes int
	opening        bool
}

func (r *evidenceZIPReader) ReadAt(p []byte, off int64) (int, error) {
	if r.opening {
		r.directoryBytes -= len(p)
		if r.directoryBytes < 0 {
			return 0, errors.New("ZIP central directory exceeds evidence limit")
		}
	}
	return r.file.ReadAt(p, off)
}

func dumpName(protocol string) string {
	switch protocol {
	case ProtocolHTTPFLV:
		return streamName
	case ProtocolRTMP:
		return rtmpStreamName
	case ProtocolHLS:
		return hlsStreamName
	case ProtocolHTTPTS:
		return tsStreamName
	case ProtocolUDPTS:
		return udpStreamName
	case ProtocolRTPTS:
		return rtpStreamName
	case ProtocolRTSP, ProtocolRTSPS:
		return rtspStreamName
	default:
		return ""
	}
}

func bundleEntries(files []*zip.File) (map[string]*zip.File, error) {
	limits := map[string]uint64{
		manifestName: maxManifestBytes, timelineName: maxTimeline,
		streamName: maxStreamBytes, rtmpStreamName: maxStreamBytes, hlsStreamName: maxStreamBytes,
		tsStreamName: maxStreamBytes, udpStreamName: maxStreamBytes, rtpStreamName: maxStreamBytes, rtspStreamName: maxStreamBytes,
		reportName: 8 << 20, htmlName: 20 << 20, investigationName: maxInvestigationBytes,
		playerName: maxPlayerBytes, playerNetworkName: maxPlayerNetworkBytes,
		explanationName:            maxReportExplanationBytes,
		automaticInvestigationName: maxAutomaticBytes,
	}
	entries := make(map[string]*zip.File, len(limits))
	for _, file := range files {
		limit, allowed := limits[file.Name]
		if assetLimit := stallAssetLimit(file.Name); assetLimit > 0 {
			limit, allowed = assetLimit, true
		}
		if !allowed || !file.Mode().IsRegular() || entries[file.Name] != nil || file.UncompressedSize64 > limit {
			return nil, fmt.Errorf("%w: ZIP 包含未知、重复或超限的条目", ErrInvalid)
		}
		entries[file.Name] = file
	}
	if entries[manifestName] == nil || entries[timelineName] == nil {
		return nil, fmt.Errorf("%w: ZIP 缺少清单或时间线", ErrInvalid)
	}
	return entries, nil
}

func readManifest(entry *zip.File) (Manifest, error) {
	var manifest Manifest
	reader, err := entry.Open()
	if err != nil {
		return manifest, fmt.Errorf("%w: 清单不可读", ErrInvalid)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxManifestBytes+1))
	if err != nil || len(data) > maxManifestBytes || json.Unmarshal(data, &manifest) != nil || !validAutomaticManifestShape(data, manifest) {
		return manifest, fmt.Errorf("%w: 清单无效", ErrInvalid)
	}
	legacyFLV := manifest.Protocol == ProtocolHTTPFLV && manifest.Version == legacyFLVSchemaVersion
	if dumpName(manifest.Protocol) == "" || (manifest.Version != multiProtocolSchemaVersion && manifest.Version != playerSchemaVersion && manifest.Version != investigationSchemaVersion && manifest.Version != playerNetworkSchemaVersion && manifest.Version != rtspPlayerSchemaVersion && !legacyFLV) ||
		((manifest.Protocol == ProtocolRTSP || manifest.Protocol == ProtocolRTSPS) && manifest.Player != nil && manifest.Version != rtspPlayerSchemaVersion) ||
		(manifest.Player != nil && manifest.Version != playerSchemaVersion && manifest.Version != investigationSchemaVersion && manifest.Version != playerNetworkSchemaVersion && manifest.Version != rtspPlayerSchemaVersion) ||
		(manifest.StallEvidence != nil && (manifest.Version < investigationSchemaVersion || manifest.Player == nil)) ||
		(manifest.Version == investigationSchemaVersion && manifest.StallEvidence == nil) ||
		(manifest.PlayerNetwork != nil && (manifest.Version != playerNetworkSchemaVersion || manifest.Protocol != ProtocolHLS || manifest.Player == nil)) ||
		(manifest.Version == playerNetworkSchemaVersion && manifest.PlayerNetwork == nil) ||
		(manifest.Version == rtspPlayerSchemaVersion && (manifest.Protocol != ProtocolRTSP && manifest.Protocol != ProtocolRTSPS || manifest.Player == nil || manifest.PlayerNetwork != nil)) {
		return manifest, fmt.Errorf("%w: 证据包版本或协议不受支持", ErrInvalid)
	}
	if manifest.StreamLimitBytes < 0 || manifest.StreamLimitBytes > MaxCaptureLimitBytes ||
		manifest.StreamLimitBytes > 0 && manifest.Stream.Bytes > manifest.StreamLimitBytes {
		return manifest, fmt.Errorf("%w: 原始流上限记录无效", ErrInvalid)
	}
	obs := manifest.Observation
	if obs.StartedAt.IsZero() || obs.RequestedSeconds < 1 || obs.RequestedSeconds > maxSeconds ||
		obs.ElapsedMS < 0 || obs.ElapsedMS > 3_600_000 || endReasonText(obs.EndReason) == "" ||
		obs.HTTPStatus < 0 || obs.HTTPStatus > 599 || (obs.TTFBMS != nil && (*obs.TTFBMS < 0 || *obs.TTFBMS > obs.ElapsedMS)) {
		return manifest, fmt.Errorf("%w: 采样元数据无效", ErrInvalid)
	}
	if err := validateObservationDetails(obs); err != nil {
		return manifest, err
	}
	target, err := parseProtocolURL(obs.Origin, manifest.Protocol)
	if err != nil || origin(target) != obs.Origin {
		return manifest, fmt.Errorf("%w: 采样来源无效", ErrInvalid)
	}
	return manifest, nil
}

func extractArtifact(dir string, entry *zip.File, expected Artifact) error {
	if expected.Bytes < 0 || uint64(expected.Bytes) != entry.UncompressedSize64 {
		return fmt.Errorf("%w: 产物长度与清单不符", ErrInvalid)
	}
	reader, err := entry.Open()
	if err != nil {
		return fmt.Errorf("%w: ZIP 产物不可读", ErrInvalid)
	}
	defer reader.Close()
	file, err := newArtifact(dir, entry.Name)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, io.LimitReader(reader, expected.Bytes+1))
	if closeErr := errors.Join(file.err, file.close()); closeErr != nil {
		return closeErr
	}
	if err != nil || file.artifact() != expected {
		return fmt.Errorf("%w: 产物未通过 SHA-256 校验", ErrInvalid)
	}
	return nil
}

func replay(dir string, manifest Manifest) ([]Event, error) {
	return readFLVReplayTimeline(context.Background(), dir, manifest)
}

func readFLVReplayTimeline(ctx context.Context, dir string, manifest Manifest) ([]Event, error) {
	stream, err := os.Open(filepath.Join(dir, streamName))
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	timeline, err := os.Open(filepath.Join(dir, timelineName))
	if err != nil {
		return nil, err
	}
	defer timeline.Close()

	decoder := json.NewDecoder(timeline)
	parser, parseErr := openFLV(bufio.NewReaderSize(stream, 64<<10))
	if parser != nil {
		parser.legacyEnhancedCTS = manifest.Version == legacyFLVSchemaVersion
	}
	var events []Event
	lastArrival := int64(0)
	for parseErr == nil && len(events) < maxTags {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var event Event
		event, parseErr = parser.next()
		if parseErr != nil {
			break
		}

		// All tag metadata is recomputed from the dump. Only arrival time is
		// inherited from the original observation.
		var recorded Event
		if err := decoder.Decode(&recorded); err != nil {
			return nil, fmt.Errorf("%w: 时间线缺少原始流中的标签", ErrInvalid)
		}
		event.ArrivalMS = recorded.ArrivalMS
		if manifest.Version == legacyFLVSchemaVersion && recorded.Codec == "" {
			// The first bundle schema omitted the codec name for some legacy tags.
			recorded.Codec = event.Codec
		}
		if event != recorded || event.ArrivalMS < lastArrival || event.ArrivalMS > manifest.Observation.ElapsedMS {
			return nil, fmt.Errorf("%w: 时间线与原始流不一致", ErrInvalid)
		}
		events = append(events, event)
		lastArrival = event.ArrivalMS
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: 时间线有多余或损坏的记录", ErrInvalid)
	}
	return events, nil
}
