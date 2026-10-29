package insight

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Drive is one local volume.
type Drive struct {
	Root       string `json:"root"` // e.g. `C:\`
	Label      string `json:"label"`
	FileSystem string `json:"fileSystem"`
	Total      uint64 `json:"total"`
	Free       uint64 `json:"free"`
	Removable  bool   `json:"removable"`
}

// Drives lists fixed and removable drives that are ready.
func Drives() []Drive {
	mask, _ := windows.GetLogicalDrives()
	var out []Drive
	for i := range 26 {
		if mask&(1<<i) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		p := windows.StringToUTF16Ptr(root)
		typ := windows.GetDriveType(p)
		if typ != windows.DRIVE_FIXED && typ != windows.DRIVE_REMOVABLE {
			continue
		}
		d, err := DriveInfo(root)
		if err != nil {
			continue // e.g. an empty card reader
		}
		d.Removable = typ == windows.DRIVE_REMOVABLE
		out = append(out, d)
	}
	return out
}

// DriveInfo reads one volume's label, file system and space.
func DriveInfo(root string) (Drive, error) {
	d := Drive{Root: root}
	p := windows.StringToUTF16Ptr(root)
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return d, err
	}
	d.Total, d.Free = total, totalFree
	label := make([]uint16, windows.MAX_PATH+1)
	fs := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumeInformation(p, &label[0], uint32(len(label)), nil, nil, nil, &fs[0], uint32(len(fs))); err == nil {
		d.Label = windows.UTF16ToString(label)
		d.FileSystem = windows.UTF16ToString(fs)
	}
	return d, nil
}

var (
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	procSHQueryRecycleBn = shell32.NewProc("SHQueryRecycleBinW")
)

// RecycleBin returns the bytes and item count in the Recycle Bin for root ("" for all drives).
func RecycleBin(root string) (bytes, items uint64, err error) {
	// SHQUERYRBINFO: DWORD cbSize, then two int64s. Go pads cbSize to 8 bytes,
	// matching the struct's natural alignment on x64.
	var info struct {
		size  uint32
		bytes int64
		items int64
	}
	info.size = uint32(unsafe.Sizeof(info))
	var p *uint16
	if root != "" {
		p = windows.StringToUTF16Ptr(root)
	}
	if err := procSHQueryRecycleBn.Find(); err != nil {
		return 0, 0, err
	}
	hr, _, _ := procSHQueryRecycleBn.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&info)))
	if hr != 0 {
		return 0, 0, windows.Errno(hr)
	}
	return uint64(info.bytes), uint64(info.items), nil
}
