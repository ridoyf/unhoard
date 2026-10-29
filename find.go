package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ridoyf/unhoard/internal/cleanup"
	"github.com/ridoyf/unhoard/internal/finders"
	"github.com/ridoyf/unhoard/internal/insight"
	"github.com/ridoyf/unhoard/internal/kb"
	"github.com/ridoyf/unhoard/internal/store"
	"github.com/ridoyf/unhoard/internal/tree"
)

// findNodes runs a finder on the loaded scan and returns its results as UI nodes.
func (a *App) findNodes(find func(t *tree.Tree) []tree.ID) ([]Node, error) {
	return readTree(a, func(t *tree.Tree) ([]Node, error) {
		return a.nodes(t, find(t)), nil
	})
}

func (a *App) Largest(files bool, limit int) ([]Node, error) {
	return a.findNodes(func(t *tree.Tree) []tree.ID {
		return finders.Largest(t, 0, clamp(limit, 1, 1000), !files)
	})
}

func (a *App) Search(f finders.Filter) ([]Node, error) {
	return a.findNodes(func(t *tree.Tree) []tree.ID {
		return finders.Search(t, 0, f, 1000, time.Now())
	})
}

func (a *App) BigStale(minMB, olderThanDays int) ([]Node, error) {
	return a.findNodes(func(t *tree.Tree) []tree.ID {
		return finders.BigStale(t, 0, uint64(max(minMB, 1))<<20, max(olderThanDays, 1), 500, time.Now())
	})
}

func (a *App) OldInstallers(olderThanDays int) ([]Node, error) {
	return a.findNodes(func(t *tree.Tree) []tree.ID {
		return finders.OldInstallers(t, finders.DownloadsDir(), max(olderThanDays, 0), time.Now())
	})
}

func (a *App) CloudCopies(minMB int) ([]Node, error) {
	return a.findNodes(func(t *tree.Tree) []tree.ID {
		return finders.CloudLocal(t, finders.SyncRoots(), uint64(max(minMB, 0))<<20, 500)
	})
}

func (a *App) FileTypes() ([]finders.TypeStat, error) {
	return readTree(a, func(t *tree.Tree) ([]finders.TypeStat, error) {
		return finders.TypeBreakdown(t, 0, 40), nil
	})
}

// Finding is an item the knowledge base recognised.
type Finding struct {
	Node
	Rule *kb.Rule `json:"rule"`
}

// RuleFindings lists recognised items. scope is "dev" (developer junk), "caches"
// (safe and rebuildable items outside dev) or "all".
func (a *App) RuleFindings(scope string) ([]Finding, error) {
	keep := func(r *kb.Rule) bool { return true }
	switch scope {
	case "dev":
		keep = func(r *kb.Rule) bool { return r.Category == "dev" }
	case "caches":
		keep = func(r *kb.Rule) bool {
			return r.Category != "dev" && (r.Safety == kb.Safe || r.Safety == kb.Rebuilds)
		}
	}
	return readTree(a, func(t *tree.Tree) ([]Finding, error) {
		var out []Finding
		for _, m := range finders.KBMatches(t, 0, a.kb, keep) {
			if t.Alloc(m.ID) > 0 {
				out = append(out, Finding{a.node(t, m.ID), m.Rule})
			}
		}
		return out, nil
	})
}

type GameNode struct {
	Node
	Launcher string `json:"launcher"`
}

func (a *App) Games() ([]GameNode, error) {
	return readTree(a, func(t *tree.Tree) ([]GameNode, error) {
		var out []GameNode
		for _, g := range finders.Games(t, 0) {
			out = append(out, GameNode{a.node(t, g.ID), g.Launcher})
		}
		return out, nil
	})
}

// DupGroup is a set of identical files.
type DupGroup struct {
	Bytes uint64 `json:"bytes"`
	Files []Node `json:"files"`
}

// StartDuplicates searches for identical files of at least minMB in the background;
// results arrive as dups:done.
func (a *App) StartDuplicates(minMB int) error {
	a.mu.Lock()
	if a.scan == nil {
		a.mu.Unlock()
		return errNoScan
	}
	if a.cancelDups != nil {
		a.mu.Unlock()
		return errors.New("a duplicate search is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelDups = cancel
	scan := a.scan
	// Only this step reads the tree; hashing below runs without the lock.
	candidates := finders.DupCandidates(scan.Tree, 0, uint64(max(minMB, 0))<<20+1)
	a.mu.Unlock()

	report := a.progress("duplicates")
	go func() {
		defer cancel()
		groups, err := finders.Duplicates(ctx, candidates, func(p finders.DupProgress) {
			report(cleanup.Progress{
				Percent: float64(p.Checked) / float64(max(p.Total, 1)) * 100,
				Detail:  fmt.Sprintf("Compared %d of %d files", p.Checked, p.Total),
			})
		})
		a.mu.Lock()
		a.cancelDups = nil
		if err == nil && a.scan == scan {
			a.dups = groups
		}
		out := a.dupGroups()
		a.mu.Unlock()
		emit(evDupDone, out)
	}()
	return nil
}

func (a *App) CancelDuplicates() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelDups != nil {
		a.cancelDups()
	}
}

// Duplicates returns the last duplicate search's results for the loaded scan.
func (a *App) Duplicates() []DupGroup {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.dupGroups()
}

// dupGroups needs a.mu held. Files quarantined since the search drop out.
func (a *App) dupGroups() []DupGroup {
	var out []DupGroup
	if a.scan == nil {
		return out
	}
	t := a.scan.Tree
	for _, g := range finders.LiveDups(t, a.dups) {
		out = append(out, DupGroup{Bytes: g.Bytes, Files: a.nodes(t, g.IDs)})
	}
	return out
}

func (a *App) HiddenSpace() (insight.Hidden, error) {
	type measured struct {
		root, method string
		scanned      uint64
		inaccessible int
	}
	m, err := readTree(a, func(t *tree.Tree) (measured, error) {
		return measured{a.scanRoot, a.scan.Method, t.Alloc(0), t.Count(0, tree.Inaccessible)}, nil
	})
	if err != nil {
		return insight.Hidden{}, err
	}
	if len(m.root) != 3 {
		return insight.Hidden{}, errors.New("hidden space is only measured for whole-drive scans")
	}
	// Runs outside the lock: measuring shadow copies starts a command that can take seconds.
	return insight.Explain(context.Background(), m.root, m.scanned, m.inaccessible, m.method, a.elevated, cleanup.Exec), nil
}

func (a *App) Apps() []insight.AppFootprint {
	apps := insight.AppsWithData()
	a.mu.RLock()
	defer a.mu.RUnlock()
	var t *tree.Tree
	if a.scan != nil {
		t = a.scan.Tree
	}
	return insight.Footprints(apps, t)
}

// Summary lists what could be freed, biggest first.
func (a *App) Summary() ([]insight.SummaryItem, error) {
	quarantined, _ := a.q.List()
	return readTree(a, func(t *tree.Tree) ([]insight.SummaryItem, error) {
		return insight.Summary(t, a.kb, a.dups, quarantined, finders.DownloadsDir(), time.Now()), nil
	})
}

func (a *App) Snapshots() ([]store.Snapshot, error) { return a.db.Snapshots("") }

func (a *App) DeleteSnapshot(id int64) error { return a.db.DeleteSnapshot(id) }

// GrowthReport compares a snapshot with the loaded scan.
type GrowthReport struct {
	Since  string           `json:"since"`
	Grew   []insight.Growth `json:"grew"`
	Shrank []insight.Growth `json:"shrank"`
}

func (a *App) GrewSince(snapshotID int64) (GrowthReport, error) {
	snap, err := a.db.Snapshot(snapshotID)
	if err != nil {
		return GrowthReport{}, errors.New("that snapshot no longer exists")
	}
	before, err := a.db.SnapshotDirs(snapshotID)
	if err != nil {
		return GrowthReport{}, err
	}
	return readTree(a, func(t *tree.Tree) (GrowthReport, error) {
		if !strings.EqualFold(snap.Root, a.scanRoot) {
			return GrowthReport{}, fmt.Errorf("that snapshot is of %s but the loaded scan is of %s", snap.Root, a.scanRoot)
		}
		grew, shrank := insight.Grew(t, before, 50)
		return GrowthReport{Since: snap.At.Format(time.RFC3339), Grew: grew, Shrank: shrank}, nil
	})
}

func (a *App) FolderHistory(root, path string) ([]store.HistoryPoint, error) {
	return a.db.FolderHistory(root, path)
}
