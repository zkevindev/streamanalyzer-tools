package analyzer

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// streamDumper writes bytes exactly as received from the network, before any
// demuxing, so the result can be replayed or fed back into offline analysis.
// A single dumper may hold several files (HLS runs one per media playlist).
//
// A long-running live task would otherwise dump without bound, so the writes
// share a byte budget across all of the task's files. When it runs out, writing
// stops and the analysis continues untouched: keeping the start of a stream is
// what makes a dump useful, so the tail is what gets dropped.
type streamDumper struct {
	dir      string
	maxBytes int64 // 0 means unlimited

	written   atomic.Int64
	limitOnce sync.Once
	limitHit  atomic.Bool

	mu    sync.Mutex
	files map[string]*dumpFile
}

type dumpFile struct {
	owner *streamDumper
	mu    sync.Mutex
	f     *os.File
	w     *bufio.Writer
	err   error
}

// newStreamDumper creates the dump directory for a task. maxBytes caps the
// total bytes written across the task's files; 0 means unlimited.
func newStreamDumper(baseDir, taskID string, maxBytes int64) (*streamDumper, error) {
	dir := filepath.Join(baseDir, "dump", taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create dump dir failed: %w", err)
	}
	return &streamDumper{
		dir:      dir,
		maxBytes: maxBytes,
		files:    make(map[string]*dumpFile),
	}, nil
}

// reserve claims up to n bytes of the shared budget, returning how many may be
// written. It returns 0 once the cap is reached.
func (d *streamDumper) reserve(n int) int {
	if d.maxBytes <= 0 {
		return n
	}

	for {
		used := d.written.Load()
		remaining := d.maxBytes - used
		if remaining <= 0 {
			d.noteLimit()
			return 0
		}
		allowed := int64(n)
		if allowed > remaining {
			allowed = remaining
		}
		if d.written.CompareAndSwap(used, used+allowed) {
			if allowed < int64(n) {
				d.noteLimit()
			}
			return int(allowed)
		}
	}
}

func (d *streamDumper) noteLimit() {
	d.limitHit.Store(true)
	d.limitOnce.Do(func() {
		fmt.Printf("dump: reached max_dump_mb (%d MB) in %s, stopped writing (analysis continues)\n",
			d.maxBytes/(1024*1024), d.dir)
	})
}

// LimitReached reports whether the dump was truncated by the size cap.
func (d *streamDumper) LimitReached() bool {
	if d == nil {
		return false
	}
	return d.limitHit.Load()
}

func (d *streamDumper) Dir() string {
	if d == nil {
		return ""
	}
	return d.dir
}

// Writer returns an io.Writer appending to name inside the dump dir. The same
// name always maps to the same file. Write errors are recorded and reported
// once instead of interrupting the analysis.
func (d *streamDumper) Writer(name string) *dumpFile {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if df, ok := d.files[name]; ok {
		return df
	}

	df := &dumpFile{owner: d}
	f, err := os.Create(filepath.Join(d.dir, name))
	if err != nil {
		df.err = err
		fmt.Printf("dump: create %s failed: %v\n", name, err)
	} else {
		df.f = f
		df.w = bufio.NewWriterSize(f, 256*1024)
	}
	d.files[name] = df
	return df
}

func (d *streamDumper) Flush() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, df := range d.files {
		df.flush()
	}
}

func (d *streamDumper) Close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for name, df := range d.files {
		df.flush()
		if df.f != nil {
			if err := df.f.Close(); err != nil {
				fmt.Printf("dump: close %s failed: %v\n", name, err)
			}
		}
	}
	d.files = make(map[string]*dumpFile)
}

// Write appends to the dump. It always reports the full length as written and
// never returns an error: this writer sits on the receive path via io.TeeReader,
// so a dump problem must never disturb the analysis or the stream itself.
func (df *dumpFile) Write(p []byte) (int, error) {
	if df == nil {
		return len(p), nil
	}

	n := df.owner.reserve(len(p))
	if n <= 0 {
		return len(p), nil
	}

	df.mu.Lock()
	defer df.mu.Unlock()
	if df.w == nil || df.err != nil {
		return len(p), nil
	}
	if _, err := df.w.Write(p[:n]); err != nil {
		df.err = err
		fmt.Printf("dump: write failed: %v\n", err)
	}
	return len(p), nil
}

func (df *dumpFile) flush() {
	if df == nil || df.w == nil {
		return
	}
	df.mu.Lock()
	defer df.mu.Unlock()
	if err := df.w.Flush(); err != nil && df.err == nil {
		df.err = err
		fmt.Printf("dump: flush failed: %v\n", err)
	}
}
