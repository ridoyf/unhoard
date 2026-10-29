package cleanup

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/ridoyf/unhoard/internal/insight"
)

// Plan is the dry run of an action: exactly what it will do and roughly what it frees.
// The UI shows it and asks for confirmation before calling Run.
type Plan struct {
	Action      string   `json:"action"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Commands    []string `json:"commands"` // the exact commands Run will execute, for display
	Targets     []string `json:"targets"`  // paths or names the action affects
	Bytes       uint64   `json:"bytes"`    // estimated space freed; 0 if unknown
	Notes       string   `json:"notes"`    // e.g. raw analysis output
	NeedsAdmin  bool     `json:"needsAdmin"`
	Available   bool     `json:"available"`
	Reason      string   `json:"reason"` // why it is unavailable

	cmds [][]string // what Run executes; Commands is its display form
}

func (p *Plan) add(name string, args ...string) {
	p.cmds = append(p.cmds, append([]string{name}, args...))
	p.Commands = append(p.Commands, display(name, args...))
}

// Env is what actions need from the outside world. Tests replace Run with a fake.
type Env struct {
	Elevated bool
	Run      insight.Runner
	// RunInput is Run with input written to the command's standard input.
	RunInput        func(ctx context.Context, onLine func(string), input, name string, args ...string) (string, error)
	EmptyRecycleBin func() error   // nil in tests
	Progress        func(Progress) // optional; receives updates from actions that can measure them
	// executing is set by Execute, which rebuilds the plan only to compare its commands,
	// so a slow size estimate (DISM's analysis takes minutes) does not run twice.
	executing bool
}

func (e Env) report(p Progress) {
	if e.Progress != nil {
		e.Progress(p)
	}
}

// Progress reports how far a running action has got.
type Progress struct {
	Percent float64 `json:"percent"` // 0 to 100, or -1 when the action cannot tell
	Detail  string  `json:"detail"`  // what it is doing now, such as the latest line a command printed
}

// percentText finds progress that tools print, such as DISM's "[=== 25.0% ===]" and
// diskpart's "10 percent completed".
var percentText = regexp.MustCompile(`(\d{1,3}(?:\.\d+)?)\s*(?:%|percent)`)

// stepLines turns the output of command step (counting from 0) out of steps into progress.
// A printed percentage moves the bar within the step; any other line only updates the detail.
func (e Env) stepLines(step, steps int) func(string) {
	return func(line string) {
		p := Progress{Percent: -1, Detail: line}
		if steps > 1 {
			p.Percent = float64(step) / float64(steps) * 100
		}
		if m := percentText.FindStringSubmatch(line); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil && v <= 100 {
				p.Percent = (float64(step) + v/100) / float64(steps) * 100
			}
		}
		e.report(p)
	}
}

// Action is one cleanup action. target is action-specific (an app key, a list of paths) and often empty.
type Action struct {
	ID          string
	Title       string
	Description string
	NeedsAdmin  bool
	Preview     func(ctx context.Context, env Env, target []string) (Plan, error)
	// Run executes a plan from Preview. Nil means run plan.Commands in order.
	Run func(ctx context.Context, env Env, plan Plan) (string, error)
}

// All lists every action. Knowledge-base rules refer to these ids.
var All = []Action{hibernation, dism, dockerPrune, wslCompact, recycleBin, uninstallApp, cloudOnlineOnly}

func Find(id string) (Action, bool) {
	for _, a := range All {
		if a.ID == id {
			return a, true
		}
	}
	return Action{}, false
}

// Preview dry-runs an action. Actions fill in the specifics (commands, targets,
// bytes, availability); this adds what every plan shares.
func Preview(ctx context.Context, env Env, id string, target []string) (Plan, error) {
	a, ok := Find(id)
	if !ok {
		return Plan{}, fmt.Errorf("unknown action %q", id)
	}
	p := Plan{Available: true}
	if a.NeedsAdmin && !env.Elevated {
		p.Available = false
		p.Reason = "Restart Unhoard as administrator to run this."
	} else {
		var err error
		if p, err = a.Preview(ctx, env, target); err != nil {
			return p, err
		}
		p.Available = p.Reason == ""
	}
	p.Action, p.Title, p.Description, p.NeedsAdmin = a.ID, a.Title, a.Description, a.NeedsAdmin
	return p, nil
}

// unavailable is a plan an action returns when it cannot run on this machine.
func unavailable(reason string) Plan { return Plan{Reason: reason} }

// Exec runs a command without flashing a console window and returns its combined output.
// onLine, when not nil, receives each non-empty output line as it is printed.
func Exec(ctx context.Context, onLine func(string), name string, args ...string) (string, error) {
	return ExecInput(ctx, onLine, "", name, args...)
}

// ExecInput is Exec with input given to the command on its standard input.
func ExecInput(ctx context.Context, onLine func(string), input, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	waited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		pw.Close()
		waited <- err
	}()
	var out bytes.Buffer
	sc := bufio.NewScanner(io.TeeReader(pr, &out))
	sc.Split(scanLines)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && onLine != nil {
			onLine(line)
		}
	}
	// A line too long for the scanner stops it early; keep reading so the command can finish.
	io.Copy(&out, pr)
	if err := <-waited; err != nil {
		return out.String(), fmt.Errorf("%s: %w", name, err)
	}
	return out.String(), nil
}

// scanLines splits on "\n" and also on "\r", which tools such as DISM use to redraw a
// progress bar in place, so each redraw arrives as its own line.
func scanLines(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// LastLines returns the last n lines of command output.
func LastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// display renders a command line the way a user would type it.
func display(name string, args ...string) string {
	parts := []string{name}
	for _, a := range args {
		if strings.ContainsAny(a, " \t") {
			a = `"` + a + `"`
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

// Execute runs a previewed plan. confirmed must equal the Commands the user saw:
// the plan is rebuilt here, and anything that changed since the preview is refused.
func Execute(ctx context.Context, env Env, id string, target, confirmed []string) (Plan, string, error) {
	a, _ := Find(id)
	env.executing = true
	plan, err := Preview(ctx, env, id, target)
	if err != nil {
		return plan, "", err
	}
	if !plan.Available {
		return plan, "", fmt.Errorf("%s is not available: %s", a.Title, plan.Reason)
	}
	if !slices.Equal(plan.Commands, confirmed) {
		return plan, "", fmt.Errorf("the plan changed since you reviewed it; preview it again")
	}
	if a.Run != nil {
		out, err := a.Run(ctx, env, plan)
		return plan, out, err
	}
	out, err := runAll(ctx, env, plan.cmds)
	return plan, out, err
}

// runAll runs each command in order, stopping at the first failure.
func runAll(ctx context.Context, env Env, cmds [][]string) (string, error) {
	var out strings.Builder
	for i, c := range cmds {
		cmdline := display(c[0], c[1:]...)
		fmt.Fprintf(&out, "> %s\n", cmdline)
		env.stepLines(i, len(cmds))("Running " + cmdline)
		o, err := env.Run(ctx, env.stepLines(i, len(cmds)), c[0], c[1:]...)
		out.WriteString(o)
		if err != nil {
			return out.String(), err
		}
	}
	return out.String(), nil
}
