package diagnose

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/ridoyf/unhoard/internal/kb"
	"github.com/ridoyf/unhoard/internal/protect"
)

func kinds(p *Problem) string {
	var k []string
	for _, f := range p.Fixes {
		k = append(k, f.Kind)
	}
	return strings.Join(k, ",")
}

func TestMove(t *testing.T) {
	wrap := func(e error) error {
		return fmt.Errorf("moving x to quarantine: %w", &os.LinkError{Op: "rename", Err: e})
	}
	chrome := func(string) []string { return []string{"Google Chrome"} }
	none := func(string) []string { return nil }
	tests := []struct {
		name      string
		ctx       Context
		err       error
		summary   string
		wantKinds string
	}{
		{"open in an app", Context{LockedBy: chrome}, wrap(windows.ERROR_SHARING_VIOLATION), "Google Chrome has it open", ",retry"},
		{"folder with an open file reports access denied", Context{Elevated: true, LockedBy: chrome}, wrap(windows.ERROR_ACCESS_DENIED), "Google Chrome has it open", ",retry"},
		{"denied, not admin", Context{LockedBy: none}, wrap(windows.ERROR_ACCESS_DENIED), "only lets administrators", "admin"},
		{"denied, already admin", Context{Elevated: true, LockedBy: none}, wrap(windows.ERROR_ACCESS_DENIED), "Even administrators", ",,view"},
		{"gone", Context{}, wrap(windows.ERROR_FILE_NOT_FOUND), "no longer there", "rescan"},
		{"write-protected", Context{}, wrap(windows.ERROR_WRITE_PROTECT), "write-protected", ""},
		{"anything else", Context{}, wrap(windows.ERROR_INVALID_NAME), "refused", "retry"},
	}
	for _, tc := range tests {
		p := tc.ctx.Move(`C:\x`, tc.err)
		if p == nil || !strings.Contains(p.Summary, tc.summary) || kinds(p) != tc.wantKinds {
			t.Errorf("%s: got %+v, want summary with %q and fixes %q", tc.name, p, tc.summary, tc.wantKinds)
		}
	}
	if (Context{}).Move(`C:\x`, nil) != nil {
		t.Error("no error should mean no problem")
	}
}

func TestRestoreConflict(t *testing.T) {
	p := Context{}.Restore(`C:\a.txt`, fmt.Errorf("cannot restore to %s: %w", `C:\a.txt`, fs.ErrExist))
	if p == nil || kinds(p) != "explorer,,retry" || p.Fixes[0].Arg != `C:\a.txt` {
		t.Errorf("got %+v", p)
	}
}

func TestCommand(t *testing.T) {
	tests := []struct {
		output    string
		elevated  bool
		summary   string
		wantKinds string
	}{
		{"Error: 0x800f0806\nThe operation could not be completed due to pending operations.", true, "waiting for a restart", ""},
		{"error during connect: this error may indicate that the docker daemon is not running", false, "Docker Desktop is not running", ",retry"},
		{"Elevated permissions are required to run DISM.", false, "administrator rights", "admin"},
		{"Elevated permissions are required to run DISM.", true, "administrator rights", "retry"},
		{"something nobody has seen before", false, "stopped with an error", "retry"},
	}
	for _, tc := range tests {
		p := Context{Elevated: tc.elevated}.Command(tc.output, errors.New("exit status 1"))
		if !strings.Contains(p.Summary, tc.summary) || kinds(p) != tc.wantKinds {
			t.Errorf("%q: got %+v, want %q / %q", tc.output, p, tc.summary, tc.wantKinds)
		}
	}
}

func TestJoinNames(t *testing.T) {
	for want, names := range map[string][]string{
		"A":          {"A"},
		"A and B":    {"A", "B"},
		"A, B and C": {"A", "B", "C"},
	} {
		if got := joinNames(names); got != want {
			t.Errorf("joinNames(%v) = %q", names, got)
		}
	}
}

// LockedBy asks the real Restart Manager about a file this test holds open.
func TestLockedByFindsThisProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "held.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(path), windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	if got := LockedBy(path); len(got) == 0 {
		t.Error("Restart Manager should report this test process holding the file")
	}
	if got := LockedBy(filepath.Dir(path)); len(got) == 0 {
		t.Error("checking the folder should find the open file inside it")
	}
}

func TestQuarantineRefused(t *testing.T) {
	refused := fmt.Errorf("%w: it is a system file", protect.ErrProtected)
	for _, tc := range []struct {
		name  string
		rule  *kb.Rule
		kinds string
	}{
		{"no rule", nil, ""},
		{"rule with action", &kb.Rule{Action: "disable-hibernation"}, "action"},
		{"via app", &kb.Rule{Safety: kb.DeleteViaApp, Owner: "Steam"}, ",view"},
	} {
		p := Context{}.Quarantine(`C:\x`, refused, tc.rule)
		if p == nil || kinds(p) != tc.kinds || !strings.Contains(p.Summary, "it is a system file.") {
			t.Errorf("%s: got %+v", tc.name, p)
		}
	}
}
