package cleanup

import (
	"context"
	"os"

	"github.com/ridoyf/unhoard/internal/insight"
)

var hibernation = Action{
	ID:          "disable-hibernation",
	Title:       "Turn off hibernation",
	Description: "Deletes the hibernation file (hiberfil.sys). Hibernate and Fast Startup stop working until you turn it back on with powercfg /hibernate on.",
	NeedsAdmin:  true,
	Preview: func(ctx context.Context, env Env, _ []string) (Plan, error) {
		file := os.Getenv("SystemDrive") + `\hiberfil.sys`
		fi, err := os.Stat(file)
		if err != nil {
			return unavailable("Hibernation is already off."), nil
		}
		p := Plan{Targets: []string{file}, Bytes: uint64(fi.Size())}
		p.add(insight.System32("powercfg.exe"), "/hibernate", "off")
		return p, nil
	},
}
