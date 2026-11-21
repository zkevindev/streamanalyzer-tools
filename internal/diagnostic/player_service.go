package diagnostic

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrReportChanged = errors.New("diagnostic report changed during explanation")

type playerSessionState struct {
	session *PlayerSession
	token   string
	created time.Time
}

func playerStart(id string, state *playerSessionState) *PlayerStart {
	path := "/api/diagnostics/runs/" + id + "/player/stream"
	if state.session.Transport() == "hls" {
		path = "/api/diagnostics/runs/" + id + "/player/hls/index.m3u8"
	} else if state.session.Transport() == "whep" {
		path = "/api/diagnostics/runs/" + id + "/player/whep"
	}
	return &PlayerStart{
		Token: state.token, Transport: state.session.Transport(), Source: state.session.Source(),
		URL: path + "?token=" + url.QueryEscape(state.token),
	}
}

// prunePlayersLocked bounds retired media sessions. Active captures retain
// their sessions until the worker stops. The caller holds s.mu.
func (s *Service) prunePlayersLocked() {
	if len(s.players) < 16 {
		return
	}
	type retired struct {
		id string
		at time.Time
	}
	var candidates []retired
	for id, state := range s.players {
		if s.active[id] == nil {
			candidates = append(candidates, retired{id, state.created})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].at.Before(candidates[j].at) })
	for _, item := range candidates {
		if len(s.players) < 16 {
			break
		}
		s.players[item.id].session.Close()
		delete(s.players, item.id)
	}
}

func (s *Service) PlayerSession(id, token string) (*PlayerSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.players[id]
	if state == nil || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(state.token)) != 1 {
		return nil, ErrNotFound
	}
	return state.session, nil
}

func playerRevision(report *PlayerReport) int {
	if report == nil {
		return -1
	}
	return report.Revision
}

func (s *Service) SavePlayerRecording(id, token string, record PlayerRecording) (result *PlayerReport, err error) {
	session, err := s.PlayerSession(id, token)
	if err != nil {
		return nil, err
	}
	run, err := s.getRun(id)
	if err != nil {
		return nil, err
	}
	if record.Revision < 1 || record.Source != session.Source() {
		return nil, fmt.Errorf("%w: 播放器记录的会话或版本无效", ErrInvalid)
	}
	if err := validatePlayerRecording(record, run.CreatedAt); err != nil {
		return nil, err
	}
	defer func() {
		if err == nil && record.Complete {
			session.Close()
			s.maybeStartAutomaticInvestigation(id)
		}
	}()

	s.persistence.Lock()
	defer s.persistence.Unlock()
	dir := s.runDir(id)
	previous, readErr := readPlayerRecording(dir)
	identical := false
	if readErr == nil && (previous.Complete || record.Revision <= previous.Revision) {
		before, _ := json.Marshal(previous)
		after, _ := json.Marshal(record)
		if string(before) != string(after) {
			return nil, ErrReportChanged
		}
		identical = true
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	} else {
		if previous.Revision > 0 && (!record.StartedAt.Equal(previous.StartedAt) ||
			record.ElapsedMS < previous.ElapsedMS || len(record.Events) < len(previous.Events) || len(record.Samples) < len(previous.Samples) ||
			!slices.Equal(record.Events[:len(previous.Events)], previous.Events) || !equalPlayerSamples(record.Samples[:len(previous.Samples)], previous.Samples) || !playerSchedulingExtends(previous.Scheduling, record.Scheduling) || !playerPresentationExtends(previous.Presentation, record.Presentation)) {
			return nil, ErrReportChanged
		}
		if err := writePlayerRecording(dir, record); err != nil {
			return nil, err
		}
	}

	report, err := readJSONFile[Report](dir, reportName)
	if errors.Is(err, os.ErrNotExist) {
		if session.hls != nil {
			pending := Report{Manifest: Manifest{Protocol: run.Protocol, Observation: Observation{StartedAt: run.CreatedAt}}}
			if err := persistPlayerNetwork(dir, &pending, session.hls.network.snapshot(record.Revision, record.Complete), run.CreatedAt); err != nil {
				return nil, err
			}
		}
		return analyzePlayer(record, Observation{StartedAt: run.CreatedAt}), nil
	}
	if err != nil {
		return nil, err
	}
	if identical && report.Player != nil && report.Player.Revision == record.Revision {
		return report.Player, nil
	}
	if err := attachPlayerArtifact(dir, &report); err != nil {
		return nil, err
	}
	if session.hls != nil {
		if err := persistPlayerNetwork(dir, &report, session.hls.network.snapshot(record.Revision, record.Complete), run.CreatedAt); err != nil {
			return nil, err
		}
	}
	// Invalidate the regenerable cache before publishing new finding IDs.
	if err := os.Remove(filepath.Join(dir, explanationName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	report.HLSABR = buildHLSABR(report)
	report.Assessment = assessReport(report)
	if err := persistReport(dir, report); err != nil {
		return nil, err
	}
	cleanupPlayerVersions(dir, record.Revision)
	if report.Manifest.PlayerNetwork != nil {
		cleanupPlayerNetwork(dir, report.Manifest.PlayerNetwork.SHA256)
	}
	return report.Player, nil
}

func (s *Service) attachPlayerReport(dir string, report *Report, id string) error {
	s.mu.Lock()
	state := s.players[id]
	var source string
	if state != nil {
		source = state.session.Source()
	}
	s.mu.Unlock()
	if source == "" {
		return nil
	}
	_, err := readPlayerRecording(dir)
	if errors.Is(err, os.ErrNotExist) {
		err = writePlayerRecording(dir, PlayerRecording{
			Version: 1, StartedAt: state.created, Source: source, Events: []PlayerEvent{}, Samples: []PlayerSample{},
		})
	}
	if err != nil {
		return err
	}
	if err := attachPlayerArtifact(dir, report); err != nil {
		return err
	}
	if state.session.hls != nil {
		n := state.session.hls.network
		n.mu.Lock()
		value := clonePlayerNetwork(n.last)
		n.mu.Unlock()
		if value != nil {
			return persistPlayerNetwork(dir, report, value, state.created)
		}
	}
	return nil
}

func attachPlayerArtifact(dir string, report *Report) error {
	record, err := readPlayerRecording(dir)
	if err != nil {
		return err
	}
	if err := validatePlayerRecording(record, report.Manifest.Observation.StartedAt); err != nil {
		return err
	}
	if (report.Manifest.Protocol == ProtocolRTSP || report.Manifest.Protocol == ProtocolRTSPS) && (report.RTSP == nil || report.RTSP.Preview == nil) {
		return fmt.Errorf("%w: RTSP 播放器缺少同次采样转封装证据", ErrInvalid)
	}
	expectedSource := "same_capture"
	if report.Manifest.Protocol == ProtocolHLS || report.Manifest.Protocol == ProtocolWHEP {
		expectedSource = "independent_pull"
	}
	if record.Source != expectedSource {
		return fmt.Errorf("%w: 播放器来源与采样协议不符", ErrInvalid)
	}
	if err := freezePlayerArtifact(dir, record); err != nil {
		return err
	}
	artifact, err := hashPlayerArtifact(dir)
	if err != nil {
		return err
	}
	if report.Manifest.Version < playerSchemaVersion {
		report.Manifest.Version = playerSchemaVersion
	}
	if report.Manifest.Protocol == ProtocolRTSP || report.Manifest.Protocol == ProtocolRTSPS {
		report.Manifest.Version = max(report.Manifest.Version, rtspPlayerSchemaVersion)
	}
	report.Manifest.Player = &artifact
	report.Player = analyzePlayer(record, report.Manifest.Observation)
	addPlayerFindings(report)
	return nil
}

func playerVersionName(revision int) string {
	return "player-revision-" + strconv.Itoa(revision) + ".json"
}

func freezePlayerArtifact(dir string, record PlayerRecording) error {
	data, err := os.ReadFile(filepath.Join(dir, playerName))
	if err != nil {
		return err
	}
	path := filepath.Join(dir, playerVersionName(record.Revision))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(existing, data) {
			return fmt.Errorf("%w: 播放器记录版本内容冲突", ErrInvalid)
		}
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func cleanupPlayerVersions(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "player-revision-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		value := strings.TrimSuffix(strings.TrimPrefix(name, "player-revision-"), ".json")
		revision, err := strconv.Atoi(value)
		if err == nil && revision != keep {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

func hashPlayerArtifact(dir string) (Artifact, error) {
	data, err := os.ReadFile(filepath.Join(dir, playerName))
	if err != nil {
		return Artifact{}, err
	}
	hash := sha256.Sum256(data)
	return Artifact{Bytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}, nil
}

func writePlayerRecording(dir string, record PlayerRecording) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(data) > maxPlayerBytes {
		return fmt.Errorf("%w: 播放器记录超出大小上限", ErrInvalid)
	}
	file, err := os.CreateTemp(dir, ".player-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), filepath.Join(dir, playerName)); err != nil {
		return err
	}
	return freezePlayerArtifact(dir, record)
}

func readPlayerRecording(dir string) (PlayerRecording, error) {
	var record PlayerRecording
	file, err := os.Open(filepath.Join(dir, playerName))
	if err != nil {
		return record, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxPlayerBytes+1))
	if err != nil || len(data) > maxPlayerBytes || json.Unmarshal(data, &record) != nil {
		return record, fmt.Errorf("%w: 播放器证据无法读取", ErrInvalid)
	}
	return record, nil
}
