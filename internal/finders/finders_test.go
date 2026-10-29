package finders

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ridoyf/unhoard/internal/kb"
	"github.com/ridoyf/unhoard/internal/scanner"
	"github.com/ridoyf/unhoard/internal/tree"
)

var now = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

// fixture builds an in-memory tree; sizes are bytes on disk, ages in days.
func fixture() *tree.Tree {
	t := tree.New(`C:\`)
	day := int64(24 * 3600)
	add := func(parent tree.ID, name string, dir bool, size uint64, ageDays int64) tree.ID {
		var f tree.Flags
		if dir {
			f = tree.Dir
		}
		return t.Add(parent, name, f, size, size, now.Unix()-ageDays*day)
	}
	users := add(0, "Users", true, 0, 0)
	me := add(users, "me", true, 0, 0)
	dl := add(me, "Downloads", true, 0, 0)
	add(dl, "setup.exe", false, 50, 400)
	add(dl, "fresh.msi", false, 60, 1)
	add(dl, "photo.jpg", false, 70, 400)
	code := add(me, "code", true, 0, 0)
	nm := add(code, "node_modules", true, 0, 0)
	inner := add(nm, "pkg", true, 0, 0)
	add(inner, "node_modules", true, 0, 0)
	add(inner, "index.js", false, 5, 10)
	add(code, "movie.MKV", false, 900, 800)
	add(code, "notes.txt", false, 3, 1)
	steam := add(0, "Steam", true, 0, 0)
	sa := add(steam, "steamapps", true, 0, 0)
	common := add(sa, "common", true, 0, 0)
	g := add(common, "BigGame", true, 0, 0)
	add(g, "data.pak", false, 500, 30)
	od := add(me, "OneDrive", true, 0, 0)
	add(od, "local.bin", false, 40, 5)
	t.Add(od, "placeholder.bin", tree.Cloud, 40, 0, 0)
	hl := t.Add(code, "link-to-movie.mkv", tree.HardLink, 0, 0, 0)
	_ = hl
	t.Finalize()
	return t
}

func names(t *tree.Tree, ids []tree.ID) string {
	var out []string
	for _, id := range ids {
		out = append(out, t.Name(id))
	}
	return strings.Join(out, ",")
}

func TestLargest(t *testing.T) {
	tr := fixture()
	if got := names(tr, Largest(tr, 0, 3, false)); got != "movie.MKV,data.pak,photo.jpg" {
		t.Errorf("largest files = %s", got)
	}
	if got := names(tr, Largest(tr, 0, 2, true)); got != "Users,me" {
		t.Errorf("largest folders = %s", got)
	}
}

func TestTypeBreakdown(t *testing.T) {
	tr := fixture()
	got := TypeBreakdown(tr, 0, 2)
	if len(got) != 3 || got[0].Ext != "mkv" || got[1].Ext != "pak" || got[2].Ext != "(other)" {
		t.Fatalf("breakdown = %+v", got)
	}
	// exe msi jpg js txt bin bin; the hard link is not counted.
	if got[2].Files != 7 {
		t.Errorf("other = %+v, want 7 files", got[2])
	}
}

func TestSearch(t *testing.T) {
	tr := fixture()
	tests := []struct {
		name string
		f    Filter
		want string
	}{
		{"text", Filter{Text: "MOVIE"}, "movie.MKV"},
		{"ext", Filter{Exts: []string{".exe", "MSI"}}, "fresh.msi,setup.exe"},
		{"min size files", Filter{MinBytes: 500, Kind: "files"}, "movie.MKV,data.pak"},
		{"folders by name", Filter{Text: "node_modules", Kind: "folders"}, "node_modules,node_modules"},
		{"older than", Filter{OlderThanDays: 365, Kind: "files"}, "movie.MKV,photo.jpg,setup.exe"},
		{"newer than", Filter{NewerThanDays: 2, Kind: "files"}, "fresh.msi,notes.txt"},
		{"path", Filter{PathText: `steamapps\common`, Kind: "files"}, "data.pak"},
	}
	for _, tc := range tests {
		if got := names(tr, Search(tr, 0, tc.f, 10, now)); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
	if got := Search(tr, 0, Filter{}, 2, now); len(got) != 2 {
		t.Errorf("limit not applied: %d results", len(got))
	}
}

func TestBigStaleAndInstallers(t *testing.T) {
	tr := fixture()
	if got := names(tr, BigStale(tr, 0, 100, 180, 10, now)); got != "movie.MKV" {
		t.Errorf("big stale = %s", got)
	}
	if got := names(tr, OldInstallers(tr, `C:\Users\me\Downloads`, 30, now)); got != "setup.exe" {
		t.Errorf("old installers = %s", got)
	}
	if got := OldInstallers(tr, `C:\nope`, 30, now); got != nil {
		t.Errorf("missing downloads folder should return nil")
	}
}

func TestKBMatchesStopsAtMatch(t *testing.T) {
	tr := fixture()
	k, err := kb.Load(func(string) string { return "" }, fstest.MapFS{"dev.yaml": {Data: []byte(
		`[{id: nm, match: '**\node_modules', name: n, description: d, safety: rebuilds}]`)}})
	if err != nil {
		t.Fatal(err)
	}
	got := KBMatches(tr, 0, k, func(r *kb.Rule) bool { return r.Safety == kb.Rebuilds })
	if len(got) != 1 || tr.Path(got[0].ID) != `C:\Users\me\code\node_modules` {
		t.Errorf("KBMatches = %v", got)
	}
	if got := KBMatches(tr, 0, k, func(*kb.Rule) bool { return false }); len(got) != 0 {
		t.Errorf("keep=false should return nothing")
	}
}

func TestGamesAndCloud(t *testing.T) {
	tr := fixture()
	games := Games(tr, 0)
	if len(games) != 1 || tr.Name(games[0].ID) != "BigGame" || games[0].Launcher != "Steam" {
		t.Errorf("games = %+v", games)
	}
	if got := names(tr, CloudLocal(tr, []string{`C:\Users\me\OneDrive`}, 1, 10)); got != "local.bin" {
		t.Errorf("cloud local = %s", got)
	}
}

func TestDuplicates(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("a", partialBytes+10)
	bigOther := strings.Repeat("a", partialBytes) + strings.Repeat("b", 10) // same start, different end
	files := map[string]string{
		`one\a.txt`: "same content", `two\b.txt`: "same content", `three\c.txt`: "same content",
		`d.txt`:    "different!!!", // same size as above
		`big1.bin`: big, `x\big2.bin`: big, `big3.bin`: bigOther,
		`unique.bin`: "u",
	}
	for p, c := range files {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(c), 0o644)
	}
	res, err := scanner.Walk(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := Duplicates(context.Background(), DupCandidates(res.Tree, 0, 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	sizes := map[int]bool{}
	for _, g := range groups {
		sizes[len(g.IDs)] = true
	}
	if !sizes[3] || !sizes[2] {
		t.Errorf("want a group of 3 small files and a group of 2 big files, got %+v", groups)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Duplicates(ctx, DupCandidates(res.Tree, 0, 1), nil); err == nil {
		t.Errorf("cancelled search returned no error")
	}
}

func TestLiveDups(t *testing.T) {
	tr := tree.New(`C:\`)
	a := tr.Add(0, "a", 0, 1, 1, 0)
	b := tr.Add(0, "b", 0, 1, 1, 0)
	c := tr.Add(0, "c", 0, 1, 1, 0)
	d := tr.Add(0, "d", 0, 1, 1, 0)
	tr.Finalize()
	tr.Remove(c)
	got := LiveDups(tr, []DupGroup{{IDs: []tree.ID{a, b, c}}, {IDs: []tree.ID{c, d}}})
	if len(got) != 1 || names(tr, got[0].IDs) != "a,b" {
		t.Errorf("got %+v", got)
	}
}
