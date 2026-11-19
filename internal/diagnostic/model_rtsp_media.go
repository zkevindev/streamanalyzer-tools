package diagnostic

// The model receives only controlled metadata. The JPEG pixels and clip bytes
// remain local artifacts; ImagePixelsProvided deliberately stays false.
func modelRTSPMedia(source *RTSPMediaEvidence) *RTSPMediaEvidence {
	if !validRTSPMediaEvidenceShape(source) {
		return nil
	}
	value := *source
	value.Keyframes = append([]Event(nil), source.Keyframes...)
	for index := range value.Keyframes {
		sequence := *value.Keyframes[index].RTPSequence
		value.Keyframes[index].RTPSequence = &sequence
	}
	return &value
}
