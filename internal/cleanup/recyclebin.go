package cleanup

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/ridoyf/unhoard/internal/insight"
)

var recycleBin = Action{
	ID:          "empty-recycle-bin",
	Title:       "Empty the Recycle Bin",
	Description: "Permanently deletes everything in the Recycle Bin on every drive. These are files you already deleted; they cannot be restored afterwards.",
	Preview: func(ctx context.Context, env Env, _ []string) (Plan, error) {
		bytes, items, err := insight.RecycleBin("")
		if err != nil {
			return unavailable("Could not read the Recycle Bin: " + err.Error()), nil
		}
		if items == 0 {
			return unavailable("The Recycle Bin is already empty."), nil
		}
		return Plan{
			Targets:  []string{fmt.Sprintf("%d items in the Recycle Bin", items)},
			Bytes:    bytes,
			Commands: []string{"SHEmptyRecycleBinW, all drives (the Windows function behind File Explorer's Empty Recycle Bin)"},
		}, nil
	},
	Run: func(ctx context.Context, env Env, _ Plan) (string, error) {
		if env.EmptyRecycleBin == nil {
			return "", errors.New("emptying the Recycle Bin is not available here")
		}
		_, total, err := insight.RecycleBin("")
		if err != nil {
			return "", err
		}
		remaining := func() (uint64, error) {
			_, n, err := insight.RecycleBin("")
			return n, err
		}
		err = runWatched(env.EmptyRecycleBin, remaining, total, time.Second, env.report)
		// The shell reports an error for some partial states, such as a drive whose bin
		// vanished mid-way. What matters is whether anything is left.
		if left, qerr := remaining(); qerr == nil && left == 0 {
			return "Removed " + thousands(total) + " items.", nil
		}
		return "", err
	},
}

// runWatched runs work and, every interval until it returns, reports progress as
// total minus what remaining still counts. It returns work's error.
func runWatched(work func() error, remaining func() (uint64, error), total uint64, interval time.Duration, report func(Progress)) error {
	removed := func(done uint64) Progress {
		return Progress{
			Percent: float64(done) / float64(max(total, 1)) * 100,
			Detail:  fmt.Sprintf("%s of %s items removed", thousands(done), thousands(total)),
		}
	}
	report(removed(0))
	done := make(chan error, 1)
	go func() { done <- work() }()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-tick.C:
			// Files deleted elsewhere during the run can push the count above total; skip those readings.
			if left, err := remaining(); err == nil && left <= total {
				report(removed(total - left))
			}
		}
	}
}

// thousands formats n with comma separators, as the UI does with toLocaleString.
func thousands(n uint64) string {
	s := strconv.FormatUint(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

var procSHEmptyRecycleBin = windows.NewLazySystemDLL("shell32.dll").NewProc("SHEmptyRecycleBinW")

// SHERB_NOCONFIRMATION | SHERB_NOPROGRESSUI | SHERB_NOSOUND: the user already confirmed in
// Unhoard, and Unhoard shows its own progress.
const sherbQuiet = 0x1 | 0x2 | 0x4

// EmptyRecycleBin empties every drive's Recycle Bin. It is the real Env.EmptyRecycleBin;
// tests never set it, so they cannot reach the real Recycle Bin.
func EmptyRecycleBin() error {
	// Shell functions expect a COM apartment on the calling thread, and COM state
	// belongs to the OS thread, so the goroutine must stay on one thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// S_FALSE means COM was already set up on this thread; it still needs the matching uninitialise.
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil || err == syscall.Errno(1) {
		defer windows.CoUninitialize()
	}
	if err := procSHEmptyRecycleBin.Find(); err != nil {
		return err
	}
	if hr, _, _ := procSHEmptyRecycleBin.Call(0, 0, sherbQuiet); hr != 0 {
		return fmt.Errorf("SHEmptyRecycleBinW failed with HRESULT 0x%08X", uint32(hr))
	}
	return nil
}
