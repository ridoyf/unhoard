package cleanup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ridoyf/unhoard/internal/insight"
)

var wslCompact = Action{
	ID:          "wsl-compact",
	Title:       "Compact WSL and Docker virtual disks",
	Description: "WSL and Docker Desktop keep Linux files in virtual disks (.vhdx) that grow but never shrink on their own. This shuts down WSL (closing every Linux app and Docker), then uses Windows' diskpart to compact each disk, returning space freed inside Linux to Windows.",
	NeedsAdmin:  true,
	Preview: func(ctx context.Context, env Env, _ []string) (Plan, error) {
		disks := findVHDX(os.Getenv("LOCALAPPDATA"))
		if len(disks) == 0 {
			return unavailable("No WSL or Docker virtual disks were found."), nil
		}
		p := Plan{Targets: disks}
		var total int64
		p.add(insight.System32("wsl.exe"), "--shutdown")
		for _, d := range disks {
			if fi, err := os.Stat(d); err == nil {
				total += fi.Size()
			}
			// The script goes to diskpart's standard input. A script file would sit in a
			// folder any program running as the user can write to, then run as administrator.
			p.cmds = append(p.cmds, []string{insight.System32("diskpart.exe")})
			p.Commands = append(p.Commands, display(insight.System32("diskpart.exe"))+" with input: "+strings.ReplaceAll(strings.TrimSpace(script(d)), "\r\n", "; "))
		}
		p.Notes = fmt.Sprintf("The disks use %d bytes now. How much is freed depends on what was deleted inside Linux.", total)
		return p, nil
	},
	Run: func(ctx context.Context, env Env, plan Plan) (string, error) {
		if env.RunInput == nil {
			return "", errors.New("diskpart cannot be started here")
		}
		out, err := runAll(ctx, env, plan.cmds[:1])
		if err != nil {
			return out, err
		}
		steps := len(plan.cmds)
		for i, d := range plan.Targets {
			out += "> " + plan.Commands[i+1] + "\n"
			env.stepLines(i+1, steps)("Compacting " + d)
			o, err := env.RunInput(ctx, env.stepLines(i+1, steps), script(d), plan.cmds[i+1][0])
			out += o
			if err != nil {
				return out, err
			}
		}
		return out, nil
	},
}

func script(disk string) string {
	return fmt.Sprintf("select vdisk file=\"%s\"\r\nattach vdisk readonly\r\ncompact vdisk\r\ndetach vdisk\r\n", disk)
}

// findVHDX looks in the standard WSL and Docker Desktop locations.
func findVHDX(localAppData string) []string {
	if localAppData == "" {
		return nil
	}
	var out []string
	for _, pattern := range []string{
		`Packages\*\LocalState\ext4.vhdx`,
		`Docker\wsl\*\*.vhdx`,
		`Docker\wsl\*.vhdx`,
		`wsl\*\ext4.vhdx`,
	} {
		m, _ := filepath.Glob(filepath.Join(localAppData, pattern))
		out = append(out, m...)
	}
	return out
}
