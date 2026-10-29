package cleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/ridoyf/unhoard/internal/insight"
)

var cloudOnlineOnly = Action{
	ID:          "cloud-online-only",
	Title:       "Make files online-only",
	Description: "Tells OneDrive (or another Windows cloud-files provider) to free the local copies. The files stay in the cloud and in File Explorer, and download again when opened. Nothing is deleted.",
	Preview: func(ctx context.Context, env Env, target []string) (Plan, error) {
		if len(target) == 0 {
			return Plan{}, errors.New("choose at least one file or folder")
		}
		p := Plan{}
		for _, t := range target {
			fi, err := os.Stat(t)
			if err != nil {
				continue
			}
			p.Targets = append(p.Targets, t)
			// attrib +U -P marks files "unpinned", which the sync engine then dehydrates.
			if fi.IsDir() {
				p.Bytes += localBytes(t)
				p.add(insight.System32("attrib.exe"), "+U", "-P", filepath.Join(t, "*"), "/S", "/D")
			} else {
				p.Bytes += onDisk(fi)
				p.add(insight.System32("attrib.exe"), "+U", "-P", t)
			}
		}
		if len(p.Targets) == 0 {
			return unavailable("None of those files exist any more."), nil
		}
		return p, nil
	},
}

// localBytes totals files under dir whose data is on this PC.
func localBytes(dir string) uint64 {
	var total uint64
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				total += onDisk(fi)
			}
		}
		return nil
	})
	return total
}

// onDisk is the file's size, or 0 for a placeholder whose data is only in the cloud.
func onDisk(fi os.FileInfo) uint64 {
	if a, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok &&
		a.FileAttributes&(windows.FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS|windows.FILE_ATTRIBUTE_OFFLINE) != 0 {
		return 0
	}
	return uint64(fi.Size())
}
