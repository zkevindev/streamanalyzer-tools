package analyzer

import (
	"context"
	"fmt"

	"streamanalyzer/internal/hls"
	"streamanalyzer/internal/models"
)

func (s *StreamAnalyzer) runHLSTask(ctx context.Context, at *analyzeTask) {
	client := &hls.Client{
		PlaylistURL: at.URL,
		UserAgent:   "StreamAnalyzer-HLS/1.0",
		OnSegment: func(st hls.SegmentStats) {
			row := models.ChartHLSSegment{
				TaskID:           at.ID,
				StreamID:         st.StreamID,
				Seq:              st.Seq,
				URI:              st.URI,
				DurationSec:      st.DurationSec,
				SizeBytes:        st.SizeBytes,
				VideoPTSFirst90k: st.VideoPTSFirst90k,
				VideoPTSLast90k:  st.VideoPTSLast90k,
				VideoDTSFirst90k: st.VideoDTSFirst90k,
				VideoDTSLast90k:  st.VideoDTSLast90k,
				AudioPTSFirst90k: st.AudioPTSFirst90k,
				AudioPTSLast90k:  st.AudioPTSLast90k,
				AudioDTSFirst90k: st.AudioDTSFirst90k,
				AudioDTSLast90k:  st.AudioDTSLast90k,
				AVDiffPTS90k:     st.AVDiffPTS90k,
				AVDiffValid:      st.AVDiffValid,
				AVDiffDTS90k:     st.AVDiffDTS90k,
				AVDiffDTSValid:   st.AVDiffDTSValid,
				IFrameIntervalsMs: append([]int64(nil), st.IFrameIntervalsMs...),
				PATCount:         st.PATCount,
				PMTCount:         st.PMTCount,
				PESCount:         st.PESCount,
				VideoPES:         st.VideoPES,
				AudioPES:         st.AudioPES,
			}
			if err := s.storage.WriteHLSSegment(&row); err != nil {
				fmt.Printf("task=%s write hls segment failed: %v\n", at.ID, err)
			} else {
				s.storage.Flush(at.ID)
			}
		},
	}
	if err := client.Run(ctx); err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		fmt.Printf("task=%s hls client ended: %v\n", at.ID, err)
	}
}
