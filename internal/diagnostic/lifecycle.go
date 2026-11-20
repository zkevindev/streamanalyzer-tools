package diagnostic

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type StorageStats struct {
	RunCount       int        `json:"run_count"`
	TotalBytes     int64      `json:"total_bytes"`
	ProtectedCount int        `json:"protected_count"`
	ProtectedBytes int64      `json:"protected_bytes"`
	OldestAt       *time.Time `json:"oldest_at,omitempty"`
}

type PruneRequest struct {
	RetentionDays int   `json:"retention_days,omitempty"`
	MaxTotalBytes int64 `json:"max_total_bytes,omitempty"`
}

type PruneResult struct {
	Deleted      int          `json:"deleted"`
	DeletedBytes int64        `json:"deleted_bytes"`
	Storage      StorageStats `json:"storage"`
}

func (s *Service) Storage() (StorageStats, error) {
	runs, err := s.listAll("ORDER BY created_at ASC, id ASC")
	if err != nil {
		return StorageStats{}, err
	}
	result := StorageStats{RunCount: len(runs)}
	for _, run := range runs {
		size, err := directorySize(s.runDir(run.ID))
		if err != nil {
			return StorageStats{}, err
		}
		result.TotalBytes += size
		if run.Protected {
			result.ProtectedCount++
			result.ProtectedBytes += size
		}
		if result.OldestAt == nil {
			at := run.CreatedAt
			result.OldestAt = &at
		}
	}
	return result, nil
}

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, err
}

func (s *Service) listAll(order string) ([]Run, error) {
	rows, err := s.db.Query(runColumns + " " + order)
	if err != nil {
		return nil, err
	}
	return scanRuns(rows)
}

func (s *Service) SetProtected(id string, protected bool) (Run, error) {
	if _, err := s.getRun(id); err != nil {
		return Run{}, err
	}
	if _, err := s.db.Exec("UPDATE runs SET protected = ? WHERE id = ?", protected, id); err != nil {
		return Run{}, err
	}
	return s.getRun(id)
}

func (s *Service) Delete(id string) error {
	s.persistence.Lock()
	defer s.persistence.Unlock()
	run, err := s.getRun(id)
	if err != nil {
		return err
	}
	if run.Protected {
		return fmt.Errorf("%w: 重要证据已保护，请先取消保护", ErrInvalid)
	}
	s.mu.Lock()
	busy := s.active[id] != nil || s.automaticJobs[id] != nil || s.investigationJobs[id] != nil
	player := s.players[id]
	if !busy && player != nil {
		player.session.Close()
		delete(s.players, id)
	}
	s.mu.Unlock()
	if busy || run.Status == "running" {
		return ErrBusy
	}
	s.StopRTSPReplay(id)

	dir := s.runDir(id)
	trash := filepath.Join(s.root, ".delete-"+id)
	if err := os.RemoveAll(trash); err != nil {
		return err
	}
	if err := os.Rename(dir, trash); err != nil {
		if os.IsNotExist(err) {
			_, err = s.db.Exec("DELETE FROM runs WHERE id = ?", id)
		}
		return err
	}
	if _, err := s.db.Exec("DELETE FROM runs WHERE id = ?", id); err != nil {
		_ = os.Rename(trash, dir)
		return err
	}
	return os.RemoveAll(trash)
}

func (s *Service) DeleteMany(ids []string) error {
	if len(ids) == 0 || len(ids) > 100 {
		return fmt.Errorf("%w: 每次须选择 1 至 100 条记录", ErrInvalid)
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		run, err := s.getRun(id)
		if err != nil {
			return err
		}
		if run.Protected || run.Status == "running" {
			return fmt.Errorf("%w: 只能删除未保护且已结束的记录", ErrInvalid)
		}
	}
	for id := range seen {
		if err := s.Delete(id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Prune(req PruneRequest) (PruneResult, error) {
	if req.RetentionDays < 0 || req.RetentionDays > 3650 || req.MaxTotalBytes < 0 || req.MaxTotalBytes > 1<<50 {
		return PruneResult{}, fmt.Errorf("%w: 保留天数或磁盘预算无效", ErrInvalid)
	}
	before, err := s.Storage()
	if err != nil {
		return PruneResult{}, err
	}
	runs, err := s.listAll("ORDER BY created_at ASC, id ASC")
	if err != nil {
		return PruneResult{}, err
	}
	cutoff := time.Now().Add(-time.Duration(req.RetentionDays) * 24 * time.Hour)
	projected := before.TotalBytes
	result := PruneResult{}
	for _, run := range runs {
		if run.Protected || run.Status == "running" {
			continue
		}
		tooOld := req.RetentionDays > 0 && run.CreatedAt.Before(cutoff)
		overBudget := req.MaxTotalBytes > 0 && projected > req.MaxTotalBytes
		if !tooOld && !overBudget {
			continue
		}
		size, err := directorySize(s.runDir(run.ID))
		if err != nil {
			return result, err
		}
		if err := s.Delete(run.ID); err != nil {
			if err == ErrBusy {
				continue
			}
			return result, err
		}
		projected -= size
		result.Deleted++
		result.DeletedBytes += size
	}
	result.Storage, err = s.Storage()
	return result, err
}
