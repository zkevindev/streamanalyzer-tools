package analyzer

import (
	"context"
	"fmt"
	"sync"

	"streamanalyzer/internal/hls"
	"streamanalyzer/internal/models"
)

func (s *StreamAnalyzer) runHLSTask(ctx context.Context, at *analyzeTask) error {
	client := &hls.Client{
		PlaylistURL:   at.URL,
		UserAgent:     "StreamAnalyzer-HLS/1.0",
		OnSegmentData: s.newHLSDumpFunc(at),
		OnSegment: func(st hls.SegmentStats) {
			row := models.ChartHLSSegment{
				TaskID:            at.ID,
				StreamID:          st.StreamID,
				Seq:               st.Seq,
				URI:               st.URI,
				DurationSec:       st.DurationSec,
				SizeBytes:         st.SizeBytes,
				VideoPTSFirst90k:  st.VideoPTSFirst90k,
				VideoPTSLast90k:   st.VideoPTSLast90k,
				VideoDTSFirst90k:  st.VideoDTSFirst90k,
				VideoDTSLast90k:   st.VideoDTSLast90k,
				AudioPTSFirst90k:  st.AudioPTSFirst90k,
				AudioPTSLast90k:   st.AudioPTSLast90k,
				AudioDTSFirst90k:  st.AudioDTSFirst90k,
				AudioDTSLast90k:   st.AudioDTSLast90k,
				AVDiffPTS90k:      st.AVDiffPTS90k,
				AVDiffValid:       st.AVDiffValid,
				AVDiffDTS90k:      st.AVDiffDTS90k,
				AVDiffDTSValid:    st.AVDiffDTSValid,
				IFrameIntervalsMs: append([]int64(nil), st.IFrameIntervalsMs...),
				PATCount:          st.PATCount,
				PMTCount:          st.PMTCount,
				PESCount:          st.PESCount,
				VideoPES:          st.VideoPES,
				AudioPES:          st.AudioPES,
			}
			if err := s.storage.WriteHLSSegment(&row); err != nil {
				fmt.Printf("task=%s write hls segment failed: %v\n", at.ID, err)
			} else {
				s.storage.Flush(at.ID)
			}
		},
	}
	if err := client.Run(ctx); err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		return &taskStartupError{err: err}
	}

	return nil
}

// newHLSDumpFunc appends downloaded segments, byte for byte, into one TS file
// per rendition. Renditions are dumped concurrently, so the playlist->file
// mapping is guarded.
func (s *StreamAnalyzer) newHLSDumpFunc(at *analyzeTask) func(string, uint64, []byte) {
	if at.dump == nil {
		return nil
	}

	var mu sync.Mutex
	names := make(map[string]string)

	return func(playlistURL string, seq uint64, raw []byte) {
		mu.Lock()
		name, ok := names[playlistURL]
		if !ok {
			if len(names) == 0 {
				name = at.ID + ".ts"
			} else {
				name = fmt.Sprintf("%s_%d.ts", at.ID, len(names))
			}
			names[playlistURL] = name
			fmt.Printf("task=%s dump rendition %s -> %s\n", at.ID, playlistURL, name)
		}
		mu.Unlock()

		_, _ = at.dump.Writer(name).Write(raw)
	}
}
