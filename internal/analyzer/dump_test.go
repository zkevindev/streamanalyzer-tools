package analyzer

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func dumpedBytes(t *testing.T, dir, name string) int64 {
	t.Helper()
	fi, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("stat %s: %v", name, err)
	}
	return fi.Size()
}

func TestDumpUnlimitedWritesEverything(t *testing.T) {
	d, err := newStreamDumper(t.TempDir(), "task_x", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	w := d.Writer("a.flv")
	payload := bytes.Repeat([]byte("x"), 1024)
	for i := 0; i < 5000; i++ { // ~5 MB, far past any accidental cap
		if n, err := w.Write(payload); n != len(payload) || err != nil {
			t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(payload))
		}
	}
	d.Flush()

	if got := dumpedBytes(t, d.Dir(), "a.flv"); got != 5000*1024 {
		t.Errorf("dumped %d bytes, want %d", got, 5000*1024)
	}
	if d.LimitReached() {
		t.Error("LimitReached = true for an unlimited dumper")
	}
}

func TestDumpStopsAtLimit(t *testing.T) {
	const maxBytes = 100 * 1024
	d, err := newStreamDumper(t.TempDir(), "task_x", maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	w := d.Writer("a.rtmp")
	payload := bytes.Repeat([]byte("y"), 4096)
	for i := 0; i < 100; i++ { // 400 KB offered against a 100 KB cap
		// The writer must keep claiming success: it sits on the receive path.
		if n, err := w.Write(payload); n != len(payload) || err != nil {
			t.Fatalf("Write = (%d, %v), want (%d, nil) even past the cap", n, err, len(payload))
		}
	}
	d.Flush()

	if got := dumpedBytes(t, d.Dir(), "a.rtmp"); got != maxBytes {
		t.Errorf("dumped %d bytes, want exactly the cap %d", got, maxBytes)
	}
	if !d.LimitReached() {
		t.Error("LimitReached = false after exceeding the cap")
	}
}

// The cap is shared across a task's files, as HLS renditions dump in parallel.
func TestDumpLimitSharedAcrossFiles(t *testing.T) {
	const maxBytes = 10 * 1024
	d, err := newStreamDumper(t.TempDir(), "task_x", maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	payload := bytes.Repeat([]byte("z"), 1024)
	for i := 0; i < 8; i++ {
		d.Writer("a.ts").Write(payload)
		d.Writer("b.ts").Write(payload)
	}
	d.Flush()

	total := dumpedBytes(t, d.Dir(), "a.ts") + dumpedBytes(t, d.Dir(), "b.ts")
	if total != maxBytes {
		t.Errorf("total dumped %d bytes across both files, want the shared cap %d", total, maxBytes)
	}
}

func TestDumpLimitConcurrentWriters(t *testing.T) {
	const maxBytes = 64 * 1024
	d, err := newStreamDumper(t.TempDir(), "task_x", maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	payload := bytes.Repeat([]byte("c"), 512)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			w := d.Writer("s" + string(rune('0'+n)) + ".ts")
			for j := 0; j < 200; j++ {
				w.Write(payload)
			}
		}(i)
	}
	wg.Wait()
	d.Flush()

	var total int64
	entries, err := os.ReadDir(d.Dir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		total += dumpedBytes(t, d.Dir(), e.Name())
	}
	if total != maxBytes {
		t.Errorf("total dumped %d bytes, want exactly %d (budget must not be over-committed)", total, maxBytes)
	}
}

func TestDumpNilIsNoop(t *testing.T) {
	var d *streamDumper
	// A task without dump enabled holds a nil dumper; every call must be safe.
	d.Flush()
	d.Close()
	if d.Dir() != "" || d.LimitReached() {
		t.Error("nil dumper should report empty dir and no limit")
	}
	if w := d.Writer("x"); w != nil {
		t.Error("nil dumper should return a nil writer")
	}
}
