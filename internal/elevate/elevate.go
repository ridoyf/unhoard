package elevate

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// IsElevated reports whether the process runs with admin rights (UAC elevated).
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// Relaunch starts a new elevated copy of the app. Windows shows the UAC prompt;
// the caller should quit once this returns nil.
func Relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := make([]string, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		args = append(args, windows.EscapeArg(a))
	}
	// "runas" is the ShellExecute verb that asks for elevation.
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("runas"), windows.StringToUTF16Ptr(exe),
		windows.StringToUTF16Ptr(strings.Join(args, " ")), nil, windows.SW_SHOWNORMAL)
}
