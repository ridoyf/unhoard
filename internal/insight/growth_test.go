package insight

import (
	"testing"

	"github.com/ridoyf/unhoard/internal/store"
	"github.com/ridoyf/unhoard/internal/tree"
)

func TestSnapshotDirsAndGrew(t *testing.T) {
	const mb = 1 << 20
	tr := tree.New(`C:\`)
	a := tr.Add(0, "a", tree.Dir, 0, 0, 0)
	b := tr.Add(a, "b", tree.Dir, 0, 0, 0)
	c := tr.Add(b, "c", tree.Dir, 0, 0, 0)
	tiny := tr.Add(c, "tiny", tree.Dir, 0, 0, 0) // depth 4, 1 MB: not recorded
	tr.Add(tiny, "f", 0, mb, mb, 0)
	big := tr.Add(c, "big", tree.Dir, 0, 0, 0)
	tr.Add(big, "f", 0, 50*mb, 50*mb, 0)
	tr.Add(0, "link", tree.Dir|tree.Reparse, 0, 0, 0)
	tr.Finalize()

	dirs := SnapshotDirs(tr)
	got := map[string]uint64{}
	for _, d := range dirs {
		got[d.Path] = d.Bytes
	}
	for _, p := range []string{`C:\`, `C:\a`, `C:\a\b`, `C:\a\b\c`, `C:\a\b\c\big`} {
		if _, ok := got[p]; !ok {
			t.Errorf("%s not recorded", p)
		}
	}
	if _, ok := got[`C:\a\b\c\tiny`]; ok {
		t.Errorf("small deep folder recorded")
	}
	if _, ok := got[`C:\link`]; ok {
		t.Errorf("junction recorded")
	}

	before := map[string]store.DirSize{
		`C:\`:          {Bytes: 20 * mb},
		`C:\a`:         {Bytes: 20 * mb},
		`C:\a\b`:       {Bytes: 20 * mb},
		`C:\a\b\c`:     {Bytes: 20 * mb},
		`C:\a\b\c\old`: {Bytes: 19 * mb}, // deleted since
	}
	grew, shrank := Grew(tr, before, 10)
	if len(grew) != 5 || grew[0].Path != `C:\a\b\c\big` || !grew[0].IsNew || grew[0].Delta != 50*mb {
		t.Errorf("grew = %+v", grew)
	}
	if grew[1].Delta != 31*mb || grew[1].IsNew {
		t.Errorf("parent growth = %+v, want 31 MB", grew[1])
	}
	if len(shrank) != 1 || shrank[0].Path != `C:\a\b\c\old` || shrank[0].After != 0 {
		t.Errorf("shrank = %+v", shrank)
	}
}
