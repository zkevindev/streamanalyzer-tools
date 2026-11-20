package diagnostic

import "fmt"

type AnalysisThresholds struct {
	FirstFrameWarnMS int64 `json:"first_frame_warn_ms,omitempty"`
	VideoGapWarnMS   int64 `json:"video_gap_warn_ms,omitempty"`
	DTSGapWarnMS     int64 `json:"dts_gap_warn_ms,omitempty"`
	GOPWarnMS        int64 `json:"gop_warn_ms,omitempty"`
}

func standardAnalysisThresholds() AnalysisThresholds {
	return AnalysisThresholds{FirstFrameWarnMS: 3000, VideoGapWarnMS: 3000, DTSGapWarnMS: 2000, GOPWarnMS: 10000}
}

func resolveAnalysisThresholds(profile string, custom *AnalysisThresholds) (AnalysisThresholds, error) {
	var result AnalysisThresholds
	switch profile {
	case "", "standard":
		result = standardAnalysisThresholds()
		profile = "standard"
	case "low-latency":
		result = AnalysisThresholds{FirstFrameWarnMS: 1000, VideoGapWarnMS: 1000, DTSGapWarnMS: 1000, GOPWarnMS: 5000}
	case "camera":
		result = AnalysisThresholds{FirstFrameWarnMS: 8000, VideoGapWarnMS: 8000, DTSGapWarnMS: 5000, GOPWarnMS: 20000}
	case "custom":
		if custom == nil {
			return result, fmt.Errorf("%w: 自定义场景需要提供完整阈值", ErrInvalid)
		}
		result = *custom
	default:
		return result, fmt.Errorf("%w: 未知诊断场景", ErrInvalid)
	}
	if custom != nil && profile != "custom" {
		return result, fmt.Errorf("%w: 自定义阈值需要选择 custom 场景", ErrInvalid)
	}
	for _, value := range []int64{result.FirstFrameWarnMS, result.VideoGapWarnMS, result.DTSGapWarnMS, result.GOPWarnMS} {
		if value < 100 || value > 60000 {
			return result, fmt.Errorf("%w: 诊断阈值须为 100–60000 ms", ErrInvalid)
		}
	}
	return result, nil
}

func (manifest Manifest) thresholds() AnalysisThresholds {
	value := manifest.AnalysisThresholds
	if value.FirstFrameWarnMS == 0 {
		value.FirstFrameWarnMS = 3000
	}
	if value.VideoGapWarnMS == 0 {
		value.VideoGapWarnMS = 3000
	}
	if value.DTSGapWarnMS == 0 {
		value.DTSGapWarnMS = 2000
	}
	if value.GOPWarnMS == 0 {
		value.GOPWarnMS = 10000
	}
	return value
}

func validManifestThresholds(manifest Manifest) bool {
	if manifest.AnalysisProfile == "" && manifest.AnalysisThresholds == (AnalysisThresholds{}) {
		return true
	}
	profile := manifest.AnalysisProfile
	if profile == "custom" {
		_, err := resolveAnalysisThresholds(profile, &manifest.AnalysisThresholds)
		return err == nil
	}
	resolved, err := resolveAnalysisThresholds(profile, nil)
	return err == nil && resolved == manifest.AnalysisThresholds
}
