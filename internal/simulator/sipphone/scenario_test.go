package sipphone

import (
	"context"
	"testing"
	"time"

	"github.com/pion/rtp"
)

func TestScenarioPresetVerifiesOppositeDirectionAndRestores(t *testing.T) {
	for _, sustained := range []bool{true, false} {
		name := "continued reception"
		if !sustained {
			name = "opposite direction also silent"
		}
		t.Run(name, func(t *testing.T) {
			cfg := Config{Preset: "one-way-send", PresetDelayMS: 200, PresetDurationMS: 700}
			if err := cfg.validatePreset(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			a := &agent{cfg: cfg}
			c := &phoneCall{status: CallStatus{Scenario: &ScenarioReport{}}, audio: &audioSession{}}
			done := make(chan struct{})
			go func() {
				defer close(done)
				tick := time.NewTicker(20 * time.Millisecond)
				defer tick.Stop()
				var header rtp.Header
				for {
					select {
					case <-ctx.Done():
						return
					case now := <-tick.C:
						header.SequenceNumber++
						header.Timestamp += 160
						muted := audioFault(c.audio.fault.Load()) == audioFaultSend
						if muted {
							c.audio.sent.drop()
						} else {
							c.audio.sent.observe(header, now)
						}
						if sustained || !muted {
							c.audio.received.observe(header, now)
						}
					}
				}
			}()
			a.runPreset(ctx, c, audioParameters{send: true, recv: true})
			cancel()
			<-done
			actual := c.status.Scenario
			want := "passed"
			if !sustained {
				want = "failed"
			}
			if actual.State != want || actual.Dropped == 0 || actual.RestoredAt.IsZero() ||
				audioFault(c.audio.fault.Load()) != audioFaultOff {
				t.Fatalf("unexpected result: %+v", actual)
			}
		})
	}
}
