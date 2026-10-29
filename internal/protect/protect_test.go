package protect

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCheck(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, `win\sys`, `win\temp\junk`, `users\bob\appdata\local\cache`, `users\bob\documents\a`, `progs\app`, `quar`, `free\stuff`, `users\bob\keys\k1`)
	g := &Guard{
		Subtree: []string{filepath.Join(root, "win"), filepath.Join(root, "quar"), filepath.Join(root, `users\*\keys`)},
		Allow:   []string{filepath.Join(root, `win\temp`)},
		Exact: []string{
			filepath.Join(root, "progs"),
			filepath.Join(root, "users"),
			filepath.Join(root, `users\*`),
			filepath.Join(root, `users\*\appdata\local`),
			filepath.Join(root, `users\*\documents`),
		},
		Never: func(p string) (bool, string) {
			return strings.HasSuffix(strings.ToLower(p), `\stuff`), "rule says never"
		},
	}
	tests := []struct {
		path    string
		allowed bool
	}{
		{`free`, true},
		{`free\stuff`, false}, // Never hook
		{`win`, false},
		{`win\sys`, false},
		{`win\temp`, false}, // the allowed folder itself stays protected
		{`win\temp\junk`, true},
		{`quar`, false},
		{`progs`, false},
		{`progs\app`, true},
		{`users`, false},
		{`users\bob`, false},
		{`users\bob\appdata`, false}, // ancestor of a protected folder
		{`users\bob\appdata\local`, false},
		{`users\bob\appdata\local\cache`, true},
		{`users\bob\documents`, false},
		{`users\bob\documents\a`, true},
		{`free\..\win\sys`, false},
		{`users\bob\keys`, false}, // wildcard subtree
		{`users\bob\keys\k1`, false},
		{`free\NTUSER.DAT`, false},
		{`free\ntuser.dat.LOG1`, false},
		{`free\UsrClass.dat`, false},
		{`free\new\deeper\x`, true}, // missing parents resolve through the part that exists
		{`.`, false},                // the temp root contains everything protected
	}
	for _, tc := range tests {
		err := g.Check(filepath.Join(root, tc.path))
		if (err == nil) != tc.allowed {
			t.Errorf("Check(%s) err=%v, want allowed=%v", tc.path, err, tc.allowed)
		}
		if err != nil && !errors.Is(err, ErrProtected) {
			t.Errorf("Check(%s) error is not ErrProtected: %v", tc.path, err)
		}
	}
}

func TestCheckRejectsOddPaths(t *testing.T) {
	g := &Guard{}
	for _, p := range []string{`C:\`, `D:\`, `relative\path`, `\\server\share\x`, `\\?\C:\x`, `C:`, ``,
		`C:\System Volume Information\x`, `D:\$Recycle.Bin\S-1`, `C:\pagefile.sys`, `C:\Windows.old`,
		`C:\Users\bob\Documents.`, `C:\Users\bob\Documents `, `C:\Users\bob\Documents. .`, `C:\x\file.txt:stream`,
		`D:\.unhoard-quarantine`, `D:\.unhoard-quarantine-S-1-5-21-1\item`} {
		if err := g.Check(p); err == nil {
			t.Errorf("Check(%q) allowed", p)
		}
	}
}

func TestCheckRejectsNamesWindowsChanges(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, `docs`)
	g := &Guard{Exact: []string{filepath.Join(root, "docs")}}
	for _, name := range []string{"docs.", "docs ", "docs. .", "docs:stream"} {
		if err := g.Check(filepath.Join(root, name)); err == nil {
			t.Errorf("Check(%q) allowed a name Windows opens as the protected docs folder", name)
		}
	}
}

func TestCheckFollowsJunctions(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, `win\sys`, `free`)
	link := filepath.Join(root, `free\link`)
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, filepath.Join(root, "win")).CombinedOutput(); err != nil {
		t.Skipf("cannot create junction: %v %s", err, out)
	}
	g := &Guard{Subtree: []string{filepath.Join(root, "win")}}
	if err := g.Check(filepath.Join(link, "sys")); err == nil {
		t.Errorf("path through a junction into a protected folder was allowed")
	}
	if err := g.Check(link); err != nil {
		t.Errorf("the junction itself should be movable: %v", err)
	}
	if err := g.Check(filepath.Join(link, `missing\x`)); err == nil {
		t.Errorf("a missing path behind a junction into a protected folder was allowed")
	}
}

func TestCheckExpandsShortNames(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, `Very Long Folder Name\inner`)
	short := filepath.Join(root, "VERYLO~1")
	if _, err := os.Stat(short); err != nil {
		t.Skip("8.3 names disabled on this volume")
	}
	g := &Guard{Subtree: []string{filepath.Join(root, "Very Long Folder Name")}}
	if err := g.Check(filepath.Join(short, "inner")); err == nil {
		t.Errorf("short-name path bypassed the guard")
	}
}

func TestDefaultProtectsSystemLocations(t *testing.T) {
	g := Default()
	home, _ := os.UserHomeDir()
	for _, p := range []string{os.Getenv("SystemRoot"), os.Getenv("ProgramFiles"), os.Getenv("ProgramData"),
		filepath.Join(os.Getenv("ProgramFiles"), "Common Files"), filepath.Join(os.Getenv("ProgramData"), `Microsoft\Crypto`),
		filepath.Join(home, `AppData\Roaming\Microsoft\Protect`), filepath.Join(home, `AppData\Local\Microsoft`)} {
		// Only string checks run before resolution fails, so this never touches the files themselves.
		if err := g.Check(p); err == nil {
			t.Errorf("Default guard allowed %s", p)
		}
	}
	// The knowledge base's cleanup targets inside protected trees stay reachable.
	for _, p := range []string{filepath.Join(os.Getenv("ProgramData"), `Microsoft\Windows\WER\ReportArchive`),
		filepath.Join(os.Getenv("ProgramData"), `Microsoft\VisualStudio\Packages`), filepath.Join(home, `AppData\Local\Microsoft\Windows\INetCache`)} {
		if err := g.Check(p); err != nil {
			t.Errorf("Default guard refused %s: %v", p, err)
		}
	}
}
