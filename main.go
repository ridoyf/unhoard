package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// The built frontend (frontend/dist) is compiled into the .exe, so the app ships as one file.
// The "all:" prefix includes files starting with "." or "_", which Vite can emit.
//
//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var trayIcon []byte

// browserArgs are extra WebView2 switches; see devtools.go.
var browserArgs []string

func main() {
	notifier := notifications.New()
	svc := &App{notifier: notifier}
	app := application.New(application.Options{
		Name:        "Unhoard",
		Description: "Understand and reclaim disk space",
		// Every exported method on a service becomes callable from the frontend
		// through the generated TypeScript bindings in frontend/bindings.
		// Services start in order, so notifications are ready before App's background loop sends one.
		Services: []application.Service{
			application.NewService(notifier),
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Windows: application.WindowsOptions{AdditionalBrowserArgs: browserArgs},
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     "Unhoard",
		Width:     1280,
		Height:    820,
		MinWidth:  900,
		MinHeight: 600,
		URL:       "/",
	})

	// With background mode on, closing the window hides it; the tray icon brings it back.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if svc.Settings().Background {
			e.Cancel()
			window.Hide()
		}
	})

	show := func() { window.Show(); window.Focus() }
	menu := app.NewMenu()
	menu.Add("Open Unhoard").OnClick(func(*application.Context) { show() })
	menu.Add("Quit").OnClick(func(*application.Context) { app.Quit() })
	app.SystemTray.New().SetIcon(trayIcon).SetMenu(menu).OnClick(show).SetTooltip("Unhoard")

	// Run blocks until the app quits.
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
