package diagnostic

import (
	"context"
	"time"
)

func (s *Service) AnalyzeContentQuality(parent context.Context, id string) (ContentQualityReport, error) {
	s.mu.Lock()
	if s.closed || s.contentQualityJobs[id] != nil || len(s.contentQualityJobs) >= 2 {
		s.mu.Unlock()
		return ContentQualityReport{}, ErrBusy
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	s.contentQualityJobs[id] = cancel
	s.workers.Add(1)
	s.mu.Unlock()
	defer func() {
		s.workers.Done()
		cancel()
		s.mu.Lock()
		delete(s.contentQualityJobs, id)
		s.mu.Unlock()
	}()

	detail, err := s.Detail(id)
	if err != nil {
		return ContentQualityReport{}, err
	}
	if detail.Report == nil || detail.Status == "running" {
		return ContentQualityReport{}, ErrPending
	}
	if detail.Report.ContentQuality != nil && detail.Report.ContentQuality.SourceArtifact == detail.Report.Manifest.Stream {
		return *detail.Report.ContentQuality, nil
	}
	quality, err := analyzeContentQuality(ctx, s.runDir(id), *detail.Report)
	if err != nil {
		return ContentQualityReport{}, err
	}
	s.persistence.Lock()
	defer s.persistence.Unlock()
	current, err := readJSONFile[Report](s.runDir(id), reportName)
	if err != nil {
		return ContentQualityReport{}, err
	}
	if current.Manifest.Stream != detail.Report.Manifest.Stream {
		return ContentQualityReport{}, ErrReportChanged
	}
	current.ContentQuality = &quality
	if err := persistReport(s.runDir(id), current); err != nil {
		return ContentQualityReport{}, err
	}
	return quality, nil
}
