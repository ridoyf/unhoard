// Package diagnose turns failures into plain explanations with practical fixes, so the
// user is never left with only an error code.
package diagnose

import (
	"errors"
	"io/fs"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/ridoyf/unhoard/internal/cleanup"
	"github.com/ridoyf/unhoard/internal/kb"
	"github.com/ridoyf/unhoard/internal/protect"
	"github.com/ridoyf/unhoard/internal/quarantine"
)

// Problem explains why something could not be done and what the user can do about it.
type Problem struct {
	Summary string `json:"summary"` // what went wrong, in plain words
	Fixes   []Fix  `json:"fixes"`   // most useful first
	Detail  string `json:"detail"`  // the underlying error, for anyone who wants it
}

// Fix is one thing the user can do. Kind tells the UI which button to show; an empty
// Kind is advice to follow outside the app.
type Fix struct {
	Label string `json:"label"`
	Kind  string `json:"kind"` // "", "retry", "admin", "rescan", "action", "view" or "explorer"
	Arg   string `json:"arg"`  // the action id, view name or path the fix needs
}

var (
	retry = Fix{Label: "Try again", Kind: "retry"}
	admin = Fix{Label: "Restart as administrator", Kind: "admin"}
)

// Context is what the classifiers need to know about the machine.
type Context struct {
	Elevated bool
	LockedBy func(path string) []string // names the programs holding path open
}

// Quarantine explains a failed quarantine request. rule is the knowledge-base entry for
// path, if any: when the safety checks refused, it says the right way to remove the item.
func (c Context) Quarantine(path string, err error, rule *kb.Rule) *Problem {
	if !errors.Is(err, protect.ErrProtected) {
		return c.Move(path, err)
	}
	p := &Problem{Summary: "Unhoard will not remove this: " + protect.Reason(err) + ".", Detail: err.Error()}
	switch {
	case rule != nil && rule.Action != "":
		if act, ok := cleanup.Find(rule.Action); ok {
			p.Fixes = append(p.Fixes, Fix{Label: act.Title, Kind: "action", Arg: act.ID})
		}
	case rule != nil && rule.Safety == kb.DeleteViaApp:
		p.Fixes = append(p.Fixes,
			Fix{Label: "Remove it from inside " + rule.Owner + ", or uninstall " + rule.Owner + "."},
			Fix{Label: "Open Apps", Kind: "view", Arg: "apps"})
	}
	return p
}

// Move explains why moving path to the quarantine (or deleting it from there) failed.
func (c Context) Move(path string, err error) *Problem {
	if err == nil {
		return nil
	}
	var pe *fs.PathError
	if path == "" && errors.As(err, &pe) {
		path = pe.Path
	}
	p := &Problem{Detail: err.Error()}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		p.Summary = "It is no longer there, so something else has already moved or deleted it."
		p.Fixes = []Fix{{Label: "Scan again", Kind: "rescan"}}
	case errors.Is(err, quarantine.ErrForeignFolder):
		p.Summary = "This drive already has a quarantine folder for your account that Unhoard did not make, so Unhoard will not use it."
		p.Fixes = []Fix{{Label: "Ask an administrator to delete the folder named in the details, then try again."}, retry}
	case errors.Is(err, windows.ERROR_WRITE_PROTECT):
		p.Summary = "The drive is write-protected, so nothing on it can be changed."
		p.Fixes = []Fix{{Label: "If it is a USB drive or memory card, check for a lock switch on its side."}}
	case errors.Is(err, windows.ERROR_SHARING_VIOLATION), errors.Is(err, windows.ERROR_LOCK_VIOLATION), errors.Is(err, fs.ErrPermission):
		c.inUseOrDenied(p, path)
	default:
		p.Summary = "Windows refused the change."
		p.Fixes = []Fix{retry}
	}
	return p
}

// inUseOrDenied covers access errors. Windows reports a folder with an open file inside
// it as "access denied", so the lock check runs first for both.
func (c Context) inUseOrDenied(p *Problem, path string) {
	if apps := c.lockers(path); len(apps) > 0 {
		p.Summary = joinNames(apps) + " " + verb(apps, "has", "have") + " it open, so Windows will not let it move."
		p.Fixes = []Fix{{Label: "Close " + joinNames(apps) + ", then try again."}, retry}
		return
	}
	if !c.Elevated {
		p.Summary = "Windows only lets administrators change this."
		p.Fixes = []Fix{admin}
		return
	}
	p.Summary = "Even administrators cannot change this. Windows or the program that owns it protects it."
	p.Fixes = []Fix{
		{Label: "If it belongs to an app, uninstall the app instead."},
		{Label: "If a program is running from it, close that program or restart Windows, then try again."},
		{Label: "Open Apps", Kind: "view", Arg: "apps"},
	}
}

// Restore explains why putting an item back failed.
func (c Context) Restore(path string, err error) *Problem {
	if err == nil {
		return nil
	}
	if errors.Is(err, protect.ErrProtected) {
		return &Problem{
			Summary: "Unhoard will not put this back, because " + protect.Reason(err) + ". Unhoard never quarantines such places, so another program may have changed the quarantine record.",
			Fixes:   []Fix{{Label: "If you do not recognise it, delete it from the quarantine."}},
			Detail:  err.Error(),
		}
	}
	if errors.Is(err, fs.ErrExist) {
		return &Problem{
			Summary: "Something new is already at " + path + ", and Unhoard never overwrites.",
			Fixes: []Fix{
				{Label: "Show in File Explorer", Kind: "explorer", Arg: path},
				{Label: "Rename or move what is there now, then try again."},
				retry,
			},
			Detail: err.Error(),
		}
	}
	return c.Move(path, err)
}

func (c Context) lockers(path string) []string {
	if c.LockedBy == nil {
		return nil
	}
	return c.LockedBy(path)
}

// commandHints are known messages from the tools cleanup actions run, matched in lower case.
var commandHints = []struct {
	match   []string
	summary string
	fixes   []Fix
}{
	{[]string{"0x800f0806", "pending"},
		"Windows has updates waiting for a restart, and DISM cannot clean up until they finish.",
		[]Fix{{Label: "Restart Windows, then run this again."}}},
	{[]string{"0x800f081f", "source files could not be found"},
		"DISM could not find the Windows files it needs.",
		[]Fix{{Label: "Install any pending updates from Windows Update, then run this again."}}},
	{[]string{"cannot connect to the docker daemon", "error during connect", "docker desktop is not running", "is the docker daemon running"},
		"Docker Desktop is not running.",
		[]Fix{{Label: "Start Docker Desktop and wait until it says it is running, then try again."}, retry}},
	{[]string{"being used by another process", "in use", "sharing violation"},
		"A virtual disk is still in use, so it cannot be compacted.",
		[]Fix{{Label: "Quit Docker Desktop and close every WSL window, then try again."}, retry}},
	{[]string{"requires elevation", "elevated permissions", "access is denied", "administrator"},
		"This needs administrator rights.",
		[]Fix{admin}},
	{[]string{"is not recognized", "executable file not found", "cannot find the file"},
		"The tool this needs is not installed on this PC.",
		nil},
}

// Command explains why a cleanup action's command failed, from its error and output.
func (c Context) Command(output string, err error) *Problem {
	if err == nil {
		return nil
	}
	text := strings.ToLower(output + "\n" + err.Error())
	for _, h := range commandHints {
		for _, m := range h.match {
			if strings.Contains(text, m) {
				fixes := h.fixes
				if len(fixes) == 1 && fixes[0] == admin && c.Elevated {
					fixes = []Fix{retry} // already elevated: the hint does not apply
				}
				return &Problem{Summary: h.summary, Fixes: fixes, Detail: err.Error()}
			}
		}
	}
	return &Problem{Summary: "The command stopped with an error. Its output is below.", Fixes: []Fix{retry}, Detail: err.Error()}
}

func joinNames(names []string) string {
	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func verb(names []string, one, many string) string {
	if len(names) == 1 {
		return one
	}
	return many
}
