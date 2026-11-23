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

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
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

	app, err := NewApp()
	if err != nil {
		log.Fatalf("Failed to initialize GPU Assistant app: %v", err)
	}

	frontendFS := resolveFrontendFS()
	staticHandler := http.FileServer(http.FS(frontendFS))
	combinedHandler := app.APIHandler(staticHandler)

	// If --http-addr / HTTP_ADDR is provided, serve the HTTP JSON API + static frontend directly
	// (useful for headless browser, Playwright, and smoke tests without an X11/Wayland display).
	if httpAddr != "" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		app.startup(ctx)
		defer app.shutdown(ctx)

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

	// Standard Wails v2 desktop window mode with /api/* middleware fallback attached.
	err = wails.Run(&options.App{
		Title:            "GPU Lab Assistant",
		Width:            1120,
		Height:           760,
		MinWidth:         320,
		MinHeight:        480,
		BackgroundColour: &options.RGBA{R: 20, G: 22, B: 26, A: 1},
		AssetServer: &assetserver.Options{
			Assets:  frontendFS,
			Handler: app.APIHandler(nil),
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatalf("Wails error: %v", err)
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
