package tree

import (
	"fmt"
	"runtime"
	"slices"
	"testing"
)

// build makes C:\ with: a\ (x 10B, y 20B), a\b\ (z 5B), w 100B.
func build() (*Tree, map[string]ID) {
	t := New(`C:\`)
	ids := map[string]ID{}
	ids["a"] = t.Add(0, "a", Dir, 0, 0, 0)
	ids["x"] = t.Add(ids["a"], "x", 0, 10, 4096, 0)
	ids["y"] = t.Add(ids["a"], "y", 0, 20, 4096, 0)
	ids["b"] = t.Add(ids["a"], "b", Dir, 0, 0, 0)
	ids["z"] = t.Add(ids["b"], "z", 0, 5, 4096, 0)
	ids["w"] = t.Add(0, "w", 0, 100, 4096, 0)
	t.Finalize()
	return t, ids
}

func TestFinalizeTotals(t *testing.T) {
	tr, ids := build()
	tests := []struct {
		node        string
		size, alloc uint64
		files       uint32
	}{
		{"root", 135, 16384, 4},
		{"a", 35, 12288, 3},
		{"b", 5, 4096, 1},
	}
	for _, tc := range tests {
		id := ids[tc.node]
		if tc.node == "root" {
			id = 0
		}
		if got := tr.Size(id); got != tc.size {
			t.Errorf("%s size = %d, want %d", tc.node, got, tc.size)
		}
		if got := tr.Alloc(id); got != tc.alloc {
			t.Errorf("%s alloc = %d, want %d", tc.node, got, tc.alloc)
		}
		if got := tr.Files(id); got != tc.files {
			t.Errorf("%s files = %d, want %d", tc.node, got, tc.files)
		}
	}
}

func TestPathAndFind(t *testing.T) {
	tr, ids := build()
	tests := []struct {
		node, path string
	}{
		{"a", `C:\a`},
		{"z", `C:\a\b\z`},
		{"w", `C:\w`},
	}
	for _, tc := range tests {
		if got := tr.Path(ids[tc.node]); got != tc.path {
			t.Errorf("Path(%s) = %q, want %q", tc.node, got, tc.path)
		}
		if got, ok := tr.Find(tc.path); !ok || got != ids[tc.node] {
			t.Errorf("Find(%q) = %d,%v, want %d", tc.path, got, ok, ids[tc.node])
		}
	}
	if got := tr.Path(0); got != `C:\` {
		t.Errorf("root path = %q", got)
	}
	if id, ok := tr.Find(`c:\A\B\Z`); !ok || id != ids["z"] {
		t.Errorf("Find should be case-insensitive")
	}
	if _, ok := tr.Find(`C:\a\nope`); ok {
		t.Errorf("Find of a missing path should fail")
	}
	if _, ok := tr.Find(`D:\a`); ok {
		t.Errorf("Find on another drive should fail")
	}
}

func TestRemove(t *testing.T) {
	tr, ids := build()
	tr.Remove(ids["b"])
	if tr.Size(0) != 130 || tr.Size(ids["a"]) != 30 || tr.Files(0) != 3 {
		t.Fatalf("after removing b: root=%d a=%d files=%d", tr.Size(0), tr.Size(ids["a"]), tr.Files(0))
	}
	tr.Remove(ids["b"]) // twice must be a no-op
	if tr.Size(0) != 130 {
		t.Fatalf("second Remove changed sizes")
	}
	if slices.Contains(tr.Children(ids["a"]), ids["b"]) {
		t.Errorf("removed node still listed as a child")
	}
	if _, ok := tr.Find(`C:\a\b`); ok {
		t.Errorf("removed node still findable")
	}
	tr.Remove(ids["x"])
	if tr.Files(0) != 2 || tr.Size(ids["a"]) != 20 {
		t.Errorf("after removing file x: files=%d a=%d", tr.Files(0), tr.Size(ids["a"]))
	}
}

func TestWalkSkipsSubtree(t *testing.T) {
	tr, ids := build()
	var seen []string
	tr.Walk(0, func(id ID) bool {
		seen = append(seen, tr.Name(id))
		return id != ids["b"]
	})
	if slices.Contains(seen, "z") || !slices.Contains(seen, "b") || len(seen) != 6 {
		t.Errorf("walk saw %v", seen)
	}
}

// BenchmarkBuild reports memory per node for a tree shaped like a real drive.
func TestSort(t *testing.T) {
	tr := New(`C:\`)
	b := tr.Add(0, "b", 0, 10, 300, 3)
	a := tr.Add(0, "A", 0, 30, 100, 1)
	c := tr.Add(0, "c", 0, 20, 200, 2)
	tests := []struct {
		by        string
		ascending bool
		want      []ID
	}{
		{"size", false, []ID{b, c, a}},
		{"name", true, []ID{a, b, c}},
		{"modified", true, []ID{a, c, b}},
		{"logical", false, []ID{a, c, b}},
		{"unknown", true, []ID{a, c, b}},
	}
	for _, tc := range tests {
		ids := []ID{a, b, c}
		tr.Sort(ids, tc.by, tc.ascending)
		if !slices.Equal(ids, tc.want) {
			t.Errorf("Sort by %s: got %v, want %v", tc.by, ids, tc.want)
		}
	}
}

func BenchmarkBuild(b *testing.B) {
	const n = 1_000_000
	for b.Loop() {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		tr := New(`C:\`)
		dir := tr.Root()
		for i := range n {
			if i%20 == 0 {
				dir = tr.Add(0, fmt.Sprintf("folder-%07d", i), Dir, 0, 0, 0)
				continue
			}
			tr.Add(dir, fmt.Sprintf("file-%07d.dat", i), 0, uint64(i), uint64(i), int64(i))
		}
		tr.Finalize()
		runtime.GC()
		runtime.ReadMemStats(&after)
		b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/n, "bytes/node")
		runtime.KeepAlive(tr)
	}
}

func TestCount(t *testing.T) {
	tr, ids := build()
	tr.SetFlag(ids["b"], Inaccessible)
	tr.SetFlag(ids["w"], Inaccessible)
	if got := tr.Count(0, Inaccessible); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	tr.Remove(ids["w"])
	if got := tr.Count(0, Inaccessible); got != 1 {
		t.Errorf("Count after Remove = %d, want 1", got)
	}
}
