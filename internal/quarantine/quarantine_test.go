package quarantine

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/ridoyf/unhoard/internal/protect"
)

func setup(t *testing.T) (q *Quarantine, data, qroot string) {
	t.Helper()
	base := t.TempDir()
	data = filepath.Join(base, "data")
	qroot = filepath.Join(base, "quarantine")
	os.MkdirAll(data, 0o755)
	guard := &protect.Guard{Subtree: []string{qroot, filepath.Join(data, "protected")}}
	q = New(guard, func(string) string { return qroot }, func() []string { return []string{qroot} })
	return q, data, qroot
}

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMoveListRestore(t *testing.T) {
	q, data, _ := setup(t)
	folder := filepath.Join(data, "cache")
	write(t, filepath.Join(folder, "a.txt"), "hello")
	it, err := q.Move(folder, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(folder); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original still exists after Move")
	}
	items, _ := q.List()
	if len(items) != 1 || items[0].ID != it.ID || items[0].OriginalPath != folder || !items[0].IsDir || items[0].Bytes != 4096 {
		t.Fatalf("List = %+v", items)
	}
	if _, err := q.Restore(it.ID); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(folder, "a.txt")); err != nil || string(b) != "hello" {
		t.Fatalf("restored content = %q, %v", b, err)
	}
	if items, _ := q.List(); len(items) != 0 {
		t.Fatalf("item still listed after restore")
	}
}

func TestRestoreRefusesToOverwrite(t *testing.T) {
	q, data, _ := setup(t)
	f := filepath.Join(data, "x.txt")
	write(t, f, "old")
	it, err := q.Move(f, 0)
	if err != nil {
		t.Fatal(err)
	}
	write(t, f, "new")
	if _, err := q.Restore(it.ID); err == nil {
		t.Fatal("restore overwrote an existing file")
	}
	if b, _ := os.ReadFile(f); string(b) != "new" {
		t.Fatalf("existing file was changed: %q", b)
	}
}

func TestMoveRefusesProtected(t *testing.T) {
	q, data, qroot := setup(t)
	write(t, filepath.Join(data, `protected\keep.txt`), "x")
	write(t, filepath.Join(qroot, `other\item.json`), "{}")
	for _, p := range []string{filepath.Join(data, `protected\keep.txt`), filepath.Join(qroot, "other"), data} {
		if _, err := q.Move(p, 0); !errors.Is(err, protect.ErrProtected) {
			t.Errorf("Move(%s) err = %v, want ErrProtected", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(data, `protected\keep.txt`)); err != nil {
		t.Errorf("protected file was moved")
	}
}

func TestMoveFailureLeavesNoDebris(t *testing.T) {
	q, data, qroot := setup(t)
	f := filepath.Join(data, "locked.txt")
	write(t, f, "x")
	h, err := os.Open(f) // Go opens without FILE_SHARE_DELETE, so the rename fails
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := q.Move(f, 0); err == nil {
		t.Fatal("expected Move of an open file to fail")
	}
	if entries, _ := os.ReadDir(qroot); len(entries) != 0 {
		t.Errorf("failed move left %d entries in quarantine", len(entries))
	}
	if _, err := os.Stat(f); err != nil {
		t.Errorf("file lost after failed move")
	}
}

func TestPurge(t *testing.T) {
	q, data, _ := setup(t)
	write(t, filepath.Join(data, "old.txt"), "x")
	write(t, filepath.Join(data, "new.txt"), "x")
	old, _ := q.Move(filepath.Join(data, "old.txt"), 0)
	q.Move(filepath.Join(data, "new.txt"), 0)
	// Age the first item.
	old.QuarantinedAt = time.Now().Add(-8 * 24 * time.Hour)
	if err := writeManifest(old); err != nil {
		t.Fatal(err)
	}
	purged, err := q.Purge(time.Now().AddDate(0, 0, -7), nil)
	if err != nil || len(purged) != 1 || purged[0].ID != old.ID {
		t.Fatalf("Purge = %v, %v", purged, err)
	}
	if items, _ := q.List(); len(items) != 1 {
		t.Fatalf("%d items left, want 1", len(items))
	}
	if _, err := q.Purge(time.Now().Add(time.Second), nil); err != nil {
		t.Fatal(err)
	}
	if items, _ := q.List(); len(items) != 0 {
		t.Fatalf("empty quarantine left %d items", len(items))
	}
}

// item.json can be edited by anything that reaches the folder, so Restore re-checks its path.
func TestRestoreRefusesProtectedTarget(t *testing.T) {
	q, data, _ := setup(t)
	f := filepath.Join(data, "x.txt")
	write(t, f, "payload")
	it, err := q.Move(f, 0)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(data, `protected\x.txt`)
	it.OriginalPath = target
	if err := writeManifest(it); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Restore(it.ID); !errors.Is(err, protect.ErrProtected) {
		t.Fatalf("Restore err = %v, want ErrProtected", err)
	}
	if _, err := os.Lstat(target); err == nil {
		t.Fatal("forged item was restored into a protected folder")
	}
}

func TestRootIsPrivate(t *testing.T) {
	q, data, qroot := setup(t)
	write(t, filepath.Join(data, "a.txt"), "x")
	if _, err := q.Move(filepath.Join(data, "a.txt"), 0); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(qroot, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if control, _, _ := sd.Control(); control&windows.SE_DACL_PROTECTED == 0 {
		t.Errorf("quarantine root inherits its parent's permissions: %s", sd)
	}
	if s := sd.String(); strings.Contains(s, ";AU)") || strings.Contains(s, ";BU)") || strings.Contains(s, ";WD)") {
		t.Errorf("quarantine root grants other accounts access: %s", s)
	}
}

// A root made before lockdown (or by an older version) gets locked on first use.
func TestExistingRootIsLockedDown(t *testing.T) {
	q, data, qroot := setup(t)
	os.MkdirAll(qroot, 0o755)
	write(t, filepath.Join(data, "a.txt"), "x")
	if _, err := q.Move(filepath.Join(data, "a.txt"), 0); err != nil {
		t.Fatal(err)
	}
	sd, _ := windows.GetNamedSecurityInfo(qroot, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if control, _, _ := sd.Control(); control&windows.SE_DACL_PROTECTED == 0 {
		t.Errorf("existing root was not locked down: %s", sd)
	}
}

// A root replaced by a junction could point the quarantine anywhere, so it is ignored.
func TestLinkedRootIsRefused(t *testing.T) {
	q, data, qroot := setup(t)
	elsewhere := filepath.Join(data, "elsewhere")
	write(t, filepath.Join(elsewhere, `20250101T000000-aaaaaaaa\item.json`), `{"id":"20250101T000000-aaaaaaaa","originalPath":"C:\\x"}`)
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", qroot, elsewhere).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v %s", err, out)
	}
	if items, err := q.List(); err != nil || len(items) != 0 {
		t.Errorf("List through a linked root = %v, %v", items, err)
	}
	write(t, filepath.Join(data, "a.txt"), "x")
	if _, err := q.Move(filepath.Join(data, "a.txt"), 0); !errors.Is(err, ErrForeignFolder) {
		t.Errorf("Move into a linked root err = %v, want ErrForeignFolder", err)
	}
	if _, err := q.Purge(time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, `20250101T000000-aaaaaaaa\item.json`)); err != nil {
		t.Errorf("purge deleted through a linked root: %v", err)
	}
}

// Purging a quarantined folder that contains a junction must delete the
// junction only, never the files it points at.
func TestPurgeDoesNotFollowJunctions(t *testing.T) {
	q, data, _ := setup(t)
	outside := filepath.Join(data, "precious")
	write(t, filepath.Join(outside, "keep.txt"), "x")
	folder := filepath.Join(data, "junk")
	os.MkdirAll(folder, 0o755)
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(folder, "link"), outside).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v %s", err, out)
	}
	if _, err := q.Move(folder, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Purge(time.Now().Add(time.Second), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("purge followed a junction and deleted its target: %v", err)
	}
}
