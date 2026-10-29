package store

import (
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestReopenKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSetting("theme", "dark")
	s.Close()
	s, err = Open(path) // migrations must not re-run
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.Setting("theme", "auto"); got != "dark" {
		t.Errorf("setting = %q", got)
	}
	if got := s.Setting("missing", "fallback"); got != "fallback" {
		t.Errorf("missing setting = %q", got)
	}
}

func TestActions(t *testing.T) {
	s := open(t)
	s.LogAction(Action{Action: "quarantine", Paths: []string{`C:\a`, `C:\b`}, Bytes: 10, OK: true, Result: "moved"})
	s.LogAction(Action{Action: "restore", Paths: []string{`C:\a`}, OK: false, Result: "exists"})
	got, err := s.Actions(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Action != "restore" || got[1].Paths[1] != `C:\b` || !got[1].OK || got[1].Bytes != 10 {
		t.Fatalf("Actions = %+v", got)
	}
}

func TestSnapshotsAndHistory(t *testing.T) {
	s := open(t)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, size := range []uint64{100, 250} {
		_, err := s.SaveSnapshot(
			Snapshot{At: t0.Add(time.Duration(i) * 24 * time.Hour), Root: `C:\`, Method: "walk", Bytes: 1000, VolTotal: 5000, VolFree: 5000 - 1200 - uint64(i)},
			[]DirSize{{Path: `C:\Users`, Bytes: size, Files: 3}, {Path: `C:\`, Bytes: 1000}},
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	snaps, _ := s.Snapshots(`C:\`)
	if len(snaps) != 2 || !snaps[0].At.After(snaps[1].At) {
		t.Fatalf("Snapshots = %+v", snaps)
	}
	if one, err := s.Snapshot(snaps[1].ID); err != nil || one != snaps[1] {
		t.Errorf("Snapshot = %+v, %v; want %+v", one, err, snaps[1])
	}
	dirs, _ := s.SnapshotDirs(snaps[1].ID)
	if dirs[`C:\Users`].Bytes != 100 {
		t.Errorf("SnapshotDirs = %+v", dirs)
	}
	h, _ := s.FolderHistory(`C:\`, `C:\Users`)
	if len(h) != 2 || h[0].Bytes != 100 || h[1].Bytes != 250 {
		t.Errorf("FolderHistory = %+v", h)
	}
	h, _ = s.FolderHistory(`C:\`, `C:\`)
	if len(h) != 2 || h[0].Bytes != 1200 || h[1].Bytes != 1201 {
		t.Errorf("root history should use volume used space, got %+v", h)
	}
	if err := s.DeleteSnapshot(snaps[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Snapshot(snaps[0].ID); err == nil {
		t.Errorf("deleted snapshot still found")
	}
	if dirs, _ := s.SnapshotDirs(snaps[0].ID); len(dirs) != 0 {
		t.Errorf("snapshot dirs not deleted with the snapshot")
	}
}
