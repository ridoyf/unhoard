package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/ridoyf/unhoard/internal/cleanup"
	"github.com/ridoyf/unhoard/internal/finders"
	"github.com/ridoyf/unhoard/internal/insight"
	"github.com/ridoyf/unhoard/internal/kb"
	"github.com/ridoyf/unhoard/internal/protect"
	"github.com/ridoyf/unhoard/internal/report"
	"github.com/ridoyf/unhoard/internal/scanner"
	"github.com/ridoyf/unhoard/internal/store"
	"github.com/ridoyf/unhoard/internal/tree"
	"github.com/ridoyf/unhoard/internal/treemap"
)

// ScanInfo summarises the loaded scan.
type ScanInfo struct {
	Root         string   `json:"root"`
	RootID       uint32   `json:"rootId"`
	Method       string   `json:"method"`
	At           string   `json:"at"`
	DurationMs   int64    `json:"durationMs"`
	Files        uint32   `json:"files"`
	Bytes        uint64   `json:"bytes"`
	Logical      uint64   `json:"logical"`
	Errors       int      `json:"errors"`
	ErrorSamples []string `json:"errorSamples"`
}

// scanInfo needs a.mu held.
func (a *App) scanInfo() ScanInfo {
	t := a.scan.Tree
	samples := a.scan.Errors
	if len(samples) > 20 {
		samples = samples[:20]
	}
	return ScanInfo{Root: a.scanRoot, RootID: t.Root(), Method: a.scan.Method, At: a.scanAt.Format(time.RFC3339),
		DurationMs: a.scan.Duration.Milliseconds(), Files: t.Files(0), Bytes: t.Alloc(0), Logical: t.Size(0),
		Errors: a.scan.ErrCount, ErrorSamples: samples}
}

// StartScan scans root (a drive like `C:\` or any folder) in the background.
// Progress arrives as scan:progress events, then scan:done or scan:error.
func (a *App) StartScan(root string) error {
	root = filepath.Clean(root)
	if len(root) == 2 && root[1] == ':' {
		root += `\`
	}
	if !isDrivePath(root) {
		return errors.New("choose a drive or a folder to scan")
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a folder that can be read", root)
	}
	a.mu.Lock()
	if a.scanning {
		a.mu.Unlock()
		return errors.New("a scan is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.scanning, a.cancelScan = true, cancel
	a.mu.Unlock()

	go func() {
		defer cancel()
		res, err := scanner.Scan(ctx, root, a.elevated, func(p scanner.Progress) { emit(evScanProgress, p) })
		a.mu.Lock()
		a.scanning, a.cancelScan = false, nil
		if err != nil {
			a.mu.Unlock()
			msg := err.Error()
			if errors.Is(err, context.Canceled) {
				msg = "Scan cancelled."
			}
			emit(evScanError, msg)
			return
		}
		a.scan, a.scanRoot, a.scanAt, a.dups = res, root, time.Now(), nil
		info := a.scanInfo()
		dirs := insight.SnapshotDirs(res.Tree)
		a.mu.Unlock()
		// The scan's working memory (hard-link map, read buffers, the previous tree) is garbage
		// now; hand it back to Windows rather than holding it until the next scan.
		debug.FreeOSMemory()

		slog.Info("scan finished", "root", root, "method", res.Method, "files", info.Files, "ms", info.DurationMs, "errors", res.ErrCount)
		snap := store.Snapshot{At: time.Now(), Root: root, Method: res.Method, Bytes: info.Bytes, Files: uint64(info.Files)}
		if d, err := insight.DriveInfo(filepath.VolumeName(root) + `\`); err == nil {
			snap.VolTotal, snap.VolFree = d.Total, d.Free
		}
		if _, err := a.db.SaveSnapshot(snap, dirs); err != nil {
			slog.Error("saving snapshot", "err", err)
		}
		emit(evScanDone, info)
	}()
	return nil
}

func (a *App) CancelScan() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelScan != nil {
		a.cancelScan()
	}
}

// PickFolder shows a folder picker and returns the chosen path ("" if cancelled).
func (a *App) PickFolder() (string, error) {
	return application.Get().Dialog.OpenFile().CanChooseDirectories(true).CanChooseFiles(false).
		SetTitle("Choose a folder to scan").PromptForSingleSelection()
}

// Node is one file or folder as the UI shows it.
type Node struct {
	ID           uint32  `json:"id"`
	Name         string  `json:"name"`
	Path         string  `json:"path"`
	IsDir        bool    `json:"isDir"`
	Bytes        uint64  `json:"bytes"`   // size on disk
	Logical      uint64  `json:"logical"` // file length
	Files        uint32  `json:"files"`
	Modified     int64   `json:"modified"` // Unix seconds
	Percent      float64 `json:"percent"`  // of the parent's size on disk
	Cloud        bool    `json:"cloud"`
	HardLink     bool    `json:"hardLink"`
	Link         bool    `json:"link"`
	Inaccessible bool    `json:"inaccessible"`
	Hidden       bool    `json:"hidden"`
	Children     bool    `json:"children"`
	Safety       string  `json:"safety"` // from the knowledge base, "" if no rule matches this exact path
	RuleName     string  `json:"ruleName"`
}

func (a *App) node(t *tree.Tree, id tree.ID) Node {
	f := t.Flags(id)
	n := Node{ID: id, Name: t.Name(id), Path: t.Path(id), IsDir: f&tree.Dir != 0, Bytes: t.Alloc(id), Logical: t.Size(id),
		Files: t.Files(id), Modified: t.ModTime(id), Cloud: f&tree.Cloud != 0, HardLink: f&tree.HardLink != 0,
		Link: f&tree.Reparse != 0, Inaccessible: f&tree.Inaccessible != 0, Hidden: f&(tree.Hidden|tree.System) != 0}
	if p := t.Parent(id); p != tree.None && t.Alloc(p) > 0 {
		n.Percent = float64(n.Bytes) / float64(t.Alloc(p)) * 100
	}
	n.Children = n.IsDir && len(t.Children(id)) > 0
	if a.kb.MayMatchName(n.Name) {
		if r := a.kb.Match(n.Path); r != nil {
			n.Safety, n.RuleName = string(r.Safety), r.Name
		}
	}
	return n
}

func (a *App) nodes(t *tree.Tree, ids []tree.ID) []Node {
	out := make([]Node, 0, len(ids))
	for _, id := range ids {
		out = append(out, a.node(t, id))
	}
	return out
}

func validID(t *tree.Tree, id uint32) error {
	if int(id) >= t.Len() || t.Flags(id)&tree.Removed != 0 {
		return errors.New("that item is no longer in the scan; scan again")
	}
	return nil
}

// Page is a slice of a long list.
type Page struct {
	Items []Node `json:"items"`
	Total int    `json:"total"`
}

// Children lists a folder's contents. sortBy is size, name, files, modified or logical.
func (a *App) Children(id uint32, sortBy string, ascending bool, offset, limit int) (Page, error) {
	return readTree(a, func(t *tree.Tree) (Page, error) {
		if err := validID(t, id); err != nil {
			return Page{}, err
		}
		ids := t.Children(id)
		t.Sort(ids, sortBy, ascending)
		offset = max(0, min(offset, len(ids)))
		end := len(ids)
		if limit > 0 {
			end = min(end, offset+limit)
		}
		return Page{Items: a.nodes(t, ids[offset:end]), Total: len(ids)}, nil
	})
}

// NodeWithPath returns one item plus its ancestors (root first) for the breadcrumb.
func (a *App) NodeWithPath(id uint32) ([]Node, error) {
	return readTree(a, func(t *tree.Tree) ([]Node, error) {
		if err := validID(t, id); err != nil {
			return nil, err
		}
		var out []Node
		for n := id; n != tree.None; n = t.Parent(n) {
			out = append([]Node{a.node(t, n)}, out...)
		}
		return out, nil
	})
}

// FindPath returns the node for a full path in the loaded scan.
func (a *App) FindPath(path string) (Node, error) {
	return readTree(a, func(t *tree.Tree) (Node, error) {
		id, ok := t.Find(filepath.Clean(path))
		if !ok {
			return Node{}, fmt.Errorf("%s is not in the scan", path)
		}
		return a.node(t, id), nil
	})
}

// Tile is one treemap rectangle in pixels. Node is nil for the tile that stands
// in for many small items.
type Tile struct {
	Node   *Node   `json:"node"`
	Label  string  `json:"label"`
	Bytes  uint64  `json:"bytes"`
	Others int     `json:"others"` // items merged into this tile
	Depth  int     `json:"depth"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
}

// Treemap lays out a folder's contents in a w x h box.
func (a *App) Treemap(id uint32, w, h float64) ([]Tile, error) {
	return readTree(a, func(t *tree.Tree) ([]Tile, error) {
		if err := validID(t, id); err != nil {
			return nil, err
		}
		var out []Tile
		for _, r := range treemap.Layout(t, id, min(max(w, 0), 8000), min(max(h, 0), 8000)) {
			tile := Tile{Bytes: r.Bytes, Others: r.Others, Depth: r.Depth, X: r.X, Y: r.Y, W: r.W, H: r.H}
			if r.ID == tree.None {
				tile.Label = fmt.Sprintf("%d smaller items", r.Others)
			} else {
				n := a.node(t, r.ID)
				tile.Node, tile.Label = &n, n.Name
			}
			out = append(out, tile)
		}
		return out, nil
	})
}

// Explanation is what the knowledge base and the safety checks say about a path.
type Explanation struct {
	Rule        *kb.Rule `json:"rule"`
	RulePath    string   `json:"rulePath"`  // where the rule matched: the item or a parent folder
	Inherited   bool     `json:"inherited"` // the rule is for a parent folder
	CanDelete   bool     `json:"canDelete"` // quarantine would be allowed
	BlockReason string   `json:"blockReason"`
	ActionTitle string   `json:"actionTitle"`
}

// Explain runs the same checks as Quarantine, so the UI never offers what the backend would refuse.
func (a *App) Explain(path string) Explanation {
	var e Explanation
	e.Rule, e.RulePath = a.kb.Explain(path)
	e.Inherited = e.Rule != nil && !strings.EqualFold(filepath.Clean(path), e.RulePath)
	if e.Rule != nil && e.Rule.Action != "" {
		if act, ok := cleanup.Find(e.Rule.Action); ok {
			e.ActionTitle = act.Title
		}
	}
	if err := a.checkQuarantine(path); err != nil {
		e.BlockReason = protect.Reason(err)
	} else {
		e.CanDelete = true
	}
	return e
}

// SuggestRuleURL is a GitHub issue asking for a rule that recognises path, with the
// user's own folders replaced by variables.
func (a *App) SuggestRuleURL(path, details string) string {
	return kb.SuggestURL(kb.Generalise(path, os.Getenv), details)
}

// checkQuarantine runs every check Move would, plus "nothing inside is a never item".
func (a *App) checkQuarantine(path string) error {
	if err := a.guard.Check(path); err != nil {
		return err
	}
	inside, err := readTree(a, func(t *tree.Tree) (string, error) {
		id, ok := t.Find(filepath.Clean(path))
		if !ok {
			if fi, err := os.Lstat(path); err == nil && fi.IsDir() {
				return "", fmt.Errorf("%w: it is not in the current scan, so what is inside it has not been checked. Scan its drive, then try again", protect.ErrProtected)
			}
			return "", nil
		}
		m := finders.KBMatches(t, id, a.kb, func(r *kb.Rule) bool { return r.Safety == kb.Never })
		if len(m) == 0 {
			return "", nil
		}
		return fmt.Sprintf("%s (%s)", t.Path(m[0].ID), m[0].Rule.Name), nil
	})
	if err != nil {
		return err
	}
	if inside != "" {
		return fmt.Errorf("%w: it contains %s, which must never be deleted", protect.ErrProtected, inside)
	}
	return nil
}

// ExportReport asks where to save, then writes the loaded scan as CSV or HTML. It returns the saved path.
func (a *App) ExportReport(format string) (string, error) {
	if format != "csv" && format != "html" {
		return "", errors.New("format must be csv or html")
	}
	path, err := application.Get().Dialog.SaveFile().SetFilename("unhoard-report."+format).
		AddFilter(strings.ToUpper(format), "*."+format).PromptForSingleSelection()
	if err != nil || path == "" {
		return "", err
	}
	return readTree(a, func(t *tree.Tree) (string, error) {
		return path, report.Write(path, format, a.scanRoot, a.scanAt, t)
	})
}
