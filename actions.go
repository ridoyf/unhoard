package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/ridoyf/unhoard/internal/cleanup"
	"github.com/ridoyf/unhoard/internal/diagnose"
	"github.com/ridoyf/unhoard/internal/quarantine"
	"github.com/ridoyf/unhoard/internal/store"
	"github.com/ridoyf/unhoard/internal/tree"
)

// MoveResult reports one item of a quarantine request.
type MoveResult struct {
	Path    string            `json:"path"`
	OK      bool              `json:"ok"`
	Bytes   uint64            `json:"bytes"`
	Problem *diagnose.Problem `json:"problem"` // why it was not moved, and what to do
}

// Quarantine moves paths to the quarantine. Each path is checked in the backend; the UI's checks are advisory.
func (a *App) Quarantine(paths []string) []MoveResult {
	report := a.progress("quarantine")
	out := make([]MoveResult, 0, len(paths))
	for i, p := range paths {
		p = filepath.Clean(p)
		report(cleanup.Progress{Percent: float64(i) / float64(len(paths)) * 100, Detail: "Moving " + filepath.Base(p)})
		it, err := a.quarantineOne(p)
		a.log("quarantine", []string{p}, it.Bytes, err, "item "+it.ID)
		out = append(out, MoveResult{Path: p, OK: err == nil, Bytes: it.Bytes, Problem: a.moveProblem(p, err)})
	}
	return out
}

func (a *App) diagnosis() diagnose.Context {
	return diagnose.Context{Elevated: a.elevated, LockedBy: diagnose.LockedBy}
}

func (a *App) moveProblem(p string, err error) *diagnose.Problem {
	r, _ := a.kb.Explain(p)
	return a.diagnosis().Quarantine(p, err, r)
}

// quarantineOne moves p and removes it from the loaded scan.
func (a *App) quarantineOne(p string) (quarantine.Item, error) {
	if err := a.checkQuarantine(p); err != nil {
		return quarantine.Item{}, err
	}
	a.mu.RLock()
	scan, id, bytes := a.scan, tree.None, uint64(0)
	if scan != nil {
		if n, ok := scan.Tree.Find(p); ok {
			id, bytes = n, scan.Tree.Alloc(n)
		}
	}
	a.mu.RUnlock()
	it, err := a.q.Move(p, bytes)
	if err != nil {
		return it, err
	}
	a.mu.Lock()
	// A scan that finished during the move has its own IDs, and it already lacks p.
	if a.scan == scan && id != tree.None {
		scan.Tree.Remove(id)
	}
	a.mu.Unlock()
	return it, nil
}

func (a *App) QuarantineItems() ([]quarantine.Item, error) { return a.q.List() }

// Restore puts an item back. It returns nil on success, or what went wrong and how to fix it.
func (a *App) Restore(id string) *diagnose.Problem {
	it, err := a.q.Restore(id)
	a.log("restore", []string{it.OriginalPath}, it.Bytes, err, "")
	return a.diagnosis().Restore(it.OriginalPath, err)
}

// DeleteQuarantined permanently deletes one quarantined item.
func (a *App) DeleteQuarantined(id string) *diagnose.Problem {
	it, err := a.q.PurgeOne(id)
	a.log("purge", []string{it.OriginalPath}, it.Bytes, err, "deleted by request")
	return a.diagnosis().Move("", err)
}

// EmptyQuarantine permanently deletes everything in quarantine.
func (a *App) EmptyQuarantine() *diagnose.Problem {
	report := a.progress("purge")
	items, err := a.q.Purge(time.Now().Add(time.Minute), func(done, total int, it quarantine.Item) {
		report(cleanup.Progress{Percent: float64(done) / float64(total) * 100, Detail: "Deleting " + filepath.Base(it.OriginalPath)})
	})
	a.logPurged(items, err, "emptied by request")
	return a.diagnosis().Move("", err)
}

func (a *App) purgeExpired() {
	days := a.Settings().RetentionDays
	items, err := a.q.Purge(time.Now().AddDate(0, 0, -days), nil)
	if len(items) > 0 || err != nil {
		a.logPurged(items, err, fmt.Sprintf("older than %d days", days))
	}
}

func (a *App) logPurged(items []quarantine.Item, err error, why string) {
	var paths []string
	var bytes uint64
	for _, it := range items {
		paths = append(paths, it.OriginalPath)
		bytes += it.Bytes
	}
	a.log("purge", paths, bytes, err, why)
}

// log records an action in the action log. A failure's error replaces result.
func (a *App) log(action string, paths []string, bytes uint64, err error, result string) {
	entry := store.Action{Action: action, Paths: paths, Bytes: bytes, OK: err == nil, Result: result}
	if err != nil {
		entry.Result = err.Error()
	}
	if e := a.db.LogAction(entry); e != nil {
		slog.Error("writing action log", "err", e)
	}
	slog.Info("action", "action", action, "paths", paths, "bytes", bytes, "ok", err == nil, "result", entry.Result)
}

func (a *App) ActionLog(limit int) ([]store.Action, error) {
	return a.db.Actions(clamp(limit, 1, 5000))
}

type ActionInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	NeedsAdmin  bool   `json:"needsAdmin"`
}

func (a *App) Actions() []ActionInfo {
	var out []ActionInfo
	for _, act := range cleanup.All {
		out = append(out, ActionInfo{act.ID, act.Title, act.Description, act.NeedsAdmin})
	}
	return out
}

// ActivityProgress tells the UI how a long-running job is going. ID names the job:
// "preview", "action", "quarantine", "purge" or "duplicates".
type ActivityProgress struct {
	ID      string  `json:"id"`
	Percent float64 `json:"percent"` // 0 to 100, or -1 when the job cannot tell
	Detail  string  `json:"detail"`
}

// progress returns a reporter for job id. Tools such as DISM redraw their progress many
// times a second, so updates closer together than progressGap are dropped.
func (a *App) progress(id string) func(cleanup.Progress) {
	const progressGap = 150 * time.Millisecond
	var last time.Time
	return func(p cleanup.Progress) {
		if time.Since(last) < progressGap {
			return
		}
		last = time.Now()
		emit(evProgress, ActivityProgress{ID: id, Percent: p.Percent, Detail: p.Detail})
	}
}

func (a *App) env(activity string) cleanup.Env {
	return cleanup.Env{
		Elevated:        a.elevated,
		Run:             cleanup.Exec,
		RunInput:        cleanup.ExecInput,
		EmptyRecycleBin: cleanup.EmptyRecycleBin,
		Progress:        a.progress(activity),
	}
}

// PreviewAction dry-runs an action. Some previews (DISM) take minutes.
func (a *App) PreviewAction(id string, target []string) (cleanup.Plan, error) {
	return cleanup.Preview(context.Background(), a.env("preview"), id, target)
}

// RunResult is what an action printed.
type RunResult struct {
	OK      bool              `json:"ok"`
	Output  string            `json:"output"`
	Problem *diagnose.Problem `json:"problem"` // why it failed, and what to do
}

// RunAction runs an action after the user confirmed exactly these commands.
func (a *App) RunAction(id string, target, confirmedCommands []string) RunResult {
	// The dialog can be hidden while an action runs, so a second one could otherwise
	// start on top of it.
	if !a.acting.TryLock() {
		return RunResult{Problem: &diagnose.Problem{
			Summary: "Another cleanup action is still running. Its progress is shown at the top of the window.",
			Fixes:   []diagnose.Fix{{Label: "Try again once it has finished.", Kind: "retry"}},
		}}
	}
	defer a.acting.Unlock()
	plan, out, err := cleanup.Execute(context.Background(), a.env("action"), id, target, confirmedCommands)
	a.log(id, append(plan.Commands, plan.Targets...), plan.Bytes, err, cleanup.LastLines(out, 20))
	return RunResult{OK: err == nil, Output: out, Problem: a.diagnosis().Command(out, err)}
}
