package diagnose

import (
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Restart Manager is the Windows service installers use to find which programs have
// a file open. It is the only reliable way to name the app behind a "file in use" error.
var (
	rstrtmgr                = windows.NewLazySystemDLL("rstrtmgr.dll")
	procRmStartSession      = rstrtmgr.NewProc("RmStartSession")
	procRmRegisterResources = rstrtmgr.NewProc("RmRegisterResources")
	procRmGetList           = rstrtmgr.NewProc("RmGetList")
	procRmEndSession        = rstrtmgr.NewProc("RmEndSession")
)

// rmProcessInfo mirrors RM_PROCESS_INFO in restartmanager.h (668 bytes, no padding).
type rmProcessInfo struct {
	PID              uint32
	StartTime        windows.Filetime
	AppName          [256]uint16
	ServiceShortName [64]uint16
	AppType          uint32
	AppStatus        uint32
	SessionID        uint32
	Restartable      int32
}

const errorMoreData = 234

// maxLockCheck caps how many files inside a folder are checked, so a huge folder
// does not stall the explanation.
const maxLockCheck = 2000

// LockedBy names the programs that have path, or a file inside it, open. It returns
// nil when nothing is found or Restart Manager is unavailable.
func LockedBy(path string) []string {
	files := lockCandidates(path)
	if len(files) == 0 || procRmStartSession.Find() != nil {
		return nil
	}
	var session uint32
	key := make([]uint16, 33) // CCH_RM_SESSION_KEY + 1
	if r, _, _ := procRmStartSession.Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&key[0]))); r != 0 {
		return nil
	}
	defer procRmEndSession.Call(uintptr(session))

	names := make([]*uint16, 0, len(files))
	for _, f := range files {
		if p, err := windows.UTF16PtrFromString(f); err == nil {
			names = append(names, p)
		}
	}
	if r, _, _ := procRmRegisterResources.Call(uintptr(session), uintptr(len(names)), uintptr(unsafe.Pointer(&names[0])), 0, 0, 0, 0); r != 0 {
		return nil
	}

	// The first call asks how many entries there are; the second fills them.
	var needed, count, reasons uint32
	r, _, _ := procRmGetList.Call(uintptr(session), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)), 0, uintptr(unsafe.Pointer(&reasons)))
	if r != errorMoreData || needed == 0 {
		return nil
	}
	infos := make([]rmProcessInfo, needed)
	count = needed
	if r, _, _ := procRmGetList.Call(uintptr(session), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&infos[0])), uintptr(unsafe.Pointer(&reasons))); r != 0 {
		return nil
	}

	var out []string
	seen := map[string]bool{}
	for _, info := range infos[:count] {
		name := windows.UTF16ToString(info.AppName[:])
		if name == "" {
			name = windows.UTF16ToString(info.ServiceShortName[:])
		}
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// lockCandidates lists path itself if it is a file, or up to maxLockCheck files inside it.
func lockCandidates(path string) []string {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil
	}
	if !fi.IsDir() {
		return []string{path}
	}
	var files []string
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if len(files) >= maxLockCheck {
			return fs.SkipAll
		}
		if d.Type().IsRegular() {
			files = append(files, p)
		}
		return nil
	})
	return files
}
