package diagnostic

import (
	"math"

	"github.com/pion/rtcp"
)

type RTSPSenderClock struct {
	TrackID               int     `json:"track_id"`
	SSRC                  uint32  `json:"ssrc"`
	Samples               int     `json:"samples"`
	Intervals             int     `json:"intervals"`
	OffsetMS              float64 `json:"offset_ms"`
	MaxStepMS             float64 `json:"max_step_ms"`
	MaxArrivalVariationMS float64 `json:"max_arrival_variation_ms"`
	MaxStepAtMS           int64   `json:"max_step_at_ms"`

	previousNTP     uint64
	previousRTP     uint32
	previousArrival int64
	evidence        Event
}

func observeRTSPClocks(metrics *RTSPMetrics, states []rtspTrackState, media int, packets []rtcp.Packet, event Event) {
	for _, packet := range packets {
		sr, ok := packet.(*rtcp.SenderReport)
		if !ok || sr.NTPTime == 0 {
			continue
		}
		index := rtspClockTrack(metrics.Tracks, states, media, sr.SSRC)
		if index < 0 {
			continue
		}
		track := metrics.Tracks[index]
		var clock *RTSPSenderClock
		for i := range metrics.SenderClocks {
			current := &metrics.SenderClocks[i]
			if current.TrackID == track.ID && current.SSRC == sr.SSRC {
				clock = current
				break
			}
		}
		if clock == nil {
			if len(metrics.SenderClocks) >= 32 {
				continue
			}
			metrics.SenderClocks = append(metrics.SenderClocks, RTSPSenderClock{TrackID: track.ID, SSRC: sr.SSRC})
			clock = &metrics.SenderClocks[len(metrics.SenderClocks)-1]
		}
		clock.observe(sr, track.ClockRate, event)
	}
}

func rtspClockTrack(tracks []RTSPTrackMetrics, states []rtspTrackState, media int, ssrc uint32) int {
	selected, matches := -1, 0
	for index, track := range tracks {
		if track.MediaID != media || track.ClockRate <= 0 {
			continue
		}
		if _, ok := states[index].ssrcs[ssrc]; ok {
			return index
		}
		selected = index
		matches++
	}
	if matches == 1 {
		return selected
	}
	return -1
}

func (clock *RTSPSenderClock) observe(sr *rtcp.SenderReport, rate int, event Event) {
	if clock.Samples > 0 {
		if sr.NTPTime == clock.previousNTP && sr.RTPTime == clock.previousRTP {
			return
		}
		rtpMS := float64(int32(sr.RTPTime-clock.previousRTP)) * 1000 / float64(rate)
		ntpMS := float64(int64(sr.NTPTime-clock.previousNTP)) * 1000 / 4294967296
		if rtpMS > 0 && rtpMS <= 60000 {
			step := ntpMS - rtpMS
			clock.Intervals++
			clock.OffsetMS += step
			variation := float64(event.ArrivalMS-clock.previousArrival) - rtpMS
			clock.MaxArrivalVariationMS = max(clock.MaxArrivalVariationMS, math.Abs(variation))
			if math.Abs(step) > math.Abs(clock.MaxStepMS) {
				clock.MaxStepMS, clock.MaxStepAtMS, clock.evidence = step, event.ArrivalMS, event
			}
		}
	}
	clock.Samples++
	clock.previousNTP, clock.previousRTP, clock.previousArrival = sr.NTPTime, sr.RTPTime, event.ArrivalMS
}
