package insight

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

// Runner runs a command and returns its output. onLine, when not nil, receives each
// line of output as the command prints it. Tests pass a fake.
type Runner func(ctx context.Context, onLine func(string), name string, args ...string) (string, error)

// System32 is the full path of a Windows tool. Commands run as administrator name their
// program by path, so nothing earlier on PATH can stand in for it.
func System32(name string) string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", name)
}

// HiddenItem is one source of space that a file listing does not make obvious.
type HiddenItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Bytes       uint64 `json:"bytes"`
	Known       bool   `json:"known"`      // Bytes is measured, not a guess
	InScan      bool   `json:"inScan"`     // already part of the scanned total
	NeedsAdmin  bool   `json:"needsAdmin"` // run elevated to measure it
	Action      string `json:"action"`     // cleanup action id, if there is one
	Settings    string `json:"settings"`   // Windows settings page that manages it
}

// Hidden explains the difference between a drive's used space and what the scan found.
type Hidden struct {
	Root         string       `json:"root"`
	Total        uint64       `json:"total"`
	Used         uint64       `json:"used"`
	Scanned      uint64       `json:"scanned"`
	Gap          uint64       `json:"gap"`         // used - scanned
	Unexplained  uint64       `json:"unexplained"` // gap minus the measured gap items
	Inaccessible int          `json:"inaccessible"`
	Items        []HiddenItem `json:"items"`
}

// Explain measures hidden space on root after a scan. scanMethod is "mft" or "walk".
func Explain(ctx context.Context, root string, scanned uint64, inaccessible int, scanMethod string, elevated bool, run Runner) Hidden {
	h := Hidden{Root: root, Scanned: scanned, Inaccessible: inaccessible}
	if d, err := DriveInfo(root); err == nil {
		h.Total, h.Used = d.Total, d.Total-d.Free
	}
	if h.Used > scanned {
		h.Gap = h.Used - scanned
	}
	system := strings.EqualFold(root[:2], os.Getenv("SystemDrive"))

	for _, f := range []struct{ file, id, name, desc, action, settings string }{
		{"hiberfil.sys", "hiberfil", "Hibernation file",
			"Holds memory contents for hibernation and Fast Startup. Turning hibernation off deletes it.", "disable-hibernation", ""},
		{"pagefile.sys", "pagefile", "Page file",
			"Virtual memory Windows uses when RAM is full. Windows manages its size; change it only in Advanced system settings.", "", "SystemPropertiesPerformance.exe"},
		{"swapfile.sys", "swapfile", "Swap file",
			"Used by Store apps for suspending and resuming. Windows manages it together with the page file.", "", "SystemPropertiesPerformance.exe"},
	} {
		if fi, err := os.Stat(root + f.file); err == nil {
			h.Items = append(h.Items, HiddenItem{ID: f.id, Name: f.name, Description: f.desc, Bytes: uint64(fi.Size()),
				Known: true, InScan: true, Action: f.action, Settings: f.settings})
		}
	}

	if b, n, err := RecycleBin(root); err == nil {
		h.Items = append(h.Items, HiddenItem{ID: "recycle-bin", Name: "Recycle Bin",
			Description: strconv.FormatUint(n, 10) + " deleted items still use space until the Recycle Bin is emptied.",
			Bytes:       b, Known: true, InScan: true, Action: "empty-recycle-bin"})
	}

	shadow := HiddenItem{ID: "shadow-copies", Name: "System restore points and shadow copies",
		Description: "Snapshots Windows keeps for System Restore and previous versions of files, stored in the hidden System Volume Information folder. Change the space allowed in System Protection settings.",
		NeedsAdmin:  !elevated, Settings: "SystemPropertiesProtection.exe"}
	if elevated {
		shadow.Bytes, shadow.Known = shadowStorage(ctx, root, run)
	}
	h.Items = append(h.Items, shadow)

	meta := HiddenItem{ID: "ntfs-metadata", Name: "File system metadata",
		Description: "The Master File Table and other NTFS records that describe every file. It grows with the number of files and rarely shrinks.",
		NeedsAdmin:  !elevated, InScan: scanMethod == "mft"}
	if elevated {
		meta.Bytes, meta.Known = mftSize(root)
	}
	h.Items = append(h.Items, meta)

	if system {
		rs := HiddenItem{ID: "reserved-storage", Name: "Reserved storage",
			Description: "Space Windows sets aside (usually about 7 GB) so updates can install even when the drive is nearly full. Its size is not reported by Windows.",
			NeedsAdmin:  !elevated}
		if elevated {
			if out, err := run(ctx, nil, powershell, "-NoProfile", "-NonInteractive", "-Command", "(Get-WindowsReservedStorageState).ReservedStorageState"); err == nil {
				rs.Description += " Current state: " + strings.TrimSpace(out) + "."
			}
		}
		h.Items = append(h.Items, rs)
	}

	h.Unexplained = h.Gap
	for _, it := range h.Items {
		if it.Known && !it.InScan {
			h.Unexplained -= min(h.Unexplained, it.Bytes)
		}
	}
	return h
}

var powershell = System32(`WindowsPowerShell\v1.0\powershell.exe`)

// shadowStorage reads Win32_ShadowStorage.UsedSpace for the volume holding root.
func shadowStorage(ctx context.Context, root string, run Runner) (uint64, bool) {
	guid := make([]uint16, 50)
	if err := windows.GetVolumeNameForVolumeMountPoint(windows.StringToUTF16Ptr(root), &guid[0], uint32(len(guid))); err != nil {
		return 0, false
	}
	out, err := run(ctx, nil, powershell, "-NoProfile", "-NonInteractive", "-Command",
		`Get-CimInstance Win32_ShadowStorage | ForEach-Object { "$($_.UsedSpace)|$($_.Volume.DeviceID)" }`)
	if err != nil {
		return 0, false
	}
	return parseShadowStorage(out, windows.UTF16ToString(guid))
}

// parseShadowStorage sums "bytes|\\?\Volume{...}\" lines for one volume.
func parseShadowStorage(out, volume string) (uint64, bool) {
	var total uint64
	for _, line := range strings.Split(out, "\n") {
		bytes, vol, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok || !strings.EqualFold(vol, volume) {
			continue
		}
		if n, err := strconv.ParseUint(bytes, 10, 64); err == nil {
			total += n
		}
	}
	return total, true // no line for this volume means no shadow storage on it
}

// mftSize asks NTFS for the MFT's size. It needs a volume handle, so admin rights.
func mftSize(root string) (uint64, bool) {
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(`\\.\`+root[:2]), windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(h)
	// NTFS_VOLUME_DATA_BUFFER: MftValidDataLength is the 7th int64 (offset 0x38).
	var buf [128]byte
	var n uint32
	if err := windows.DeviceIoControl(h, windows.FSCTL_GET_NTFS_VOLUME_DATA, nil, 0, &buf[0], uint32(len(buf)), &n, nil); err != nil {
		return 0, false
	}
	return binary.LittleEndian.Uint64(buf[0x38:]), true
}
