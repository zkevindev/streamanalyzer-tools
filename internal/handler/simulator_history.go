package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const maxSimulatorRuns = 50
const maxSimulatorRunBytes = 128 << 10

var simulatorScenarioName = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)
var diagnosticRunID = regexp.MustCompile(`^[0-9a-f]{32}$`)

type simulatorRunPreset struct {
	Version  int    `json:"version"`
	Name     string `json:"name"`
	Scenario string `json:"scenario"`
}

type simulatorRunRecord struct {
	ID           string             `json:"id"`
	Preset       simulatorRunPreset `json:"preset"`
	StartedAt    time.Time          `json:"started_at"`
	Passed       bool               `json:"passed"`
	Detail       string             `json:"detail"`
	ElapsedMS    int64              `json:"elapsed_ms"`
	DiagnosticID string             `json:"diagnostic_id,omitempty"`
	Evidence     json.RawMessage    `json:"evidence,omitempty"`
}

type simulatorRunStore struct {
	mu   sync.Mutex
	path string
}

func RegisterSimulatorHistoryRoutes(r *gin.Engine, dataDir string) {
	store := &simulatorRunStore{path: filepath.Join(dataDir, "simulator-runs.json")}
	r.GET("/api/simulators/runs", localDiagnostics, func(c *gin.Context) {
		store.mu.Lock()
		records, err := store.read()
		store.mu.Unlock()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "无法读取模拟器记录"})
			return
		}
		if c.Query("summary") == "1" {
			for i := range records {
				records[i].Evidence = nil
			}
		}
		c.JSON(http.StatusOK, records)
	})
	r.POST("/api/simulators/runs", localDiagnostics, func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSimulatorRunBytes)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		var record simulatorRunRecord
		if err := decoder.Decode(&record); err != nil || decoder.Decode(new(any)) != io.EOF || !record.prepare() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "模拟器记录格式无效"})
			return
		}
		if err := store.save(record); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "无法保存模拟器记录"})
			return
		}
		c.JSON(http.StatusOK, record)
	})
}

func (record *simulatorRunRecord) prepare() bool {
	record.Preset.Name = strings.TrimSpace(record.Preset.Name)
	if record.Preset.Version != 1 || record.Preset.Name == "" || utf8.RuneCountInString(record.Preset.Name) > 80 ||
		!simulatorScenarioName.MatchString(record.Preset.Scenario) || record.StartedAt.IsZero() ||
		record.ElapsedMS < 0 || len(record.Detail) > 8192 || (record.DiagnosticID != "" && !diagnosticRunID.MatchString(record.DiagnosticID)) {
		return false
	}
	record.StartedAt = record.StartedAt.UTC()
	key := record.StartedAt.Format(time.RFC3339Nano) + "\x00" + record.Preset.Scenario + "\x00" + record.Preset.Name
	digest := sha256.Sum256([]byte(key))
	record.ID = hex.EncodeToString(digest[:12])
	return true
}

// read and save serialize access to the file so competing browser tabs cannot
// overwrite one another's records. The file is replaced only after a full write.
func (store *simulatorRunStore) read() ([]simulatorRunRecord, error) {
	records := []simulatorRunRecord{}
	file, err := os.Open(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return records, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxSimulatorRuns*(maxSimulatorRunBytes+4096)))
	if err := decoder.Decode(&records); err != nil {
		return nil, err
	}
	return records, nil
}

func (store *simulatorRunStore) save(record simulatorRunRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	records, err := store.read()
	if err != nil {
		return err
	}
	for _, previous := range records {
		if previous.ID == record.ID {
			return nil
		}
	}
	records = append(records, record)
	sort.SliceStable(records, func(i, j int) bool { return records[i].StartedAt.After(records[j].StartedAt) })
	if len(records) > maxSimulatorRuns {
		records = records[:maxSimulatorRuns]
	}
	file, err := os.CreateTemp(filepath.Dir(store.path), ".simulator-runs-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := errors.Join(encoder.Encode(records), file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), store.path)
}
