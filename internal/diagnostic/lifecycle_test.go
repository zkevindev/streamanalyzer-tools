package diagnostic

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func lifecycleRun(t *testing.T, service *Service, age time.Duration, size int, protected bool) Run {
	t.Helper()
	run := newRun("replay", ProtocolHTTPFLV, "https://example.test")
	run.Status, run.ReportReady, run.Protected = "done", true, protected
	run.CreatedAt = time.Now().Add(-age).UTC().Truncate(time.Millisecond)
	if err := os.Mkdir(service.runDir(run.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service.runDir(run.ID), "stream.flv"), make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.insertRun(run); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestEvidenceProtectionDeletionAndStorage(t *testing.T) {
	dir := t.TempDir()
	service, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	protected := lifecycleRun(t, service, 100*24*time.Hour, 100, false)
	ordinary := lifecycleRun(t, service, 90*24*time.Hour, 200, false)
	running := lifecycleRun(t, service, 80*24*time.Hour, 300, false)
	running.Status = "running"
	if err := service.updateRun(running); err != nil {
		t.Fatal(err)
	}
	service.active[running.ID] = func() {}

	if _, err := service.SetProtected(protected.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(protected.ID); err == nil {
		t.Fatal("deleted protected evidence")
	}
	if err := service.DeleteMany([]string{ordinary.ID, protected.ID}); err == nil {
		t.Fatal("batch delete accepted a protected record")
	}
	if _, err := os.Stat(service.runDir(ordinary.ID)); err != nil {
		t.Fatal("batch validation partially deleted records")
	}
	if err := service.Delete(running.ID); err != ErrBusy {
		t.Fatalf("deleted active evidence: %v", err)
	}

	result, err := service.Prune(PruneRequest{RetentionDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 1 || result.DeletedBytes != 200 || result.Storage.RunCount != 2 || result.Storage.ProtectedCount != 1 {
		t.Fatalf("unexpected retention result: %+v", result)
	}
	if _, err := os.Stat(service.runDir(ordinary.ID)); !os.IsNotExist(err) {
		t.Fatal("deleted evidence directory remains")
	}
	if _, err := service.getRun(ordinary.ID); err != ErrNotFound {
		t.Fatal("deleted evidence index remains")
	}
	delete(service.active, running.ID)
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stored, err := reopened.getRun(protected.ID)
	if err != nil || !stored.Protected {
		t.Fatalf("protection did not survive restart: %+v %v", stored, err)
	}
}

func TestEvidenceDiskBudgetDeletesOldestUnprotectedFirst(t *testing.T) {
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	oldest := lifecycleRun(t, service, 4*time.Hour, 100, false)
	protected := lifecycleRun(t, service, 3*time.Hour, 100, true)
	newest := lifecycleRun(t, service, 2*time.Hour, 100, false)

	result, err := service.Prune(PruneRequest{MaxTotalBytes: 250})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 1 || result.Storage.RunCount != 2 || result.Storage.TotalBytes != 200 {
		t.Fatalf("unexpected disk budget result: %+v", result)
	}
	if _, err := service.getRun(oldest.ID); err != ErrNotFound {
		t.Fatal("oldest unprotected evidence was not removed")
	}
	if _, err := service.getRun(protected.ID); err != nil {
		t.Fatal("protected evidence was removed")
	}
	if _, err := service.getRun(newest.ID); err != nil {
		t.Fatal("newer evidence was removed before the budget was satisfied")
	}
}

func TestDeleteManyRemovesSelectedRecords(t *testing.T) {
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	first := lifecycleRun(t, service, time.Hour, 1, false)
	second := lifecycleRun(t, service, time.Hour, 1, false)
	keep := lifecycleRun(t, service, time.Hour, 1, false)
	if err := service.DeleteMany([]string{first.ID, second.ID, first.ID}); err != nil {
		t.Fatal(err)
	}
	storage, err := service.Storage()
	if err != nil || storage.RunCount != 1 {
		t.Fatalf("unexpected storage after batch deletion: %+v %v", storage, err)
	}
	if _, err := service.getRun(keep.ID); err != nil {
		t.Fatal(err)
	}
}
