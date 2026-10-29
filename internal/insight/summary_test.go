package insight

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/ridoyf/unhoard/internal/finders"
	"github.com/ridoyf/unhoard/internal/kb"
	"github.com/ridoyf/unhoard/internal/quarantine"
	"github.com/ridoyf/unhoard/internal/tree"
)

func TestSummary(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -60).Unix()
	tr := tree.New(`C:\`)
	dl := tr.Add(0, "Downloads", tree.Dir, 0, 0, 0)
	tr.Add(dl, "setup.exe", 0, 40, 40, old)
	tr.Add(dl, "new.msi", 0, 99, 99, now.Unix())
	tr.Add(tr.Add(0, "code", tree.Dir, 0, 0, 0), "node_modules", tree.Dir, 0, 0, 0)
	cache := tr.Add(0, "Cache", tree.Dir, 0, 0, 0)
	tr.Add(cache, "x", 0, 500, 500, 0)
	tr.Add(tr.Add(0, "profile", tree.Dir, 0, 0, 0), "f", 0, 900, 900, 0)
	tr.Finalize()
	k, err := kb.Load(func(string) string { return "" }, fstest.MapFS{"a.yaml": {Data: []byte(`
- {id: cache, match: 'C:\Cache', name: n, description: d, safety: safe}
- {id: nm, match: '**\node_modules', name: n, description: d, safety: rebuilds}
- {id: profile, match: 'C:\profile', name: n, description: d, safety: never}
`)}})
	if err != nil {
		t.Fatal(err)
	}
	dups := []finders.DupGroup{{IDs: []tree.ID{1, 2, 3}, Bytes: 10}}
	quarantined := []quarantine.Item{{Bytes: 7}, {Bytes: 3}}

	got := map[string]SummaryItem{}
	for _, it := range Summary(tr, k, dups, quarantined, `C:\Downloads`, now) {
		got[it.ID] = it
	}
	want := map[string]uint64{"caches": 500, "installers": 40, "duplicates": 20, "quarantine": 10}
	for id, bytes := range want {
		if got[id].Bytes != bytes {
			t.Errorf("%s: %d bytes, want %d", id, got[id].Bytes, bytes)
		}
	}
	// node_modules is empty, so "dev" frees nothing and is left out; never items are never counted.
	if _, ok := got["dev"]; ok {
		t.Errorf("an item that frees nothing should be left out")
	}
}

func TestFootprints(t *testing.T) {
	tr := tree.New(`C:\`)
	app := tr.Add(0, "App", tree.Dir, 0, 0, 0)
	tr.Add(app, "a.exe", 0, 100, 100, 0)
	data := tr.Add(0, "Data", tree.Dir, 0, 0, 0)
	tr.Add(data, "d", 0, 50, 50, 0)
	tr.Finalize()
	apps := []App{
		{Name: "Claimed", EstimatedBytes: 120},
		{Name: "Measured", InstallLocation: `C:\App`, DataFolders: []string{`C:\Data`, `C:\Missing`}},
		{Name: "Unknown"},
	}
	out := Footprints(apps, tr)
	if out[0].Name != "Measured" || out[0].InstallBytes != 100 || out[0].DataBytes != 50 {
		t.Errorf("first = %+v, want Measured with 100 + 50 bytes", out[0])
	}
	if out[1].Name != "Claimed" || out[2].Name != "Unknown" {
		t.Errorf("order = %s, %s; want Claimed, Unknown", out[1].Name, out[2].Name)
	}
	if out := Footprints(apps, nil); out[0].Name != "Claimed" || out[0].InstallBytes != 0 {
		t.Errorf("without a scan, apps rank by their claimed size, got %+v", out[0])
	}
}
