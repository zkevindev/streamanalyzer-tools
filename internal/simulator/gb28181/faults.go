package gb28181

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type FaultConfig struct {
	SilenceAfterMS  int  `json:"silence_after_ms,omitempty"`
	FirstDelayMS    int  `json:"first_delay_ms"`
	StartMS         int  `json:"start_ms"`
	WindowMS        int  `json:"window_ms"`
	DropEvery       int  `json:"drop_every"`
	ReorderEvery    int  `json:"reorder_every"`
	MuteAudio       bool `json:"mute_audio"`
	TimestampJumpMS int  `json:"timestamp_jump_ms"`
}

func (f *FaultConfig) validate() error {
	if f == nil {
		return nil
	}
	if f.SilenceAfterMS < 0 || f.SilenceAfterMS > 30000 || f.FirstDelayMS < 0 || f.FirstDelayMS > 30000 || f.StartMS < 0 || f.StartMS > 600000 ||
		f.WindowMS < 0 || f.WindowMS > 60000 || f.DropEvery < 0 || f.DropEvery > 10000 ||
		f.ReorderEvery < 0 || f.ReorderEvery > 10000 || f.TimestampJumpMS < -30000 || f.TimestampJumpMS > 30000 {
		return errors.New("故障配置超出允许范围")
	}
	if f.WindowMS == 0 {
		f.WindowMS = 5000
	}
	return nil
}

type FaultStats struct {
	DroppedRTP   uint64 `json:"dropped_rtp"`
	ReorderedRTP uint64 `json:"reordered_rtp"`
	MutedAudio   uint64 `json:"muted_audio"`
}

type mediaFaults struct {
	config  *FaultConfig
	origin  int64
	ready   bool
	jumped  bool
	packets packetFaults
}

func (f *mediaFaults) delay(ctx context.Context, mark func(string, string)) error {
	if f.config == nil || f.config.FirstDelayMS == 0 {
		return nil
	}
	mark("fault_delay", fmt.Sprintf("%d ms", f.config.FirstDelayMS))
	timer := time.NewTimer(time.Duration(f.config.FirstDelayMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// apply uses the original media clock so plans repeat across identical sessions.
func (f *mediaFaults) apply(unit *mediaUnit, mark func(string, string)) (inside, mute bool) {
	if f.config == nil {
		return false, false
	}
	if !f.ready {
		f.ready, f.origin = true, unit.dts
	}
	elapsed := unit.dts - f.origin
	start := int64(f.config.StartMS) * 90
	if elapsed < start {
		return false, false
	}
	if f.config.TimestampJumpMS != 0 {
		if !f.jumped {
			f.jumped = true
			mark("fault_timestamp", fmt.Sprintf("%+d ms", f.config.TimestampJumpMS))
		}
		offset := int64(f.config.TimestampJumpMS) * 90
		unit.dts, unit.pts = unit.dts+offset, unit.pts+offset
	}
	inside = elapsed < start+int64(f.config.WindowMS)*90
	return inside, inside && unit.stream == audioStreamID && f.config.MuteAudio
}

type packetFaults struct {
	dropEvery, reorderEvery   uint64
	index, dropped, reordered uint64
}
