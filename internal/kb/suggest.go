package kb

import (
	"net/url"
	"path/filepath"
	"slices"
	"strings"
)

// patternVars are the variables a suggested pattern may use, most specific first.
var patternVars = []string{"TEMP", "LOCALAPPDATA", "APPDATA", "OneDrive", "USERPROFILE", "PUBLIC",
	"ProgramFiles(x86)", "ProgramFiles", "ProgramData", "SystemRoot", "SystemDrive"}

// sharedProfiles are folders in Users that belong to no one account.
var sharedProfiles = []string{"public", "default", "default user", "all users"}

// Generalise turns a path into a rule pattern by replacing the folder a variable
// names with the variable. A path in another account's profile is treated as the
// current user's, so the pattern never carries a user name.
func Generalise(p string, getenv func(string) string) string {
	p = filepath.Clean(p)
	if home := getenv("USERPROFILE"); home != "" {
		users := filepath.Dir(home)
		if rest, ok := cutDir(p, users); ok && rest != "" {
			name, after, _ := strings.Cut(rest, `\`)
			if !slices.Contains(sharedProfiles, strings.ToLower(name)) {
				p = filepath.Join(home, after)
			}
		}
	}
	for _, v := range patternVars {
		dir := getenv(v)
		if dir == "" {
			continue
		}
		if rest, ok := cutDir(p, dir); ok {
			return strings.TrimSuffix(`%`+v+`%\`+rest, `\`)
		}
	}
	return p
}

// cutDir returns p relative to dir when p is dir or inside it, ignoring case.
func cutDir(p, dir string) (string, bool) {
	if strings.EqualFold(p, dir) {
		return "", true
	}
	prefix := strings.TrimSuffix(dir, `\`) + `\`
	if len(p) > len(prefix) && strings.EqualFold(p[:len(prefix)], prefix) {
		return p[len(prefix):], true
	}
	return "", false
}

// SuggestURL opens a rule request on GitHub with the pattern and details filled in.
// Nothing is sent until the user reads the issue and submits it.
func SuggestURL(pattern, details string) string {
	q := url.Values{
		"template": {"rule.yml"},
		"title":    {"Rule: " + pattern},
		"path":     {pattern},
		"details":  {details},
	}
	return "https://github.com/ridoyf/unhoard/issues/new?" + q.Encode()
}
