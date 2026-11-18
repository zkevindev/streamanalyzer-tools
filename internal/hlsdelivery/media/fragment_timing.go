package media

import "encoding/binary"

// FragmentMediaKinds identifies audio and video tracks with samples in an fMP4
// fragment. Initialization metadata alone never establishes media arrival.
func FragmentMediaKinds(segment, init []byte) (video, audio bool) {
	segment, init = skipID3(segment), skipID3(init)
	hasMediaData := false
	walkBoxes(segment, func(kind string, payload []byte) {
		hasMediaData = hasMediaData || kind == "mdat" && len(payload) > 0
	})
	if !hasMediaData {
		return false, false
	}

	tracks := make(map[uint32]string)
	for _, data := range [][]byte{init, segment} {
		walkBoxes(data, func(kind string, movie []byte) {
			if kind != "moov" {
				return
			}
			walkBoxes(movie, func(kind string, track []byte) {
				if kind != "trak" || len(tracks) >= 256 {
					return
				}
				id, handler := mediaTrack(track)
				if id != 0 && (handler == "vide" || handler == "soun") {
					tracks[id] = handler
				}
			})
		})
	}
	walkBoxes(segment, func(kind string, fragment []byte) {
		if kind != "moof" {
			return
		}
		walkBoxes(fragment, func(kind string, track []byte) {
			if kind != "traf" {
				return
			}
			var id uint32
			hasSample := false
			walkBoxes(track, func(kind string, payload []byte) {
				switch kind {
				case "tfhd":
					if len(payload) >= 8 {
						id = binary.BigEndian.Uint32(payload[4:8])
					}
				case "trun":
					if len(payload) >= 8 && binary.BigEndian.Uint32(payload[4:8]) > 0 {
						hasSample = true
					}
				}
			})
			if hasSample {
				video = video || tracks[id] == "vide"
				audio = audio || tracks[id] == "soun"
			}
		})
	})
	return video, audio
}

func mediaTrack(track []byte) (id uint32, handler string) {
	walkBoxes(track, func(kind string, payload []byte) {
		switch kind {
		case "tkhd":
			id = tkhdTrackID(payload)
		case "mdia":
			walkBoxes(payload, func(kind string, header []byte) {
				if kind == "hdlr" && len(header) >= 12 {
					handler = string(header[8:12])
				}
			})
		}
	})
	return id, handler
}
