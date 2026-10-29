package insight

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMatchDataFolders(t *testing.T) {
	tree := map[string][]string{
		`C:\L`:        {"Google", "Spotify", "Temp", "Microsoft"},
		`C:\L\Google`: {"Chrome", "DriveFS"},
		`C:\R`:        {"Code", "Slack"},
	}
	apps := []App{
		{Name: "Google Chrome", Publisher: "Google LLC"},
		{Name: "Spotify", Publisher: "Spotify AB"},
		{Name: "Slack (x64) 4.41.105", Publisher: "Slack Technologies Inc."},
		{Name: "Microsoft Visual Studio Code", Publisher: "Microsoft Corporation"},
		{Name: "Go", Publisher: "Go"}, // too short to match safely
	}
	MatchDataFolders(apps, []string{`C:\L`, `C:\R`}, func(p string) []string { return tree[p] })
	want := [][]string{{`C:\L\Google\Chrome`}, {`C:\L\Spotify`}, {`C:\R\Slack`}, nil, nil}
	for i, a := range apps {
		if !slices.Equal(a.DataFolders, want[i]) {
			t.Errorf("%s: got %v, want %v", a.Name, a.DataFolders, want[i])
		}
	}
}

func TestParseShadowStorage(t *testing.T) {
	out := "1000|\\\\?\\Volume{aaa}\\\r\n2500|\\\\?\\Volume{bbb}\\\r\n"
	if got, ok := parseShadowStorage(out, `\\?\Volume{bbb}\`); !ok || got != 2500 {
		t.Errorf("got %d %v", got, ok)
	}
	if got, ok := parseShadowStorage("", `\\?\Volume{bbb}\`); !ok || got != 0 {
		t.Errorf("no shadow storage should be a known 0, got %d %v", got, ok)
	}
}

func TestExplainWithoutAdmin(t *testing.T) {
	fail := func(context.Context, func(string), string, ...string) (string, error) {
		t.Fatal("no command should run without admin")
		return "", nil
	}
	dir := t.TempDir()
	root := filepath.VolumeName(dir) + `\`
	h := Explain(context.Background(), root, 0, 2, "walk", false, fail)
	if h.Used == 0 || h.Gap != h.Used || h.Inaccessible != 2 {
		t.Errorf("Explain = %+v", h)
	}
	for _, it := range h.Items {
		if it.ID == "shadow-copies" && !it.NeedsAdmin {
			t.Errorf("shadow copies should need admin")
		}
	}
}

func TestExplainElevatedRunsRealPrograms(t *testing.T) {
	var ran []string
	record := func(_ context.Context, _ func(string), name string, _ ...string) (string, error) {
		ran = append(ran, name)
		return "", errors.New("fake")
	}
	root := filepath.VolumeName(t.TempDir()) + `\`
	Explain(context.Background(), root, 0, 0, "walk", true, record)
	if len(ran) == 0 {
		t.Fatal("no command ran")
	}
	for _, name := range ran {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("ran %q: %v", name, err)
		}
	}
}
