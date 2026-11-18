package diagnostic

import (
	"archive/zip"
	"bufio"
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
	name := dumpName(report.Manifest.Protocol)
	if name == "" {
		return fmt.Errorf("%w: 不支持的采样协议", ErrInvalid)
	}
	archive := zip.NewWriter(w)
	names := []string{manifestName, name, timelineName, reportName}
	if report.Manifest.Player != nil {
		names = append(names, playerName)
	}
	for _, filename := range names {
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: filename, Method: zip.Store})
		if err != nil {
			return err
		}
		switch filename {
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
	if err := RenderHTML(entry, report, nil, ""); err != nil {
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
	for _, other := range []string{streamName, rtmpStreamName, hlsStreamName, tsStreamName, udpStreamName, rtpStreamName} {
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
	default:
		return ""
	}
}

func bundleEntries(files []*zip.File) (map[string]*zip.File, error) {
	limits := map[string]uint64{
		manifestName: 16 << 10, timelineName: maxTimeline,
		streamName: maxStreamBytes, rtmpStreamName: maxStreamBytes, hlsStreamName: maxStreamBytes,
		tsStreamName: maxStreamBytes, udpStreamName: maxStreamBytes, rtpStreamName: maxStreamBytes,
		reportName: 1 << 20, htmlName: 1 << 20,
		playerName: maxPlayerBytes,
	}
	entries := make(map[string]*zip.File, len(limits))
	for _, file := range files {
		limit, allowed := limits[file.Name]
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
	data, err := io.ReadAll(io.LimitReader(reader, 16<<10+1))
	if err != nil || len(data) > 16<<10 || json.Unmarshal(data, &manifest) != nil {
		return manifest, fmt.Errorf("%w: 清单无效", ErrInvalid)
	}
	legacyFLV := manifest.Protocol == ProtocolHTTPFLV && manifest.Version == legacyFLVSchemaVersion
	if dumpName(manifest.Protocol) == "" || (manifest.Version != multiProtocolSchemaVersion && manifest.Version != playerSchemaVersion && !legacyFLV) ||
		(manifest.Player != nil && manifest.Version != playerSchemaVersion) {
		return manifest, fmt.Errorf("%w: 证据包版本或协议不受支持", ErrInvalid)
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
