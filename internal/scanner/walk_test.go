package scanner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ridoyf/unhoard/internal/tree"
)

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWalk(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.bin"), 1000)
	write(t, filepath.Join(root, `sub\b.bin`), 2000)
	write(t, filepath.Join(root, `sub\deep\c.bin`), 3000)
	write(t, filepath.Join(root, `outside\d.bin`), 5000)
	if err := os.Link(filepath.Join(root, "a.bin"), filepath.Join(root, `sub\a-link.bin`)); err != nil {
		t.Fatal(err)
	}
	// A junction back to a sibling must not be followed or the sizes double.
	junction := filepath.Join(root, `sub\to-outside`)
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, filepath.Join(root, "outside")).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v %s", err, out)
	}

	var calls atomic.Int32
	res, err := Walk(context.Background(), root, func(Progress) { calls.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	tr := res.Tree
	if got, want := tr.Size(0), uint64(11000); got != want {
		t.Errorf("root size = %d, want %d (hard link or junction counted twice?)", got, want)
	}
	if got := tr.Files(0); got != 5 {
		t.Errorf("file count = %d, want 5 (four files plus the hard link)", got)
	}
	sub, ok := tr.Find(filepath.Join(root, "sub"))
	if !ok || tr.Size(sub) != 5000 {
		t.Errorf("sub size = %d, want 5000", tr.Size(sub))
	}
	link, _ := tr.Find(filepath.Join(root, `sub\a-link.bin`))
	a, _ := tr.Find(filepath.Join(root, "a.bin"))
	if (tr.Flags(link)|tr.Flags(a))&tree.HardLink == 0 {
		t.Errorf("one of the two links should be flagged HardLink")
	}
	j, ok := tr.Find(junction)
	if !ok || tr.Flags(j)&tree.Reparse == 0 || len(tr.Children(j)) != 0 {
		t.Errorf("junction should be listed, flagged Reparse and not descended")
	}
	if tr.Alloc(0) < tr.Size(0) {
		t.Errorf("size on disk %d is smaller than logical size %d", tr.Alloc(0), tr.Size(0))
	}
	if tr.ModTime(a) == 0 {
		t.Errorf("modified time missing")
	}
	if calls.Load() == 0 {
		t.Errorf("progress never reported")
	}
	if res.Method != "walk" || res.ErrCount != 0 {
		t.Errorf("method=%s errors=%v", res.Method, res.Errors)
	}
}

func TestWalkLongPaths(t *testing.T) {
	root := t.TempDir()
	deep := root
	for len(deep) < 300 {
		deep = filepath.Join(deep, strings.Repeat("x", 40))
	}
	write(t, filepath.Join(deep, "f.bin"), 10)
	res, err := Walk(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tree.Size(0) != 10 || res.ErrCount != 0 {
		t.Errorf("size=%d errors=%v", res.Tree.Size(0), res.Errors)
	}
}

func TestWalkCancel(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, `a\b.bin`), 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Walk(ctx, root, nil); err == nil {
		t.Errorf("cancelled walk returned no error")
	}
}

func TestWalkMissingRoot(t *testing.T) {
	if _, err := Walk(context.Background(), filepath.Join(t.TempDir(), "nope"), nil); err == nil {
		t.Errorf("expected an error for a missing root")
	}
}

func BenchmarkWalkTemp(b *testing.B) {
	root := b.TempDir()
	for i := range 2000 {
		p := filepath.Join(root, "d"+string(rune('a'+i%26)), "f"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+".bin")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	for b.Loop() {
		if _, err := Walk(context.Background(), root, nil); err != nil {
			b.Fatal(err)
		}
	}
}
