package insight

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/ridoyf/unhoard/internal/finders"
	"github.com/ridoyf/unhoard/internal/kb"
	"github.com/ridoyf/unhoard/internal/quarantine"
	"github.com/ridoyf/unhoard/internal/tree"
)

// SummaryItem is one way to free space.
type SummaryItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Bytes  uint64 `json:"bytes"`
	Count  int    `json:"count"`
	View   string `json:"view"`   // frontend view that handles it
	Action string `json:"action"` // or a cleanup action id
}

// Summary lists what could be freed, biggest first, leaving out anything that frees nothing.
// Personal files (duplicates) are described as worth reviewing, not as safe.
func Summary(t *tree.Tree, k *kb.KB, dups []finders.DupGroup, quarantined []quarantine.Item, downloads string, now time.Time) []SummaryItem {
	caches := SummaryItem{ID: "caches", Title: "App caches and temporary files", Detail: "Rebuilt automatically when needed.", View: "caches"}
	dev := SummaryItem{ID: "dev", Title: "Developer junk", Detail: "Package folders and build caches that tools restore.", View: "dev"}
	for _, m := range finders.KBMatches(t, 0, k, func(r *kb.Rule) bool { return r.Safety == kb.Safe || r.Safety == kb.Rebuilds }) {
		it := &caches
		if m.Rule.Category == "dev" {
			it = &dev
		}
		it.Bytes += t.Alloc(m.ID)
		it.Count++
	}
	installers := SummaryItem{ID: "installers", Title: "Old installers in Downloads", Detail: "Setup files you have not touched for 30 days.", View: "installers"}
	for _, id := range finders.OldInstallers(t, downloads, 30, now) {
		installers.Bytes += t.Alloc(id)
		installers.Count++
	}
	duplicates := SummaryItem{ID: "duplicates", Title: "Duplicate files", Detail: "Extra copies of identical files. Review before removing.", Count: len(dups), View: "duplicates"}
	for _, g := range dups {
		duplicates.Bytes += g.Bytes * uint64(len(g.IDs)-1)
	}
	q := SummaryItem{ID: "quarantine", Title: "Quarantine", Detail: "Already removed; space returns when the quarantine is emptied.", View: "quarantine"}
	for _, it := range quarantined {
		q.Bytes += it.Bytes
		q.Count++
	}
	items := []SummaryItem{caches, dev, installers, duplicates, q}

	if b, n, err := RecycleBin(""); err == nil {
		items = append(items, SummaryItem{ID: "recycle", Title: "Recycle Bin", Detail: fmt.Sprintf("%d deleted items.", n), Bytes: b, Count: int(n), Action: "empty-recycle-bin"})
	}
	if fi, err := os.Stat(os.Getenv("SystemDrive") + `\hiberfil.sys`); err == nil {
		items = append(items, SummaryItem{ID: "hibernation", Title: "Hibernation file", Detail: "Freed by turning off hibernation and Fast Startup.", Bytes: uint64(fi.Size()), Action: "disable-hibernation"})
	}

	var out []SummaryItem
	for _, it := range items {
		if it.Bytes > 0 {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}
