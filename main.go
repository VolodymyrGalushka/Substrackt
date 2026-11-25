package main

import (
	"context"
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	appsvc "substrackt/internal/app"
	"substrackt/internal/browser"
	"substrackt/internal/domain"
	"substrackt/internal/jobs"
	"substrackt/internal/secrets"
	"substrackt/internal/store"
)

// Wails embeds the built frontend into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

// appIcon is the window/taskbar icon on Linux and the dock icon on macOS;
// Windows takes it from the compiled .syso resource instead.
//
//go:embed build/appicon.png
var appIcon []byte

func main() {
	// Must run before application.New initialises GTK: WebKitGTK reads the GTK
	// theme when it decides prefers-color-scheme, so a dark desktop needs
	// GTK_THEME set here or pages report a light scheme.
	appsvc.PrepareSystemTheme()

	dbPath, err := databasePath()
	if err != nil {
		log.Fatalf("resolve database path: %v", err)
	}

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer func() { _ = st.Close() }()

	secretStore, err := secrets.Open(filepath.Join(filepath.Dir(dbPath), "secrets"))
	if err != nil {
		log.Fatalf("open secret store: %v", err)
	}

	subscriptions := appsvc.NewSubscriptionsService(st)
	review := appsvc.NewReviewService(st, subscriptions)
	settings := appsvc.NewSettingsService(st)

	jobManager := jobs.New(st, 2, jobs.Events{
		OnProgress: func(job domain.Job) { emitEvent("job:progress", appsvc.JobDTO(job)) },
		OnFinished: func(job domain.Job) { emitEvent("job:finished", appsvc.JobDTO(job)) },
	})
	ingestion := appsvc.NewIngestionHandler(st, subscriptions)
	ingestion.SetReviewNotifier(func(item appsvc.ReviewItem) {
		emitEvent("review:new", item)
	})
	emailHandler := appsvc.NewEmailHandler(st, secretStore, ingestion)
	emailHandler.SetNotifier(func(src appsvc.Source) {
		emitEvent("source:status", src)
	})

	browserEngine := browser.NewPlaywright()
	browserManager := appsvc.NewBrowserManager(st, secretStore, browserEngine)
	defer browserManager.Close()
	browserManager.SetLoginNotifier(func(status appsvc.BrowserLoginStatus) {
		emitEvent("browser:login", status)
	})
	browserManager.SetSourceNotifier(func(src appsvc.Source) {
		emitEvent("source:status", src)
	})
	browserHandler := appsvc.NewBrowserHandler(st, ingestion, browserManager)
	apiHandler := appsvc.NewAPIHandler(st, secretStore, ingestion)
	apiHandler.SetNotifier(func(src appsvc.Source) {
		emitEvent("source:status", src)
	})

	jobManager.Handle(appsvc.ManualJobKind, ingestion.RunJob)
	jobManager.Handle(appsvc.EmailJobKind, emailHandler.RunJob)
	jobManager.Handle(appsvc.BrowserJobKind, browserHandler.RunJob)
	jobManager.Handle(appsvc.APIJobKind, apiHandler.RunJob)

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		jobManager.Stop()
	}()

	if recovered, err := jobManager.Recover(ctx); err != nil {
		log.Printf("recover jobs: %v", err)
	} else if recovered > 0 {
		log.Printf("requeued %d interrupted job(s)", recovered)
	}
	jobManager.Start(ctx)

	scheduler := appsvc.NewSourceScheduler(st, jobManager)
	go scheduler.Run(ctx)

	wailsApp := application.New(application.Options{
		Name:        "Substrackt",
		Description: "Track and manage your subscriptions",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(subscriptions),
			application.NewService(appsvc.NewPaymentMethodsService(st)),
			application.NewService(appsvc.NewVendorsService(st)),
			application.NewService(appsvc.NewDashboardService(st, subscriptions)),
			application.NewService(appsvc.NewJobsService(st, jobManager)),
			application.NewService(review),
			application.NewService(settings),
			application.NewService(appsvc.NewIngestionService(jobManager)),
			application.NewService(appsvc.NewSourcesService(st, secretStore, jobManager, browserManager)),
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

// emitEvent sends a frontend event when the application has started. Jobs can
// finish before the window exists; those events are dropped harmlessly.
func emitEvent(name string, data ...any) {
	app := application.Get()
	if app == nil || app.Event == nil {
		return
	}
	app.Event.Emit(name, data...)
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
