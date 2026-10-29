package main

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/ridoyf/unhoard/internal/insight"
	"github.com/ridoyf/unhoard/internal/report"
)

// background runs low-space checks, quarantine auto-purge and scheduled scans
// while the app is open, including while it is hidden in the tray.
func (a *App) background() {
	alerted := map[string]bool{}
	tick := time.NewTicker(15 * time.Minute)
	defer tick.Stop()
	for {
		s := a.Settings()
		a.checkLowSpace(s, alerted)
		if s.Background && s.ScanEveryHours > 0 && a.scanDue(s) {
			if err := a.StartScan(s.ScheduledRoot); err != nil {
				slog.Info("scheduled scan skipped", "err", err)
			}
		}
		<-tick.C
		a.purgeExpired()
	}
}

// checkLowSpace notifies once when a drive drops below the threshold, and again only after it recovers.
func (a *App) checkLowSpace(s Settings, alerted map[string]bool) {
	if s.LowSpacePercent <= 0 || a.notifier == nil {
		return
	}
	for _, d := range insight.Drives() {
		if d.Removable || d.Total == 0 {
			continue
		}
		low := d.Free*100 < d.Total*uint64(s.LowSpacePercent)
		if low && !alerted[d.Root] {
			err := a.notifier.SendNotification(notifications.NotificationOptions{
				ID:    "low-space-" + strings.TrimSuffix(d.Root, `:\`),
				Title: fmt.Sprintf("Drive %s is almost full", d.Root),
				Body:  fmt.Sprintf("Only %s free. Open Unhoard to see what you can safely remove.", report.Human(d.Free)),
			})
			if err != nil {
				slog.Warn("low space notification failed", "err", err)
			}
		}
		alerted[d.Root] = low
	}
}

func (a *App) scanDue(s Settings) bool {
	snaps, err := a.db.Snapshots(s.ScheduledRoot)
	if err != nil {
		return false
	}
	return len(snaps) == 0 || time.Since(snaps[0].At) >= time.Duration(s.ScanEveryHours)*time.Hour
}
