package cleanup

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/ridoyf/unhoard/internal/insight"
)

var uninstallApp = Action{
	ID:          "uninstall-app",
	Title:       "Uninstall an app",
	Description: "Starts the app's own uninstaller, exactly as Settings > Apps would. The uninstaller decides what is removed and may ask questions or for admin rights.",
	Preview: func(ctx context.Context, env Env, target []string) (Plan, error) {
		if len(target) != 1 {
			return Plan{}, errors.New("uninstall needs exactly one app")
		}
		a, ok := insight.FindApp(target[0])
		if !ok {
			return unavailable("That app is no longer installed."), nil
		}
		// Any program running as the user can rewrite an HKCU uninstall entry, and a
		// process started from an elevated one inherits its admin rights.
		if env.Elevated && strings.HasPrefix(a.Key, `HKCU\`) {
			return unavailable("This app is installed for your account only, and Unhoard is running as administrator. Its uninstaller would get administrator rights it does not need, so Unhoard will not start it. Close Unhoard, open it again normally, then uninstall the app from Apps."), nil
		}
		p := Plan{Targets: []string{a.Name}, Bytes: a.EstimatedBytes}
		p.cmds = [][]string{{a.UninstallString}}
		p.Commands = []string{a.UninstallString}
		return p, nil
	},
	Run: func(ctx context.Context, env Env, plan Plan) (string, error) {
		exe, args := splitCommand(plan.cmds[0][0])
		// ShellExecute (not CreateProcess) so Windows can show the UAC prompt
		// that most uninstallers ask for. It returns once the uninstaller starts.
		err := windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(exe),
			windows.StringToUTF16Ptr(args), nil, windows.SW_SHOWNORMAL)
		if err != nil {
			return "", err
		}
		return "Started the uninstaller for " + plan.Targets[0] + ".", nil
	},
}

// splitCommand separates a registry command line into the program and the rest,
// leaving the arguments untouched (uninstallers parse them in their own ways).
func splitCommand(cmd string) (exe, args string) {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if end := strings.Index(cmd[1:], `"`); end >= 0 {
			return cmd[1 : end+1], strings.TrimSpace(cmd[end+2:])
		}
	}
	// Unquoted: the program ends at ".exe" when there is one (paths may contain spaces).
	if i := strings.Index(strings.ToLower(cmd), ".exe"); i >= 0 {
		return cmd[:i+4], strings.TrimSpace(cmd[i+4:])
	}
	exe, args, _ = strings.Cut(cmd, " ")
	return exe, strings.TrimSpace(args)
}
