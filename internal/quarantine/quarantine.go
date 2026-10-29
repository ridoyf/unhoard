package quarantine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/ridoyf/unhoard/internal/protect"
)

// DriveFolder starts the name of each user's quarantine folder at the root of non-system
// drives: DriveFolder-<user's SID>. Each user has their own, directly under the drive root,
// so no other account owns a folder above it and could swap it for a link.
const DriveFolder = ".unhoard-quarantine"

// ErrForeignFolder means a quarantine folder exists that this account did not make, or
// that is a link. Unhoard never puts anything in it or deletes anything from it.
var ErrForeignFolder = errors.New("the quarantine folder was not made by this account")

// Item is one quarantined file or folder.
type Item struct {
	ID            string    `json:"id"`
	OriginalPath  string    `json:"originalPath"`
	QuarantinedAt time.Time `json:"quarantinedAt"`
	Bytes         uint64    `json:"bytes"`
	IsDir         bool      `json:"isDir"`

	dir string // the item's folder inside a quarantine root
}

// Quarantine moves items aside instead of deleting them. Items always stay on
// their own volume, so a move is an instant rename and never copies data.
type Quarantine struct {
	guard   *protect.Guard
	rootFor func(path string) string // quarantine root on the same volume as path
	roots   func() []string          // every root that may hold items
}

// New is used by tests; Default is the real configuration.
func New(guard *protect.Guard, rootFor func(string) string, roots func() []string) *Quarantine {
	return &Quarantine{guard: guard, rootFor: rootFor, roots: roots}
}

// Default keeps items for the app-data volume in appDataQuarantine and uses
// X:\.unhoard-quarantine-<SID> on every other drive.
func Default(guard *protect.Guard, appDataQuarantine string) (*Quarantine, error) {
	user, err := currentUser()
	if err != nil {
		return nil, fmt.Errorf("reading the current user: %w", err)
	}
	folder := DriveFolder + "-" + user.String()
	appVol := strings.ToUpper(filepath.VolumeName(appDataQuarantine))
	rootFor := func(p string) string {
		vol := strings.ToUpper(filepath.VolumeName(p))
		if vol == appVol {
			return appDataQuarantine
		}
		return vol + `\` + folder
	}
	roots := func() []string {
		out := []string{appDataQuarantine}
		mask, _ := windows.GetLogicalDrives()
		for i := range 26 {
			vol := string(rune('A'+i)) + ":"
			if mask&(1<<i) == 0 || vol == appVol {
				continue
			}
			// The guard refuses network paths, so only local drives can hold items.
			switch windows.GetDriveType(windows.StringToUTF16Ptr(vol + `\`)) {
			case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE:
				out = append(out, vol+`\`+folder)
			}
		}
		return out
	}
	return New(guard, rootFor, roots), nil
}

// Move quarantines path. bytes is the size on disk the scan reported, kept for display.
func (q *Quarantine) Move(path string, bytes uint64) (Item, error) {
	if err := q.guard.Check(path); err != nil {
		return Item{}, err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return Item{}, err
	}
	root := q.rootFor(path)
	if err := checkRoot(root, true); err != nil {
		return Item{}, err
	}
	if strings.Contains(filepath.Base(root), DriveFolder) {
		hide(root)
	}
	it := Item{
		ID:            time.Now().UTC().Format("20060102T150405") + "-" + randomHex(4),
		OriginalPath:  filepath.Clean(path),
		QuarantinedAt: time.Now().UTC(),
		Bytes:         bytes,
		IsDir:         fi.IsDir(),
	}
	it.dir = filepath.Join(root, it.ID)
	if err := os.MkdirAll(filepath.Join(it.dir, "data"), 0o700); err != nil {
		return Item{}, err
	}
	// Write the manifest before moving, so an interrupted move can still be found and restored.
	if err := writeManifest(it); err != nil {
		os.RemoveAll(it.dir)
		return Item{}, err
	}
	if err := os.Rename(path, it.dataPath()); err != nil {
		os.RemoveAll(it.dir) // only our empty folder and manifest; the item never moved
		return Item{}, fmt.Errorf("moving %s to quarantine: %w", path, err)
	}
	return it, nil
}

func (it Item) dataPath() string {
	return filepath.Join(it.dir, "data", filepath.Base(it.OriginalPath))
}

// List returns every quarantined item, newest first.
func (q *Quarantine) List() ([]Item, error) {
	var items []Item
	for _, root := range q.roots() {
		err := checkRoot(root, false)
		// An empty card reader is not ready and holds nothing, like a missing folder. A foreign
		// folder cannot hold our items, because Move refuses to use it.
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, windows.ERROR_NOT_READY) || errors.Is(err, ErrForeignFolder) {
			continue
		}
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(root, e.Name(), "item.json"))
			if err != nil {
				continue
			}
			var it Item
			if json.Unmarshal(data, &it) != nil || it.ID != e.Name() {
				continue
			}
			it.dir = filepath.Join(root, e.Name())
			items = append(items, it)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].QuarantinedAt.After(items[j].QuarantinedAt) })
	return items, nil
}

func (q *Quarantine) find(id string) (Item, error) {
	items, err := q.List()
	if err != nil {
		return Item{}, err
	}
	for _, it := range items {
		if it.ID == id {
			return it, nil
		}
	}
	return Item{}, fmt.Errorf("quarantine item %s not found", id)
}

// Restore moves an item back to where it came from.
func (q *Quarantine) Restore(id string) (Item, error) {
	it, err := q.find(id)
	if err != nil {
		return Item{}, err
	}
	// item.json is only a file on disk; its path gets the same checks as a quarantine request.
	if err := q.guard.Check(it.OriginalPath); err != nil {
		return it, err
	}
	if _, err := os.Lstat(it.OriginalPath); err == nil {
		return it, fmt.Errorf("cannot restore to %s: %w", it.OriginalPath, fs.ErrExist)
	}
	if err := os.MkdirAll(filepath.Dir(it.OriginalPath), 0o755); err != nil {
		return it, err
	}
	from, err := windows.UTF16PtrFromString(it.dataPath())
	if err != nil {
		return it, err
	}
	to, err := windows.UTF16PtrFromString(it.OriginalPath)
	if err != nil {
		return it, err
	}
	// Flags 0 leaves out MOVEFILE_REPLACE_EXISTING, which os.Rename sets, so a file
	// that appeared at the original path after the check above is never overwritten.
	if err := windows.MoveFileEx(from, to, 0); err != nil {
		return it, fmt.Errorf("restoring %s: %w", it.OriginalPath, err)
	}
	return it, os.RemoveAll(it.dir)
}

// Purge permanently deletes items quarantined before cutoff. Pass time.Now() to empty everything.
// progress, when not nil, is called before each item with how many are done out of how many.
func (q *Quarantine) Purge(cutoff time.Time, progress func(done, total int, it Item)) ([]Item, error) {
	items, err := q.List()
	if err != nil {
		return nil, err
	}
	var due []Item
	for _, it := range items {
		if it.QuarantinedAt.Before(cutoff) {
			due = append(due, it)
		}
	}
	var purged []Item
	var errs []error
	for i, it := range due {
		if progress != nil {
			progress(i, len(due), it)
		}
		// Belt and braces: only ever delete a folder directly inside a quarantine root.
		if !q.isItemDir(it.dir) {
			errs = append(errs, fmt.Errorf("refusing to delete %s: not a quarantine item", it.dir))
			continue
		}
		if err := removeItem(it); err != nil {
			errs = append(errs, err)
			continue
		}
		purged = append(purged, it)
	}
	return purged, errors.Join(errs...)
}

// PurgeOne permanently deletes one item regardless of age.
func (q *Quarantine) PurgeOne(id string) (Item, error) {
	it, err := q.find(id)
	if err != nil {
		return Item{}, err
	}
	if !q.isItemDir(it.dir) {
		return it, fmt.Errorf("refusing to delete %s: not a quarantine item", it.dir)
	}
	return it, removeItem(it)
}

// removeItem deletes an item's folder through an os.Root opened on its quarantine root.
// A Root works from a handle and refuses to leave the folder, so a junction swapped in
// while the delete runs cannot redirect it, and links inside are removed, not followed.
func removeItem(it Item) error {
	root, err := os.OpenRoot(filepath.Dir(it.dir))
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(filepath.Base(it.dir))
}

// checkRoot makes sure root is a real folder that this account (or an administrator)
// made, and that only this account, administrators and SYSTEM can open it. With create
// set, a missing root is made that way in one step, so nobody can get in before it is locked.
func checkRoot(root string, create bool) error {
	fi, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) && create {
		return makePrivateDir(root)
	}
	if err != nil {
		return err
	}
	if a, ok := fi.Sys().(*syscall.Win32FileAttributeData); !fi.IsDir() || !ok || a.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("%w: %s is not a plain folder", ErrForeignFolder, root)
	}
	// FAT and exFAT drives have no permissions to set: anyone with the drive could read
	// its files before they were quarantined, and links cannot exist there.
	if !hasPermissions(root) {
		return nil
	}
	sd, err := windows.GetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("reading permissions of %s: %w", root, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	user, err := currentUser()
	if err != nil {
		return err
	}
	if !owner.Equals(user) && !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) && !owner.IsWellKnown(windows.WinLocalSystemSid) {
		return fmt.Errorf("%w: %s belongs to %s", ErrForeignFolder, root, owner)
	}
	// A root that still inherits the drive's permissions gets the private list.
	if control, _, err := sd.Control(); err == nil && control&windows.SE_DACL_PROTECTED != 0 {
		return nil
	}
	private, err := privateDescriptor(user)
	if err != nil {
		return err
	}
	dacl, _, err := private.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(root, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// privateDescriptor grants full control to SYSTEM, administrators and user only, inherited
// by everything inside, and ignores the permissions of the folder above ("P").
func privateDescriptor(user *windows.SID) (*windows.SECURITY_DESCRIPTOR, error) {
	return windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;" + user.String() + ")")
}

func makePrivateDir(path string) error {
	user, err := currentUser()
	if err != nil {
		return err
	}
	sd, err := privateDescriptor(user)
	if err != nil {
		return err
	}
	sa := windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := windows.CreateDirectory(p, &sa); err != nil {
		return fmt.Errorf("creating quarantine folder %s: %w", path, err)
	}
	return nil
}

func currentUser() (*windows.SID, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid, nil
}

// hasPermissions reports whether path's volume stores access control lists (NTFS, ReFS).
func hasPermissions(path string) bool {
	vol, err := windows.UTF16PtrFromString(filepath.VolumeName(path) + `\`)
	if err != nil {
		return false
	}
	var flags uint32
	if windows.GetVolumeInformation(vol, nil, 0, nil, nil, &flags, nil, 0) != nil {
		return true // assume it does, so the ownership checks still run
	}
	return flags&windows.FILE_PERSISTENT_ACLS != 0
}

func (q *Quarantine) isItemDir(dir string) bool {
	parent := strings.ToLower(filepath.Dir(dir))
	for _, r := range q.roots() {
		if parent == strings.ToLower(filepath.Clean(r)) {
			return true
		}
	}
	return false
}

func writeManifest(it Item) error {
	data, err := json.MarshalIndent(it, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(it.dir, "item.json"), data, 0o600)
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func hide(path string) {
	p, err := windows.UTF16PtrFromString(path)
	if err == nil {
		windows.SetFileAttributes(p, windows.FILE_ATTRIBUTE_HIDDEN)
	}
}
