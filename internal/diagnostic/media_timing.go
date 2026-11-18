package diagnostic

func markMediaArrival(metrics *Metrics, kind string, arrivalMS int64) {
	switch kind {
	case "video":
		if metrics.FirstVideoMS == nil {
			metrics.FirstVideoMS = &arrivalMS
		}
	case "audio":
		if metrics.FirstAudioMS == nil {
			metrics.FirstAudioMS = &arrivalMS
		}
	}
}

// For MPEG-TS HLS, a PAT/PMT codec declaration alone cannot establish arrival.
// Reuse the bounded TS parser to identify classified PES starts in the segment.
func hlsTSPESKinds(body []byte) (video, audio bool) {
	a := newTSAnalyzer(Manifest{Protocol: ProtocolUDPTS})
	f := tsFramer{analyzer: a}
	f.datagram(tsSpan{}, body, 0, nil, true)
	return a.report.Metrics.FirstVideoMS != nil, a.report.Metrics.FirstAudioMS != nil
}
