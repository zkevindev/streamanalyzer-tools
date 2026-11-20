package diagnostic

import (
	"fmt"
	"time"
)

const maxBatchSamples = 4

type BatchRequest struct {
	Samples []SampleRequest `json:"samples"`
}

type BatchComparison struct {
	BaselineID string           `json:"baseline_id"`
	CurrentID  string           `json:"current_id"`
	Comparison ReportComparison `json:"comparison"`
}

type BatchResult struct {
	ID          string            `json:"id"`
	CreatedAt   time.Time         `json:"created_at"`
	Status      string            `json:"status"`
	Runs        []Run             `json:"runs"`
	Warnings    []string          `json:"warnings"`
	Comparisons []BatchComparison `json:"comparisons"`
}

type diagnosticBatch struct {
	id        string
	createdAt time.Time
	runIDs    []string
}

func (s *Service) StartBatch(req BatchRequest) (BatchResult, error) {
	if len(req.Samples) < 2 || len(req.Samples) > maxBatchSamples {
		return BatchResult{}, fmt.Errorf("%w: 批量诊断须包含 2 至 %d 路地址", ErrInvalid, maxBatchSamples)
	}
	for _, sample := range req.Samples {
		if sample.Playback || sample.AutoInvestigate {
			return BatchResult{}, fmt.Errorf("%w: 批量诊断不启用播放器观测，请使用单路采样分析播放体验", ErrInvalid)
		}
	}
	batch := &diagnosticBatch{id: newRun("batch", "", "").ID, createdAt: time.Now().UTC()}
	runs := make([]Run, 0, len(req.Samples))
	for _, sample := range req.Samples {
		run, err := s.Start(sample)
		if err != nil {
			for _, started := range runs {
				_, _ = s.Stop(started.ID)
			}
			return BatchResult{}, err
		}
		runs = append(runs, run)
		batch.runIDs = append(batch.runIDs, run.ID)
	}
	s.mu.Lock()
	if s.batches == nil {
		s.batches = make(map[string]*diagnosticBatch)
	}
	s.batches[batch.id] = batch
	if len(s.batches) > 20 {
		var oldest *diagnosticBatch
		for _, candidate := range s.batches {
			if oldest == nil || candidate.createdAt.Before(oldest.createdAt) {
				oldest = candidate
			}
		}
		delete(s.batches, oldest.id)
	}
	s.mu.Unlock()
	return BatchResult{ID: batch.id, CreatedAt: batch.createdAt, Status: "running", Runs: runs, Warnings: []string{}, Comparisons: []BatchComparison{}}, nil
}

func (s *Service) Batch(id string) (BatchResult, error) {
	s.mu.Lock()
	batch := s.batches[id]
	s.mu.Unlock()
	if batch == nil {
		return BatchResult{}, ErrNotFound
	}
	result := BatchResult{ID: batch.id, CreatedAt: batch.createdAt, Status: "done", Runs: []Run{}, Warnings: []string{}, Comparisons: []BatchComparison{}}
	details := make([]Detail, 0, len(batch.runIDs))
	protocol := ""
	mixedProtocols := false
	for _, runID := range batch.runIDs {
		detail, err := s.Detail(runID)
		if err != nil {
			return BatchResult{}, err
		}
		details = append(details, detail)
		result.Runs = append(result.Runs, detail.Run)
		if protocol == "" {
			protocol = detail.Protocol
		} else if detail.Protocol != protocol {
			mixedProtocols = true
		}
		switch detail.Status {
		case "running":
			result.Status = "running"
		case "failed", "partial", "interrupted":
			if result.Status != "running" {
				result.Status = "partial"
			}
		}
	}
	if mixedProtocols {
		result.Warnings = append(result.Warnings, "批次包含不同协议，仅对相同协议的线路生成自动对比。")
	}
	if result.Status == "running" {
		return result, nil
	}
	var baseline *Detail
	for index := range details {
		if details[index].Report != nil {
			baseline = &details[index]
			break
		}
	}
	if baseline == nil {
		result.Warnings = append(result.Warnings, "本批次没有可用报告，无法生成基线对比。")
		return result, nil
	}
	for index := range details {
		current := &details[index]
		if current.ID == baseline.ID || current.Report == nil || current.Protocol != baseline.Protocol {
			continue
		}
		comparison, err := s.Compare(current.ID, baseline.ID)
		if err != nil {
			return BatchResult{}, err
		}
		result.Comparisons = append(result.Comparisons, BatchComparison{BaselineID: baseline.ID, CurrentID: current.ID, Comparison: comparison})
	}
	return result, nil
}

func (s *Service) StopBatch(id string) (BatchResult, error) {
	s.mu.Lock()
	batch := s.batches[id]
	s.mu.Unlock()
	if batch == nil {
		return BatchResult{}, ErrNotFound
	}
	for _, runID := range batch.runIDs {
		if _, err := s.Stop(runID); err != nil {
			return BatchResult{}, err
		}
	}
	return s.Batch(id)
}
