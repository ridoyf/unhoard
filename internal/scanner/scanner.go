package scanner

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ridoyf/unhoard/internal/tree"
)

// Progress is streamed to the UI while a scan runs.
type Progress struct {
	Files     uint64 `json:"files"`
	Dirs      uint64 `json:"dirs"`
	Bytes     uint64 `json:"bytes"`
	Errors    uint64 `json:"errors"`
	Current   string `json:"current"`
	ElapsedMs int64  `json:"elapsedMs"`
	Method    string `json:"method"`
}

// Result is a finished scan.
type Result struct {
	Tree     *tree.Tree
	Method   string // "mft" or "walk"
	Errors   []string
	ErrCount int
	Duration time.Duration
}

const maxRecordedErrors = 500

// Scan scans root (a drive like `C:\` or any folder). It uses the MFT when the
// process is elevated and root is an NTFS drive root, and the directory walker otherwise
// or if the MFT read fails. onProgress is called at most every 100ms.
func Scan(ctx context.Context, root string, elevated bool, onProgress func(Progress)) (*Result, error) {
	if elevated && isDriveRoot(root) {
		res, err := scanMFT(ctx, root, onProgress)
		if err == nil || ctx.Err() != nil {
			return res, err
		}
		slog.Warn("MFT scan failed, falling back to directory walk", "root", root, "err", err)
	}
	return Walk(ctx, root, onProgress)
}

func isDriveRoot(p string) bool {
	return len(p) == 3 && p[1] == ':' && p[2] == '\\'
}

// counters are shared by both scan methods for progress reporting.
type counters struct {
	files, dirs, bytes, errs atomic.Uint64
	current                  atomic.Value // string

	mu     sync.Mutex
	errors []string
}

func (c *counters) fail(path string, err error) {
	c.errs.Add(1)
	c.mu.Lock()
	if len(c.errors) < maxRecordedErrors {
		c.errors = append(c.errors, path+": "+err.Error())
	}
	c.mu.Unlock()
}

func (c *counters) snapshot(method string, start time.Time) Progress {
	cur, _ := c.current.Load().(string)
	return Progress{
		Files: c.files.Load(), Dirs: c.dirs.Load(), Bytes: c.bytes.Load(), Errors: c.errs.Load(),
		Current: cur, ElapsedMs: time.Since(start).Milliseconds(), Method: method,
	}
}

// report calls onProgress every 100ms. The returned stop func sends the final numbers and waits.
func (c *counters) report(method string, start time.Time, onProgress func(Progress)) (stop func()) {
	if onProgress == nil {
		return func() {}
	}
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-quit:
				onProgress(c.snapshot(method, start))
				return
			case <-tick.C:
				onProgress(c.snapshot(method, start))
			}
		}
	}()
	return func() { close(quit); <-done }
}

func normaliseRoot(root string) string {
	if isDriveRoot(root) || (len(root) == 2 && root[1] == ':') {
		return strings.ToUpper(root[:1]) + `:\`
	}
	return strings.TrimSuffix(root, `\`)
}
