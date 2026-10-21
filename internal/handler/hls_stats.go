package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"streamanalyzer/internal/hlsdelivery/analyze"

	"github.com/gin-gonic/gin"
)

const (
	hlsStatsMaxJobs    = 50
	hlsStatsMaxRun     = 10 * time.Minute
	hlsStatsMaxTimeout = time.Minute
)

var errHLSStatsFull = errors.New("同时运行的任务已达上限")

type hlsStatsRequest struct {
	Command             string            `json:"command"`
	URL                 string            `json:"url"`
	URLB                string            `json:"url_b"`
	Segments            int               `json:"segments"`
	Variant             *int              `json:"variant"`
	AllVariants         bool              `json:"all_variants"`
	ParseSegments       bool              `json:"parse_segments"`
	Duration            string            `json:"duration"`
	Timeout             string            `json:"timeout"`
	Insecure            bool              `json:"insecure"`
	ErrorThreshold      float64           `json:"error_threshold"`
	LatencyThreshold    float64           `json:"latency_threshold"`
	ThroughputThreshold float64           `json:"throughput_threshold"`
	Headers             map[string]string `json:"headers"`
}

type hlsStatsView struct {
	ID       string    `json:"id"`
	Status   string    `json:"status"`
	Command  string    `json:"command"`
	URL      string    `json:"url"`
	ExitCode int       `json:"exit_code"`
	Error    string    `json:"error,omitempty"`
	Report   any       `json:"report,omitempty"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitempty"`
}

type hlsStatsJob struct {
	hlsStatsView
	cancel context.CancelFunc
}

type hlsStatsStore struct {
	mu    sync.Mutex
	jobs  map[string]*hlsStatsJob
	order []string
}

func (s *hlsStatsStore) put(job *hlsStatsJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobs == nil {
		s.jobs = map[string]*hlsStatsJob{}
	}
	for len(s.order) >= hlsStatsMaxJobs {
		evicted := false
		for i, id := range s.order {
			prev := s.jobs[id]
			if prev != nil && prev.Status == "running" {
				continue
			}
			s.order = append(s.order[:i], s.order[i+1:]...)
			delete(s.jobs, id)
			evicted = true
			break
		}
		if !evicted {
			return errHLSStatsFull
		}
	}
	s.jobs[job.ID] = job
	s.order = append(s.order, job.ID)
	return nil
}

func (s *hlsStatsStore) get(id string) (hlsStatsView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[id]
	if job == nil {
		return hlsStatsView{}, false
	}
	return job.hlsStatsView, true
}

func (s *hlsStatsStore) finish(id, status string, exitCode int, errMsg string, report any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[id]
	if job == nil {
		return
	}
	job.Status = status
	job.ExitCode = exitCode
	job.Error = errMsg
	job.Report = report
	job.Finished = time.Now().UTC()
}

func (s *hlsStatsStore) cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[id]
	if job == nil || job.cancel == nil {
		return job != nil
	}
	job.cancel()
	return true
}

func (h *Handler) CreateHLSStatsTask(c *gin.Context) {
	var req hlsStatsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体不是 JSON"})
		return
	}
	if req.Command == "" {
		req.Command = "analyze"
	}
	opt, err := req.options()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "需要 url"})
		return
	}
	if req.Command == "compare" && req.URLB == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "compare 需要 url_b"})
		return
	}
	if err := analyze.ValidateURL(req.URL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.URLB != "" {
		if err := analyze.ValidateURL(req.URLB); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	job := &hlsStatsJob{
		hlsStatsView: hlsStatsView{
			ID:      newHLSStatsID(),
			Status:  "running",
			Command: req.Command,
			URL:     req.URL,
			Started: time.Now().UTC(),
		},
		cancel: cancel,
	}
	if err := h.hlsStats.put(job); err != nil {
		cancel()
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		return
	}

	go h.runHLSStats(ctx, job.ID, req, opt)
	c.JSON(http.StatusOK, gin.H{"id": job.ID, "status": "running"})
}

func (h *Handler) GetHLSStatsTask(c *gin.Context) {
	view, ok := h.hlsStats.get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h *Handler) StopHLSStatsTask(c *gin.Context) {
	if !h.hlsStats.cancel(c.Param("id")) {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "stopping"})
}

func (h *Handler) runHLSStats(ctx context.Context, id string, req hlsStatsRequest, opt analyze.Options) {
	defer h.hlsStats.cancel(id)
	var (
		report   any
		exitCode int
		err      error
	)
	switch req.Command {
	case "manifest":
		var rep *analyze.Report
		rep, err = analyze.Manifest(ctx, req.URL, opt)
		exitCode, report = packReport(rep, err)
	case "monitor":
		var rep *analyze.Report
		rep, err = analyze.Monitor(ctx, req.URL, opt)
		exitCode, report = packReport(rep, err)
	case "compare":
		var rep *analyze.CompareReport
		rep, err = analyze.Compare(ctx, req.URL, req.URLB, opt)
		if rep != nil {
			report = analyze.CompareValue(rep)
			exitCode = rep.ExitCode
		}
		if err != nil {
			exitCode = hlsStatsExit(err)
		}
	default:
		var rep *analyze.Report
		rep, err = analyze.Analyze(ctx, req.URL, opt)
		exitCode, report = packReport(rep, err)
	}
	if err != nil {
		h.hlsStats.finish(id, "failed", exitCode, err.Error(), report)
		return
	}
	h.hlsStats.finish(id, "done", exitCode, "", report)
}

func packReport(rep *analyze.Report, err error) (int, any) {
	code := 0
	if rep != nil {
		code = rep.ExitCode
	}
	if err != nil {
		code = hlsStatsExit(err)
	}
	if rep == nil {
		return code, nil
	}
	return code, analyze.ReportValue(rep)
}

func hlsStatsExit(err error) int {
	var ee *analyze.ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	if err == nil {
		return 0
	}
	return 1
}

func (r hlsStatsRequest) options() (analyze.Options, error) {
	if r.Command == "" {
		r.Command = "analyze"
	}
	switch r.Command {
	case "analyze", "manifest", "monitor", "compare":
	default:
		return analyze.Options{}, errors.New("command 只能是 analyze、manifest、monitor 或 compare")
	}
	opt := analyze.Options{
		Segments:            r.Segments,
		AllVariants:         r.AllVariants,
		ParseSegments:       r.ParseSegments,
		Insecure:            r.Insecure,
		ErrorThreshold:      r.ErrorThreshold,
		LatencyThreshold:    r.LatencyThreshold,
		ThroughputThreshold: r.ThroughputThreshold,
	}
	if r.Variant != nil {
		opt.Variant = *r.Variant
		opt.VariantSet = true
	}
	if r.Duration != "" {
		d, err := time.ParseDuration(r.Duration)
		if err != nil {
			return analyze.Options{}, errors.New("duration 不是合法时长，例如 30s")
		}
		if d <= 0 || d > hlsStatsMaxRun {
			return analyze.Options{}, errors.New("duration 需要大于 0 且不超过 10m")
		}
		opt.Duration = d
	}
	if r.Timeout != "" {
		d, err := time.ParseDuration(r.Timeout)
		if err != nil {
			return analyze.Options{}, errors.New("timeout 不是合法时长，例如 10s")
		}
		if d <= 0 || d > hlsStatsMaxTimeout {
			return analyze.Options{}, errors.New("timeout 需要大于 0 且不超过 1m")
		}
		opt.Timeout = d
	}
	if len(r.Headers) > 0 {
		opt.Headers = make(http.Header, len(r.Headers))
		for k, v := range r.Headers {
			opt.Headers.Set(k, v)
		}
	}
	return opt, nil
}

func newHLSStatsID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format("150405.000000")))
	}
	return hex.EncodeToString(b[:])
}
