package diagnostic

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const maxSamples = 4
const explanationName = "explanation.json"

type Service struct {
	root   string
	db     *sql.DB
	client *http.Client

	modelMu sync.Mutex
	model   *modelSelection

	mu                 sync.Mutex
	active             map[string]context.CancelFunc
	closed             bool
	workers            sync.WaitGroup
	close              sync.Once
	closeErr           error
	imports            chan struct{}
	persistence        sync.RWMutex
	players            map[string]*playerSessionState
	investigationJobs  map[string]context.CancelFunc
	investigationSlots chan struct{}
	investigationModel sync.Mutex
}

type modelSelection struct {
	explainer Explainer
	identity  ModelIdentity
	cancel    context.CancelFunc
}

func Open(baseDir string, model Explainer) (*Service, error) {
	root := filepath.Join(baseDir, "diagnostics")
	if err := os.MkdirAll(filepath.Join(root, "runs"), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "index.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := initRunIndex(db); err != nil {
		db.Close()
		return nil, err
	}

	s := &Service{
		root: root, db: db, client: captureClient(),
		active: make(map[string]context.CancelFunc), imports: make(chan struct{}, 2),
		players:           make(map[string]*playerSessionState),
		investigationJobs: make(map[string]context.CancelFunc), investigationSlots: make(chan struct{}, 2),
	}
	s.SetModel(model)
	if err := s.recoverRuns(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.recoverInvestigations(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func initRunIndex(db *sql.DB) error {
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS runs (
		id TEXT PRIMARY KEY,
		mode TEXT NOT NULL,
		protocol TEXT NOT NULL DEFAULT 'http-flv',
		status TEXT NOT NULL,
		origin TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		error TEXT NOT NULL,
		report_ready INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS runs_created_at ON runs(created_at DESC);`); err != nil {
		return err
	}
	var hasProtocol bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM pragma_table_info('runs') WHERE name = 'protocol')`).Scan(&hasProtocol); err != nil {
		return err
	}
	if !hasProtocol {
		if _, err := tx.Exec(`ALTER TABLE runs ADD COLUMN protocol TEXT NOT NULL DEFAULT 'http-flv'`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) Close() error {
	s.close.Do(func() {
		s.SetModel(nil)
		s.mu.Lock()
		s.closed = true
		for _, cancel := range s.active {
			cancel()
		}
		for _, state := range s.players {
			state.session.Close()
		}
		for _, cancel := range s.investigationJobs {
			cancel()
		}
		s.mu.Unlock()
		s.workers.Wait()
		s.persistence.Lock()
		for id, state := range s.players {
			if state.session.hls == nil {
				continue
			}
			dir := s.runDir(id)
			report, err := readJSONFile[Report](dir, reportName)
			if err != nil || report.Player == nil {
				continue
			}
			record, err := readPlayerRecording(dir)
			previousNetwork := playerNetworkRevision(report)
			if err == nil {
				err = persistPlayerNetwork(dir, &report, state.session.hls.network.snapshot(record.Revision, record.Complete), state.created)
			}
			if err == nil && previousNetwork != playerNetworkRevision(report) {
				if removeErr := os.Remove(filepath.Join(dir, explanationName)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					err = removeErr
				}
			}
			if err == nil {
				err = persistReport(dir, report)
			}
			s.closeErr = errors.Join(s.closeErr, err)
		}
		s.persistence.Unlock()
		s.client.CloseIdleConnections()
		s.closeErr = errors.Join(s.closeErr, s.db.Close())
	})
	return s.closeErr
}

// SetModel replaces the model used for subsequent explanations. An explanation
// that was still using the previous selection is canceled and cannot be saved.
func (s *Service) SetModel(explainer Explainer) {
	var model *modelSelection
	if explainer != nil {
		model = &modelSelection{explainer: explainer, identity: explainer.Identity()}
	}
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if s.model != nil && s.model.cancel != nil {
		s.model.cancel()
	}
	s.model = model
}

func (s *Service) ModelConfigured() bool {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	return s.model != nil
}

func (s *Service) ModelIdentity() ModelIdentity {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if s.model == nil {
		return ModelIdentity{}
	}
	return s.model.identity
}

func (s *Service) Start(req SampleRequest) (Run, error) {
	protocol := req.Protocol
	if protocol == "" {
		protocol = ProtocolHTTPFLV
	}
	transport, err := sampleTransport(protocol, req.Transport)
	if err != nil {
		return Run{}, err
	}
	target, err := parseSampleTarget(req.URL, protocol)
	if err != nil {
		return Run{}, err
	}
	seconds := req.Seconds
	if seconds == 0 {
		seconds = defaultSeconds
	}
	if seconds < 1 || seconds > maxSeconds {
		return Run{}, fmt.Errorf("%w: 采样时长须为 1 至 %d 秒", ErrInvalid, maxSeconds)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.active) >= maxSamples {
		return Run{}, ErrBusy
	}
	run := newRun("sample", protocol, origin(target))
	dir := s.runDir(run.ID)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return Run{}, err
	}
	if err := s.insertRun(run); err != nil {
		_ = os.Remove(dir)
		return Run{}, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	var player *PlayerSession
	if req.Playback && protocol != ProtocolRTSP && protocol != ProtocolRTSPS {
		session := newPlayerSession(context.Background(), protocol, target, s.client)
		if session.hls != nil {
			session.hls.network.setOrigin(run.CreatedAt)
		}
		player = session
		s.prunePlayersLocked()
		state := &playerSessionState{session: session, token: rand.Text(), created: run.CreatedAt}
		s.players[run.ID] = state
		run.Player = playerStart(run.ID, state)
		ctx = withPlayerSession(ctx, session)
	}
	s.active[run.ID] = cancel
	s.workers.Add(1)
	go func(run Run) {
		defer s.workers.Done()
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.active, run.ID)
			s.mu.Unlock()
		}()

		if player != nil && player.Transport() != "hls" {
			player.WaitForSubscriber(ctx, 500*time.Millisecond)
		}
		report, err := s.sampleProtocolWithTransport(ctx, dir, target, seconds, run.Protocol, transport)
		if err == nil {
			s.persistence.Lock()
			err = s.attachPlayerReport(dir, &report, run.ID)
			if err == nil {
				err = persistReport(dir, report)
				if err == nil && report.Player != nil {
					cleanupPlayerVersions(dir, report.Player.Revision)
				}
			}
			s.persistence.Unlock()
		}
		s.mu.Lock()
		if state := s.players[run.ID]; state != nil {
			state.session.EndMedia()
		}
		s.mu.Unlock()
		if err != nil {
			slog.Error("save diagnostic", "run_id", run.ID, "protocol", run.Protocol, "error", err)
			run.Status, run.Error = "failed", "证据或报告保存失败"
		} else {
			run.Status, run.ReportReady = reportStatus(report), true
		}
		if err := s.updateRun(run); err != nil {
			slog.Error("update diagnostic status", "run_id", run.ID, "error", err)
		}
	}(run)

	return run, nil
}

func (s *Service) sampleProtocol(ctx context.Context, dir string, target *url.URL, seconds int, protocol string) (Report, error) {
	return s.sampleProtocolWithTransport(ctx, dir, target, seconds, protocol, "tcp")
}

func sampleTransport(protocol, requested string) (string, error) {
	if protocol != ProtocolRTSP && protocol != ProtocolRTSPS {
		if requested != "" {
			return "", fmt.Errorf("%w: transport 参数仅适用于 RTSP/RTSPS", ErrInvalid)
		}
		return "", nil
	}
	if requested == "" {
		requested = "tcp"
	}
	if requested != "tcp" && requested != "udp" {
		return "", fmt.Errorf("%w: RTSP 传输仅支持 tcp 或 udp", ErrInvalid)
	}
	if protocol == ProtocolRTSPS && requested == "udp" {
		return "", fmt.Errorf("%w: RTSPS 当前使用 TLS/TCP，请选择 TCP 交织传输", ErrInvalid)
	}
	return requested, nil
}

func (s *Service) sampleProtocolWithTransport(ctx context.Context, dir string, target *url.URL, seconds int, protocol, transport string) (Report, error) {
	switch protocol {
	case ProtocolHTTPFLV:
		return sample(ctx, s.client, dir, target, seconds)
	case ProtocolRTMP:
		return sampleRTMP(ctx, dir, target, seconds)
	case ProtocolHLS:
		return sampleHLS(ctx, s.client, dir, target, seconds)
	case ProtocolHTTPTS, ProtocolUDPTS, ProtocolRTPTS:
		return sampleTS(ctx, s.client, dir, target, seconds, protocol)
	case ProtocolRTSP, ProtocolRTSPS:
		return sampleRTSPTransport(ctx, dir, target, seconds, protocol, transport)
	default:
		return Report{}, fmt.Errorf("%w: 不支持的采样协议", ErrInvalid)
	}
}

func (s *Service) Stop(id string) (Run, error) {
	s.mu.Lock()
	if cancel := s.active[id]; cancel != nil {
		cancel()
	}
	s.mu.Unlock()
	return s.getRun(id)
}

func (s *Service) Import(source io.Reader) (Run, error) {
	select {
	case s.imports <- struct{}{}:
		defer func() { <-s.imports }()
	default:
		return Run{}, ErrBusy
	}
	run := newRun("replay", "", "")
	dir := s.runDir(run.ID)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return Run{}, err
	}
	committed := false
	defer func() {
		if !committed {
			if err := os.RemoveAll(dir); err != nil {
				slog.Error("clean up diagnostic import", "run_id", run.ID, "error", err)
			}
		}
	}()

	report, err := readBundle(dir, source)
	if err != nil {
		return Run{}, err
	}
	if err := persistReport(dir, report); err != nil {
		return Run{}, err
	}
	run.Origin = report.Manifest.Observation.Origin
	run.Protocol = report.Manifest.Protocol
	run.Status, run.ReportReady = reportStatus(report), true
	if err := s.insertRun(run); err != nil {
		return Run{}, err
	}
	committed = true
	return run, nil
}

func (s *Service) Detail(id string) (Detail, error) {
	s.persistence.RLock()
	defer s.persistence.RUnlock()
	return s.detail(id)
}

func (s *Service) detail(id string) (Detail, error) {
	run, err := s.getRun(id)
	if err != nil {
		return Detail{}, err
	}
	detail := Detail{Run: run}
	if !run.ReportReady {
		return detail, nil
	}
	dir := s.runDir(run.ID)
	report, err := readJSONFile[Report](dir, reportName)
	if err != nil {
		return Detail{}, err
	}
	detail.Report = &report
	detail.DumpName = dumpName(run.Protocol)
	explanation, err := readJSONFile[Explanation](dir, explanationName)
	switch {
	case err == nil:
		detail.Explanation = &explanation
	case !errors.Is(err, os.ErrNotExist):
		return Detail{}, err
	}
	return detail, nil
}

func (s *Service) Explain(ctx context.Context, id string) (Explanation, error) {
	return s.explain(ctx, id, false)
}

// RefreshExplanation uses the current model even if the run already has an
// explanation from a model with the same provider and name.
func (s *Service) RefreshExplanation(ctx context.Context, id string) (Explanation, error) {
	return s.explain(ctx, id, true)
}

func (s *Service) explain(ctx context.Context, id string, refresh bool) (Explanation, error) {
	detail, err := s.Detail(id)
	if err != nil {
		return Explanation{}, err
	}
	if detail.Report == nil {
		return Explanation{}, ErrPending
	}
	s.modelMu.Lock()
	model := s.model
	if model == nil {
		s.modelMu.Unlock()
		if !refresh && detail.Explanation != nil {
			return *detail.Explanation, nil
		}
		return Explanation{}, ErrUnconfigured
	}
	if !refresh && detail.Explanation != nil && model.identity.matches(*detail.Explanation) {
		s.modelMu.Unlock()
		return *detail.Explanation, nil
	}
	if model.cancel != nil {
		s.modelMu.Unlock()
		return Explanation{}, ErrBusy
	}
	ctx, cancel := context.WithCancel(ctx)
	model.cancel = cancel
	s.modelMu.Unlock()
	defer func() {
		cancel()
		s.modelMu.Lock()
		model.cancel = nil
		s.modelMu.Unlock()
	}()

	dir := s.runDir(detail.ID)
	if !refresh {
		existing, err := readJSONFile[Explanation](dir, explanationName)
		if err == nil && model.identity.matches(existing) {
			return existing, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Explanation{}, err
		}
	}
	explanation, err := model.explainer.Explain(ctx, *detail.Report)
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if s.model != model {
		return Explanation{}, ErrModelChanged
	}
	if err != nil {
		return Explanation{}, err
	}
	if !validExplanation(explanation, detail.Report.Findings) {
		return Explanation{}, ErrProvider
	}
	s.persistence.Lock()
	defer s.persistence.Unlock()
	current, err := readJSONFile[Report](dir, reportName)
	if err != nil {
		return Explanation{}, err
	}
	if playerRevision(current.Player) != playerRevision(detail.Report.Player) || investigationRevision(current) != investigationRevision(*detail.Report) || playerNetworkRevision(current) != playerNetworkRevision(*detail.Report) {
		return Explanation{}, ErrReportChanged
	}
	if err := writeJSONFile(dir, explanationName, explanation); err != nil {
		return Explanation{}, err
	}
	return explanation, nil
}

func (s *Service) WriteBundle(id string, w io.Writer) error {
	s.persistence.RLock()
	detail, err := s.detail(id)
	if err != nil {
		s.persistence.RUnlock()
		return err
	}
	if detail.Report == nil {
		s.persistence.RUnlock()
		return ErrPending
	}
	dir := s.runDir(detail.ID)
	var player []byte
	if detail.Report.Player != nil {
		player, err = os.ReadFile(filepath.Join(dir, playerVersionName(detail.Report.Player.Revision)))
	}
	var ledger []byte
	if err == nil && detail.Report.Manifest.StallEvidence != nil {
		ledger, err = readStallLedger(dir, *detail.Report.Manifest.StallEvidence)
	}
	var network []byte
	if err == nil && detail.Report.Manifest.PlayerNetwork != nil {
		network, err = readPlayerNetworkArtifact(dir, *detail.Report.Manifest.PlayerNetwork)
	}
	// Snapshot the bounded recording before its revision can be cleaned up.
	// A slow download must not hold up other recordings or capture reports.
	s.persistence.RUnlock()
	if err != nil {
		return err
	}
	if player != nil {
		return writeBundleSnapshot(w, dir, *detail.Report, bytes.NewReader(player), ledger, network)
	}
	return writeBundleSnapshot(w, dir, *detail.Report, nil, ledger, network)
}

func (s *Service) OpenDump(id string) (*os.File, error) {
	run, err := s.getRun(id)
	if err != nil {
		return nil, err
	}
	if !run.ReportReady {
		return nil, ErrPending
	}
	name := dumpName(run.Protocol)
	if name == "" {
		return nil, ErrNotFound
	}
	return os.Open(filepath.Join(s.runDir(run.ID), name))
}

func (s *Service) List() ([]Run, error) {
	rows, err := s.db.Query(runColumns + " ORDER BY created_at DESC, id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	return scanRuns(rows)
}

func (s *Service) recoverRuns() error {
	rows, err := s.db.Query(runColumns + " WHERE status = 'running'")
	if err != nil {
		return err
	}
	runs, err := scanRuns(rows)
	if err != nil {
		return err
	}
	for _, run := range runs {
		report, err := readJSONFile[Report](s.runDir(run.ID), reportName)
		switch {
		case err == nil:
			run.Status, run.ReportReady = reportStatus(report), true
		case errors.Is(err, os.ErrNotExist):
			run.Status, run.Error = "interrupted", "服务在报告生成前停止"
		default:
			return err
		}
		if err := s.updateRun(run); err != nil {
			return err
		}
	}
	return nil
}

const runColumns = "SELECT id, mode, protocol, status, origin, created_at, error, report_ready FROM runs"

func (s *Service) getRun(id string) (Run, error) {
	run, err := scanRun(s.db.QueryRow(runColumns+" WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	return run, err
}

type rowScanner interface {
	Scan(...any) error
}

func scanRun(row rowScanner) (Run, error) {
	var run Run
	var created int64
	err := row.Scan(&run.ID, &run.Mode, &run.Protocol, &run.Status, &run.Origin, &created, &run.Error, &run.ReportReady)
	run.CreatedAt = time.UnixMilli(created).UTC()
	return run, err
}

func scanRuns(rows *sql.Rows) ([]Run, error) {
	defer rows.Close()
	runs := []Run{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Service) insertRun(run Run) error {
	_, err := s.db.Exec(`INSERT INTO runs(id, mode, protocol, status, origin, created_at, error, report_ready)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, run.ID, run.Mode, run.Protocol, run.Status, run.Origin, run.CreatedAt.UnixMilli(), run.Error, run.ReportReady)
	return err
}

func (s *Service) updateRun(run Run) error {
	_, err := s.db.Exec("UPDATE runs SET status = ?, error = ?, report_ready = ? WHERE id = ?", run.Status, run.Error, run.ReportReady, run.ID)
	return err
}

func (s *Service) runDir(id string) string {
	return filepath.Join(s.root, "runs", id)
}

func newRun(mode, protocol, origin string) Run {
	var id [16]byte
	_, _ = rand.Read(id[:])
	return Run{ID: hex.EncodeToString(id[:]), Mode: mode, Protocol: protocol, Status: "running", Origin: origin, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
}

func reportStatus(report Report) string {
	reason := report.Manifest.Observation.EndReason
	if reason == "canceled" {
		return "canceled"
	}
	samples := report.Metrics.VideoFrames + report.Metrics.AudioFrames
	if report.HLS != nil {
		samples = report.HLS.SegmentsCaptured
	}
	if report.MPEGTS != nil {
		samples = report.MPEGTS.Packets
	}
	if report.RTSP != nil {
		samples = report.RTSP.RTPPackets
	}
	if samples == 0 {
		return "failed"
	}
	if hls := report.HLS; hls != nil {
		if hls.SegmentsFailed+hls.InitSegmentsFailed+hls.PlaylistFailures > 0 {
			return "partial"
		}
	}
	switch reason {
	case "eof", "duration_limit":
		return "done"
	default:
		return "partial"
	}
}

func persistReport(dir string, report Report) error {
	if err := writeJSONFile(dir, manifestName, report.Manifest); err != nil {
		return err
	}
	return writeJSONFile(dir, reportName, report)
}

func writeJSONFile(dir, name string, data any) error {
	tmp, err := os.CreateTemp(dir, ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := errors.Join(enc.Encode(data), tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

func readJSONFile[T any](dir, name string) (T, error) {
	var result T
	file, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return result, err
	}
	defer file.Close()
	err = json.NewDecoder(file).Decode(&result)
	return result, err
}
