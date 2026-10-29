package insight

import (
	"sort"

	"github.com/ridoyf/unhoard/internal/store"
	"github.com/ridoyf/unhoard/internal/tree"
)

// SnapshotMinBytes is the smallest folder recorded in a snapshot (shallow folders are always recorded).
const SnapshotMinBytes = 10 << 20

// SnapshotDirs picks the folders worth recording: the top two levels plus anything of at least 10 MB.
// shortcut: folders under 10 MB are not stored, so growth from below that line is reported from 0;
// store every folder if that proves misleading, at the cost of much larger snapshots.
func SnapshotDirs(t *tree.Tree) []store.DirSize {
	var out []store.DirSize
	var visit func(id tree.ID, depth int)
	visit = func(id tree.ID, depth int) {
		if depth > 2 && t.Alloc(id) < SnapshotMinBytes {
			return
		}
		out = append(out, store.DirSize{Path: t.Path(id), Bytes: t.Alloc(id), Files: uint64(t.Files(id))})
		for _, c := range t.Children(id) {
			if t.IsDir(c) && t.Flags(c)&tree.Reparse == 0 {
				visit(c, depth+1)
			}
		}
	}
	visit(t.Root(), 0)
	return out
}

// Growth is how much a folder changed between a snapshot and now.
type Growth struct {
	Path   string `json:"path"`
	Before uint64 `json:"before"`
	After  uint64 `json:"after"`
	Delta  int64  `json:"delta"`
	IsNew  bool   `json:"isNew"`
}

// Grew compares a snapshot's folders with the current tree and returns the biggest changes
// (growth first, then shrinkage), at most limit of each.
func Grew(t *tree.Tree, before map[string]store.DirSize, limit int) (grew, shrank []Growth) {
	now := SnapshotDirs(t)
	seen := map[string]bool{}
	var all []Growth
	for _, d := range now {
		seen[d.Path] = true
		old, existed := before[d.Path]
		all = append(all, Growth{Path: d.Path, Before: old.Bytes, After: d.Bytes, Delta: int64(d.Bytes) - int64(old.Bytes), IsNew: !existed})
	}
	for p, old := range before {
		if seen[p] {
			continue
		}
		// Not in today's list: either gone, or now under the recording threshold.
		var after uint64
		if id, ok := t.Find(p); ok {
			after = t.Alloc(id)
		}
		all = append(all, Growth{Path: p, Before: old.Bytes, After: after, Delta: int64(after) - int64(old.Bytes)})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Delta > all[j].Delta })
	for _, g := range all {
		if g.Delta > 0 && len(grew) < limit {
			grew = append(grew, g)
		}
	}
	for i := len(all) - 1; i >= 0 && len(shrank) < limit; i-- {
		if all[i].Delta < 0 {
			shrank = append(shrank, all[i])
		}
	}
	return grew, shrank
}
