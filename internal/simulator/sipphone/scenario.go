package sipphone

import (
	"context"
	"fmt"
	"time"
)

const maxReceiveSilence = 500 * time.Millisecond

type ScenarioReport struct {
	State            string    `json:"state"`
	StartedAt        time.Time `json:"started_at,omitzero"`
	RestoredAt       time.Time `json:"restored_at,omitzero"`
	FinishedAt       time.Time `json:"finished_at,omitzero"`
	Dropped          uint64    `json:"dropped"`
	Received         uint64    `json:"received"`
	LongestSilenceMS int64     `json:"longest_silence_ms"`
	Reason           string    `json:"reason,omitempty"`
}

func (c *Config) validatePreset() error {
	if c.Preset == "" {
		return nil
	}
	if c.PresetDelayMS == 0 {
		c.PresetDelayMS = 3000
	}
	if c.PresetDurationMS == 0 {
		c.PresetDurationMS = 5000
	}
	if c.Preset != "one-way-send" || c.PresetDelayMS < 200 || c.PresetDelayMS > 30000 ||
		c.PresetDurationMS < 300 || c.PresetDurationMS > 30000 {
		return fmt.Errorf("%w，请选择有效的音频预设和时长", ErrConfig)
	}
	return nil
}

// Caller holds agent.mu when reading or writing the call's report.
func (r *ScenarioReport) running() bool {
	return r != nil && (r.State == "waiting" || r.State == "injecting" || r.State == "recovering")
}

func (a *agent) runPreset(ctx context.Context, c *phoneCall, media audioParameters) {
	report := ScenarioReport{State: "waiting"}
	update := func() {
		a.mu.Lock()
		*c.status.Scenario = report
		a.mu.Unlock()
	}
	finish := func(state, reason string) {
		c.audio.fault.Store(uint32(audioFaultOff))
		report.State, report.Reason, report.FinishedAt = state, reason, time.Now()
		update()
	}
	if !media.send || !media.recv {
		finish("failed", "当前 SDP 未协商双向音频")
		return
	}
	if !presetWait(ctx, time.Duration(a.cfg.PresetDelayMS)*time.Millisecond) {
		finish("canceled", "通话在故障触发前结束")
		return
	}
	beforeSend := c.audio.sent.snapshot()
	beforeReceive := c.audio.received.snapshot()
	if beforeSend.Packets == 0 || beforeReceive.Packets == 0 {
		finish("failed", "触发前尚未观测到双向 RTP")
		return
	}

	c.audio.fault.Store(uint32(audioFaultSend))
	report.State, report.StartedAt = "injecting", time.Now()
	update()

	timer := time.NewTimer(time.Duration(a.cfg.PresetDurationMS) * time.Millisecond)
	defer timer.Stop()
	check := time.NewTicker(50 * time.Millisecond)
	defer check.Stop()
	observe := func(now time.Time) {
		last := c.audio.received.snapshot()
		report.Received = last.Packets - beforeReceive.Packets
		report.LongestSilenceMS = max(report.LongestSilenceMS, now.Sub(maxTime(report.StartedAt, last.LastAt)).Milliseconds())
	}
	for report.State == "injecting" {
		select {
		case <-ctx.Done():
			finish("canceled", "通话在故障期间结束")
			return
		case tick := <-check.C:
			observe(tick)
		case end := <-timer.C:
			observe(end)
			c.audio.fault.Store(uint32(audioFaultOff))
			report.State, report.RestoredAt = "recovering", time.Now()
			update()
		}
	}
	restored := c.audio.sent.snapshot()
	report.Dropped = restored.FaultDrops - beforeSend.FaultDrops
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for c.audio.sent.snapshot().Packets <= restored.Packets {
		select {
		case <-ctx.Done():
			finish("canceled", "通话在验证恢复前结束")
			return
		case <-deadline.C:
			finish("failed", "关闭故障后未观察到发送恢复")
			return
		case <-check.C:
		}
	}
	if report.Dropped == 0 || report.Received == 0 || report.LongestSilenceMS > maxReceiveSilence.Milliseconds() {
		finish("failed", "未满足注入丢弃、接收持续与发送恢复的预期")
		return
	}
	finish("passed", "")
}

func presetWait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
