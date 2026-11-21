package offline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
	"streamanalyzer/internal/mp4parse"
)

const maxInspectFrameBytes = 4 * 1024 * 1024 // 4 MiB safety cap per frame read

// FrameInspectResponse wraps frame metadata and deep codec inspection output.
type FrameInspectResponse struct {
	TaskID     string                    `json:"task_id"`
	FlowIndex  int                       `json:"flow_index"`
	FrameIndex int                       `json:"frame_index"`
	PESIndex   *int                      `json:"pes_index,omitempty"`
	Frame      models.OfflineFrameDetail `json:"frame"`
	Inspection codec.FrameInspection     `json:"inspection"`
}

// InspectFrame reads a single frame on demand from its recorded byte offset and
// runs deep NALU / SPS·PPS·VPS·SEI / audio-header / Hex inspection.
func (m *Manager) InspectFrame(taskID string, flowIdx, frameIdx, hexOffset, hexLimit int) (*FrameInspectResponse, error) {
	task, summary, err := m.loadTaskAndSummary(taskID)
	if err != nil {
		return nil, err
	}
	if flowIdx < 0 || flowIdx >= len(summary.Flows) {
		return nil, fmt.Errorf("flow index %d out of range (flows=%d)", flowIdx, len(summary.Flows))
	}
	flow := &summary.Flows[flowIdx]
	if frameIdx < 0 || frameIdx >= len(flow.FrameDetails) {
		return nil, fmt.Errorf("frame index %d out of range (frames=%d)", frameIdx, len(flow.FrameDetails))
	}
	fd := flow.FrameDetails[frameIdx]
	if fd.StreamSource == "" || fd.StreamLen <= 0 {
		return nil, fmt.Errorf("该帧未记录字节偏移信息（如为历史任务请点击「重新解析」）")
	}

	targetPath, err := resolveStreamSourcePath(task, flow, fd.StreamSource)
	if err != nil {
		return nil, err
	}
	buf, truncatedWarn, err := readFrameBytes(targetPath, fd.StreamOffset, fd.StreamLen)
	if err != nil {
		return nil, err
	}

	codecName := ""
	if fd.MediaType == "video" {
		codecName = flow.VideoCodec
	} else if fd.MediaType == "audio" {
		codecName = flow.AudioCodec
	}
	insp := codec.InspectFrame(buf, codec.InspectOptions{
		MediaType:  fd.MediaType,
		Codec:      codecName,
		LengthSize: fd.NALULenSize,
		HexOffset:  hexOffset,
		HexLimit:   hexLimit,
	})
	if truncatedWarn != "" {
		insp.Warnings = append([]string{truncatedWarn}, insp.Warnings...)
	}

	return &FrameInspectResponse{
		TaskID:     taskID,
		FlowIndex:  flowIdx,
		FrameIndex: frameIdx,
		Frame:      fd,
		Inspection: insp,
	}, nil
}

// InspectPES reads a single TS PES packet on demand from its PID elementary stream file.
func (m *Manager) InspectPES(taskID string, flowIdx, pesIdx, hexOffset, hexLimit int) (*FrameInspectResponse, error) {
	task, summary, err := m.loadTaskAndSummary(taskID)
	if err != nil {
		return nil, err
	}
	if flowIdx < 0 || flowIdx >= len(summary.Flows) {
		return nil, fmt.Errorf("flow index %d out of range (flows=%d)", flowIdx, len(summary.Flows))
	}
	flow := &summary.Flows[flowIdx]
	if pesIdx < 0 || pesIdx >= len(flow.PESDetails) {
		return nil, fmt.Errorf("pes index %d out of range (pes=%d)", pesIdx, len(flow.PESDetails))
	}
	pes := flow.PESDetails[pesIdx]
	if pes.StreamLen <= 0 {
		return nil, fmt.Errorf("该 PES 包未记录字节偏移信息（请点击「重新解析」）")
	}

	src := fmt.Sprintf("pid:%d", pes.PID)
	targetPath, err := resolveStreamSourcePath(task, flow, src)
	if err != nil {
		return nil, err
	}
	buf, truncatedWarn, err := readFrameBytes(targetPath, pes.StreamOffset, pes.StreamLen)
	if err != nil {
		return nil, err
	}

	codecName := ""
	for _, p := range flow.PIDDetails {
		if p.PID == pes.PID {
			codecName = p.Codec
			break
		}
	}
	if codecName == "" {
		if pes.Category == "video" {
			codecName = flow.VideoCodec
		} else if pes.Category == "audio" {
			codecName = flow.AudioCodec
		}
	}

	insp := codec.InspectFrame(buf, codec.InspectOptions{
		MediaType: pes.Category,
		Codec:     codecName,
		HexOffset: hexOffset,
		HexLimit:  hexLimit,
	})
	if truncatedWarn != "" {
		insp.Warnings = append([]string{truncatedWarn}, insp.Warnings...)
	}

	dtsMs := int64(0)
	ptsMs := int64(0)
	if pes.DTSValid {
		dtsMs = ptsToMs(pes.DTSBase)
	} else if pes.PTSValid {
		dtsMs = ptsToMs(pes.PTSBase)
	}
	if pes.PTSValid {
		ptsMs = ptsToMs(pes.PTSBase)
	} else {
		ptsMs = dtsMs
	}
	ft := "-"
	if pes.Category == "video" {
		ft = "P"
		for _, n := range pes.NALUs {
			if n.Key {
				ft = "I"
				break
			}
		}
	}
	frameIdx := -1
	if pes.FrameIndex != nil {
		frameIdx = *pes.FrameIndex
	}
	pIdx := pesIdx
	return &FrameInspectResponse{
		TaskID:     taskID,
		FlowIndex:  flowIdx,
		FrameIndex: frameIdx,
		PESIndex:   &pIdx,
		Frame: models.OfflineFrameDetail{
			MediaType:    pes.Category,
			DTS:          dtsMs,
			PTS:          ptsMs,
			CTS:          ptsMs - dtsMs,
			FrameLen:     pes.PayloadLen,
			FrameType:    ft,
			StreamSource: src,
			StreamOffset: pes.StreamOffset,
			StreamLen:    pes.StreamLen,
		},
		Inspection: insp,
	}, nil
}

// InspectMP4Boxes returns the interactive MP4 box tree for an MP4 task.
func (m *Manager) InspectMP4Boxes(taskID string) (*mp4parse.MP4BoxTree, error) {
	if !isSafeTaskID(taskID) {
		return nil, fmt.Errorf("invalid task id")
	}
	task, err := m.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	if task.Mode != models.OfflineModeMP4 {
		return nil, fmt.Errorf("当前任务模式为 %s，仅 MP4 任务支持 Box 树解析", task.Mode)
	}
	if task.InputPath == "" {
		return nil, fmt.Errorf("输入文件路径缺失")
	}
	if _, err := os.Stat(task.InputPath); err != nil {
		return nil, fmt.Errorf("输入文件不存在: %w", err)
	}
	return mp4parse.ExtractBoxTree(task.InputPath, mp4parse.DefaultMaxBoxNodes)
}

func (m *Manager) loadTaskAndSummary(taskID string) (*models.OfflineTask, *models.OfflineSummary, error) {
	if !isSafeTaskID(taskID) {
		return nil, nil, fmt.Errorf("invalid task id")
	}
	task, err := m.GetTask(taskID)
	if err != nil {
		return nil, nil, err
	}
	if task.SummaryPath == "" {
		return nil, nil, fmt.Errorf("任务尚未生成解析结果")
	}
	b, err := os.ReadFile(task.SummaryPath)
	if err != nil {
		return nil, nil, fmt.Errorf("读取 summary 失败: %w", err)
	}
	var summary models.OfflineSummary
	if err := json.Unmarshal(b, &summary); err != nil {
		return nil, nil, fmt.Errorf("解析 summary 失败: %w", err)
	}
	return task, &summary, nil
}

func resolveStreamSourcePath(task *models.OfflineTask, flow *models.OfflineFlowResult, source string) (string, error) {
	switch source {
	case "video":
		if flow.VideoPath == "" {
			return "", fmt.Errorf("视频裸流产物文件不存在")
		}
		return flow.VideoPath, nil
	case "audio":
		if flow.AudioPath == "" {
			return "", fmt.Errorf("音频裸流产物文件不存在")
		}
		return flow.AudioPath, nil
	case "input":
		if task.InputPath != "" {
			return task.InputPath, nil
		}
		if flow.RawPath != "" {
			return flow.RawPath, nil
		}
		return "", fmt.Errorf("原始输入文件不存在")
	default:
		if strings.HasPrefix(source, "pid:") {
			pidNum, err := strconv.Atoi(strings.TrimPrefix(source, "pid:"))
			if err == nil {
				for _, p := range flow.PIDDetails {
					if int(p.PID) == pidNum && p.OutputPath != "" {
						return p.OutputPath, nil
					}
				}
			}
		}
		return "", fmt.Errorf("未知的帧数据源: %s", source)
	}
}

func readFrameBytes(path string, offset int64, length int) ([]byte, string, error) {
	if offset < 0 {
		return nil, "", fmt.Errorf("invalid stream offset: %d", offset)
	}
	readLen := length
	warn := ""
	if readLen > maxInspectFrameBytes {
		readLen = maxInspectFrameBytes
		warn = fmt.Sprintf("单帧长度 %d B 超过深钻上限 %d B，已截断显示前 %d B", length, maxInspectFrameBytes, maxInspectFrameBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("打开码流文件失败: %w", err)
	}
	defer f.Close()

	buf := make([]byte, readLen)
	n, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, "", fmt.Errorf("读取帧字节失败: %w", err)
	}
	if n == 0 {
		return nil, "", fmt.Errorf("指定偏移 %d 处无可读取字节", offset)
	}
	if n < readLen {
		warn = fmt.Sprintf("文件实际仅可读 %d/%d B（可能存在截断）", n, readLen)
	}
	return buf[:n], warn, nil
}

// progressReader wraps an io.Reader to report bytes processed and check cancellation.
type progressReader struct {
	r        io.Reader
	ctx      context.Context
	total    int64
	read     int64
	lastTick time.Time
	onUpdate func(read int64)
}

func newProgressReader(ctx context.Context, r io.Reader, total int64, onUpdate func(read int64)) *progressReader {
	return &progressReader{
		r:        r,
		ctx:      ctx,
		total:    total,
		onUpdate: onUpdate,
	}
}

func (p *progressReader) Read(b []byte) (int, error) {
	if p.ctx != nil && p.ctx.Err() != nil {
		return 0, p.ctx.Err()
	}
	n, err := p.r.Read(b)
	if n > 0 {
		p.read += int64(n)
		now := time.Now()
		if p.onUpdate != nil && (p.lastTick.IsZero() || now.Sub(p.lastTick) >= 120*time.Millisecond) {
			p.lastTick = now
			p.onUpdate(p.read)
		}
	}
	return n, err
}

func hashNALSlices(groups ...[][]byte) string {
	h := sha256.New()
	wrote := false
	for _, g := range groups {
		for _, nal := range g {
			if len(nal) == 0 {
				continue
			}
			_, _ = h.Write(nal)
			_, _ = h.Write([]byte{0xff})
			wrote = true
		}
	}
	if !wrote {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}
