package mp4parse

import (
	"context"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/abema/go-mp4"
	"github.com/sunfish-shogi/bufseekio"
	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
)

// CollectTrakRoots 扫描 moov 下所有 trak 根 BoxInfo（与 Run 一致）。
func CollectTrakRoots(r io.ReadSeeker) ([]mp4.BoxInfo, error) {
	var trakRoots []mp4.BoxInfo
	_, err := mp4.ReadBoxStructure(r, func(h *mp4.ReadHandle) (interface{}, error) {
		if h.BoxInfo.Type == mp4.BoxTypeTrak() {
			trakRoots = append(trakRoots, h.BoxInfo)
		}
		return navigate(h)
	})
	return trakRoots, err
}

// OfflineFrameDetailsFromPath 打开文件、Probe，生成与离线页图表兼容的帧明细（DTS/PTS/CTS 为毫秒）；
// 并返回视频/音频编码简写（如 h264、aac）和可从样本描述与容器头中读到的媒体参数。
func OfflineFrameDetailsFromPath(inputPath string) ([]models.OfflineFrameDetail, string, string, *models.OfflineMediaProfile, error) {
	return OfflineFrameDetailsWithContext(context.Background(), inputPath)
}

// OfflineFrameDetailsWithContext 支持取消信号的 MP4 帧明细提取，并附带每帧字节定位信息。
func OfflineFrameDetailsWithContext(ctx context.Context, inputPath string) ([]models.OfflineFrameDetail, string, string, *models.OfflineMediaProfile, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, "", "", nil, ctx.Err()
	}
	f, err := os.Open(inputPath)
	if err != nil {
		return nil, "", "", nil, err
	}
	defer f.Close()
	r := bufseekio.NewReadSeeker(f, 128*1024, 4)
	trakRoots, err := CollectTrakRoots(r)
	if err != nil {
		return nil, "", "", nil, err
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, "", "", nil, ctx.Err()
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, "", "", nil, err
	}
	info, err := mp4.Probe(r)
	if err != nil {
		return nil, "", "", nil, err
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, "", "", nil, ctx.Err()
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, "", "", nil, err
	}
	frames, v, a, profile, err := offlineFrameDetails(ctx, r, info, trakRoots)
	if err != nil {
		return nil, "", "", nil, err
	}
	return frames, v, a, profile, nil
}

func tickToMs(tick int64, ts uint32) int64 {
	if ts == 0 {
		return 0
	}
	return (tick * 1000) / int64(ts)
}

type frameRow struct {
	dts int64
	d   models.OfflineFrameDetail
}

type trackMeta struct {
	timescale uint32
	isVideo   bool
	isAudio   bool
	isAAC     bool
	naluLen   int
}

func sampleOffsetsForTrack(tr *mp4.Track) []int64 {
	offsets := make([]int64, len(tr.Samples))
	si := 0
	for _, chunk := range tr.Chunks {
		end := si + int(chunk.SamplesPerChunk)
		off := chunk.DataOffset
		for si < end && si < len(tr.Samples) {
			offsets[si] = int64(off)
			off += uint64(tr.Samples[si].Size)
			si++
		}
	}
	return offsets
}

func offlineFrameDetails(ctx context.Context, r io.ReadSeeker, info *mp4.ProbeInfo, trakRoots []mp4.BoxInfo) ([]models.OfflineFrameDetail, string, string, *models.OfflineMediaProfile, error) {
	var rows []frameRow
	videoCodec := ""
	audioCodec := ""
	profile := &models.OfflineMediaProfile{}
	trackByID := map[uint32]trackMeta{}
	firstAACExported := false

	for ti, tr := range info.Tracks {
		if ctx != nil && ctx.Err() != nil {
			return nil, "", "", nil, ctx.Err()
		}
		if ti >= len(trakRoots) {
			break
		}
		hType, isVideo := trackHandlerAndVideo(r, &trakRoots[ti])
		isAudio := !isVideo && hType == "soun"

		naluLen := 0
		if isVideo {
			if ve, ok := videoExportInfoFromTrak(r, tr, &trakRoots[ti]); ok {
				naluLen = ve.naluLen
				if videoCodec == "" {
					if strings.HasPrefix(ve.codecLabel, "H.264") {
						videoCodec = "h264"
					} else if strings.HasPrefix(ve.codecLabel, "H.265") {
						videoCodec = "h265"
					}
				}
				if meta := codec.VideoMetaFromAnnexB(ve.paramAnnexB); meta != nil && profile.Width == 0 {
					profile.Width = meta.Width
					profile.Height = meta.Height
					profile.Profile = meta.ProfileName
					profile.Level = meta.Level
					if videoCodec == "h265" {
						profile.LevelName = codec.H265LevelName(meta.Level)
					} else {
						profile.LevelName = codec.H264LevelName(meta.Level)
					}
					if profile.FrameRate == 0 {
						profile.FrameRate = meta.FrameRate
					}
				}
			}
			if tr.AVC != nil && profile.Width == 0 {
				profile.Width = int(tr.AVC.Width)
				profile.Height = int(tr.AVC.Height)
				profile.Profile = codec.H264ProfileName(int(tr.AVC.Profile))
				profile.Level = int(tr.AVC.Level)
				profile.LevelName = codec.H264LevelName(int(tr.AVC.Level))
			}
			if naluLen == 0 && tr.AVC != nil {
				naluLen = int(tr.AVC.LengthSize)
			}
			if naluLen == 0 {
				naluLen = 4
			}
		}

		trackByID[tr.TrackID] = trackMeta{
			timescale: tr.Timescale,
			isVideo:   isVideo,
			isAudio:   isAudio,
			isAAC:     tr.Codec == mp4.CodecMP4A,
			naluLen:   naluLen,
		}
		syncSet, _ := loadStss(r, &trakRoots[ti])

		useAudioADTS := false
		if isAudio {
			if audioCodec == "" {
				if tr.Codec == mp4.CodecMP4A {
					audioCodec = "aac"
				} else {
					audioCodec = "unknown"
				}
			}
			if tr.Codec == mp4.CodecMP4A {
				if asc, err := loadAudioSpecificConfig(r, &trakRoots[ti]); err == nil && len(asc) >= 2 {
					if !firstAACExported {
						firstAACExported = true
						useAudioADTS = true
					}
					if profile.AudioSampleRate == 0 {
						if am, err := codec.ParseAACAudioSpecificConfig(asc); err == nil {
							profile.AudioSampleRate = am.SampleRate
							profile.AudioChannels = am.Channels
						}
					}
				}
			}
			if tr.MP4A != nil && profile.AudioChannels == 0 {
				profile.AudioChannels = int(tr.MP4A.ChannelCount)
			}
		}

		sampleOffsets := sampleOffsetsForTrack(tr)
		var adtsOffset int64
		var dts int64
		for i := 0; i < len(tr.Samples); i++ {
			if ctx != nil && i%2048 == 0 && ctx.Err() != nil {
				return nil, "", "", nil, ctx.Err()
			}
			s := tr.Samples[i]
			pts := dts + s.CompositionTimeOffset
			dtsMs := tickToMs(dts, tr.Timescale)
			ptsMs := tickToMs(pts, tr.Timescale)
			idx1 := i + 1

			media := "other"
			ft := "-"
			flen := int(s.Size)
			streamSrc := "input"
			streamOff := sampleOffsets[i]
			streamLen := int(s.Size)
			nalSize := 0

			if isVideo {
				media = "video"
				ft = frameLabel(true, idx1, syncSet)
				nalSize = naluLen
			} else if isAudio {
				media = "audio"
				ft = "-"
				if tr.Codec == mp4.CodecMP4A {
					flen = 7 + int(s.Size)
					if useAudioADTS && s.Size > 0 {
						streamSrc = "audio"
						streamOff = adtsOffset
						streamLen = 7 + int(s.Size)
						adtsOffset += int64(streamLen)
					}
				}
			}

			rows = append(rows, frameRow{
				dts: dtsMs,
				d: models.OfflineFrameDetail{
					MediaType:    media,
					DTS:          dtsMs,
					PTS:          ptsMs,
					CTS:          ptsMs - dtsMs,
					FrameLen:     flen,
					FrameType:    ft,
					StreamSource: streamSrc,
					StreamOffset: streamOff,
					StreamLen:    streamLen,
					NALULenSize:  nalSize,
				},
			})
			dts += int64(s.TimeDelta)
		}
	}

	// Support fragmented MP4 (moof/traf/trun) when segments exist or stbl has no samples.
	fragRows, hasMoof, err := extractFragmentedSamples(ctx, r, trackByID)
	if err != nil {
		return nil, "", "", nil, err
	}
	if hasMoof || (info != nil && len(info.Segments) > 0) {
		profile.MP4Fragmented = true
	}
	if len(fragRows) > 0 {
		rows = append(rows, fragRows...)
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].dts < rows[j].dts })
	out := make([]models.OfflineFrameDetail, len(rows))
	for i := range rows {
		out[i] = rows[i].d
	}
	if videoCodec == "" {
		videoCodec = "unknown"
	}
	profile.VideoCodec = videoCodec
	profile.AudioCodec = audioCodec
	if info != nil {
		fastStart := info.FastStart
		profile.MP4FastStart = &fastStart
		if !fastStart && !profile.MP4Fragmented && len(out) > 0 {
			profile.Warnings = append(profile.Warnings, "MP4 的 moov 位于 mdat 之后（非 FastStart），边下边播时起播可能变慢")
		}
		if profile.DurationMS == 0 && info.Timescale > 0 && info.Duration > 0 {
			profile.DurationMS = int64(info.Duration) * 1000 / int64(info.Timescale)
		}
	}
	return out, videoCodec, audioCodec, profile, nil
}

func extractFragmentedSamples(ctx context.Context, r io.ReadSeeker, trackByID map[uint32]trackMeta) ([]frameRow, bool, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, false, nil
	}

	trexByID := map[uint32]*mp4.Trex{}
	var (
		rows              []frameRow
		hasMoof           bool
		moofOffset        int64
		curTrackID        uint32
		curDefDur         uint32
		curDefSize        uint32
		curDefFlags       uint32
		curDTS            int64
		hasTFDT           bool
		curBaseDataOffset int64
		hasBaseDataOffset bool
		trafDataCursor    int64
		hasTrafDataCursor bool
		trackCursor       = map[uint32]int64{}
	)

	_, err := mp4.ReadBoxStructure(r, func(h *mp4.ReadHandle) (interface{}, error) {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		switch h.BoxInfo.Type {
		case mp4.BoxTypeMdat(), mp4.BoxTypeFree(), mp4.BoxTypeSkip():
			return nil, nil
		case mp4.BoxTypeTrex():
			box, _, err := h.ReadPayload()
			if err == nil {
				if trex, ok := box.(*mp4.Trex); ok {
					trexByID[trex.TrackID] = trex
				}
			}
			return nil, nil
		case mp4.BoxTypeMoof():
			hasMoof = true
			moofOffset = int64(h.BoxInfo.Offset)
			return h.Expand()
		case mp4.BoxTypeTraf():
			curTrackID = 0
			curDefDur = 0
			curDefSize = 0
			curDefFlags = 0
			curDTS = 0
			hasTFDT = false
			curBaseDataOffset = moofOffset
			hasBaseDataOffset = false
			trafDataCursor = moofOffset
			hasTrafDataCursor = false
			return h.Expand()
		case mp4.BoxTypeTfhd():
			box, _, err := h.ReadPayload()
			if err == nil {
				if tfhd, ok := box.(*mp4.Tfhd); ok {
					curTrackID = tfhd.TrackID
					if trex := trexByID[curTrackID]; trex != nil {
						curDefDur = trex.DefaultSampleDuration
						curDefSize = trex.DefaultSampleSize
						curDefFlags = trex.DefaultSampleFlags
					}
					flags := tfhd.GetFlags()
					if flags&mp4.TfhdBaseDataOffsetPresent != 0 {
						curBaseDataOffset = int64(tfhd.BaseDataOffset)
						hasBaseDataOffset = true
					} else {
						curBaseDataOffset = moofOffset
						hasBaseDataOffset = (flags & 0x020000) != 0 // default-base-is-moof
					}
					if flags&mp4.TfhdDefaultSampleDurationPresent != 0 {
						curDefDur = tfhd.DefaultSampleDuration
					}
					if flags&mp4.TfhdDefaultSampleSizePresent != 0 {
						curDefSize = tfhd.DefaultSampleSize
					}
					if flags&mp4.TfhdDefaultSampleFlagsPresent != 0 {
						curDefFlags = tfhd.DefaultSampleFlags
					}
					if !hasTFDT {
						curDTS = trackCursor[curTrackID]
					}
				}
			}
			return nil, nil
		case mp4.BoxTypeTfdt():
			box, _, err := h.ReadPayload()
			if err == nil {
				if tfdt, ok := box.(*mp4.Tfdt); ok {
					curDTS = int64(tfdt.GetBaseMediaDecodeTime())
					hasTFDT = true
				}
			}
			return nil, nil
		case mp4.BoxTypeTrun():
			box, _, err := h.ReadPayload()
			if err != nil {
				return nil, nil
			}
			trun, ok := box.(*mp4.Trun)
			if !ok {
				return nil, nil
			}
			tm, known := trackByID[curTrackID]
			timescale := tm.timescale
			if timescale == 0 {
				timescale = 90000
			}
			media := "other"
			nalSize := 0
			if known {
				if tm.isVideo {
					media = "video"
					nalSize = tm.naluLen
					if nalSize <= 0 {
						nalSize = 4
					}
				} else if tm.isAudio {
					media = "audio"
				}
			} else if len(trackByID) == 0 {
				// Standalone media segment without moov: still emit timing rows as video/media.
				media = "video"
				nalSize = 4
			}

			flags := trun.GetFlags()
			hasDataOffset := flags&0x000001 != 0
			hasDur := flags&0x000100 != 0
			hasSize := flags&0x000200 != 0
			hasSampleFlags := flags&0x000400 != 0
			hasCTO := flags&0x000800 != 0
			hasFirstFlags := flags&0x000004 != 0

			var samplePos int64
			hasValidPos := false
			if hasDataOffset {
				samplePos = curBaseDataOffset + int64(trun.DataOffset)
				hasValidPos = samplePos >= 0
			} else if hasTrafDataCursor {
				samplePos = trafDataCursor
				hasValidPos = samplePos >= 0
			} else if hasBaseDataOffset {
				samplePos = curBaseDataOffset
				hasValidPos = samplePos >= 0
			}

			for i := 0; i < int(trun.SampleCount); i++ {
				var entry mp4.TrunEntry
				if i < len(trun.Entries) {
					entry = trun.Entries[i]
				}
				dur := curDefDur
				if hasDur {
					dur = entry.SampleDuration
				}
				sz := curDefSize
				if hasSize {
					sz = entry.SampleSize
				}
				sFlags := curDefFlags
				if i == 0 && hasFirstFlags {
					sFlags = trun.FirstSampleFlags
				} else if hasSampleFlags {
					sFlags = entry.SampleFlags
				}
				var cto int64
				if hasCTO {
					if trun.GetVersion() == 0 {
						cto = int64(entry.SampleCompositionTimeOffsetV0)
					} else {
						cto = int64(entry.SampleCompositionTimeOffsetV1)
					}
				}
				dtsMs := tickToMs(curDTS, timescale)
				ptsMs := tickToMs(curDTS+cto, timescale)
				ft := "-"
				if media == "video" {
					// ISO 14496-12 sample_flags bit 16 (0x00010000) is sample_is_non_sync_sample.
					if (sFlags & 0x00010000) == 0 {
						ft = "I"
					} else {
						ft = "non-I"
					}
				}
				fd := models.OfflineFrameDetail{
					MediaType: media,
					DTS:       dtsMs,
					PTS:       ptsMs,
					CTS:       ptsMs - dtsMs,
					FrameLen:  int(sz),
					FrameType: ft,
				}
				if hasValidPos && sz > 0 {
					fd.StreamSource = "input"
					fd.StreamOffset = samplePos
					fd.StreamLen = int(sz)
					fd.NALULenSize = nalSize
					samplePos += int64(sz)
				}
				rows = append(rows, frameRow{dts: dtsMs, d: fd})
				curDTS += int64(dur)
			}
			if hasValidPos {
				trafDataCursor = samplePos
				hasTrafDataCursor = true
			}
			trackCursor[curTrackID] = curDTS
			return nil, nil
		default:
			return h.Expand()
		}
	})
	if err != nil && ctx != nil && ctx.Err() != nil {
		return nil, hasMoof, ctx.Err()
	}
	return rows, hasMoof, nil
}
