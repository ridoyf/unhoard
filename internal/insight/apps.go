package insight

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/sys/windows/registry"

	"github.com/ridoyf/unhoard/internal/tree"
)

// App is one entry from Programs and Features.
type App struct {
	Key             string   `json:"key"` // unique id: hive\path of the uninstall key
	Name            string   `json:"name"`
	Publisher       string   `json:"publisher"`
	Version         string   `json:"version"`
	InstallLocation string   `json:"installLocation"`
	EstimatedBytes  uint64   `json:"estimatedBytes"` // what the installer claims
	UninstallString string   `json:"uninstallString"`
	DataFolders     []string `json:"dataFolders"` // likely data folders, filled by MatchDataFolders
}

var uninstallKeys = []struct {
	root registry.Key
	name string
	path string
}{
	{registry.LOCAL_MACHINE, "HKLM", `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
	{registry.LOCAL_MACHINE, "HKLM", `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	{registry.CURRENT_USER, "HKCU", `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
}

// InstalledApps reads the uninstall registry keys, skipping system components and updates.
func InstalledApps() []App {
	var out []App
	for _, u := range uninstallKeys {
		k, err := registry.OpenKey(u.root, u.path, registry.READ)
		if err != nil {
			continue
		}
		names, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, n := range names {
			sk, err := registry.OpenKey(u.root, u.path+`\`+n, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			str := func(v string) string { s, _, _ := sk.GetStringValue(v); return strings.TrimSpace(s) }
			num := func(v string) uint64 { x, _, _ := sk.GetIntegerValue(v); return x }
			a := App{
				Key:             u.name + `\` + u.path + `\` + n,
				Name:            str("DisplayName"),
				Publisher:       str("Publisher"),
				Version:         str("DisplayVersion"),
				InstallLocation: strings.Trim(str("InstallLocation"), `"`),
				EstimatedBytes:  num("EstimatedSize") * 1024,
				UninstallString: str("UninstallString"),
			}
			hidden := num("SystemComponent") == 1 || str("ParentKeyName") != "" || str("ReleaseType") != ""
			sk.Close()
			if a.Name == "" || a.UninstallString == "" || hidden {
				continue
			}
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// FindApp looks an app up by its Key.
func FindApp(key string) (App, bool) {
	for _, a := range InstalledApps() {
		if a.Key == key {
			return a, true
		}
	}
	return App{}, false
}

var (
	nonAlnum     = regexp.MustCompile(`[^a-z0-9]+`)
	companyNoise = regexp.MustCompile(`(?i)[,.]?\s+(inc|llc|ltd|limited|corporation|corp|co|gmbh|s\.?a|ag|b\.?v)\.?$`)
	versionNoise = regexp.MustCompile(`(?i)\s*(\(?(x64|x86|64-bit|32-bit|arm64)\)?|\bv?\d+(\.\d+)+)\s*`)
)

func norm(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

// MatchDataFolders guesses each app's data folders among the children of the
// given data roots (AppData\Local, AppData\Roaming, ProgramData). children maps a
// root to its child folder names, and grandchildren maps root\child to names.
// It matches a folder named like the app, or Publisher\App.
// shortcut: name matching only, upgrade to KB owner rules if it misses common apps.
func MatchDataFolders(apps []App, roots []string, children func(string) []string) {
	for i := range apps {
		a := &apps[i]
		name := norm(versionNoise.ReplaceAllString(a.Name, " "))
		pub := norm(companyNoise.ReplaceAllString(a.Publisher, ""))
		if len(name) < 3 {
			continue
		}
		for _, root := range roots {
			for _, c := range children(root) {
				nc := norm(c)
				if nc == name {
					a.DataFolders = append(a.DataFolders, filepath.Join(root, c))
					continue
				}
				if pub == "" || nc != pub {
					continue
				}
				for _, g := range children(filepath.Join(root, c)) {
					if ng := norm(g); ng == name || pub+ng == name {
						a.DataFolders = append(a.DataFolders, filepath.Join(root, c, g))
					}
				}
			}
		}
	}
}

// AppsWithData returns the installed apps with their data folders filled in from
// AppData\Local, AppData\Roaming and ProgramData.
func AppsWithData() []App {
	apps := InstalledApps()
	roots := []string{os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA"), os.Getenv("ProgramData")}
	MatchDataFolders(apps, roots, func(dir string) []string {
		entries, _ := os.ReadDir(dir)
		var out []string
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, e.Name())
			}
		}
		return out
	})
	return apps
}

// AppFootprint is an installed app and the space it uses.
type AppFootprint struct {
	App
	InstallBytes uint64 `json:"installBytes"` // measured from the scan, 0 if not scanned
	DataBytes    uint64 `json:"dataBytes"`
}

// Footprints measures each app's folders in t (nil when nothing is scanned), biggest first.
// An app the scan did not measure is ranked by the size its installer claims.
func Footprints(apps []App, t *tree.Tree) []AppFootprint {
	out := make([]AppFootprint, 0, len(apps))
	for _, a := range apps {
		f := AppFootprint{App: a}
		if t != nil {
			if id, ok := t.Find(a.InstallLocation); ok && a.InstallLocation != "" {
				f.InstallBytes = t.Alloc(id)
			}
			for _, d := range a.DataFolders {
				if id, ok := t.Find(d); ok {
					f.DataBytes += t.Alloc(id)
				}
			}
		}
		out = append(out, f)
	}
	size := func(f AppFootprint) uint64 { return max(f.InstallBytes+f.DataBytes, f.EstimatedBytes) }
	sort.SliceStable(out, func(i, j int) bool { return size(out[i]) > size(out[j]) })
	return out
}
