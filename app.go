package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/ridoyf/unhoard/internal/cleanup"
	"github.com/ridoyf/unhoard/internal/elevate"
	"github.com/ridoyf/unhoard/internal/finders"
	"github.com/ridoyf/unhoard/internal/insight"
	"github.com/ridoyf/unhoard/internal/kb"
	"github.com/ridoyf/unhoard/internal/protect"
	"github.com/ridoyf/unhoard/internal/quarantine"
	"github.com/ridoyf/unhoard/internal/scanner"
	"github.com/ridoyf/unhoard/internal/store"
	"github.com/ridoyf/unhoard/internal/tree"
	"github.com/ridoyf/unhoard/internal/update"
	"github.com/ridoyf/unhoard/rules"
)

// App is the service the frontend calls. Its methods are split by area across
// app.go, scan.go, find.go and actions.go. They validate input, call into
// internal/, and return plain data. All exported methods are safe for concurrent use.
type App struct {
	notifier *notifications.NotificationService
	dataDir  string
	elevated bool
	kb       *kb.KB
	guard    *protect.Guard
	q        *quarantine.Quarantine
	db       *store.Store
	acting   sync.Mutex // held while a cleanup action runs

	mu         sync.RWMutex // guards everything below
	scan       *scanner.Result
	scanRoot   string
	scanAt     time.Time
	scanning   bool
	cancelScan context.CancelFunc
	dups       []finders.DupGroup
	cancelDups context.CancelFunc
}

// Events sent to the frontend.
const (
	evScanProgress = "scan:progress"
	evScanDone     = "scan:done"
	evScanError    = "scan:error"
	evDupDone      = "dups:done"
	evProgress     = "activity:progress"
)

func init() {
	application.RegisterEvent[scanner.Progress](evScanProgress)
	application.RegisterEvent[ScanInfo](evScanDone)
	application.RegisterEvent[string](evScanError)
	application.RegisterEvent[[]DupGroup](evDupDone)
	application.RegisterEvent[ActivityProgress](evProgress)
}

func emit(name string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data)
	}
}

// ServiceStartup runs once before the window opens.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	local, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		return err
	}
	a.dataDir = filepath.Join(local, "Unhoard")
	if err := os.MkdirAll(filepath.Join(a.dataDir, "logs"), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(a.dataDir, "logs", "unhoard.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		slog.SetDefault(slog.New(slog.NewJSONHandler(logFile, nil)))
	}
	a.elevated = elevate.IsElevated()

	// Rule files dropped into %LOCALAPPDATA%\Unhoard\rules override the built-in ones by id.
	overrides := os.DirFS(filepath.Join(a.dataDir, "rules"))
	if a.kb, err = kb.Load(os.Getenv, rules.FS, overrides); err != nil {
		slog.Error("rule override failed to load, using built-in rules", "err", err)
		if a.kb, err = kb.Load(os.Getenv, rules.FS); err != nil {
			return fmt.Errorf("loading built-in rules: %w", err)
		}
	}

	a.guard = protect.Default(a.dataDir)
	a.guard.Never = a.kb.Forbids
	if a.q, err = quarantine.Default(a.guard, filepath.Join(a.dataDir, "Quarantine")); err != nil {
		return err
	}

	if a.db, err = store.Open(filepath.Join(a.dataDir, "unhoard.db")); err != nil {
		return err
	}
	slog.Info("started", "version", version, "elevated", a.elevated, "rules", len(a.kb.Rules()))
	go a.purgeExpired()
	go a.background()
	return nil
}

func (a *App) ServiceShutdown() error {
	a.mu.Lock()
	if a.cancelScan != nil {
		a.cancelScan()
	}
	if a.cancelDups != nil {
		a.cancelDups()
	}
	a.mu.Unlock()
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Status is everything the UI needs on first load.
type Status struct {
	Version  string          `json:"version"`
	Elevated bool            `json:"elevated"`
	Drives   []insight.Drive `json:"drives"`
	Scan     *ScanInfo       `json:"scan"`
	Scanning bool            `json:"scanning"`
	Rules    int             `json:"rules"`
	DataDir  string          `json:"dataDir"`
}

func (a *App) Status() Status {
	s := Status{Version: version, Elevated: a.elevated, Drives: insight.Drives(), Rules: len(a.kb.Rules()), DataDir: a.dataDir}
	a.mu.RLock()
	defer a.mu.RUnlock()
	s.Scanning = a.scanning
	if a.scan != nil {
		info := a.scanInfo()
		s.Scan = &info
	}
	return s
}

// Settings are the user's preferences.
type Settings struct {
	RetentionDays   int    `json:"retentionDays"`   // quarantine auto-purge
	Background      bool   `json:"background"`      // keep running in the tray when the window closes
	ScanEveryHours  int    `json:"scanEveryHours"`  // 0 = off; needs Background
	ScheduledRoot   string `json:"scheduledRoot"`   // drive scanned on the schedule
	LowSpacePercent int    `json:"lowSpacePercent"` // 0 = off
	CheckUpdates    bool   `json:"checkUpdates"`
}

func (a *App) Settings() Settings {
	s := Settings{RetentionDays: 7, ScheduledRoot: os.Getenv("SystemDrive") + `\`, LowSpacePercent: 10, CheckUpdates: true}
	if raw := a.db.Setting("settings", ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &s)
	}
	return s
}

func (a *App) SaveSettings(s Settings) error {
	if s.RetentionDays < 1 || s.RetentionDays > 365 {
		return errors.New("keep quarantined items for 1 to 365 days")
	}
	if s.ScanEveryHours < 0 || s.ScanEveryHours > 24*30 {
		return errors.New("scan interval must be between 0 and 720 hours")
	}
	if s.LowSpacePercent < 0 || s.LowSpacePercent > 50 {
		return errors.New("low space alert must be between 0 and 50 percent")
	}
	if !isDrivePath(s.ScheduledRoot) {
		return errors.New("scheduled scan needs a folder path like C:\\")
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return a.db.SetSetting("settings", string(raw))
}

func (a *App) CheckForUpdate() (update.Info, error) {
	return update.Check(context.Background(), version)
}

// RelaunchAsAdmin starts an elevated copy and quits this one.
func (a *App) RelaunchAsAdmin() error {
	if err := elevate.Relaunch(); err != nil {
		return fmt.Errorf("could not restart as administrator: %w", err)
	}
	application.Get().Quit()
	return nil
}

var errNoScan = errors.New("scan a drive first")

// readTree runs fn on the loaded scan under the read lock and returns its result.
// It is a function rather than a method because Go methods cannot have type parameters.
func readTree[T any](a *App, fn func(t *tree.Tree) (T, error)) (T, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.scan == nil {
		var none T
		return none, errNoScan
	}
	return fn(a.scan.Tree)
}

func (a *App) OpenInExplorer(path string) error {
	if !isDrivePath(path) {
		return errors.New("not a local path")
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return application.Get().Env.OpenFileManager(path, !fi.IsDir())
}

func (a *App) CopyText(text string) bool { return application.Get().Clipboard.SetText(text) }

// settingsPages are the only programs OpenSettings may start.
var settingsPages = map[string]bool{
	"SystemPropertiesProtection.exe":  true,
	"SystemPropertiesPerformance.exe": true,
	"ms-settings:storagesense":        true,
	"ms-settings:appsfeatures":        true,
}

func (a *App) OpenSettings(page string) error {
	if !settingsPages[page] {
		return errors.New("unknown settings page")
	}
	if strings.HasPrefix(page, "ms-settings:") {
		return application.Get().Browser.OpenURL(page)
	}
	_, err := cleanup.Exec(context.Background(), nil, insight.System32(page))
	return err
}

func (a *App) OpenURL(url string) error {
	if !strings.HasPrefix(url, "https://") {
		return errors.New("only https links can be opened")
	}
	return application.Get().Browser.OpenURL(url)
}

func isDrivePath(p string) bool {
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') &&
		(p[0]|0x20 >= 'a' && p[0]|0x20 <= 'z')
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }
