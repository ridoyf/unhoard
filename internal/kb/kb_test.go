package kb

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ridoyf/unhoard/rules"
)

// testEnv is a fixed machine so rule tests never depend on the developer's PC.
var testEnv = map[string]string{
	"SystemDrive":       `C:`,
	"SystemRoot":        `C:\Windows`,
	"windir":            `C:\Windows`,
	"ProgramFiles":      `C:\Program Files`,
	"ProgramFiles(x86)": `C:\Program Files (x86)`,
	"ProgramData":       `C:\ProgramData`,
	"USERPROFILE":       `C:\Users\test`,
	"LOCALAPPDATA":      `C:\Users\test\AppData\Local`,
	"APPDATA":           `C:\Users\test\AppData\Roaming`,
	"TEMP":              `C:\Users\test\AppData\Local\Temp`,
	"PUBLIC":            `C:\Users\Public`,
	"OneDrive":          `C:\Users\test\OneDrive`,
}

func getenv(k string) string { return testEnv[k] }

func expand(t *testing.T, p string) string {
	t.Helper()
	out, ok := expandVars(p, getenv)
	if !ok {
		t.Fatalf("test path %q uses a variable missing from testEnv", p)
	}
	return out
}

// TestEveryRule runs each rule's own match / no_match cases from its YAML.
func TestEveryRule(t *testing.T) {
	k, err := Load(getenv, rules.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(k.Rules()) == 0 {
		t.Fatal("no rules loaded")
	}
	ids := map[string]bool{}
	for _, r := range k.Rules() {
		if ids[r.ID] {
			t.Errorf("duplicate rule id %s", r.ID)
		}
		ids[r.ID] = true
		if len(r.Tests.Match) == 0 || len(r.Tests.NoMatch) == 0 {
			t.Errorf("rule %s needs at least one match and one no_match test", r.ID)
		}
		for _, p := range r.Tests.Match {
			if got := k.Match(expand(t, p)); got != r {
				name := "nothing"
				if got != nil {
					name = got.ID
				}
				t.Errorf("rule %s: %q matched %s", r.ID, p, name)
			}
		}
		for _, p := range r.Tests.NoMatch {
			if r.Matches(expand(t, p)) {
				t.Errorf("rule %s over-matches %q", r.ID, p)
			}
		}
	}
	// Every rule with an unset variable is silently dropped; make sure none were in this env.
	var all []*Rule
	k2, _ := Load(func(string) string { return "X" }, rules.FS)
	all = k2.Rules()
	if len(all) != len(k.Rules()) {
		t.Errorf("%d rules use variables missing from testEnv", len(all)-len(k.Rules()))
	}
}

func TestMatching(t *testing.T) {
	fsys := fstest.MapFS{
		"a.yaml": {Data: []byte(`
- {id: any-cache, match: '**\Cache', name: n, description: d, safety: rebuilds}
- {id: chrome-cache, match: '%LOCALAPPDATA%\Google\Chrome\User Data\*\Cache', name: n, description: d, safety: safe}
- {id: dumps, match: '%LOCALAPPDATA%\CrashDumps\*.dmp', name: n, description: d}
- {id: needs-unset, match: '%NOPE%\x', name: n, description: d, safety: safe}
`)},
		"b.yaml": {Data: []byte(`
- {id: any-cache, match: '**\Cache', name: overridden, description: d, safety: safe}
`)},
	}
	k, err := Load(getenv, fsys)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path, want string
	}{
		{`C:\Users\test\AppData\Local\Google\Chrome\User Data\Default\Cache`, "chrome-cache"},
		{`c:\users\TEST\appdata\local\google\chrome\user data\Profile 1\cache`, "chrome-cache"},
		{`D:\games\thing\Cache`, "any-cache"},
		{`C:\Users\test\AppData\Local\CrashDumps\app.exe.123.dmp`, "dumps"},
		{`C:\Users\test\AppData\Local\CrashDumps\readme.txt`, ""},
		{`C:\Users\test\AppData\Local\Google\Chrome\User Data\Default\Cache\data_0`, ""},
	}
	for _, tc := range tests {
		got := k.Match(tc.path)
		if (got == nil && tc.want != "") || (got != nil && got.ID != tc.want) {
			t.Errorf("Match(%q) = %v, want %q", tc.path, got, tc.want)
		}
	}
	if r := k.Match(`D:\x\Cache`); r == nil || r.Name != "overridden" || r.Category != "b" {
		t.Errorf("later file should override by id, got %+v", r)
	}
	if r := k.Match(`C:\Users\test\AppData\Local\CrashDumps\x.dmp`); r.Safety != Never {
		t.Errorf("missing safety should default to never, got %q", r.Safety)
	}
	for _, r := range k.Rules() {
		if r.ID == "needs-unset" {
			t.Errorf("rule with an unset variable should be skipped")
		}
	}
	for name, want := range map[string]bool{"app.exe.123.dmp": true, "CACHE": true, "readme.txt": false} {
		if k.MayMatchName(name) != want {
			t.Errorf("MayMatchName(%q) = %v, want %v", name, !want, want)
		}
	}
	r, at := k.Explain(`C:\Users\test\AppData\Local\Google\Chrome\User Data\Default\Cache\a\b`)
	if r == nil || r.ID != "chrome-cache" || at != `C:\Users\test\AppData\Local\Google\Chrome\User Data\Default\Cache` {
		t.Errorf("Explain = %v at %q", r, at)
	}
}

func TestForbids(t *testing.T) {
	k, err := Load(getenv, fstest.MapFS{"a.yaml": {Data: []byte(`
- {id: profile, match: 'C:\p', name: Profile, description: d, safety: never}
- {id: cache, match: 'C:\p\Cache', name: Cache, description: d, safety: safe}
- {id: venv, match: '**\.venv', name: Venv, owner: Python, description: d, safety: delete-via-app}
- {id: app, match: 'C:\app', name: App, description: d, safety: delete-via-app}
- {id: tmp, match: 'C:\tmp', name: Tmp, description: d, safety: safe}
`)}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path   string
		forbid bool
		why    string
	}{
		{`C:\p`, true, "Profile must never be deleted"},
		{`C:\p\Bookmarks`, true, `it is inside C:\p (Profile), which must never be touched`},
		// The most specific rule wins, so a safe cache inside a never profile may go.
		{`C:\p\Cache`, false, ""},
		{`C:\p\Cache\data_0`, false, ""},
		{`D:\code\.venv`, true, "Venv should be removed through Python, not deleted"},
		{`C:\app`, true, "App should be removed through its app, not deleted"},
		{`C:\app\file`, false, ""}, // delete-via-app only blocks the item itself
		{`C:\tmp`, false, ""},
		{`C:\unknown`, false, ""},
	}
	for _, tc := range tests {
		forbid, why := k.Forbids(tc.path)
		if forbid != tc.forbid || why != tc.why {
			t.Errorf("Forbids(%q) = %v %q, want %v %q", tc.path, forbid, why, tc.forbid, tc.why)
		}
	}
}

func TestLoadRejectsBadRules(t *testing.T) {
	for name, yaml := range map[string]string{
		"missing name": `[{id: x, match: 'C:\x', description: d}]`,
		"bad safety":   `[{id: x, match: 'C:\x', name: n, description: d, safety: maybe}]`,
		"not a list":   `id: x`,
	} {
		if _, err := Load(getenv, fstest.MapFS{"x.yaml": {Data: []byte(yaml)}}); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestGeneralise(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`C:\Users\test\AppData\Local\Acme\Cache`, `%LOCALAPPDATA%\Acme\Cache`},
		{`C:\Users\test\AppData\Local\Temp\x`, `%TEMP%\x`},
		{`C:\Users\bob\AppData\Roaming\Acme`, `%APPDATA%\Acme`},
		{`c:\users\TEST\.cache`, `%USERPROFILE%\.cache`},
		{`C:\Users\bob`, `%USERPROFILE%`},
		{`C:\Users\Public\Documents\x`, `%PUBLIC%\Documents\x`},
		{`C:\Program Files (x86)\Acme`, `%ProgramFiles(x86)%\Acme`},
		{`C:\Windows\Logs`, `%SystemRoot%\Logs`},
		{`C:\Acme`, `%SystemDrive%\Acme`},
		{`D:\Games\Acme`, `D:\Games\Acme`},
	} {
		if got := Generalise(tc.in, getenv); got != tc.want {
			t.Errorf("Generalise(%s) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestSuggestURLHasNoRawPath(t *testing.T) {
	u := SuggestURL(`%LOCALAPPDATA%\Acme`, "Folder, 2.0 GB")
	if !strings.Contains(u, "template=rule.yml") || !strings.Contains(u, "path=%25LOCALAPPDATA%25%5CAcme") {
		t.Errorf("SuggestURL = %s", u)
	}
}
