package gb28181

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

const captureLimit = 4 << 20
const sessionJSONLimit = 2 << 20
const MaxEvidenceBytes = captureLimit + sessionJSONLimit + (128 << 10)

var ErrSessionNotFound = errors.New("未找到点播会话")

type SessionMark struct {
	Stage    string  `json:"stage"`
	OffsetMS float64 `json:"offset_ms"`
	Detail   string  `json:"detail,omitempty"`
}

type SessionReport struct {
	ID               string         `json:"id"`
	CallID           string         `json:"call_id"`
	Start            time.Time      `json:"start"`
	Mode             string         `json:"mode,omitempty"`
	Transport        mediaTransport `json:"transport,omitempty"`
	Source           SourceInfo     `json:"source"`
	Timeline         []SessionMark  `json:"timeline"`
	Faults           *FaultConfig   `json:"faults,omitempty"`
	FaultStats       *FaultStats    `json:"fault_stats,omitempty"`
	CaptureEnabled   bool           `json:"capture_enabled"`
	CaptureBytes     int            `json:"capture_bytes,omitempty"`
	CaptureTruncated bool           `json:"capture_truncated,omitempty"`
}

type sessionEvidence struct {
	SessionReport
	signal  []Event
	packets []byte
}

func (d *device) newEvidence(callID string, transport mediaTransport) *sessionEvidence {
	var id [16]byte
	rand.Read(id[:])
	ev := &sessionEvidence{SessionReport: SessionReport{
		ID: hex.EncodeToString(id[:]), CallID: truncate(callID, 240), Start: time.Now(),
		Transport: transport, Source: d.status.MediaSource, CaptureEnabled: d.cfg.CaptureRTP, Faults: d.cfg.Faults,
	}}
	for i := len(d.status.Events) - 1; i >= 0; i-- {
		event := d.status.Events[i]
		if event.CallID == ev.CallID && event.Direction == "in" && strings.HasPrefix(event.Summary, "INVITE ") {
			ev.Start = event.Time
			ev.addSignal(event)
			break
		}
	}
	ev.mark("invite", "", ev.Start, true)
	if len(d.evidence) == 6 {
		d.evidence = slices.Delete(d.evidence, 0, 1)
	}
	d.evidence = append(d.evidence, ev)
	return ev
}

func (e *sessionEvidence) mark(stage, detail string, now time.Time, once bool) {
	if once && slices.ContainsFunc(e.Timeline, func(m SessionMark) bool { return m.Stage == stage }) {
		return
	}
	if len(e.Timeline) < 80 {
		e.Timeline = append(e.Timeline, SessionMark{stage, max(0, float64(now.Sub(e.Start))/float64(time.Millisecond)), detail})
	}
}

func (e *sessionEvidence) addSignal(event Event) {
	if len(e.signal) < 64 {
		e.signal = append(e.signal, event)
	}
	switch {
	case event.Direction == "out" && event.method == "INVITE" && strings.HasPrefix(event.Summary, "SIP/2.0 200 "):
		e.mark("invite_ok", "", event.Time, true)
	case event.Direction == "in" && strings.HasPrefix(event.Summary, "ACK "):
		e.mark("ack", "", event.Time, true)
	case event.Direction == "in" && strings.HasPrefix(event.Summary, "BYE "):
		e.mark("bye", "", event.Time, true)
	}
}

func (e *sessionEvidence) capture(packet []byte, first bool) {
	if !first && (!e.CaptureEnabled || e.CaptureTruncated) {
		return
	}
	now := time.Now()
	if first {
		e.mark("first_rtp", "", now, true)
	}
	if !e.CaptureEnabled || e.CaptureTruncated {
		return
	}
	if len(e.packets)+10+len(packet) > captureLimit {
		e.CaptureTruncated = true
		return
	}
	e.packets = binary.BigEndian.AppendUint64(e.packets, uint64(max(0, now.Sub(e.Start))))
	e.packets = binary.BigEndian.AppendUint16(e.packets, uint16(len(packet)))
	e.packets = append(e.packets, packet...)
	e.CaptureBytes = len(e.packets)
}

func (m *Manager) SessionArchive(id string, dest io.Writer) error {
	m.mu.Lock()
	d := m.device
	m.mu.Unlock()
	if d == nil {
		return ErrSessionNotFound
	}
	d.mu.Lock()
	var data sessionEvidence
	for _, e := range d.evidence {
		if e.ID == id {
			data = *e
			data.Timeline = slices.Clone(e.Timeline)
			data.signal = slices.Clone(e.signal)
			data.packets = bytes.Clone(e.packets)
			break
		}
	}
	d.mu.Unlock()
	if data.ID == "" {
		return ErrSessionNotFound
	}
	report, err := json.MarshalIndent(struct {
		SessionReport
		Signaling []Event `json:"signaling"`
	}{data.SessionReport, data.signal}, "", "  ")
	if err != nil {
		return err
	}
	files := map[string][]byte{"session.json": report}
	if len(data.packets) > 0 {
		files["rtp.bin"] = data.packets
	}
	manifest := evidenceManifest{Version: 1, SHA256: make(map[string]string, len(files))}
	for name, content := range files {
		digest := sha256.Sum256(content)
		manifest.SHA256[name] = hex.EncodeToString(digest[:])
	}
	files["manifest.json"], err = json.Marshal(manifest)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(dest)
	for _, name := range []string{"manifest.json", "session.json", "rtp.bin"} {
		if content, ok := files[name]; ok {
			entry, err := archive.Create(name)
			if err != nil {
				return err
			}
			if _, err := entry.Write(content); err != nil {
				return err
			}
		}
	}
	return archive.Close()
}

type evidenceManifest struct {
	Version int               `json:"version"`
	SHA256  map[string]string `json:"sha256"`
}

type EvidenceReview struct {
	Signaling []Event       `json:"signaling,omitempty"`
	Session   SessionReport `json:"session"`
	RTP       *RTPReview    `json:"rtp,omitempty"`
}

func ImportEvidence(r io.Reader) (EvidenceReview, error) {
	var result EvidenceReview
	payload, err := io.ReadAll(io.LimitReader(r, MaxEvidenceBytes+1))
	if err != nil || len(payload) > MaxEvidenceBytes {
		return result, errors.New("证据包读取失败或超过大小上限")
	}
	z, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return result, errors.New("证据包不是有效的 ZIP 文件")
	}
	files := make(map[string][]byte)
	for _, entry := range z.File {
		var limit int
		switch entry.Name {
		case "manifest.json":
			limit = 4096
		case "session.json":
			limit = sessionJSONLimit
		case "rtp.bin":
			limit = captureLimit
		default:
			return result, errors.New("证据包含未知文件")
		}
		if _, exists := files[entry.Name]; exists || entry.UncompressedSize64 > uint64(limit) {
			return result, errors.New("证据包包含重复文件或文件过大")
		}
		source, err := entry.Open()
		if err != nil {
			return result, err
		}
		content, err := io.ReadAll(io.LimitReader(source, int64(limit)+1))
		source.Close()
		if err != nil || len(content) > limit {
			return result, errors.New("证据文件读取失败或过大")
		}
		files[entry.Name] = content
	}
	var manifest evidenceManifest
	if json.Unmarshal(files["manifest.json"], &manifest) != nil || manifest.Version != 1 ||
		len(manifest.SHA256) != len(files)-1 || len(files["session.json"]) == 0 {
		return result, errors.New("GB28181 证据清单无效")
	}
	for name, content := range files {
		if name == "manifest.json" {
			continue
		}
		sum := sha256.Sum256(content)
		if manifest.SHA256[name] != hex.EncodeToString(sum[:]) {
			return result, fmt.Errorf("%s 的 SHA-256 校验失败", name)
		}
	}
	var record struct {
		SessionReport
		Signaling []Event `json:"signaling"`
	}
	if err := json.Unmarshal(files["session.json"], &record); err != nil || record.ID == "" {
		return result, errors.New("会话报告无效")
	}
	result.Session, result.Signaling = record.SessionReport, record.Signaling
	rtp := files["rtp.bin"]
	if len(rtp) != result.Session.CaptureBytes {
		return result, errors.New("RTP 采样长度与报告不一致")
	}
	if len(rtp) > 0 {
		report, err := reviewRTP(rtp)
		if err != nil {
			return result, err
		}
		result.RTP = &report
	}
	return result, nil
}
