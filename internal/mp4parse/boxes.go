package mp4parse

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/abema/go-mp4"
	"github.com/sunfish-shogi/bufseekio"
	"streamanalyzer/internal/codec"
)

const DefaultMaxBoxNodes = 2500

// MP4BoxTree represents the hierarchical ISO BMFF box structure of an MP4 file.
type MP4BoxTree struct {
	FileSize   int64         `json:"file_size"`
	FastStart  bool          `json:"fast_start"`
	Fragmented bool          `json:"fragmented"`
	TotalNodes int           `json:"total_nodes"`
	Truncated  bool          `json:"truncated,omitempty"`
	Boxes      []*MP4BoxNode `json:"boxes"`
}

// MP4BoxNode represents a single MP4 box in the interactive box tree.
type MP4BoxNode struct {
	Type            string        `json:"type"`
	Offset          int64         `json:"offset"`
	Size            int64         `json:"size"`
	HeaderSize      int64         `json:"header_size"`
	Percent         float64       `json:"percent"`
	Summary         string        `json:"summary,omitempty"`
	Highlight       string        `json:"highlight,omitempty"`
	HighlightReason string        `json:"highlight_reason,omitempty"`
	Children        []*MP4BoxNode `json:"children,omitempty"`
}

// ExtractBoxTree recursively parses MP4 boxes and builds a human-readable tree
// with node-count protection.
func ExtractBoxTree(inputPath string, maxNodes int) (*MP4BoxTree, error) {
	if strings.TrimSpace(inputPath) == "" {
		return nil, fmt.Errorf("input path empty")
	}
	if maxNodes <= 0 {
		maxNodes = DefaultMaxBoxNodes
	}
	f, err := os.Open(inputPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := st.Size()

	r := bufseekio.NewReadSeeker(f, 128*1024, 4)

	tree := &MP4BoxTree{
		FileSize: fileSize,
		Boxes:    make([]*MP4BoxNode, 0, 16),
	}

	stack := make([]*MP4BoxNode, 0, 16)
	var firstMoovOff int64 = -1
	var firstMdatOff int64 = -1
	var firstMoovNode *MP4BoxNode

	_, err = mp4.ReadBoxStructure(r, func(h *mp4.ReadHandle) (interface{}, error) {
		if tree.TotalNodes >= maxNodes {
			tree.Truncated = true
			return nil, nil
		}

		depth := len(h.Path)
		if depth <= 0 {
			return nil, nil
		}
		boxType := h.BoxInfo.Type.String()
		size := int64(h.BoxInfo.Size)
		offset := int64(h.BoxInfo.Offset)
		pct := 0.0
		if fileSize > 0 {
			pct = math.Round(float64(size)*10000.0/float64(fileSize)) / 100.0
		}

		node := &MP4BoxNode{
			Type:       boxType,
			Offset:     offset,
			Size:       size,
			HeaderSize: int64(h.BoxInfo.HeaderSize),
			Percent:    pct,
		}

		if depth == 1 {
			if boxType == "moov" && firstMoovOff < 0 {
				firstMoovOff = offset
				firstMoovNode = node
			}
			if boxType == "mdat" && firstMdatOff < 0 {
				firstMdatOff = offset
			}
		}
		if boxType == "moof" || boxType == "mvex" {
			tree.Fragmented = true
		}

		summary, highlight, reason, shouldExpand := summarizeBox(h)
		node.Summary = summary
		node.Highlight = highlight
		node.HighlightReason = reason

		tree.TotalNodes++
		if depth == 1 {
			tree.Boxes = append(tree.Boxes, node)
		} else if depth-2 < len(stack) && stack[depth-2] != nil {
			parent := stack[depth-2]
			parent.Children = append(parent.Children, node)
		} else {
			tree.Boxes = append(tree.Boxes, node)
		}

		if len(stack) < depth {
			stack = append(stack, node)
		} else {
			stack[depth-1] = node
			stack = stack[:depth]
		}

		if !shouldExpand || tree.TotalNodes >= maxNodes {
			return nil, nil
		}
		return h.Expand()
	})
	if err != nil {
		return nil, fmt.Errorf("parse mp4 boxes: %w", err)
	}

	if firstMoovOff >= 0 {
		if firstMdatOff < 0 || firstMoovOff < firstMdatOff {
			tree.FastStart = true
		} else {
			tree.FastStart = false
			if !tree.Fragmented && firstMoovNode != nil {
				firstMoovNode.Highlight = "warn"
				firstMoovNode.HighlightReason = "moov 位于 mdat 之后（非 FastStart），流式播放起播可能变慢"
				if firstMoovNode.Summary != "" {
					firstMoovNode.Summary += " · 非 FastStart"
				} else {
					firstMoovNode.Summary = "位于 mdat 之后（非 FastStart）"
				}
			}
		}
	}

	return tree, nil
}

func summarizeBox(h *mp4.ReadHandle) (summary string, highlight string, reason string, expand bool) {
	name := h.BoxInfo.Type.String()
	switch name {
	case "mdat":
		return fmt.Sprintf("媒体数据载荷 %s", formatBoxBytes(int64(h.BoxInfo.Size-h.BoxInfo.HeaderSize))), "", "", false
	case "free", "skip":
		return fmt.Sprintf("填充空间 %s", formatBoxBytes(int64(h.BoxInfo.Size))), "", "", false
	case "moov":
		return "电影元数据容器", "info", "", true
	case "trak":
		return "媒体轨道容器", "", "", true
	case "edts":
		return "编辑列表容器", "", "", true
	case "mdia":
		return "媒体信息容器", "", "", true
	case "minf":
		return "媒体信息表容器", "", "", true
	case "dinf":
		return "数据信息容器", "", "", true
	case "stbl":
		return "样本表容器 (Sample Table)", "", "", true
	case "mvex":
		return "分片扩展容器 (fMP4)", "info", "", true
	case "moof":
		return "媒体分片头 (Movie Fragment)", "info", "", true
	case "traf":
		return "轨道分片容器 (Track Fragment)", "", "", true
	case "udta":
		return "用户数据容器", "", "", true
	case "ilst", "wave", "sinf", "schi":
		return "", "", "", true
	}

	if !h.BoxInfo.IsSupportedType() {
		return "未定义/自定义载荷", "", "", false
	}

	box, _, err := h.ReadPayload()
	if err != nil {
		return fmt.Sprintf("读取载荷失败: %v", err), "warn", "", false
	}

	switch b := box.(type) {
	case *mp4.Ftyp:
		major := strings.TrimRight(string(b.MajorBrand[:]), "\x00")
		brands := make([]string, 0, len(b.CompatibleBrands))
		for _, cb := range b.CompatibleBrands {
			s := strings.TrimRight(string(cb.CompatibleBrand[:]), "\x00")
			if s != "" {
				brands = append(brands, s)
			}
		}
		return fmt.Sprintf("major=%s, minor=%d, brands=[%s]", major, b.MinorVersion, strings.Join(brands, ", ")), "info", "", false

	case *mp4.Mvhd:
		dur := uint64(b.DurationV0)
		if b.GetVersion() == 1 {
			dur = b.DurationV1
		}
		sec := 0.0
		if b.Timescale > 0 {
			sec = float64(dur) / float64(b.Timescale)
		}
		return fmt.Sprintf("timescale=%d, duration=%.2fs, next_track=%d", b.Timescale, sec, b.NextTrackID), "", "", false

	case *mp4.Tkhd:
		dur := uint64(b.DurationV0)
		if b.GetVersion() == 1 {
			dur = b.DurationV1
		}
		w := b.Width >> 16
		ht := b.Height >> 16
		if w > 0 && ht > 0 {
			return fmt.Sprintf("track_id=%d, %dx%d, duration=%d", b.TrackID, w, ht, dur), "", "", false
		}
		return fmt.Sprintf("track_id=%d, duration=%d", b.TrackID, dur), "", "", false

	case *mp4.Elst:
		extra := ""
		if len(b.Entries) > 0 {
			e0 := b.Entries[0]
			var segDur uint64
			var mediaTime int64
			if b.GetVersion() == 1 {
				segDur = e0.SegmentDurationV1
				mediaTime = e0.MediaTimeV1
			} else {
				segDur = uint64(e0.SegmentDurationV0)
				mediaTime = int64(e0.MediaTimeV0)
			}
			extra = fmt.Sprintf(" (首条 seg_dur=%d, media_time=%d)", segDur, mediaTime)
		}
		return fmt.Sprintf("%d 条编辑列表项%s", len(b.Entries), extra), "warn", "存在 Edit List，播放时间轴可能含首帧偏移或裁剪", false

	case *mp4.Mdhd:
		dur := uint64(b.DurationV0)
		if b.GetVersion() == 1 {
			dur = b.DurationV1
		}
		sec := 0.0
		if b.Timescale > 0 {
			sec = float64(dur) / float64(b.Timescale)
		}
		return fmt.Sprintf("timescale=%d, duration=%.2fs", b.Timescale, sec), "", "", false

	case *mp4.Hdlr:
		ht := strings.TrimRight(string(b.HandlerType[:]), "\x00")
		label := ht
		switch ht {
		case "vide":
			label = "vide (视频轨)"
		case "soun":
			label = "soun (音频轨)"
		case "hint":
			label = "hint"
		case "meta":
			label = "meta"
		}
		if b.Name != "" {
			return fmt.Sprintf("%s · %s", label, b.Name), "", "", false
		}
		return label, "", "", false

	case *mp4.Stsd:
		return fmt.Sprintf("采样描述 %d 项", b.EntryCount), "", "", true

	case *mp4.VisualSampleEntry:
		return fmt.Sprintf("%s %dx%d", name, b.Width, b.Height), "", "", true

	case *mp4.AudioSampleEntry:
		sr := b.SampleRate >> 16
		return fmt.Sprintf("%s %d Hz, %d ch, %d bit", name, sr, b.ChannelCount, b.SampleSize), "", "", true

	case *mp4.AVCDecoderConfiguration:
		return fmt.Sprintf("H.264 %s@%s, nalLen=%d, SPS=%d, PPS=%d",
			codec.H264ProfileName(int(b.Profile)),
			codec.H264LevelName(int(b.Level)),
			int(b.LengthSizeMinusOne)+1,
			len(b.SequenceParameterSets),
			len(b.PictureParameterSets)), "info", "", false

	case *mp4.HvcC:
		return fmt.Sprintf("H.265 profile=%d level=%s, nalLen=%d, arrays=%d",
			b.GeneralProfileIdc,
			codec.H265LevelName(int(b.GeneralLevelIdc)),
			int(b.LengthSizeMinusOne)+1,
			len(b.NaluArrays)), "info", "", false

	case *mp4.Esds:
		if asc := ascFromEsds(b); len(asc) >= 2 {
			if am, err := codec.ParseAACAudioSpecificConfig(asc); err == nil {
				return fmt.Sprintf("AAC ASC: %d Hz, %d ch", am.SampleRate, am.Channels), "info", "", false
			}
		}
		return fmt.Sprintf("descriptors=%d", len(b.Descriptors)), "", "", false

	case *mp4.Stts:
		var total uint64
		for _, e := range b.Entries {
			total += uint64(e.SampleCount)
		}
		return fmt.Sprintf("%d 条时间增量记录，共 %d 样本", len(b.Entries), total), "", "", false

	case *mp4.Ctts:
		return fmt.Sprintf("v%d, %d 条 CTS 偏移项（含 B 帧重排）", b.GetVersion(), len(b.Entries)), "info", "", false

	case *mp4.Stss:
		return fmt.Sprintf("同步关键帧 (IDR) %d 个", len(b.SampleNumber)), "", "", false

	case *mp4.Stsc:
		return fmt.Sprintf("%d 条 Sample-to-Chunk 映射", len(b.Entries)), "", "", false

	case *mp4.Stsz:
		if b.SampleSize > 0 {
			return fmt.Sprintf("共 %d 样本，固定大小 %d B", b.SampleCount, b.SampleSize), "", "", false
		}
		return fmt.Sprintf("共 %d 样本（变长）", b.SampleCount), "", "", false

	case *mp4.Stco:
		return fmt.Sprintf("32-bit Chunk 偏移表，共 %d 项", len(b.ChunkOffset)), "", "", false

	case *mp4.Co64:
		return fmt.Sprintf("64-bit Chunk 偏移表，共 %d 项", len(b.ChunkOffset)), "", "", false

	case *mp4.Mfhd:
		return fmt.Sprintf("分片序号 #%d", b.SequenceNumber), "", "", false

	case *mp4.Tfhd:
		return fmt.Sprintf("track_id=%d, flags=0x%06X", b.TrackID, b.GetFlags()), "", "", false

	case *mp4.Tfdt:
		return fmt.Sprintf("base_decode_time=%d", b.GetBaseMediaDecodeTime()), "", "", false

	case *mp4.Trun:
		return fmt.Sprintf("样本数=%d, data_offset=%d", b.SampleCount, b.DataOffset), "", "", false

	case *mp4.Trex:
		return fmt.Sprintf("track_id=%d, def_dur=%d, def_size=%d", b.TrackID, b.DefaultSampleDuration, b.DefaultSampleSize), "", "", false

	case *mp4.Sidx:
		return fmt.Sprintf("ref_id=%d, timescale=%d, refs=%d", b.ReferenceID, b.Timescale, len(b.References)), "", "", false

	case *mp4.Meta:
		return "元数据容器", "", "", true

	case *mp4.Dref:
		return fmt.Sprintf("数据引用 %d 项", b.EntryCount), "", "", true

	case *mp4.Colr:
		ct := strings.TrimRight(string(b.ColourType[:]), "\x00")
		return fmt.Sprintf("type=%s, primaries=%d, transfer=%d, matrix=%d", ct, b.ColourPrimaries, b.TransferCharacteristics, b.MatrixCoefficients), "", "", false

	case *mp4.Btrt:
		return fmt.Sprintf("max=%d bps, avg=%d bps", b.MaxBitrate, b.AvgBitrate), "", "", false
	}

	if s, err := mp4.Stringify(box, h.BoxInfo.Context); err == nil && s != "" {
		if len(s) > 120 {
			s = s[:120] + "…"
		}
		return s, "", "", false
	}
	return "", "", "", false
}

func formatBoxBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.2f MB", float64(n)/(1024*1024))
}
