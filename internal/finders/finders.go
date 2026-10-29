package finders

import (
	"container/heap"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ridoyf/unhoard/internal/kb"
	"github.com/ridoyf/unhoard/internal/tree"
)

// skip marks nodes whose bytes are not really theirs to reclaim.
const skip = tree.HardLink | tree.Removed

// Largest returns the n biggest files (dirs=false) or folders (dirs=true) under root, by size on disk.
func Largest(t *tree.Tree, root tree.ID, n int, dirs bool) []tree.ID {
	h := &topN{t: t, n: n}
	t.Walk(root, func(id tree.ID) bool {
		if id != root && t.IsDir(id) == dirs && t.Flags(id)&skip == 0 {
			h.offer(id)
		}
		return true
	})
	return h.sorted()
}

// TypeStat is the space used by one file extension.
type TypeStat struct {
	Ext   string `json:"ext"` // lower case without the dot; "" for no extension
	Bytes uint64 `json:"bytes"`
	Files uint64 `json:"files"`
}

// TypeBreakdown totals files under root by extension, biggest first, keeping the top n and an "other" bucket.
func TypeBreakdown(t *tree.Tree, root tree.ID, n int) []TypeStat {
	byExt := map[string]*TypeStat{}
	t.Walk(root, func(id tree.ID) bool {
		if !t.IsDir(id) && t.Flags(id)&skip == 0 {
			ext := Ext(t.Name(id))
			s := byExt[ext]
			if s == nil {
				s = &TypeStat{Ext: ext}
				byExt[ext] = s
			}
			s.Bytes += t.Alloc(id)
			s.Files++
		}
		return true
	})
	out := make([]TypeStat, 0, len(byExt))
	for _, s := range byExt {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	if len(out) > n {
		other := TypeStat{Ext: "(other)"}
		for _, s := range out[n:] {
			other.Bytes += s.Bytes
			other.Files += s.Files
		}
		out = append(out[:n], other)
	}
	return out
}

// Ext returns a file name's extension, lower case without the dot.
func Ext(name string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
}

// Filter narrows a search. Zero values mean "no limit".
type Filter struct {
	Text          string   `json:"text"`          // case-insensitive substring of the name
	PathText      string   `json:"pathText"`      // case-insensitive substring of the full path
	MinBytes      uint64   `json:"minBytes"`      // size on disk
	Exts          []string `json:"exts"`          // lower case, no dot
	OlderThanDays int      `json:"olderThanDays"` // not modified for at least this long
	NewerThanDays int      `json:"newerThanDays"` // modified within this many days
	Kind          string   `json:"kind"`          // "files", "folders" or "" for both
}

// Search returns up to limit matches under root, biggest first.
func Search(t *tree.Tree, root tree.ID, f Filter, limit int, now time.Time) []tree.ID {
	text := strings.ToLower(f.Text)
	pathText := strings.ToLower(f.PathText)
	exts := map[string]bool{}
	for _, e := range f.Exts {
		exts[strings.TrimPrefix(strings.ToLower(strings.TrimSpace(e)), ".")] = true
	}
	h := &topN{t: t, n: limit}
	t.Walk(root, func(id tree.ID) bool {
		if id == root || t.Flags(id)&skip != 0 {
			return true
		}
		dir := t.IsDir(id)
		switch {
		case f.Kind == "files" && dir, f.Kind == "folders" && !dir:
			return true
		case t.Alloc(id) < f.MinBytes:
			// Folders are at least as big as anything inside them, so a small folder rules out its subtree.
			return !dir
		case len(exts) > 0 && (dir || !exts[Ext(t.Name(id))]):
			return true
		case f.OlderThanDays > 0 && (t.ModTime(id) == 0 || t.ModTime(id) > now.AddDate(0, 0, -f.OlderThanDays).Unix()):
			return true
		case f.NewerThanDays > 0 && t.ModTime(id) < now.AddDate(0, 0, -f.NewerThanDays).Unix():
			return true
		case text != "" && !strings.Contains(strings.ToLower(t.Name(id)), text):
			return true
		case pathText != "" && !strings.Contains(strings.ToLower(t.Path(id)), pathText):
			return true
		}
		h.offer(id)
		return true
	})
	return h.sorted()
}

// BigStale returns files of at least minBytes not modified for olderThanDays, biggest first.
func BigStale(t *tree.Tree, root tree.ID, minBytes uint64, olderThanDays, limit int, now time.Time) []tree.ID {
	return Search(t, root, Filter{MinBytes: minBytes, OlderThanDays: olderThanDays, Kind: "files"}, limit, now)
}

// Match pairs a node with the knowledge-base rule that explains it.
type Match struct {
	ID   tree.ID
	Rule *kb.Rule
}

// KBMatches finds nodes under root matched by a rule that keep returns true for.
// It does not look inside a matched node, so node_modules inside node_modules is reported once.
func KBMatches(t *tree.Tree, root tree.ID, k *kb.KB, keep func(*kb.Rule) bool) []Match {
	var out []Match
	t.Walk(root, func(id tree.ID) bool {
		if t.Flags(id)&skip != 0 || !k.MayMatchName(t.Name(id)) {
			return true
		}
		r := k.Match(t.Path(id))
		if r == nil || !keep(r) {
			return true
		}
		out = append(out, Match{id, r})
		return false
	})
	sort.Slice(out, func(i, j int) bool { return t.Alloc(out[i].ID) > t.Alloc(out[j].ID) })
	return out
}

var installerExts = map[string]bool{"exe": true, "msi": true, "msix": true, "msixbundle": true, "appx": true, "appxbundle": true, "iso": true, "img": true}

// OldInstallers returns installers and disk images under the Downloads folder untouched for olderThanDays.
func OldInstallers(t *tree.Tree, downloads string, olderThanDays int, now time.Time) []tree.ID {
	id, ok := t.Find(downloads)
	if !ok {
		return nil
	}
	exts := make([]string, 0, len(installerExts))
	for e := range installerExts {
		exts = append(exts, e)
	}
	return Search(t, id, Filter{Exts: exts, OlderThanDays: olderThanDays, Kind: "files"}, 1000, now)
}

// Game is one installed game found in a known library folder.
type Game struct {
	ID       tree.ID
	Launcher string
}

// gameLibraries maps a library folder's path suffix (lower case) to its launcher.
var gameLibraries = []struct{ suffix, launcher string }{
	{`\steamapps\common`, "Steam"},
	{`\epic games`, "Epic Games"},
	{`\xboxgames`, "Xbox"},
	{`\gog galaxy\games`, "GOG"},
	{`\gog games`, "GOG"},
	{`\ea games`, "EA app"},
	{`\ubisoft game launcher\games`, "Ubisoft Connect"},
	{`\riot games`, "Riot"},
}

// Games lists every child folder of known game library folders under root, biggest first.
func Games(t *tree.Tree, root tree.ID) []Game {
	var out []Game
	t.Walk(root, func(id tree.ID) bool {
		if !t.IsDir(id) || t.Flags(id)&skip != 0 {
			return true
		}
		name := strings.ToLower(t.Name(id))
		for _, lib := range gameLibraries {
			if !strings.HasSuffix(lib.suffix, `\`+name) || !strings.HasSuffix(strings.ToLower(t.Path(id)), lib.suffix) {
				continue
			}
			for _, c := range t.Children(id) {
				if t.IsDir(c) && !strings.EqualFold(t.Name(c), "launcher") && !strings.EqualFold(t.Name(c), "DirectXRedist") {
					out = append(out, Game{c, lib.launcher})
				}
			}
			return false
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return t.Alloc(out[i].ID) > t.Alloc(out[j].ID) })
	return out
}

// CloudLocal returns files under the sync roots that are fully downloaded (not
// placeholders) and at least minBytes, biggest first. These can be made online-only.
func CloudLocal(t *tree.Tree, syncRoots []string, minBytes uint64, limit int) []tree.ID {
	h := &topN{t: t, n: limit}
	for _, r := range syncRoots {
		id, ok := t.Find(r)
		if !ok {
			continue
		}
		t.Walk(id, func(n tree.ID) bool {
			if !t.IsDir(n) && t.Flags(n)&(skip|tree.Cloud) == 0 && t.Alloc(n) >= minBytes {
				h.offer(n)
			}
			return true
		})
	}
	return h.sorted()
}

// topN keeps the n largest nodes by size on disk using a min-heap.
type topN struct {
	t   *tree.Tree
	n   int
	ids []tree.ID
}

func (h *topN) Len() int           { return len(h.ids) }
func (h *topN) Less(i, j int) bool { return h.t.Alloc(h.ids[i]) < h.t.Alloc(h.ids[j]) }
func (h *topN) Swap(i, j int)      { h.ids[i], h.ids[j] = h.ids[j], h.ids[i] }
func (h *topN) Push(x any)         { h.ids = append(h.ids, x.(tree.ID)) }
func (h *topN) Pop() any {
	x := h.ids[len(h.ids)-1]
	h.ids = h.ids[:len(h.ids)-1]
	return x
}

func (h *topN) offer(id tree.ID) {
	if h.n <= 0 {
		return
	}
	if len(h.ids) < h.n {
		heap.Push(h, id)
	} else if h.t.Alloc(id) > h.t.Alloc(h.ids[0]) {
		h.ids[0] = id
		heap.Fix(h, 0)
	}
}

func (h *topN) sorted() []tree.ID {
	out := append([]tree.ID(nil), h.ids...)
	h.t.Sort(out, "size", false)
	return out
}
