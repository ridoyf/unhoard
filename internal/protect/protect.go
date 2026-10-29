package protect

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Guard decides whether a path may be quarantined. Every deletion path in the
// backend must call Check; the UI's own checks are only a convenience.
type Guard struct {
	// Subtree entries protect the path and everything beneath it.
	Subtree []string
	// Exact entries protect only the path itself, but deleting any ancestor of it is also refused.
	// In both lists a "*" segment matches any single name, e.g. `C:\Users\*`.
	Exact []string
	// Allow entries carve out the children of a protected subtree, e.g. `C:\Windows\Temp`.
	Allow []string
	// Never reports knowledge-base rules marked `safety: never`. Optional.
	Never func(path string) (bool, string)
}

// rootNames are protected at the root of every drive.
var rootNames = []string{
	"system volume information", "$recycle.bin", "recovery", "boot", "bootmgr", "bootnxt",
	"$winreagent", "$windows.~bt", "$windows.~ws", "windows.old", "config.msi", "$sysreset",
	"pagefile.sys", "hiberfil.sys", "swapfile.sys", "dumpstack.log", "dumpstack.log.tmp",
	"msocache", "documents and settings", "$extend", "$mft",
}

// quarantinePrefix is quarantine.DriveFolder; protect cannot import quarantine.
const quarantinePrefix = ".unhoard-quarantine"

// knownUserFolders are protected (as folders) inside every user profile.
var knownUserFolders = []string{
	"appdata", `appdata\local`, `appdata\roaming`, `appdata\locallow`, "desktop", "documents",
	"downloads", "pictures", "music", "videos", "favorites", "contacts", "links",
	"saved games", "searches", "onedrive", "3d objects",
	`appdata\local\microsoft`, `appdata\roaming\microsoft`, `appdata\local\microsoft\windows`,
}

// userSecrets hold a profile's encryption keys, certificates and saved passwords. Losing
// them loses every password Windows has stored, so they are protected wholesale.
var userSecrets = []string{
	`appdata\roaming\microsoft\protect`, `appdata\roaming\microsoft\crypto`,
	`appdata\roaming\microsoft\systemcertificates`, `appdata\roaming\microsoft\credentials`,
	`appdata\roaming\microsoft\vault`, `appdata\local\microsoft\credentials`, `appdata\local\microsoft\vault`,
}

// windowsProgramFolders are the parts of Program Files that ship with Windows.
var windowsProgramFolders = []string{
	"Common Files", "WindowsApps", "ModifiableWindowsApps", "Windows Defender",
	"Windows Defender Advanced Threat Protection", "Windows NT", "WindowsPowerShell", "Internet Explorer",
}

// Default builds the guard for this machine. appDirs are folders the app owns
// (its data folder and every quarantine root) and are protected wholesale.
func Default(appDirs ...string) *Guard {
	g := &Guard{}
	sysRoot := envOr("SystemRoot", `C:\Windows`)
	g.Subtree = append(g.Subtree, sysRoot)
	g.Allow = append(g.Allow, filepath.Join(sysRoot, "Temp"))
	g.Subtree = append(g.Subtree, appDirs...)
	for _, v := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "ProgramData"} {
		if p := os.Getenv(v); p != "" {
			g.Exact = append(g.Exact, p)
		}
	}
	for _, v := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432"} {
		if p := os.Getenv(v); p != "" {
			for _, f := range windowsProgramFolders {
				g.Subtree = append(g.Subtree, filepath.Join(p, f))
			}
		}
	}
	// ProgramData\Microsoft holds machine keys, Defender and Store app state. Only
	// error reports and Visual Studio's package cache are cleanup targets.
	if p := os.Getenv("ProgramData"); p != "" {
		g.Subtree = append(g.Subtree, filepath.Join(p, "Microsoft"))
		g.Allow = append(g.Allow, filepath.Join(p, `Microsoft\Windows\WER`), filepath.Join(p, `Microsoft\VisualStudio`))
	}
	users, err := windows.KnownFolderPath(windows.FOLDERID_UserProfiles, 0)
	if err != nil {
		users = filepath.Join(envOr("SystemDrive", "C:"), `\Users`)
	}
	profiles := []string{filepath.Join(users, "*")}
	g.Exact = append(g.Exact, users)
	// A profile can live outside the Users folder; protect the current one by path too.
	if home, err := os.UserHomeDir(); err == nil {
		profiles = append(profiles, home)
	}
	for _, p := range profiles {
		g.Exact = append(g.Exact, p)
		for _, f := range knownUserFolders {
			g.Exact = append(g.Exact, filepath.Join(p, f))
		}
		for _, f := range userSecrets {
			g.Subtree = append(g.Subtree, filepath.Join(p, f))
		}
	}
	for _, v := range []string{"OneDrive", "OneDriveConsumer", "OneDriveCommercial"} {
		if p := os.Getenv(v); p != "" {
			g.Exact = append(g.Exact, p)
		}
	}
	return g
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ErrProtected wraps every refusal so callers can tell it apart from I/O errors.
var ErrProtected = errors.New("protected path")

// Reason is a refusal's explanation without the ErrProtected prefix.
func Reason(err error) string {
	return strings.TrimPrefix(err.Error(), ErrProtected.Error()+": ")
}

// Check returns nil only if path may be quarantined. It refuses anything it cannot fully resolve.
func (g *Guard) Check(path string) error {
	clean, err := normalise(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProtected, err)
	}
	if err := g.checkOne(clean); err != nil {
		return err
	}
	// Re-check the real location: a junction or symlink in a parent folder
	// could otherwise route a harmless-looking path into C:\Windows.
	real, err := realPath(clean)
	if err != nil {
		return fmt.Errorf("%w: cannot resolve %s: %v", ErrProtected, clean, err)
	}
	if !strings.EqualFold(real, clean) {
		if err := g.checkOne(real); err != nil {
			return err
		}
	}
	return nil
}

func (g *Guard) checkOne(p string) error {
	lp := strings.ToLower(p)
	segs := strings.Split(lp, `\`)
	if len(segs) < 2 || segs[1] == "" {
		return fmt.Errorf("%w: %s is a drive root", ErrProtected, p)
	}
	for _, n := range rootNames {
		if segs[1] == n {
			return fmt.Errorf("%w: %s is a Windows system location", ErrProtected, p)
		}
	}
	if strings.HasPrefix(segs[1], quarantinePrefix) {
		return fmt.Errorf("%w: %s is part of the Unhoard quarantine", ErrProtected, p)
	}
	// Registry hives: ntuser.dat and its logs in a profile, usrclass.dat in AppData.
	if name := segs[len(segs)-1]; strings.HasPrefix(name, "ntuser.") || strings.HasPrefix(name, "usrclass.dat") {
		return fmt.Errorf("%w: %s is part of a user's registry", ErrProtected, p)
	}
	allowed := false
	for _, a := range g.Allow {
		if under(lp, lower(a)) {
			allowed = true
		}
	}
	for _, s := range g.Subtree {
		ss := strings.Split(lower(s), `\`)
		if !allowed && prefixMatch(ss, segs) {
			return fmt.Errorf("%w: %s is inside %s, which is protected", ErrProtected, p, s)
		}
		if len(segs) < len(ss) && prefixMatch(segs, ss) {
			return fmt.Errorf("%w: %s contains %s, which is protected", ErrProtected, p, s)
		}
	}
	for _, e := range g.Exact {
		if prefixMatch(segs, strings.Split(lower(e), `\`)) {
			return fmt.Errorf("%w: %s is or contains %s, which is protected", ErrProtected, p, e)
		}
	}
	if g.Never != nil {
		if never, why := g.Never(p); never {
			return fmt.Errorf("%w: %s", ErrProtected, why)
		}
	}
	return nil
}

func lower(p string) string { return strings.ToLower(filepath.Clean(p)) }

// under reports whether p is strictly inside dir.
func under(p, dir string) bool {
	return strings.HasPrefix(p, strings.TrimSuffix(dir, `\`)+`\`)
}

// prefixMatch reports whether a is b or an ancestor of it. A "*" segment in either matches
// any one name; real paths cannot contain "*", so only patterns do.
func prefixMatch(a, b []string) bool {
	if len(a) > len(b) {
		return false
	}
	for i, s := range a {
		if s != b[i] && s != "*" && b[i] != "*" {
			return false
		}
	}
	return true
}

// normalise accepts only absolute drive-letter paths and expands 8.3 short names
// (C:\PROGRA~1 would otherwise slip past a string comparison).
func normalise(path string) (string, error) {
	if strings.HasPrefix(path, `\\`) || len(path) < 3 || path[1] != ':' || (path[2] != '\\' && path[2] != '/') {
		return "", fmt.Errorf("%q is not an absolute drive path", path)
	}
	p := filepath.Clean(path)
	// Win32 strips trailing dots and spaces, so `Documents.` opens Documents, and ":"
	// names an alternate data stream. Both would get past the string checks below.
	for _, seg := range strings.Split(p[3:], `\`) {
		if strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") || strings.Contains(seg, ":") {
			return "", fmt.Errorf("%q has a name that Windows would change", path)
		}
	}
	if strings.Contains(p, "~") {
		long, err := longPath(p)
		if err != nil {
			return "", fmt.Errorf("cannot expand short name in %s: %v", p, err)
		}
		p = filepath.Clean(long)
	}
	return p, nil
}

func longPath(p string) (string, error) {
	in, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetLongPathName(in, &buf[0], uint32(len(buf)))
	if err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}

// realPath resolves junctions and symlinks in the parent folders of p. The last
// element itself is not followed: quarantining a link only moves the link.
// Restore recreates missing parent folders, so only the part that exists is resolved.
func realPath(p string) (string, error) {
	dir, rest := filepath.Dir(p), filepath.Base(p)
	for {
		real, err := finalPath(dir)
		if err == nil {
			return filepath.Join(real, rest), nil
		}
		missing := errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND)
		if !missing || filepath.Dir(dir) == dir {
			return "", err
		}
		dir, rest = filepath.Dir(dir), filepath.Join(filepath.Base(dir), rest)
	}
}

func finalPath(dir string) (string, error) {
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(dir), 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
	}
	real := strings.TrimPrefix(windows.UTF16ToString(buf[:n]), `\\?\`)
	if strings.HasPrefix(real, `UNC\`) {
		return "", fmt.Errorf("%s resolves to a network path", dir)
	}
	return real, nil
}
