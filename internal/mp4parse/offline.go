package mp4parse

import (
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
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, "", "", nil, err
	}
	info, err := mp4.Probe(r)
	if err != nil {
		return nil, "", "", nil, err
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, "", "", nil, err
	}
	frames, v, a, profile := offlineFrameDetails(r, info, trakRoots)
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
}

func offlineFrameDetails(r io.ReadSeeker, info *mp4.ProbeInfo, trakRoots []mp4.BoxInfo) ([]models.OfflineFrameDetail, string, string, *models.OfflineMediaProfile) {
	var rows []frameRow
	videoCodec := ""
	audioCodec := ""
	profile := &models.OfflineMediaProfile{}
	trackByID := map[uint32]trackMeta{}

	for ti, tr := range info.Tracks {
		if ti >= len(trakRoots) {
			break
		}
		hType, isVideo := trackHandlerAndVideo(r, &trakRoots[ti])
		isAudio := !isVideo && hType == "soun"
		trackByID[tr.TrackID] = trackMeta{
			timescale: tr.Timescale,
			isVideo:   isVideo,
			isAudio:   isAudio,
			isAAC:     tr.Codec == mp4.CodecMP4A,
		}
		syncSet, _ := loadStss(r, &trakRoots[ti])

		if isVideo && videoCodec == "" {
			if ve, ok := videoExportInfoFromTrak(r, tr, &trakRoots[ti]); ok {
				if strings.HasPrefix(ve.codecLabel, "H.264") {
					videoCodec = "h264"
				} else if strings.HasPrefix(ve.codecLabel, "H.265") {
					videoCodec = "h265"
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
		}
		if isAudio && audioCodec == "" {
			if tr.Codec == mp4.CodecMP4A {
				audioCodec = "aac"
				if asc, err := loadAudioSpecificConfig(r, &trakRoots[ti]); err == nil && len(asc) >= 2 {
					if am, err := codec.ParseAACAudioSpecificConfig(asc); err == nil {
						profile.AudioSampleRate = am.SampleRate
						profile.AudioChannels = am.Channels
					}
				}
			} else {
				audioCodec = "unknown"
			}
			if tr.MP4A != nil && profile.AudioChannels == 0 {
				profile.AudioChannels = int(tr.MP4A.ChannelCount)
			}
		}

		var dts int64
		for i := 0; i < len(tr.Samples); i++ {
			s := tr.Samples[i]
			pts := dts + s.CompositionTimeOffset
			dtsMs := tickToMs(dts, tr.Timescale)
			ptsMs := tickToMs(pts, tr.Timescale)
			idx1 := i + 1

			media := "other"
			ft := "-"
			flen := int(s.Size)
			if isVideo {
				media = "video"
				ft = frameLabel(true, idx1, syncSet)
			} else if isAudio {
				media = "audio"
				ft = "-"
				if tr.Codec == mp4.CodecMP4A {
					flen = 7 + int(s.Size)
				}
			}

			rows = append(rows, frameRow{
				dts: dtsMs,
				d: models.OfflineFrameDetail{
					MediaType: media,
					DTS:       dtsMs,
					PTS:       ptsMs,
					CTS:       ptsMs - dtsMs,
					FrameLen:  flen,
					FrameType: ft,
				},
			})
			dts += int64(s.TimeDelta)
		}
	}

	// Support fragmented MP4 (moof/traf/trun) when segments exist or stbl has no samples.
	fragRows, hasMoof := extractFragmentedSamples(r, trackByID)
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
	return out, videoCodec, audioCodec, profile
}

func extractFragmentedSamples(r io.ReadSeeker, trackByID map[uint32]trackMeta) ([]frameRow, bool) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}

	trexByID := map[uint32]*mp4.Trex{}
	var (
		rows        []frameRow
		hasMoof     bool
		curTrackID  uint32
		curDefDur   uint32
		curDefSize  uint32
		curDefFlags uint32
		curDTS      int64
		hasTFDT     bool
		trackCursor = map[uint32]int64{}
	)

	_, _ = mp4.ReadBoxStructure(r, func(h *mp4.ReadHandle) (interface{}, error) {
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
			return h.Expand()
		case mp4.BoxTypeTraf():
			curTrackID = 0
			curDefDur = 0
			curDefSize = 0
			curDefFlags = 0
			curDTS = 0
			hasTFDT = false
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
			if known {
				if tm.isVideo {
					media = "video"
				} else if tm.isAudio {
					media = "audio"
				}
			} else if len(trackByID) == 0 {
				// Standalone media segment without moov: still emit timing rows as video/media.
				media = "video"
			}

			flags := trun.GetFlags()
			hasDur := flags&0x000100 != 0
			hasSize := flags&0x000200 != 0
			hasSampleFlags := flags&0x000400 != 0
			hasCTO := flags&0x000800 != 0
			hasFirstFlags := flags&0x000004 != 0

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
				rows = append(rows, frameRow{
					dts: dtsMs,
					d: models.OfflineFrameDetail{
						MediaType: media,
						DTS:       dtsMs,
						PTS:       ptsMs,
						CTS:       ptsMs - dtsMs,
						FrameLen:  int(sz),
						FrameType: ft,
					},
				})
				curDTS += int64(dur)
			}
			trackCursor[curTrackID] = curDTS
			return nil, nil
		default:
			return h.Expand()
		}
	})
	return rows, hasMoof
}
