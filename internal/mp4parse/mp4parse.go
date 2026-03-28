// Package mp4parse：MP4 结构解析、样例时间线、Annex-B/ADTS 导出（与 example/parse_mp4 及离线分析共用）。
package mp4parse

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/abema/go-mp4"
	"github.com/sunfish-shogi/bufseekio"
)

// Config 控制 Run：ReportWriter 为 nil 时不输出文本报告。
type Config struct {
	InputPath      string
	MaxSampleLines int
	VideoOut       string
	AudioOut       string
	ReportWriter   io.Writer
}

// Run 执行与 parse_mp4 示例相同的流程（报告 + 可选导出）。
func Run(cfg Config) error {
	if strings.TrimSpace(cfg.InputPath) == "" {
		return fmt.Errorf("input path empty")
	}
	w := cfg.ReportWriter
	if w == nil {
		w = io.Discard
	}
	maxN := cfg.MaxSampleLines // 0 = 打印全部样例行（与 -n 0 一致）

	f, err := os.Open(cfg.InputPath)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer f.Close()

	r := bufseekio.NewReadSeeker(f, 128*1024, 4)

	var trakRoots []mp4.BoxInfo
	_, err = mp4.ReadBoxStructure(r, func(h *mp4.ReadHandle) (interface{}, error) {
		if h.BoxInfo.Type == mp4.BoxTypeTrak() {
			trakRoots = append(trakRoots, h.BoxInfo)
		}
		return navigate(h)
	})
	if err != nil {
		return fmt.Errorf("scan trak: %w", err)
	}

	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}

	var hdrStsd, hdrMeta bool
	_, err = mp4.ReadBoxStructure(r, func(h *mp4.ReadHandle) (interface{}, error) {
		if inSubtree(h.Path, mp4.BoxTypeStsd()) {
			if !hdrStsd {
				fmt.Fprintln(w, "======== stsd（各轨采样描述：编码器、SPS/PPS、esds 等）========")
				hdrStsd = true
			}
			if err := printBoxLine(w, h); err != nil {
				return nil, err
			}
			return h.Expand()
		}
		if inSubtree(h.Path, mp4.BoxTypeMeta()) {
			if !hdrMeta {
				fmt.Fprintln(w, "\n======== meta（QuickTime：hdlr + ilst/data；keys 中 mdta 命名空间会单独标出）========")
				hdrMeta = true
			}
			if err := printBoxLine(w, h); err != nil {
				return nil, err
			}
			return h.Expand()
		}
		return navigate(h)
	})
	if err != nil {
		return fmt.Errorf("stsd/meta: %w", err)
	}

	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}

	info, err := mp4.Probe(r)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}

	fmt.Fprintln(w, "\n======== 样例时间线（由 stts + ctts + stsz + stss 推导）========")
	for ti, tr := range info.Tracks {
		if ti >= len(trakRoots) {
			break
		}
		hType, isVideo := trackHandlerAndVideo(r, &trakRoots[ti])
		syncSet, _ := loadStss(r, &trakRoots[ti])
		printTrackSamples(w, ti+1, hType, isVideo, tr, syncSet, maxN)
	}

	if strings.TrimSpace(cfg.VideoOut) != "" || strings.TrimSpace(cfg.AudioOut) != "" {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if err := dumpStreams(r, info, trakRoots, strings.TrimSpace(cfg.VideoOut), strings.TrimSpace(cfg.AudioOut)); err != nil {
			return fmt.Errorf("export: %w", err)
		}
	}
	return nil
}

func navigate(h *mp4.ReadHandle) (interface{}, error) {
	switch h.BoxInfo.Type.String() {
	case "mdat", "free", "skip":
		return nil, nil
	default:
		return h.Expand()
	}
}

func inSubtree(path mp4.BoxPath, root mp4.BoxType) bool {
	idx := -1
	for i, p := range path {
		if p == root {
			idx = i
			break
		}
	}
	return idx >= 0 && len(path) >= idx+1
}

func printBoxLine(w io.Writer, h *mp4.ReadHandle) error {
	depth := len(h.Path)
	ind := strings.Repeat("  ", depth)
	name := h.BoxInfo.Type.String()

	fmt.Fprintf(w, "%s[%s] offset=%d size=%d\n", ind, name, h.BoxInfo.Offset, h.BoxInfo.Size)

	if !h.BoxInfo.IsSupportedType() {
		fmt.Fprintf(w, "%s  (库未解析 payload，仅保留偏移与大小)\n", ind)
		return nil
	}

	box, _, err := h.ReadPayload()
	if err != nil {
		return err
	}
	s, err := mp4.Stringify(box, h.BoxInfo.Context)
	if err != nil {
		return err
	}
	const max = 2000
	if len(s) > max {
		s = s[:max] + "…"
	}
	fmt.Fprintf(w, "%s  %s\n", ind, s)

	// keys 中单独标出 mdta 命名空间项
	if name == "keys" {
		if keys, ok := box.(*mp4.Keys); ok {
			for _, e := range keys.Entries {
				ns := strings.TrimRight(string(e.KeyNamespace), "\x00")
				if ns == "mdta" {
					fmt.Fprintf(w, "%s  [mdta key] %s\n", ind, string(e.KeyValue))
				}
			}
		}
	}
	return nil
}

func trackHandlerAndVideo(r io.ReadSeeker, trak *mp4.BoxInfo) (handler string, isVideo bool) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return "?", false
	}
	bips, err := mp4.ExtractBoxesWithPayload(r, trak, []mp4.BoxPath{
		{mp4.BoxTypeMdia(), mp4.BoxTypeHdlr()},
	})
	if err != nil || len(bips) == 0 {
		return "?", false
	}
	h, ok := bips[0].Payload.(*mp4.Hdlr)
	if !ok {
		return "?", false
	}
	t := strings.TrimRight(string(h.HandlerType[:]), "\x00")
	return t, t == "vide"
}

func loadStss(r io.ReadSeeker, trak *mp4.BoxInfo) (map[int]bool, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	bips, err := mp4.ExtractBoxesWithPayload(r, trak, []mp4.BoxPath{
		{mp4.BoxTypeMdia(), mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStss()},
	})
	if err != nil || len(bips) == 0 {
		return map[int]bool{}, nil
	}
	out := make(map[int]bool)
	for _, bip := range bips {
		stss, ok := bip.Payload.(*mp4.Stss)
		if !ok {
			continue
		}
		for _, sn := range stss.SampleNumber {
			out[int(sn)] = true
		}
	}
	return out, nil
}

func printTrackSamples(w io.Writer, trackIdx int, handler string, isVideo bool, tr *mp4.Track, sync map[int]bool, maxN int) {
	fmt.Fprintf(w, "\n--- track #%d handler=%s timescale=%d samples=%d ---\n",
		trackIdx, handler, tr.Timescale, len(tr.Samples))
	if tr.Timescale == 0 {
		fmt.Fprintln(w, "(timescale=0，跳过)")
		return
	}

	var dts int64
	lim := len(tr.Samples)
	if maxN > 0 && maxN < lim {
		lim = maxN
	}

	fmt.Fprintln(w, "idx(1-based)  DTS(tick)   PTS(tick)   DTS(s)      PTS(s)      size(B)  frame")
	fmt.Fprintln(w, "------------  ----------  ----------  ----------  ----------  -------  -----")

	for i := 0; i < lim; i++ {
		s := tr.Samples[i]
		pts := dts + s.CompositionTimeOffset
		idx1 := i + 1
		ft := frameLabel(isVideo, idx1, sync)
		fmt.Fprintf(w, "%-12d  %-10d  %-10d  %-10.6f  %-10.6f  %-7d  %s\n",
			idx1,
			dts, pts,
			float64(dts)/float64(tr.Timescale),
			float64(pts)/float64(tr.Timescale),
			s.Size,
			ft,
		)
		dts += int64(s.TimeDelta)
	}

	if maxN > 0 && len(tr.Samples) > lim {
		fmt.Fprintf(w, "... 省略 %d 条（使用 -n 0 可打印全部）\n", len(tr.Samples)-lim)
	}
}

func frameLabel(isVideo bool, sampleIndex1 int, sync map[int]bool) string {
	if !isVideo {
		return "audio"
	}
	if len(sync) == 0 {
		return "?"
	}
	if sync[sampleIndex1] {
		return "I(stss)"
	}
	return "non-I"
}

// 从 mdat 按 stco/stsc/stsz 顺序导出，与 parse_flv 对齐：视频 Annex-B（H.264 avc1 / H.265 hvc1·hev1），音频 ADTS AAC。

func dumpStreams(r io.ReadSeeker, info *mp4.ProbeInfo, trakRoots []mp4.BoxInfo, videoPath, audioPath string) error {
	var wroteVideo, wroteAudio bool
	for ti, tr := range info.Tracks {
		if ti >= len(trakRoots) {
			break
		}
		hType, isVideo := trackHandlerAndVideo(r, &trakRoots[ti])
		if videoPath != "" && isVideo {
			if wroteVideo {
				fmt.Fprintf(os.Stderr, "警告: 忽略额外视频轨 #%d\n", ti+1)
				continue
			}
			ve, ok := videoExportInfoFromTrak(r, tr, &trakRoots[ti])
			if !ok {
				return fmt.Errorf("-video: 仅支持 H.264(avc1) 或 H.265(hvc1/hev1+hvcC)，轨道 #%d", ti+1)
			}
			if _, err := r.Seek(0, io.SeekStart); err != nil {
				return err
			}
			syncSet, _ := loadStss(r, &trakRoots[ti])
			f, err := os.Create(videoPath)
			if err != nil {
				return fmt.Errorf("创建视频输出: %w", err)
			}
			if err := writeLengthPrefixedNALAnnexB(r, tr, ve.naluLen, f, ve.paramAnnexB, syncSet); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			wroteVideo = true
		}
		if audioPath != "" && hType == "soun" {
			if wroteAudio {
				fmt.Fprintf(os.Stderr, "警告: 忽略额外音频轨 #%d\n", ti+1)
				continue
			}
			if tr.Codec != mp4.CodecMP4A {
				return fmt.Errorf("-audio: 仅支持 AAC(mp4a)，轨道 #%d 不是 mp4a", ti+1)
			}
			asc, err := loadAudioSpecificConfig(r, &trakRoots[ti])
			if err != nil {
				return err
			}
			if len(asc) < 2 {
				return fmt.Errorf("-audio: 无法从 esds 解析 AudioSpecificConfig")
			}
			f, err := os.Create(audioPath)
			if err != nil {
				return fmt.Errorf("创建音频输出: %w", err)
			}
			if err := writeAACTrackADTS(r, tr, asc, f); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			wroteAudio = true
		}
	}
	if videoPath != "" && !wroteVideo {
		return fmt.Errorf("-video: 未找到视频轨(vide)")
	}
	if audioPath != "" && !wroteAudio {
		return fmt.Errorf("-audio: 未找到音频轨(soun)")
	}
	return nil
}

// videoExportInfoFromTrak：NAL 长度；paramAnnexB 为 avcC/hvcC 中参数 NAL（SPS/PPS 或 HEVC 各 VPS/SPS/PPS…）的 Annex B，按需在首帧与各 I 帧前写入。
type videoExportInfo struct {
	codecLabel  string
	naluLen     int
	paramAnnexB []byte
}

func videoExportInfoFromTrak(r io.ReadSeeker, tr *mp4.Track, trak *mp4.BoxInfo) (videoExportInfo, bool) {
	if tr.Codec == mp4.CodecAVC1 && tr.AVC != nil {
		naluLen := int(tr.AVC.LengthSize)
		if naluLen < 1 || naluLen > 4 {
			naluLen = 4
		}
		avcc, _ := loadAvcCFromTrak(r, trak)
		var param []byte
		if avcc != nil {
			param = annexBPrefixFromAvcC(avcc)
		}
		return videoExportInfo{codecLabel: "H.264", naluLen: naluLen, paramAnnexB: param}, true
	}
	hvcc, err := extractHvcC(r, trak)
	if err != nil || hvcc == nil {
		return videoExportInfo{}, false
	}
	naluLen := int(hvcc.LengthSizeMinusOne) + 1
	if naluLen < 1 || naluLen > 4 {
		naluLen = 4
	}
	return videoExportInfo{
		codecLabel:  "H.265/HEVC",
		naluLen:     naluLen,
		paramAnnexB: annexBPrefixFromHvcC(hvcc),
	}, true
}

func loadAvcCFromTrak(r io.ReadSeeker, trak *mp4.BoxInfo) (*mp4.AVCDecoderConfiguration, error) {
	paths := []mp4.BoxPath{
		{mp4.BoxTypeMdia(), mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeAvc1(), mp4.BoxTypeAvcC()},
		{mp4.BoxTypeMdia(), mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeEncv(), mp4.BoxTypeAvcC()},
	}
	for _, p := range paths {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		bips, err := mp4.ExtractBoxesWithPayload(r, trak, []mp4.BoxPath{p})
		if err != nil {
			continue
		}
		for _, bip := range bips {
			if avcc, ok := bip.Payload.(*mp4.AVCDecoderConfiguration); ok {
				return avcc, nil
			}
		}
	}
	return nil, nil
}

func extractHvcC(r io.ReadSeeker, trak *mp4.BoxInfo) (*mp4.HvcC, error) {
	paths := []mp4.BoxPath{
		{mp4.BoxTypeMdia(), mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeHvc1(), mp4.BoxTypeHvcC()},
		{mp4.BoxTypeMdia(), mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeHev1(), mp4.BoxTypeHvcC()},
	}
	for _, p := range paths {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		bips, err := mp4.ExtractBoxesWithPayload(r, trak, []mp4.BoxPath{p})
		if err != nil {
			continue
		}
		for _, bip := range bips {
			if hvcc, ok := bip.Payload.(*mp4.HvcC); ok {
				return hvcc, nil
			}
		}
	}
	return nil, nil
}

func annexBPrefixFromAvcC(avcc *mp4.AVCDecoderConfiguration) []byte {
	start := []byte{0x00, 0x00, 0x00, 0x01}
	var b []byte
	for i := range avcc.SequenceParameterSets {
		s := &avcc.SequenceParameterSets[i]
		if len(s.NALUnit) == 0 {
			continue
		}
		b = append(b, start...)
		b = append(b, s.NALUnit...)
	}
	for i := range avcc.PictureParameterSets {
		s := &avcc.PictureParameterSets[i]
		if len(s.NALUnit) == 0 {
			continue
		}
		b = append(b, start...)
		b = append(b, s.NALUnit...)
	}
	if avcc.HighProfileFieldsEnabled {
		for i := range avcc.SequenceParameterSetsExt {
			s := &avcc.SequenceParameterSetsExt[i]
			if len(s.NALUnit) == 0 {
				continue
			}
			b = append(b, start...)
			b = append(b, s.NALUnit...)
		}
	}
	return b
}

func annexBPrefixFromHvcC(hvcc *mp4.HvcC) []byte {
	start := []byte{0x00, 0x00, 0x00, 0x01}
	var b []byte
	for ai := range hvcc.NaluArrays {
		arr := &hvcc.NaluArrays[ai]
		for ni := range arr.Nalus {
			nal := &arr.Nalus[ni]
			if len(nal.NALUnit) == 0 {
				continue
			}
			b = append(b, start...)
			b = append(b, nal.NALUnit...)
		}
	}
	return b
}

func loadAudioSpecificConfig(r io.ReadSeeker, trak *mp4.BoxInfo) ([]byte, error) {
	paths := []mp4.BoxPath{
		{mp4.BoxTypeMdia(), mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeMp4a(), mp4.BoxTypeEsds()},
		{mp4.BoxTypeMdia(), mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeMp4a(), mp4.BoxTypeWave(), mp4.BoxTypeEsds()},
	}
	for _, p := range paths {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		bips, err := mp4.ExtractBoxesWithPayload(r, trak, []mp4.BoxPath{p})
		if err != nil {
			continue
		}
		for _, bip := range bips {
			esds, ok := bip.Payload.(*mp4.Esds)
			if !ok {
				continue
			}
			if asc := ascFromEsds(esds); len(asc) >= 2 {
				return asc, nil
			}
		}
	}
	return nil, nil
}

func ascFromEsds(e *mp4.Esds) []byte {
	for _, d := range e.Descriptors {
		if d.Tag == int8(mp4.DecSpecificInfoTag) && len(d.Data) >= 2 {
			out := make([]byte, len(d.Data))
			copy(out, d.Data)
			return out
		}
	}
	return nil
}

// paramAnnexB 为 avcC/hvcC 解析出的参数集 Annex B。在 sample 1 之前写一次（流头即 SPS，便于探测），并在每个 stss 同步点（I 帧）前再写一次。
func writeLengthPrefixedNALAnnexB(r io.ReadSeeker, tr *mp4.Track, naluLenSize int, w io.Writer, paramAnnexB []byte, sync map[int]bool) error {
	if naluLenSize < 1 || naluLenSize > 4 {
		naluLenSize = 4
	}
	if sync == nil {
		sync = map[int]bool{}
	}
	buf := make([]byte, 64*1024)
	var si int
	sampleIdx := 0
	for _, chunk := range tr.Chunks {
		end := si + int(chunk.SamplesPerChunk)
		off := chunk.DataOffset
		for si < end && si < len(tr.Samples) {
			s := tr.Samples[si]
			si++
			sampleIdx++
			if s.Size == 0 {
				continue
			}
			if int(s.Size) > len(buf) {
				buf = make([]byte, s.Size)
			}
			if _, err := r.Seek(int64(off), io.SeekStart); err != nil {
				return err
			}
			if _, err := io.ReadFull(r, buf[:s.Size]); err != nil {
				return err
			}
			annex, err := lengthPrefixedNALToAnnexB(buf[:s.Size], naluLenSize)
			if err != nil {
				return fmt.Errorf("sample #%d length-prefix→annexb: %w", sampleIdx, err)
			}
			if len(paramAnnexB) > 0 && (sampleIdx == 1 || sync[sampleIdx]) {
				if _, err := w.Write(paramAnnexB); err != nil {
					return err
				}
			}
			if _, err := w.Write(annex); err != nil {
				return err
			}
			off += uint64(s.Size)
		}
	}
	return nil
}

func writeAACTrackADTS(r io.ReadSeeker, tr *mp4.Track, asc []byte, w io.Writer) error {
	buf := make([]byte, 16*1024)
	var si int
	sampleIdx := 0
	for _, chunk := range tr.Chunks {
		end := si + int(chunk.SamplesPerChunk)
		off := chunk.DataOffset
		for si < end && si < len(tr.Samples) {
			s := tr.Samples[si]
			si++
			sampleIdx++
			if s.Size == 0 {
				continue
			}
			if int(s.Size) > len(buf) {
				buf = make([]byte, s.Size)
			}
			if _, err := r.Seek(int64(off), io.SeekStart); err != nil {
				return err
			}
			if _, err := io.ReadFull(r, buf[:s.Size]); err != nil {
				return err
			}
			hdr, err := buildADTSHeader(asc, int(s.Size))
			if err != nil {
				return fmt.Errorf("sample #%d adts 头: %w", sampleIdx, err)
			}
			if _, err := w.Write(hdr); err != nil {
				return err
			}
			if _, err := w.Write(buf[:s.Size]); err != nil {
				return err
			}
			off += uint64(s.Size)
		}
	}
	return nil
}

// lengthPrefixedNALToAnnexB 将 MP4 样本中 AVCC/HVCC 风格的长度前缀 NAL 转为 Annex B（H.264/H.265 均适用）。
func lengthPrefixedNALToAnnexB(payload []byte, naluLenSize int) ([]byte, error) {
	if naluLenSize < 1 || naluLenSize > 4 {
		naluLenSize = 4
	}
	var out bytes.Buffer
	off := 0
	for off+naluLenSize <= len(payload) {
		n := 0
		for i := 0; i < naluLenSize; i++ {
			n = (n << 8) | int(payload[off+i])
		}
		off += naluLenSize
		if n <= 0 || off+n > len(payload) {
			return nil, fmt.Errorf("invalid nalu size")
		}
		out.Write([]byte{0x00, 0x00, 0x00, 0x01})
		out.Write(payload[off : off+n])
		off += n
	}
	if off != len(payload) {
		return nil, fmt.Errorf("trailing bytes in length-prefixed payload")
	}
	return out.Bytes(), nil
}

func buildADTSHeader(asc []byte, aacRawLen int) ([]byte, error) {
	if len(asc) < 2 {
		return nil, fmt.Errorf("asc too short")
	}
	audioObjectType := (asc[0] & 0xF8) >> 3
	freqIdx := ((asc[0] & 0x07) << 1) | ((asc[1] & 0x80) >> 7)
	channelCfg := (asc[1] & 0x78) >> 3
	if audioObjectType == 0 || freqIdx > 12 {
		return nil, fmt.Errorf("unsupported asc")
	}
	profile := audioObjectType - 1
	fullLen := aacRawLen + 7
	if fullLen > 0x1FFF {
		return nil, fmt.Errorf("aac frame too large")
	}
	h := make([]byte, 7)
	h[0] = 0xFF
	h[1] = 0xF1
	h[2] = byte(profile<<6) | byte(freqIdx<<2) | byte((channelCfg>>2)&0x01)
	h[3] = byte((channelCfg&0x03)<<6) | byte((fullLen>>11)&0x03)
	h[4] = byte((fullLen >> 3) & 0xFF)
	h[5] = byte((fullLen&0x07)<<5) | 0x1F
	h[6] = 0xFC
	return h, nil
}
