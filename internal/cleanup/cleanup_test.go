package cleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ridoyf/unhoard/internal/insight"
)

// fakeRunner records commands instead of running them; tests never run real system commands.
type fakeRunner struct {
	ran    [][]string
	inputs []string
	output map[string]string // program file name -> output
	fail   map[string]bool
}

func (f *fakeRunner) run(_ context.Context, _ func(string), name string, args ...string) (string, error) {
	f.ran = append(f.ran, append([]string{name}, args...))
	if f.fail[filepath.Base(name)] {
		return "boom", errors.New("failed")
	}
	return f.output[filepath.Base(name)], nil
}

func (f *fakeRunner) runInput(ctx context.Context, onLine func(string), input, name string, args ...string) (string, error) {
	f.inputs = append(f.inputs, input)
	return f.run(ctx, onLine, name, args...)
}

func TestEveryActionIsReachable(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range All {
		if a.ID == "" || a.Title == "" || a.Description == "" || a.Preview == nil {
			t.Errorf("action %q is missing fields", a.ID)
		}
		if seen[a.ID] {
			t.Errorf("duplicate action id %s", a.ID)
		}
		seen[a.ID] = true
		if got, ok := Find(a.ID); !ok || got.ID != a.ID {
			t.Errorf("Find(%s) failed", a.ID)
		}
	}
}

func TestAdminActionsUnavailableWhenNotElevated(t *testing.T) {
	f := &fakeRunner{}
	env := Env{Elevated: false, Run: f.run}
	for _, a := range All {
		if !a.NeedsAdmin {
			continue
		}
		p, err := Preview(context.Background(), env, a.ID, nil)
		if err != nil || p.Available || p.Reason == "" {
			t.Errorf("%s: plan=%+v err=%v; want unavailable with a reason", a.ID, p, err)
		}
		if _, _, err := Execute(context.Background(), env, a.ID, nil, p.Commands); err == nil {
			t.Errorf("%s ran without admin", a.ID)
		}
	}
	if len(f.ran) != 0 {
		t.Errorf("commands ran without admin: %v", f.ran)
	}
}

func TestDockerPreviewAndRun(t *testing.T) {
	df := `TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE
Images          10        2         5.2GB     3.1GB (59%)
Containers      3         1         20MB      15MB (75%)
Local Volumes   4         2         1GB       500MB (50%)
Build Cache     40        0         2GB       2GB`
	f := &fakeRunner{output: map[string]string{"docker": df}}
	env := Env{Run: f.run}
	p, err := Preview(context.Background(), env, "docker-prune", nil)
	if err != nil || !p.Available {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	if want := uint64(15e6 + 2e9); p.Bytes != want {
		t.Errorf("bytes = %d, want %d (containers and build cache only)", p.Bytes, want)
	}
	if !slices.Equal(p.Commands, []string{"docker system prune --force"}) {
		t.Errorf("commands = %v", p.Commands)
	}
	f.ran = nil
	if _, _, err := Execute(context.Background(), env, "docker-prune", nil, p.Commands); err != nil {
		t.Fatal(err)
	}
	if len(f.ran) != 2 || strings.Join(f.ran[1], " ") != "docker system prune --force" {
		t.Errorf("ran %v", f.ran)
	}
}

func TestExecuteRefusesChangedPlan(t *testing.T) {
	f := &fakeRunner{output: map[string]string{"docker": ""}}
	env := Env{Run: f.run}
	if _, _, err := Execute(context.Background(), env, "docker-prune", nil, []string{"docker system prune --all --volumes"}); err == nil {
		t.Fatal("ran commands the user did not confirm")
	}
	for _, c := range f.ran {
		if slices.Contains(c, "prune") {
			t.Errorf("prune ran despite the mismatch")
		}
	}
	if _, _, err := Execute(context.Background(), env, "no-such-action", nil, nil); err == nil {
		t.Errorf("unknown action accepted")
	}
}

func TestDockerUnavailable(t *testing.T) {
	f := &fakeRunner{fail: map[string]bool{"docker": true}}
	p, _ := Preview(context.Background(), Env{Run: f.run}, "docker-prune", nil)
	if p.Available {
		t.Errorf("docker failure should make the action unavailable")
	}
}

func TestDismParse(t *testing.T) {
	out := `Component Store (WinSxS) information:

Windows Explorer Reported Size of Component Store : 8.21 GB

Actual Size of Component Store : 7.95 GB

    Shared with Windows : 5.56 GB
    Backups and Disabled Features : 2.12 GB
    Cache and Temporary Data : 266.42 MB

Component Store Cleanup Recommended : Yes`
	gb, mb := 2.12, 266.42
	want := uint64(gb*(1<<30) + mb*(1<<20))
	if got := parseDismReclaimable(out); got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	if got := parseDismReclaimable("Größe des Komponentenspeichers: 7,95 GB"); got != 0 {
		t.Errorf("unknown language should give 0, got %d", got)
	}
	f := &fakeRunner{output: map[string]string{"Dism.exe": out}}
	p, _ := Preview(context.Background(), Env{Elevated: true, Run: f.run}, "dism-component-cleanup", nil)
	if !p.Available || p.Bytes != want || p.Commands[0] != insight.System32("Dism.exe")+" /Online /Cleanup-Image /StartComponentCleanup" {
		t.Errorf("plan = %+v", p)
	}
	f.ran = nil
	if _, _, err := Execute(context.Background(), Env{Elevated: true, Run: f.run}, "dism-component-cleanup", nil, p.Commands); err != nil {
		t.Fatal(err)
	}
	if len(f.ran) != 1 || f.ran[0][3] != "/StartComponentCleanup" {
		t.Errorf("running should skip the slow analysis, ran %v", f.ran)
	}
}

func TestRunAllStopsOnFailure(t *testing.T) {
	f := &fakeRunner{fail: map[string]bool{"first": true}}
	_, err := runAll(context.Background(), Env{Run: f.run}, [][]string{{"first"}, {"second"}})
	if err == nil || len(f.ran) != 1 {
		t.Errorf("err=%v ran=%v", err, f.ran)
	}
}

func TestSplitCommand(t *testing.T) {
	tests := []struct{ in, exe, args string }{
		{`"C:\Program Files\App\uninst.exe" /S`, `C:\Program Files\App\uninst.exe`, "/S"},
		{`MsiExec.exe /X{1234-ABCD}`, "MsiExec.exe", "/X{1234-ABCD}"},
		{`C:\Program Files\App\unins000.exe /SILENT`, `C:\Program Files\App\unins000.exe`, "/SILENT"},
		{`"C:\x\setup.exe"`, `C:\x\setup.exe`, ""},
		{`rundll32 foo.dll,Uninstall`, "rundll32", "foo.dll,Uninstall"},
	}
	for _, tc := range tests {
		if exe, args := splitCommand(tc.in); exe != tc.exe || args != tc.args {
			t.Errorf("splitCommand(%q) = %q, %q", tc.in, exe, args)
		}
	}
}

func TestCloudPreview(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.bin"), make([]byte, 100), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, `sub\b.bin`), make([]byte, 50), 0o644)
	f := &fakeRunner{}
	p, err := Preview(context.Background(), Env{Run: f.run}, "cloud-online-only",
		[]string{filepath.Join(dir, "a.bin"), filepath.Join(dir, "sub"), filepath.Join(dir, "gone")})
	if err != nil || !p.Available || p.Bytes != 150 || len(p.Commands) != 2 || len(p.Targets) != 2 {
		t.Fatalf("plan = %+v err=%v", p, err)
	}
	if !strings.HasPrefix(p.Commands[1], insight.System32("attrib.exe")+" +U -P ") || !strings.HasSuffix(p.Commands[1], `\* /S /D`) {
		t.Errorf("folder command = %q", p.Commands[1])
	}
	if len(f.ran) != 0 {
		t.Errorf("preview ran commands: %v", f.ran)
	}
}

func TestFindVHDX(t *testing.T) {
	base := t.TempDir()
	for _, p := range []string{`Packages\Ubuntu_abc\LocalState\ext4.vhdx`, `Docker\wsl\disk\docker_data.vhdx`, `Packages\Other\LocalState\notes.txt`} {
		full := filepath.Join(base, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("x"), 0o644)
	}
	if got := findVHDX(base); len(got) != 2 {
		t.Errorf("found %v", got)
	}
	if script(`C:\a b\ext4.vhdx`) != "select vdisk file=\"C:\\a b\\ext4.vhdx\"\r\nattach vdisk readonly\r\ncompact vdisk\r\ndetach vdisk\r\n" {
		t.Errorf("unexpected diskpart script")
	}
}

// diskpart runs as administrator, so its script goes over standard input, never through a file.
func TestWSLCompactSendsScriptOnStdin(t *testing.T) {
	base := t.TempDir()
	disk := filepath.Join(base, `Packages\Ubuntu_abc\LocalState\ext4.vhdx`)
	os.MkdirAll(filepath.Dir(disk), 0o755)
	os.WriteFile(disk, []byte("x"), 0o644)
	t.Setenv("LOCALAPPDATA", base)
	tmp := t.TempDir()
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)

	f := &fakeRunner{}
	env := Env{Elevated: true, Run: f.run, RunInput: f.runInput}
	p, err := Preview(context.Background(), env, "wsl-compact", nil)
	if err != nil || !p.Available || len(p.Commands) != 2 || !strings.Contains(p.Commands[1], disk) {
		t.Fatalf("plan = %+v err=%v", p, err)
	}
	if _, _, err := Execute(context.Background(), env, "wsl-compact", nil, p.Commands); err != nil {
		t.Fatal(err)
	}
	if len(f.ran) != 2 || filepath.Base(f.ran[1][0]) != "diskpart.exe" || len(f.ran[1]) != 1 {
		t.Fatalf("ran %v", f.ran)
	}
	if len(f.inputs) != 1 || f.inputs[0] != script(disk) {
		t.Errorf("diskpart input = %q", f.inputs)
	}
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		t.Errorf("compacting left files in the temp folder: %v", entries)
	}
}
