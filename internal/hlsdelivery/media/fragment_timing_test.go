package media

import (
	"encoding/binary"
	"testing"
)

func TestFragmentMediaKindsRequiresSampleAndMediaData(t *testing.T) {
	box := func(kind string, body []byte) []byte {
		return append(append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), kind...), body...)
	}
	track := func(id uint32, kind string) []byte {
		tkhd, hdlr := make([]byte, 16), make([]byte, 12)
		binary.BigEndian.PutUint32(tkhd[12:], id)
		copy(hdlr[8:], kind)
		return box("trak", append(box("tkhd", tkhd), box("mdia", box("hdlr", hdlr))...))
	}
	init := box("moov", append(track(1, "vide"), track(2, "soun")...))
	timing := func(id, samples uint32) []byte {
		tfhd, trun := make([]byte, 8), make([]byte, 8)
		binary.BigEndian.PutUint32(tfhd[4:], id)
		binary.BigEndian.PutUint32(trun[4:], samples)
		return box("traf", append(box("tfhd", tfhd), box("trun", trun)...))
	}
	moof := box("moof", append(timing(1, 1), timing(2, 0)...))
	if video, audio := FragmentMediaKinds(moof, init); video || audio {
		t.Fatal("empty media payload established media presence")
	}
	segment := append(moof, box("mdat", []byte{1, 2, 3})...)
	if video, audio := FragmentMediaKinds(segment, init); !video || audio {
		t.Fatalf("expected video sample only, got video=%t audio=%t", video, audio)
	}
}
