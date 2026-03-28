package mp4parse

import (
	"io"
	"os"
	"sort"
	"strings"

	"github.com/abema/go-mp4"
	"github.com/sunfish-shogi/bufseekio"
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

// OfflineFrameDetailsFromPath 打开文件、Probe，生成与离线页图表兼容的帧明细（DTS/PTS 为毫秒）；并返回视频/音频编码简写（如 h264、aac）。
func OfflineFrameDetailsFromPath(inputPath string) ([]models.OfflineFrameDetail, string, string, error) {
	f, err := os.Open(inputPath)
	if err != nil {
		return nil, "", "", err
	}
	defer f.Close()
	r := bufseekio.NewReadSeeker(f, 128*1024, 4)
	trakRoots, err := CollectTrakRoots(r)
	if err != nil {
		return nil, "", "", err
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, "", "", err
	}
	info, err := mp4.Probe(r)
	if err != nil {
		return nil, "", "", err
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, "", "", err
	}
	frames, v, a := offlineFrameDetails(r, info, trakRoots)
	return frames, v, a, nil
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

func offlineFrameDetails(r io.ReadSeeker, info *mp4.ProbeInfo, trakRoots []mp4.BoxInfo) ([]models.OfflineFrameDetail, string, string) {
	var rows []frameRow
	videoCodec := ""
	audioCodec := ""

	for ti, tr := range info.Tracks {
		if ti >= len(trakRoots) {
			break
		}
		hType, isVideo := trackHandlerAndVideo(r, &trakRoots[ti])
		syncSet, _ := loadStss(r, &trakRoots[ti])

		if isVideo && videoCodec == "" {
			if ve, ok := videoExportInfoFromTrak(r, tr, &trakRoots[ti]); ok {
				if strings.HasPrefix(ve.codecLabel, "H.264") {
					videoCodec = "h264"
				} else if strings.HasPrefix(ve.codecLabel, "H.265") {
					videoCodec = "h265"
				}
			}
		}
		if !isVideo && hType == "soun" && audioCodec == "" {
			if tr.Codec == mp4.CodecMP4A {
				audioCodec = "aac"
			} else {
				audioCodec = "unknown"
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
			} else if hType == "soun" {
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
					FrameLen:  flen,
					FrameType: ft,
				},
			})
			dts += int64(s.TimeDelta)
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].dts < rows[j].dts })
	out := make([]models.OfflineFrameDetail, len(rows))
	for i := range rows {
		out[i] = rows[i].d
	}
	if videoCodec == "" {
		videoCodec = "unknown"
	}
	return out, videoCodec, audioCodec
}
