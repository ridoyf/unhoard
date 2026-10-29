package finders

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// DownloadsDir returns the user's Downloads folder, following it if the user moved it.
func DownloadsDir() string {
	if p, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, 0); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

// SyncRoots returns the OneDrive folders on this PC. Personal and work accounts
// can share a folder, so each one is listed once.
func SyncRoots() []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range []string{"OneDrive", "OneDriveConsumer", "OneDriveCommercial"} {
		p := os.Getenv(v)
		if p == "" || seen[strings.ToLower(p)] {
			continue
		}
		seen[strings.ToLower(p)] = true
		out = append(out, p)
	}
	return out
}
