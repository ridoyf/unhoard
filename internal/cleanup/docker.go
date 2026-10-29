package cleanup

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

var dockerPrune = Action{
	ID:          "docker-prune",
	Title:       "Prune unused Docker data",
	Description: "Runs docker system prune: removes stopped containers, unused networks, dangling images and build cache. Named volumes and images used by containers are kept. Docker Desktop must be running.",
	Preview: func(ctx context.Context, env Env, _ []string) (Plan, error) {
		out, err := env.Run(ctx, nil, "docker", "system", "df")
		if err != nil {
			return unavailable("Docker is not installed, or Docker Desktop is not running. Start Docker Desktop, wait until it says it is running, then preview this again."), nil
		}
		p := Plan{Targets: []string{"Stopped containers, dangling images and build cache"}, Bytes: parseDockerReclaimable(out), Notes: strings.TrimSpace(out)}
		p.add("docker", "system", "prune", "--force")
		return p, nil
	},
}

// Matches the RECLAIMABLE column at the end of a line, e.g. "2.31GB (45%)". The Build
// Cache line has no percentage. Docker uses decimal units.
var dockerSize = regexp.MustCompile(`([\d.]+)\s*(B|kB|KB|MB|GB|TB)(\s*\(\d+%\))?\s*$`)

// parseDockerReclaimable estimates what prune frees: stopped containers and build cache.
// It leaves out images, because their reclaimable figure counts every unused image and
// prune without -a removes only dangling ones, and volumes, which prune keeps.
func parseDockerReclaimable(out string) uint64 {
	var total float64
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "Containers") && !strings.HasPrefix(line, "Build Cache") {
			continue
		}
		m := dockerSize.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, _ := strconv.ParseFloat(m[1], 64)
		mult := map[string]float64{"B": 1, "kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12}[m[2]]
		total += n * mult
	}
	return uint64(total)
}
