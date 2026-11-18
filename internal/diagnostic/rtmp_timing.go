package diagnostic

import (
	"context"
	"net/http/httptrace"
	"slices"
	"sync"
	"time"

	"github.com/yutopp/go-rtmp"
)

type rtmpSetupTiming struct {
	mu           sync.Mutex
	start        time.Time
	stages       []CaptureStage
	active       map[string]int
	failureStage string
	failureCode  string
	done         bool
}

func (t *rtmpSetupTiming) begin(name, key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done || len(t.stages) >= 64 {
		return
	}
	t.active[key] = len(t.stages)
	t.stages = append(t.stages, CaptureStage{Name: name, StartMS: time.Since(t.start).Milliseconds(), EndMS: -1})
}

func (t *rtmpSetupTiming) end(key string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return
	}
	if index, ok := t.active[key]; ok {
		stage := &t.stages[index]
		stage.EndMS = time.Since(t.start).Milliseconds()
		t.recordFailure(stage.Name, err)
		delete(t.active, key)
	}
}

func (t *rtmpSetupTiming) recordFailure(stage string, err error) {
	if err == nil {
		return
	}
	t.failureStage, t.failureCode = stage, modelNetworkError(err, 0).Code
	if t.failureCode == "network_error" && stage != "connect" && stage != "dns" {
		t.failureCode = "rtmp_error"
	}
}

func (t *rtmpSetupTiming) context(ctx context.Context) context.Context {
	t.active = make(map[string]int)
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		DNSStart:     func(httptrace.DNSStartInfo) { t.begin("dns", "dns") },
		DNSDone:      func(info httptrace.DNSDoneInfo) { t.end("dns", info.Err) },
		ConnectStart: func(network, address string) { t.begin("connect", network+"\x00"+address) },
		ConnectDone:  func(network, address string, err error) { t.end(network+"\x00"+address, err) },
	})
}

func (t *rtmpSetupTiming) completed(stage rtmp.PlayStage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done || len(t.stages) >= 64 {
		return
	}
	t.stages = append(t.stages, CaptureStage{
		Name: stage.Name, StartMS: stage.StartedAt.Sub(t.start).Milliseconds(), EndMS: stage.EndedAt.Sub(t.start).Milliseconds(),
	})
	t.recordFailure(stage.Name, stage.Err)
}

func (t *rtmpSetupTiming) finish(observation *Observation, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.done = true
	for index := range t.stages {
		if t.stages[index].EndMS < 0 {
			t.stages[index].EndMS = time.Since(t.start).Milliseconds()
		}
	}
	slices.SortStableFunc(t.stages, func(a, b CaptureStage) int {
		if a.StartMS < b.StartMS {
			return -1
		}
		if a.StartMS > b.StartMS {
			return 1
		}
		return 0
	})
	observation.Stages = t.stages
	if err != nil {
		observation.FailureStage, observation.FailureCode = t.failureStage, t.failureCode
		if observation.FailureStage == "" {
			observation.FailureStage, observation.FailureCode = "connect", modelNetworkError(err, 0).Code
		}
	}
}
