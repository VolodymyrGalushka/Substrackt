package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	appsvc "substrackt/internal/app"
	"substrackt/internal/store"
)

// Wails embeds the built frontend into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	dbPath, err := databasePath()
	if err != nil {
		log.Fatalf("resolve database path: %v", err)
	}

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer func() { _ = st.Close() }()

	subscriptions := appsvc.NewSubscriptionsService(st)

	wailsApp := application.New(application.Options{
		Name:        "Substrackt",
		Description: "Track and manage your subscriptions",
		Services: []application.Service{
			application.NewService(subscriptions),
			application.NewService(appsvc.NewPaymentMethodsService(st)),
			application.NewService(appsvc.NewVendorsService(st)),
			application.NewService(appsvc.NewDashboardService(st, subscriptions)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Substrackt",
		Width:            1280,
		Height:           820,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(9, 10, 14),
		URL:              "/",
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}

// databasePath returns the SQLite database location. Substrackt stores its data
// in the user's config directory; SUBSTRACKT_HOME overrides it for tests and
// development.
func databasePath() (string, error) {
	home := os.Getenv("SUBSTRACKT_HOME")
	if home == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(dir, "Substrackt")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(home, "substrackt.db"), nil
}
