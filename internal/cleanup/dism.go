package cleanup

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/ridoyf/unhoard/internal/insight"
)

var dism = Action{
	ID:          "dism-component-cleanup",
	Title:       "Clean up Windows component store",
	Description: "Uses Windows' own DISM tool to remove superseded versions of system components in the WinSxS folder. Installed updates can no longer be uninstalled afterwards. The analysis and the cleanup can each take several minutes.",
	NeedsAdmin:  true,
	Preview: func(ctx context.Context, env Env, _ []string) (Plan, error) {
		p := Plan{Targets: []string{`%SystemRoot%\WinSxS`}}
		if !env.executing {
			out, err := env.Run(ctx, env.stepLines(0, 1), insight.System32("Dism.exe"), "/Online", "/Cleanup-Image", "/AnalyzeComponentStore")
			if err != nil {
				reason := LastLines(out, 1)
				if reason == "" {
					reason = err.Error()
				}
				return unavailable("DISM analysis failed: " + reason), nil
			}
			p.Bytes, p.Notes = parseDismReclaimable(out), strings.TrimSpace(out)
		}
		p.add(insight.System32("Dism.exe"), "/Online", "/Cleanup-Image", "/StartComponentCleanup")
		return p, nil
	},
}

var dismSize = regexp.MustCompile(`(?m)^\s*(Backups and Disabled Features|Cache and Temporary Data)\s*:\s*([\d.,]+)\s*(bytes|KB|MB|GB|TB)`)

// parseDismReclaimable adds up the reclaimable lines of English DISM output.
// Other display languages return 0, which the UI shows as "unknown".
func parseDismReclaimable(out string) uint64 {
	var total float64
	for _, m := range dismSize.FindAllStringSubmatch(out, -1) {
		n, err := strconv.ParseFloat(strings.ReplaceAll(m[2], ",", ""), 64)
		if err != nil {
			continue
		}
		total += n * unitBytes(m[3])
	}
	return uint64(total)
}

func unitBytes(unit string) float64 {
	switch strings.ToUpper(unit) {
	case "KB":
		return 1 << 10
	case "MB":
		return 1 << 20
	case "GB":
		return 1 << 30
	case "TB":
		return 1 << 40
	}
	return 1
}
