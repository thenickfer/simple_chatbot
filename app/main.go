package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var embeddedFrontend embed.FS

func main() {
	httpAddrFlag := flag.String("http-addr", "", "Run in standalone HTTP API + static server mode on this address (e.g. :8080 or 127.0.0.1:8080)")
	flag.Parse()

	httpAddr := strings.TrimSpace(*httpAddrFlag)
	if httpAddr == "" {
		httpAddr = strings.TrimSpace(os.Getenv("HTTP_ADDR"))
	}

	appService, err := NewApp()
	if err != nil {
		log.Fatalf("Failed to initialize GPU Assistant app: %v", err)
	}

	frontendFS := resolveFrontendFS()
	staticHandler := http.FileServer(http.FS(frontendFS))
	combinedHandler := appService.APIHandler(staticHandler)

	// If --http-addr / HTTP_ADDR is provided, serve the HTTP JSON API + static frontend directly
	// (useful for headless browser, Playwright, and smoke tests without an X11/Wayland display).
	if httpAddr != "" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		appService.startup(ctx)
		defer appService.shutdown(ctx)

		srv := &http.Server{
			Addr:    httpAddr,
			Handler: combinedHandler,
		}
		go func() {
			<-ctx.Done()
			_ = srv.Close()
		}()
		log.Printf("GPU Assistant HTTP server listening on %s", httpAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
		return
	}

	// Standard Wails v3 native desktop window mode (GTK4 + WebKitGTK 6.0 on Linux)
	// with /api/* JSON endpoints mounted on the internal asset server alongside bound Go services.
	appService.startup(context.Background())

	wailsApp := application.New(application.Options{
		Name:        "GPU Lab Assistant",
		Description: "Cobalt Lab GPU Infrastructure Assistant",
		Services: []application.Service{
			application.NewService(appService),
		},
		Assets: application.AssetOptions{
			Handler: appService.APIHandler(application.AssetFileServerFS(frontendFS)),
		},
		OnShutdown: func() {
			appService.shutdown(context.Background())
		},
	})

	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "GPU Lab Assistant",
		Width:            1120,
		Height:           760,
		MinWidth:         320,
		MinHeight:        480,
		BackgroundColour: application.NewRGB(20, 22, 26),
		URL:              "/",
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatalf("Wails v3 error: %v", err)
	}
}

func resolveFrontendFS() fs.FS {
	if sub, err := fs.Sub(embeddedFrontend, "frontend/dist"); err == nil {
		if _, statErr := fs.Stat(sub, "index.html"); statErr == nil {
			return sub
		}
	}
	candidates := []string{
		filepath.Join("frontend", "dist"),
		filepath.Join("app", "frontend", "dist"),
		filepath.Join("frontend", "public"),
	}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return os.DirFS(dir)
		}
	}
	return os.DirFS(".")
}
